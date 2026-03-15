package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt/prompttext"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// WebSearchTool performs web searches via SearXNG.
type WebSearchTool struct {
	endpoint string
	client   *http.Client
}

// NewWebSearchTool creates a web search tool with SearXNG endpoint.
func NewWebSearchTool(endpoint string) *WebSearchTool {
	return &WebSearchTool{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *WebSearchTool) Name() string        { return "web_search" }
func (t *WebSearchTool) Description() string { return prompttext.ToolWebSearch }

func (t *WebSearchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query"},
			"limit": map[string]any{"type": "integer", "description": "Max results (default: 5)"},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return dtool.ToolResult{Output: "Error: 'query' is required"}, nil
	}

	limit := 5
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	if t.endpoint == "" {
		return dtool.ToolResult{Output: "Error: web search not configured. Set 'search.endpoint' in config.yaml (e.g. http://localhost:8888)"}, nil
	}

	// Call SearXNG JSON API
	searchURL := fmt.Sprintf("%s/search?q=%s&format=json&categories=general&language=auto",
		strings.TrimRight(t.endpoint, "/"),
		url.QueryEscape(query),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error creating request: %v", err)}, nil
	}
	req.Header.Set("User-Agent", "NGOAgent/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error searching: %v", err)}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return dtool.ToolResult{Output: fmt.Sprintf("Search API error: HTTP %d", resp.StatusCode)}, nil
	}

	var result struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 100000))
	if err := json.Unmarshal(body, &result); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error parsing search results: %v", err)}, nil
	}

	if len(result.Results) == 0 {
		return dtool.ToolResult{Output: fmt.Sprintf("No results found for: %s", query)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s\n\n", query))
	for i, r := range result.Results {
		if i >= limit {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Content))
	}
	return dtool.ToolResult{Output: sb.String()}, nil
}

// WebFetchTool fetches content from a URL.
type WebFetchTool struct {
	client *http.Client
}

func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

func (t *WebFetchTool) Name() string        { return "web_fetch" }
func (t *WebFetchTool) Description() string { return prompttext.ToolWebFetch }

func (t *WebFetchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":        map[string]any{"type": "string", "description": "URL to fetch (http/https only)"},
			"max_length": map[string]any{"type": "integer", "description": "Max response length in bytes (default: 50000)"},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	url, _ := args["url"].(string)
	maxLength := 50000

	if v, ok := args["max_length"].(float64); ok && v > 0 {
		maxLength = int(v)
	}

	if url == "" {
		return dtool.ToolResult{Output: "Error: 'url' is required"}, nil
	}

	// Protocol check
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return dtool.ToolResult{Output: "Error: only http:// and https:// protocols are supported"}, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error creating request: %v", err)}, nil
	}
	req.Header.Set("User-Agent", "NGOAgent/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain")

	resp, err := t.client.Do(req)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error fetching URL: %v", err)}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return dtool.ToolResult{Output: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status)}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxLength)))
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error reading response: %v", err)}, nil
	}

	content := string(body)

	// Basic HTML → text conversion (strip tags)
	content = stripHTMLTags(content)
	content = strings.TrimSpace(content)

	if len(content) >= maxLength {
		content = content[:maxLength] + "\n... (truncated)"
	}

	return dtool.ToolResult{Output: fmt.Sprintf("URL: %s\nStatus: %d\n\n%s", url, resp.StatusCode, content)}, nil
}

// stripHTMLTags converts HTML to plain text.
// Skips script/style content, decodes HTML entities, converts block tags to newlines.
func stripHTMLTags(s string) string {
	// Remove script and style blocks (case insensitive)
	for _, tag := range []string{"script", "style", "noscript"} {
		for {
			lower := strings.ToLower(s)
			start := strings.Index(lower, "<"+tag)
			if start < 0 {
				break
			}
			end := strings.Index(lower[start:], "</"+tag+">")
			if end < 0 {
				// No closing tag, remove to end
				s = s[:start]
				break
			}
			s = s[:start] + s[start+end+len("</"+tag+">"):]
		}
	}

	// Convert block-level tags to newlines
	for _, tag := range []string{"br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote"} {
		s = strings.ReplaceAll(s, "<"+tag, "\n<"+tag)
		s = strings.ReplaceAll(s, "</"+tag+">", "\n")
	}

	// Strip remaining tags
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	result := b.String()

	// Decode common HTML entities
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&nbsp;", " ")
	result = strings.ReplaceAll(result, "&quot;", "\"")
	result = strings.ReplaceAll(result, "&#39;", "'")
	result = strings.ReplaceAll(result, "&#x27;", "'")

	// Compress consecutive blank lines → max 2
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	return result
}
