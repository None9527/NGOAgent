// Package openai implements an OpenAI-compatible LLM provider.
// Supports any API that follows the OpenAI chat/completions endpoint format,
// including DashScope (Qwen), DeepSeek, etc.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func (c *Client) Name() string     { return c.name }
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
		return nil, &llm.LLMError{
			Level:   llm.ClassifyHTTPError(resp.StatusCode),
			Code:    fmt.Sprintf("http_%d", resp.StatusCode),
			Message: string(bodyBytes),
		}
	}

	if req.Stream {
		return c.handleStream(resp.Body, ch)
	}
	return c.handleSync(resp.Body, ch)
}

// handleStream processes SSE (Server-Sent Events) response.
func (c *Client) handleStream(body io.Reader, ch chan<- llm.StreamChunk) (*llm.Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	result := &llm.Response{}
	var contentBuf strings.Builder
	var reasoningBuf strings.Builder
	var currentToolCalls []llm.ToolCall
	toolCallArgs := make(map[int]*strings.Builder) // index → accumulated args

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			ch <- llm.StreamChunk{Type: llm.ChunkDone}
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // Skip malformed chunks
		}

		if len(chunk.Choices) == 0 {
			// Usage-only chunk (some providers send this at the end)
			if chunk.Usage.TotalTokens > 0 {
				result.Usage = llm.Usage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				}
			}
			continue
		}

		delta := chunk.Choices[0].Delta

		// Content
		if delta.Content != "" {
			contentBuf.WriteString(delta.Content)
			ch <- llm.StreamChunk{Type: llm.ChunkText, Text: delta.Content}
		}

		// Reasoning (thinking)
		if delta.Reasoning != "" {
			reasoningBuf.WriteString(delta.Reasoning)
			ch <- llm.StreamChunk{Type: llm.ChunkReasoning, Text: delta.Reasoning}
		}

		// Tool calls (streamed incrementally)
		for _, tc := range delta.ToolCalls {
			idx := tc.Index
			if idx >= len(currentToolCalls) {
				// New tool call
				currentToolCalls = append(currentToolCalls, llm.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: llm.ToolCallFunc{
						Name: tc.Function.Name,
					},
				})
				toolCallArgs[idx] = &strings.Builder{}
			}
			if tc.Function.Arguments != "" {
				toolCallArgs[idx].WriteString(tc.Function.Arguments)
			}
		}

		// Model info
		if chunk.Model != "" {
			result.Model = chunk.Model
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("stream read: %w", err)
	}

	// Finalize tool calls (send after scanner loop completes)
	for i := range currentToolCalls {
		if buf, ok := toolCallArgs[i]; ok {
			currentToolCalls[i].Function.Arguments = buf.String()
		}
		// Safe send: consumer may have exited if context was cancelled
		select {
		case ch <- llm.StreamChunk{
			Type:     llm.ChunkToolCall,
			ToolCall: &currentToolCalls[i],
		}:
		default:
			// Channel full or consumer gone — skip
		}
	}

	result.Content = contentBuf.String()
	result.Reasoning = reasoningBuf.String()
	result.ToolCalls = currentToolCalls
	return result, nil
}

// handleSync processes a non-streaming JSON response.
func (c *Client) handleSync(body io.Reader, ch chan<- llm.StreamChunk) (*llm.Response, error) {
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content   string         `json:"content"`
				ToolCalls []llm.ToolCall `json:"tool_calls"`
			} `json:"message"`
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
		result.Content = msg.Content
		result.ToolCalls = msg.ToolCalls

		if msg.Content != "" {
			ch <- llm.StreamChunk{Type: llm.ChunkText, Text: msg.Content}
		}
		for i := range msg.ToolCalls {
			ch <- llm.StreamChunk{Type: llm.ChunkToolCall, ToolCall: &msg.ToolCalls[i]}
		}
	}

	ch <- llm.StreamChunk{Type: llm.ChunkDone}
	return result, nil
}

// --- SSE chunk structures ---

type sseChunk struct {
	Choices []sseChoice `json:"choices"`
	Model   string      `json:"model"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type sseChoice struct {
	Delta sseDelta `json:"delta"`
}

type sseDelta struct {
	Content   string        `json:"content"`
	Reasoning string        `json:"reasoning_content"` // Qwen/DeepSeek thinking
	ToolCalls []sseToolCall `json:"tool_calls"`
}

type sseToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
