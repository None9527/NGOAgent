package google

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

type Client struct {
	name    string
	baseURL string
	apiKey  string
	models  []string
	client  *http.Client
}

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

func (c *Client) GenerateStream(ctx context.Context, req *llm.Request, ch chan<- llm.StreamChunk) (*llm.Response, error) {
	defer close(ch)

	bodyMap := c.buildPayload(req)
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// For stream, append ?alt=sse&key=... to obtain standard SSE instead of loose JSON stream.
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", c.baseURL, req.Model, c.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, &llm.LLMError{Level: llm.ErrorRecoverable, Code: "network", Message: err.Error(), Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		bodyStr := string(bodyBytes)
		level := llm.ClassifyByBody(resp.StatusCode, bodyStr)
		slog.Info(fmt.Sprintf("[DEBUG] HTTP %d from Google [%s]\nRequest body: %s\nResponse: %s",
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

		return nil, &llm.LLMError{Level: level, Code: fmt.Sprintf("http_%d", resp.StatusCode), Message: bodyStr}
	}

	adapter := llm.NewStreamAdapter(&googleMapper{})
	return adapter.Process(resp.Body, ch)
}

func (c *Client) buildPayload(req *llm.Request) map[string]any {
	body := map[string]any{}

	// System Instruction
	var systemParts []map[string]any
	var contents []map[string]any

	for _, m := range req.Messages {
		if m.Role == "system" {
			systemParts = append(systemParts, map[string]any{"text": m.Content})
			continue
		}

		role := "user"
		if m.Role == "assistant" {
			role = "model"
		} else if m.Role == "tool" {
			role = "function" // Wait, Gemini uses 'user' role for tool responses? It uses 'user'. But actually Google SDK might accept 'function' or 'user' with FunctionResponse.
			// The exact mapping for Gemini tool response is role: user, content: parts: [{functionResponse: {name: tcID, response: {}}}]
			var respMap map[string]any
			if err := json.Unmarshal([]byte(m.Content), &respMap); err != nil {
				slog.Info(fmt.Sprintf("[google] WARN: failed to parse tool response as JSON (toolCallID=%s): %v", m.ToolCallID, err))
			}
			if respMap == nil {
				respMap = map[string]any{"result": m.Content}
			}
			contents = append(contents, map[string]any{
				"role": "user",
				"parts": []map[string]any{{
					"functionResponse": map[string]any{
						"name":     m.ToolCallID,
						"response": respMap,
					},
				}},
			})
			continue
		}

		parts := []map[string]any{}
		if len(m.ToolCalls) > 0 {
			if m.Content != "" {
				parts = append(parts, map[string]any{"text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args map[string]any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					slog.Info(fmt.Sprintf("[google] WARN: failed to parse function args for %s: %v", tc.Function.Name, err))
					args = map[string]any{} // empty args fallback
				}
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": tc.Function.Name,
						"args": args,
					},
				})
			}
		} else if len(m.ContentParts) > 0 {
			for _, p := range m.ContentParts {
				if p.Type == "text" {
					parts = append(parts, map[string]any{"text": p.Text})
				} else if p.Type == "image_url" && p.ImageURL != nil {
					// Gemini expects inlineData
					mime, data := "image/png", ""
					if strings.HasPrefix(p.ImageURL.URL, "data:") {
						idx := strings.Index(p.ImageURL.URL, ",")
						if idx != -1 {
							data = p.ImageURL.URL[idx+1:]
							mimeInfo := p.ImageURL.URL[5:idx]
							if semi := strings.Index(mimeInfo, ";"); semi != -1 {
								mime = mimeInfo[:semi]
							}
						}
					}
					if data != "" {
						parts = append(parts, map[string]any{
							"inlineData": map[string]any{
								"mimeType": mime,
								"data":     data,
							},
						})
					}
				}
			}
		} else {
			parts = append(parts, map[string]any{"text": m.Content})
		}

		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}

	if len(systemParts) > 0 {
		body["systemInstruction"] = map[string]any{
			"parts": systemParts,
		}
	}
	body["contents"] = contents

	// GenerationConfig
	gc := map[string]any{}
	if req.Temperature > 0 {
		gc["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		gc["topP"] = req.TopP
	}
	if req.MaxTokens > 0 {
		gc["maxOutputTokens"] = req.MaxTokens
	}
	if len(gc) > 0 {
		body["generationConfig"] = gc
	}

	// Tools
	if len(req.Tools) > 0 {
		var decls []map[string]any
		for _, t := range req.Tools {
			decls = append(decls, map[string]any{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			})
		}
		body["tools"] = []map[string]any{{
			"functionDeclarations": decls,
		}}

		if req.ToolChoice != "" {
			body["toolConfig"] = map[string]any{
				"functionCallingConfig": map[string]any{
					"mode":                 "ANY",
					"allowedFunctionNames": []string{req.ToolChoice},
				},
			}
		}
	}

	return body
}
