// Package llm provides LLM provider abstraction and multi-provider routing.
//
// adapter.go implements a generic SSE stream adapter that converts
// provider-specific SSE formats into unified StreamChunk events.
// Each provider only needs to implement the ChunkMapper interface
// to map its raw JSON into a NormalizedChunk.
package llm

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// NormalizedChunk is the provider-agnostic representation of a single SSE delta.
// Each provider's ChunkMapper produces this from raw JSON.
type NormalizedChunk struct {
	Content      string             // Text content delta
	Reasoning    string             // Thinking/reasoning delta
	ToolCalls    []RawToolCallDelta // Incremental tool call deltas
	FinishReason string             // "" during streaming, "stop"/"tool_calls"/"length" at end
	Model        string             // Model identifier
	Usage        *Usage             // Token usage (usually only in last chunk)
	Error        error              // Parse error (nil = OK)
	Skip         bool               // True if this chunk should be skipped (empty/usage-only)
}

// RawToolCallDelta represents an incremental tool call chunk from the API.
type RawToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string // Partial JSON fragment
}

// ChunkMapper converts provider-specific raw JSON bytes into a NormalizedChunk.
// Each provider implements this interface — this is the ONLY thing providers need to write.
type ChunkMapper interface {
	// MapChunk parses a single SSE data payload (after "data: " prefix) into a NormalizedChunk.
	MapChunk(data []byte) NormalizedChunk

	// DoneSignal returns the string that signals end-of-stream (e.g., "[DONE]").
	DoneSignal() string
}

// thinkParser is a stateful parser that splits a content stream into text and reasoning segments.
// It handles <think>...</think> tags that may span multiple streaming chunks.
// Content inside <think> tags is routed to reasoning; content outside is routed to text.
type thinkParser struct {
	inThink bool           // are we currently inside a <think> block?
	pending strings.Builder // buffered partial tag fragment (e.g. "<thi" not yet confirmed)
}

// thinkParseResult holds the output of a single parse call.
type thinkParseResult struct {
	text      string
	reasoning string
}

// feed processes the next content fragment and returns text/reasoning segments.
// The state machine handles tags that span chunk boundaries by buffering partial tag
// bytes in `pending` until the tag is confirmed or refuted.
func (p *thinkParser) feed(s string) thinkParseResult {
	var textBuf, reasonBuf strings.Builder

	// Flush any pending fragment prepended to new input.
	if p.pending.Len() > 0 {
		s = p.pending.String() + s
		p.pending.Reset()
	}

	for len(s) > 0 {
		if !p.inThink {
			// Looking for <think>
			idx := strings.Index(s, "<think>")
			if idx == -1 {
				// Check if the tail could be a partial opening tag.
				cut := partialPrefixLen(s, "<think>")
				if cut > 0 {
					textBuf.WriteString(s[:len(s)-cut])
					p.pending.WriteString(s[len(s)-cut:])
					s = ""
				} else {
					textBuf.WriteString(s)
					s = ""
				}
			} else {
				// Found opening tag: emit text before it, switch to inThink.
				textBuf.WriteString(s[:idx])
				s = s[idx+len("<think>"):]
				p.inThink = true
			}
		} else {
			// Looking for </think>
			idx := strings.Index(s, "</think>")
			if idx == -1 {
				// Check if tail could be a partial closing tag.
				cut := partialPrefixLen(s, "</think>")
				if cut > 0 {
					reasonBuf.WriteString(s[:len(s)-cut])
					p.pending.WriteString(s[len(s)-cut:])
					s = ""
				} else {
					reasonBuf.WriteString(s)
					s = ""
				}
			} else {
				// Found closing tag: emit reasoning, switch back.
				reasonBuf.WriteString(s[:idx])
				s = s[idx+len("</think>"):]
				p.inThink = false
			}
		}
	}

	return thinkParseResult{
		text:      textBuf.String(),
		reasoning: reasonBuf.String(),
	}
}

// isCompleteJSON reports whether s is a syntactically complete JSON object.
// Used to distinguish Ollama's single-chunk complete arguments from
// OpenAI-style incremental argument fragments.
func isCompleteJSON(s string) bool {
	s = strings.TrimSpace(s)
	// Must be an object, not just a standalone number/string fragment (which could theoretically
	// appear in OpenAI's streaming delta and happen to parse as valid bare JSON).
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return false
	}
	var v map[string]any
	return json.Unmarshal([]byte(s), &v) == nil
}

// partialPrefixLen returns the length of the longest suffix of s that is a prefix of tag.
// This detects partial tag fragments at the end of a chunk that might be completed in
// the next chunk, so we don't prematurely emit them as text or reasoning.
func partialPrefixLen(s, tag string) int {
	// Try each suffix of s (from longest to shortest)
	for l := minInt(len(s), len(tag)-1); l > 0; l-- {
		if strings.HasPrefix(tag, s[len(s)-l:]) {
			return l
		}
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// StreamAdapter processes an SSE stream using a ChunkMapper and emits unified StreamChunks.
// This encapsulates all generic logic: SSE line parsing, tool_call argument buffering,
// chunk assembly, and Response aggregation.
type StreamAdapter struct {
	mapper ChunkMapper
}

// NewStreamAdapter creates an adapter with the given mapper.
func NewStreamAdapter(mapper ChunkMapper) *StreamAdapter {
	return &StreamAdapter{mapper: mapper}
}

// Process reads an SSE stream, emits StreamChunks on ch, and returns the aggregated Response.
func (a *StreamAdapter) Process(body io.Reader, ch chan<- StreamChunk) (*Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	result := &Response{}
	var contentBuf strings.Builder
	var reasoningBuf strings.Builder
	var currentToolCalls []ToolCall
	toolCallArgs := make(map[int]*strings.Builder)

	// Stateful <think> tag parser — handles tags spanning chunk boundaries.
	var think thinkParser

	doneSignal := a.mapper.DoneSignal()

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == doneSignal {
			ch <- StreamChunk{Type: ChunkDone}
			break
		}

		chunk := a.mapper.MapChunk([]byte(data))

		if chunk.Error != nil {
			continue // Skip malformed chunks
		}
		if chunk.Skip {
			// Usage-only chunk
			if chunk.Usage != nil {
				result.Usage = *chunk.Usage
			}
			continue
		}

		// Capture finish_reason
		if chunk.FinishReason != "" {
			result.StopReason = chunk.FinishReason
		}

		// Content: run through think-tag state machine.
		// Providers that return reasoning in a dedicated field (reasoning_content/reasoning)
		// will have chunk.Content clean. Providers that inline <think> tags in content
		// (e.g. some Ollama models) will have the tags properly split here.
		if chunk.Content != "" {
			parsed := think.feed(chunk.Content)
			if parsed.text != "" {
				contentBuf.WriteString(parsed.text)
				ch <- StreamChunk{Type: ChunkText, Text: parsed.text}
			}
			if parsed.reasoning != "" {
				reasoningBuf.WriteString(parsed.reasoning)
				ch <- StreamChunk{Type: ChunkReasoning, Text: parsed.reasoning}
			}
		}

		// Reasoning from dedicated field (already clean — no think-tag processing needed).
		if chunk.Reasoning != "" {
			reasoningBuf.WriteString(chunk.Reasoning)
			ch <- StreamChunk{Type: ChunkReasoning, Text: chunk.Reasoning}
		}

		// Tool calls (incremental argument buffering).
		// Ollama sends complete JSON in a single chunk; OpenAI/DashScope send incremental fragments.
		// Strategy: if the incoming arguments form a complete JSON object AND the buffer already
		// has content (would cause "{...}{...}" concatenation), replace the buffer instead of appending.
		for _, tc := range chunk.ToolCalls {
			idx := tc.Index
			if idx >= len(currentToolCalls) {
				currentToolCalls = append(currentToolCalls, ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: ToolCallFunc{
						Name: tc.Name,
					},
				})
				toolCallArgs[idx] = &strings.Builder{}
			}
			if tc.Arguments != "" {
				buf := toolCallArgs[idx]
				if buf.Len() > 0 && isCompleteJSON(tc.Arguments) {
					// Complete JSON arrived but buffer already has content → replace, don't append.
					buf.Reset()
				}
				buf.WriteString(tc.Arguments)
			}
		}

		// Model
		if chunk.Model != "" {
			result.Model = chunk.Model
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Finalize tool calls — send complete tool calls after stream ends
	for i := range currentToolCalls {
		if buf, ok := toolCallArgs[i]; ok {
			currentToolCalls[i].Function.Arguments = buf.String()
		}
		select {
		case ch <- StreamChunk{
			Type:     ChunkToolCall,
			ToolCall: &currentToolCalls[i],
		}:
		default:
		}
	}

	result.Content = contentBuf.String()
	result.Reasoning = reasoningBuf.String()
	result.ToolCalls = currentToolCalls
	return result, nil
}
