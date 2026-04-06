package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// Client implements llm.Provider for the Anthropic Messages API.
type Client struct {
	name    string
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
}

// NewClient creates an Anthropic provider.
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

// GenerateStream sends a Messages request and streams the response.
func (c *Client) GenerateStream(ctx context.Context, req *llm.Request, ch chan<- llm.StreamChunk) (*llm.Response, error) {
	defer close(ch)

	bodyMap, err := c.buildPayload(req)
	if err != nil {
		return nil, fmt.Errorf("build anthropic payload: %w", err)
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	// Beta features: Prompt Caching + 8k output for 3.5 Sonnet
	httpReq.Header.Set("anthropic-beta", "max-tokens-3-5-sonnet-2024-07-15, prompt-caching-2024-07-31")

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
		bodyStr := string(bodyBytes)
		level := llm.ClassifyByBody(resp.StatusCode, bodyStr)
		slog.Info(fmt.Sprintf("[DEBUG] HTTP %d from Anthropic [%s]\nRequest body: %s\nResponse: %s",
			resp.StatusCode, level, string(body), bodyStr))

		if resp.StatusCode == 400 && strings.Contains(bodyStr, "context") {
			level = llm.ErrorContextOverflow
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			level = llm.ErrorFatal
		}
		if resp.StatusCode == 429 {
			level = llm.ErrorTransient
		}

		return nil, &llm.LLMError{
			Level:   level,
			Code:    fmt.Sprintf("http_%d", resp.StatusCode),
			Message: bodyStr,
		}
	}

	// Anthropic streaming uses SSE pattern
	adapter := llm.NewStreamAdapter(&anthropicMapper{})
	return adapter.Process(resp.Body, ch)
}

func (c *Client) buildPayload(req *llm.Request) (map[string]any, error) {
	body := map[string]any{
		"model":  req.Model,
		"stream": req.Stream,
	}

	// Max Tokens
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	} else {
		body["max_tokens"] = 8192
	}
	// Temperature
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}

	// Tools
	if len(req.Tools) > 0 {
		var anthropicTools []map[string]any
		for _, t := range req.Tools {
			anthropicTools = append(anthropicTools, map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": t.Function.Parameters,
			})
		}
		body["tools"] = anthropicTools
	}

	// Tool Choice
	if req.ToolChoice != "" && len(req.Tools) > 0 {
		body["tool_choice"] = map[string]any{
			"type": "tool",
			"name": req.ToolChoice,
		}
	} else if len(req.Tools) > 0 {
		body["tool_choice"] = map[string]any{"type": "auto"}
	}

	// System & Messages
	var anthMessages []map[string]any
	var systemBlocks []map[string]any

	for _, m := range req.Messages {
		if m.Role == "system" {
			if len(m.ContentParts) > 0 {
				for _, p := range m.ContentParts {
					if p.Type == "text" {
						block := map[string]any{
							"type": "text",
							"text": p.Text,
						}
						if p.CacheControl != nil && p.CacheControl.Type == "ephemeral" {
							block["cache_control"] = map[string]any{"type": "ephemeral"}
						}
						systemBlocks = append(systemBlocks, block)
					}
				}
			} else {
				systemBlocks = append(systemBlocks, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
		} else {
			// user / assistant / tool interactions
			// Tools output conversion:
			// OpenAI `tool` msg -> Anthropic `user` msg with `tool_result` content type
			// Anthropic doesn't have a "tool" role. It must be sent as 'user' role.
			if m.Role == "tool" {
				anthMessages = append(anthMessages, map[string]any{
					"role": "user",
					"content": []map[string]any{
						{
							"type":        "tool_result",
							"tool_use_id": m.ToolCallID,
							"content":     m.Content,
						},
					},
				})
				continue
			}

			// Assistant tool calls conversion
			// OpenAI sends tool_calls inside assistant message
			// Anthropic requires `tool_use` content blocks inside `assistant` role
			if m.Role == "assistant" && len(m.ToolCalls) > 0 {
				var contentBlocks []map[string]any
				if m.Content != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type": "text",
						"text": m.Content,
					})
				}
				for _, tc := range m.ToolCalls {
					var args map[string]any
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
						args = map[string]any{}
					}
					contentBlocks = append(contentBlocks, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": args,
					})
				}
				anthMessages = append(anthMessages, map[string]any{
					"role":    "assistant",
					"content": contentBlocks,
				})
				continue
			}

			// Normal text or Multimodal
			msgData := map[string]any{"role": m.Role}
			if len(m.ContentParts) > 0 {
				var parts []map[string]any
				for _, p := range m.ContentParts {
					switch p.Type {
					case "text":
						parts = append(parts, map[string]any{"type": "text", "text": p.Text})
					case "image_url":
						if p.ImageURL != nil {
							parts = append(parts, map[string]any{
								"type": "image",
								"source": map[string]any{
									"type":       "base64",
									"media_type": guessMediaType(p.ImageURL.URL),
									"data":       extractBase64(p.ImageURL.URL),
								},
							})
						}
					}
				}
				msgData["content"] = parts
			} else {
				msgData["content"] = m.Content
			}
			anthMessages = append(anthMessages, msgData)
		}
	}

	if len(systemBlocks) > 0 {
		body["system"] = systemBlocks
	}
	body["messages"] = anthMessages

	return body, nil
}

func guessMediaType(dataURI string) string {
	if strings.HasPrefix(dataURI, "data:image/jpeg;") {
		return "image/jpeg"
	}
	if strings.HasPrefix(dataURI, "data:image/webp;") {
		return "image/webp"
	}
	if strings.HasPrefix(dataURI, "data:image/gif;") {
		return "image/gif"
	}
	return "image/png"
}

func extractBase64(dataURI string) string {
	idx := strings.Index(dataURI, ",")
	if idx != -1 {
		return dataURI[idx+1:]
	}
	return dataURI
}
