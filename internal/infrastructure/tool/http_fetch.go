package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// HTTPFetchTool performs direct HTTP GET/POST requests without going through agent-search.
// Useful for internal APIs, localhost services, or simple unauthenticated public endpoints.
// For Cloudflare-protected sites, use web_fetch instead (routes through agent-search proxy).
// P3 M2 (#45): Expands tool matrix to CC parity.
type HTTPFetchTool struct {
	client *http.Client
}

// NewHTTPFetchTool creates a new HTTPFetchTool.
func NewHTTPFetchTool() *HTTPFetchTool {
	return &HTTPFetchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *HTTPFetchTool) Name() string { return "http_fetch" }
func (t *HTTPFetchTool) Description() string {
	return `Direct HTTP request (GET/POST/PUT/DELETE/PATCH).
Best for localhost/internal APIs and simple public endpoints.
For Cloudflare-protected or JS-heavy sites, use web_fetch instead.`
}

func (t *HTTPFetchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":        map[string]any{"type": "string", "description": "Target URL"},
			"method":     map[string]any{"type": "string", "description": "HTTP method (GET/POST/PUT/DELETE/PATCH, default: GET)"},
			"body":       map[string]any{"type": "string", "description": "Request body (string or JSON — will be sent as-is)"},
			"headers":    map[string]any{"type": "object", "description": "Request headers as key-value pairs"},
			"timeout_ms": map[string]any{"type": "integer", "description": "Timeout in ms (default: 10000)"},
			"max_bytes":  map[string]any{"type": "integer", "description": "Max response body bytes (default: 100000)"},
		},
		"required": []string{"url"},
	}
}

func (t *HTTPFetchTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	targetURL, _ := args["url"].(string)
	if targetURL == "" {
		return dtool.ToolResult{Output: "Error: 'url' is required"}, nil
	}
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		return dtool.ToolResult{Output: "Error: URL must start with http:// or https://"}, nil
	}

	method := "GET"
	if v, ok := args["method"].(string); ok && v != "" {
		method = strings.ToUpper(v)
	}

	timeoutMs := 10000
	if v, ok := args["timeout_ms"].(float64); ok && v > 0 {
		timeoutMs = int(v)
	}

	maxBytes := 100000
	if v, ok := args["max_bytes"].(float64); ok && v > 0 {
		maxBytes = int(v)
	}

	// Build request body
	var bodyReader io.Reader
	bodyStr := ""
	if v, ok := args["body"].(string); ok && v != "" {
		bodyStr = v
		bodyReader = strings.NewReader(v)
	} else if v, ok := args["body"].(map[string]any); ok {
		data, _ := json.Marshal(v)
		bodyStr = string(data)
		bodyReader = bytes.NewReader(data)
	}

	// Create request with timeout context
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, targetURL, bodyReader)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error creating request: %v", err)}, nil
	}

	// Apply headers
	req.Header.Set("User-Agent", "NGOAgent/1.0")
	if bodyStr != "" {
		// Auto-detect content type
		if json.Valid([]byte(bodyStr)) {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", "text/plain")
		}
	}
	if v, ok := args["headers"].(map[string]any); ok {
		for k, val := range v {
			if s, ok := val.(string); ok {
				req.Header.Set(k, s)
			}
		}
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)))
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error reading response: %v", err)}, nil
	}

	// Format output
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("HTTP %d %s\n", resp.StatusCode, resp.Status))
	sb.WriteString(fmt.Sprintf("URL: %s\n", targetURL))
	// Show selected response headers
	for _, h := range []string{"Content-Type", "Content-Length", "X-Request-Id", "X-Trace-Id"} {
		if v := resp.Header.Get(h); v != "" {
			sb.WriteString(fmt.Sprintf("%s: %s\n", h, v))
		}
	}
	sb.WriteString("\n")

	// Pretty-print JSON body
	bodyStr2 := strings.TrimSpace(string(body))
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") && json.Valid(body) {
		var pretty bytes.Buffer
		if json.Indent(&pretty, body, "", "  ") == nil {
			sb.WriteString(pretty.String())
		} else {
			sb.WriteString(bodyStr2)
		}
	} else {
		sb.WriteString(bodyStr2)
	}

	if len(body) >= maxBytes {
		sb.WriteString(fmt.Sprintf("\n\n... (truncated at %d bytes)", maxBytes))
	}

	return dtool.ToolResult{Output: sb.String()}, nil
}
