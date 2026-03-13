// Package llm provides LLM provider abstraction and multi-provider routing.
//
// adapter.go implements a generic SSE stream adapter that converts
// provider-specific SSE formats into unified StreamChunk events.
// Each provider only needs to implement the ChunkMapper interface
// to map its raw JSON into a NormalizedChunk.
package llm

import (
	"bufio"
	"io"
	"strings"
)

// NormalizedChunk is the provider-agnostic representation of a single SSE delta.
// Each provider's ChunkMapper produces this from raw JSON.
type NormalizedChunk struct {
	Content      string     // Text content delta
	Reasoning    string     // Thinking/reasoning delta
	ToolCalls    []RawToolCallDelta // Incremental tool call deltas
	FinishReason string     // "" during streaming, "stop"/"tool_calls"/"length" at end
	Model        string     // Model identifier
	Usage        *Usage     // Token usage (usually only in last chunk)
	Error        error      // Parse error (nil = OK)
	Skip         bool       // True if this chunk should be skipped (empty/usage-only)
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

		// Content
		if chunk.Content != "" {
			contentBuf.WriteString(chunk.Content)
			ch <- StreamChunk{Type: ChunkText, Text: chunk.Content}
		}

		// Reasoning
		if chunk.Reasoning != "" {
			reasoningBuf.WriteString(chunk.Reasoning)
			ch <- StreamChunk{Type: ChunkReasoning, Text: chunk.Reasoning}
		}

		// Tool calls (incremental argument buffering)
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
				toolCallArgs[idx].WriteString(tc.Arguments)
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
