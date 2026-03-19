// Package openai implements an OpenAI-compatible LLM provider.
// Supports any API that follows the OpenAI chat/completions endpoint format,
// including DashScope (Qwen), DeepSeek, Ollama, etc.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// Client implements llm.Provider for OpenAI-compatible APIs.
type Client struct {
	name    string
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
}

// NewClient creates an OpenAI-compatible provider.
func NewClient(name, baseURL, apiKey string, models []string) *Client {
	return &Client{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		models:  models,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *Client) Name() string    { return c.name }
func (c *Client) Models() []string { return c.models }

// GenerateStream sends a chat completion request and streams the response.
func (c *Client) GenerateStream(ctx context.Context, req *llm.Request, ch chan<- llm.StreamChunk) (*llm.Response, error) {
	defer close(ch)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Inject tool_choice if force_tool_name is set (Anti's force_tool_name mechanism)
	// Skip for thinking-mode models where tool_choice is not supported
	if req.ToolChoice != "" && len(req.Tools) > 0 {
		policy := llm.GetPolicy(req.Model)
		if !policy.SupportsThinking {
			var bodyMap map[string]any
			if json.Unmarshal(body, &bodyMap) == nil {
				bodyMap["tool_choice"] = map[string]any{
					"type":     "function",
					"function": map[string]any{"name": req.ToolChoice},
				}
				body, _ = json.Marshal(bodyMap)
			}
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, &llm.LLMError{
			Level:   llm.ErrorRecoverable,
			Code:    "network",
			Message: err.Error(),
			Err:     err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		level := llm.ClassifyHTTPError(resp.StatusCode)
		if resp.StatusCode == 429 && strings.Contains(string(bodyBytes), "insufficient_quota") {
			level = llm.ErrorFatal
		}
		// DEBUG: log the request body that caused this error
		log.Printf("[DEBUG] HTTP %d from %s\nRequest body: %s\nResponse: %s",
			resp.StatusCode, c.baseURL, string(body), string(bodyBytes))
		return nil, &llm.LLMError{
			Level:   level,
			Code:    fmt.Sprintf("http_%d", resp.StatusCode),
			Message: string(bodyBytes),
		}
	}

	if req.Stream {
		// Use the shared StreamAdapter with OpenAI field mapping
		adapter := llm.NewStreamAdapter(&openAIMapper{})
		return adapter.Process(resp.Body, ch)
	}
	return c.handleSync(resp.Body, ch)
}

// handleSync processes a non-streaming JSON response.
func (c *Client) handleSync(body io.Reader, ch chan<- llm.StreamChunk) (*llm.Response, error) {
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content          string         `json:"content"`
				ReasoningContent string         `json:"reasoning_content"` // DashScope/DeepSeek
				Reasoning        string         `json:"reasoning"`         // Ollama
				ToolCalls        []llm.ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	result := &llm.Response{
		Model: apiResp.Model,
		Usage: llm.Usage{
			PromptTokens:     apiResp.Usage.PromptTokens,
			CompletionTokens: apiResp.Usage.CompletionTokens,
			TotalTokens:      apiResp.Usage.TotalTokens,
		},
	}

	if len(apiResp.Choices) > 0 {
		msg := apiResp.Choices[0].Message
		// Unify reasoning: prefer reasoning_content (DashScope), fall back to reasoning (Ollama)
		reasoning := msg.ReasoningContent
		if reasoning == "" {
			reasoning = msg.Reasoning
		}
		// For models that inline <think>...</think> in content (e.g. some Ollama variants),
		// extract the think content into reasoning and keep non-think text as content.
		content, thinkContent := extractThinkTags(msg.Content)
		if reasoning == "" {
			reasoning = thinkContent
		}
		content = strings.TrimSpace(content)
		result.Content = content
		result.Reasoning = reasoning
		result.ToolCalls = msg.ToolCalls
		result.StopReason = apiResp.Choices[0].FinishReason

		if content != "" {
			ch <- llm.StreamChunk{Type: llm.ChunkText, Text: content}
		}
		if reasoning != "" {
			ch <- llm.StreamChunk{Type: llm.ChunkReasoning, Text: reasoning}
		}
		for i := range msg.ToolCalls {
			ch <- llm.StreamChunk{Type: llm.ChunkToolCall, ToolCall: &msg.ToolCalls[i]}
		}
	}

	ch <- llm.StreamChunk{Type: llm.ChunkDone}
	return result, nil
}

// extractThinkTags splits raw content into (text, thinkContent).
// It extracts all <think>...</think> segments as reasoning, returning the remainder as text.
func extractThinkTags(raw string) (text, thinking string) {
	var textBuf, thinkBuf strings.Builder
	s := raw
	for len(s) > 0 {
		open := strings.Index(s, "<think>")
		if open == -1 {
			textBuf.WriteString(s)
			break
		}
		textBuf.WriteString(s[:open])
		s = s[open+len("<think>"):]
		close := strings.Index(s, "</think>")
		if close == -1 {
			// Unclosed tag — treat remainder as think content.
			thinkBuf.WriteString(s)
			s = ""
		} else {
			thinkBuf.WriteString(s[:close])
			s = s[close+len("</think>"):]
		}
	}
	return textBuf.String(), strings.TrimSpace(thinkBuf.String())
}

// ═══════════════════════════════════════════════════════
// OpenAI ChunkMapper — the ONLY provider-specific code
// ═══════════════════════════════════════════════════════

// openAIMapper implements llm.ChunkMapper for OpenAI-compatible APIs.
// This is the field mapping layer — add a new provider by writing a new mapper.
type openAIMapper struct{}

func (m *openAIMapper) DoneSignal() string { return "[DONE]" }

func (m *openAIMapper) MapChunk(data []byte) llm.NormalizedChunk {
	var raw struct {
		Choices []struct {
			Delta struct {
				Content          string        `json:"content"`
				ReasoningContent string        `json:"reasoning_content"` // DashScope/DeepSeek
				Reasoning        string        `json:"reasoning"`         // Ollama
				ToolCalls        []sseToolCall `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return llm.NormalizedChunk{Error: err}
	}

	// No choices — usage-only chunk
	if len(raw.Choices) == 0 {
		if raw.Usage.TotalTokens > 0 {
			usage := llm.Usage{
				PromptTokens:     raw.Usage.PromptTokens,
				CompletionTokens: raw.Usage.CompletionTokens,
				TotalTokens:      raw.Usage.TotalTokens,
			}
			return llm.NormalizedChunk{Skip: true, Usage: &usage}
		}
		return llm.NormalizedChunk{Skip: true}
	}

	choice := raw.Choices[0]
	delta := choice.Delta

	// Unify reasoning: prefer reasoning_content (DashScope/DeepSeek), fall back to reasoning (Ollama)
	reasoning := delta.ReasoningContent
	if reasoning == "" {
		reasoning = delta.Reasoning
	}

	// Pass content as-is: the StreamAdapter's thinkParser will handle <think> tags
	// inline in content for streaming mode, routing them to ChunkReasoning.
	result := llm.NormalizedChunk{
		Content:   delta.Content,
		Reasoning: reasoning,
		Model:     raw.Model,
	}

	// finish_reason
	if choice.FinishReason != nil {
		result.FinishReason = *choice.FinishReason
	}

	// Tool calls
	for _, tc := range delta.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, llm.RawToolCallDelta{
			Index:     tc.Index,
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return result
}

// sseToolCall is the OpenAI-specific tool call delta format.
type sseToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
