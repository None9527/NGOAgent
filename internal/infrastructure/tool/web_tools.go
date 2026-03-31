package tool

import (
	"bytes"
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
	searxngURL string // Direct SearXNG URL for fast raw search
	client     *http.Client
}

// NewWebSearchTool creates a web search tool.
// searxngURL should point directly to SearXNG (e.g. http://localhost:8080).
func NewWebSearchTool(searxngURL string) *WebSearchTool {
	return &WebSearchTool{
		searxngURL: strings.TrimRight(searxngURL, "/"),
		client:     &http.Client{Timeout: 15 * time.Second},
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

	if t.searxngURL == "" {
		return dtool.ToolResult{Output: "Error: web search not configured. Set 'search.searxng_url' in config.yaml (e.g. http://localhost:8080)"}, nil
	}

	// Call SearXNG JSON API directly for fast, lightweight search
	searchURL := fmt.Sprintf("%s/search?q=%s&format=json&categories=general&language=auto",
		t.searxngURL,
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

// WebFetchTool fetches content from a URL via the agent-search /api/fetch endpoint.
// This proxy gives access to curl_cffi and Camoufox stealth browser, bypassing CF protections.
type WebFetchTool struct {
	agentSearchURL string
	client         *http.Client
}

func NewWebFetchTool(agentSearchEndpoint string) *WebFetchTool {
	return &WebFetchTool{
		agentSearchURL: strings.TrimRight(agentSearchEndpoint, "/"),
		client: &http.Client{Timeout: 60 * time.Second},
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
	targetURL, _ := args["url"].(string)
	maxLength := 50000

	if v, ok := args["max_length"].(float64); ok && v > 0 {
		maxLength = int(v)
	}

	if targetURL == "" {
		return dtool.ToolResult{Output: "Error: 'url' is required"}, nil
	}

	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		return dtool.ToolResult{Output: "Error: only http:// and https:// protocols are supported"}, nil
	}

	// If agent-search endpoint is not configured, fall back to direct fetch
	if t.agentSearchURL == "" {
		return dtool.ToolResult{Output: "Error: agent-search endpoint not configured (set search.endpoint in config)"}, nil
	}

	// Proxy to agent-search /api/fetch for CF-piercing stealth fetch
	payload := map[string]any{
		"url":        targetURL,
		"max_length": maxLength,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		t.agentSearchURL+"/api/fetch",
		bytes.NewReader(body),
	)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error creating request: %v", err)}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error fetching URL via agent-search: %v", err)}, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error reading agent-search response: %v", err)}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return dtool.ToolResult{Output: fmt.Sprintf("agent-search /api/fetch error HTTP %d: %s", resp.StatusCode, string(respBody))}, nil
	}

	var fr struct {
		URL         string `json:"url"`
		Title       string `json:"title"`
		Content     string `json:"content"`
		ContentType string `json:"content_type"`
		Truncated   bool   `json:"truncated"`
		FetchTimeMs int    `json:"fetch_time_ms"`
	}
	if err := json.Unmarshal(respBody, &fr); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error parsing agent-search response: %v\nRaw: %s", err, string(respBody))}, nil
	}

	if fr.ContentType == "error" || strings.Contains(fr.Content, "[Fetch error") {
		return dtool.ToolResult{Output: fmt.Sprintf("Fetch failed for %s: %s", fr.URL, fr.Content)}, nil
	}

	var sb strings.Builder
	if fr.Title != "" {
		sb.WriteString(fmt.Sprintf("Title: %s\n", fr.Title))
	}
	sb.WriteString(fmt.Sprintf("URL: %s\n", fr.URL))
	sb.WriteString(fmt.Sprintf("Fetched in: %dms\n\n", fr.FetchTimeMs))
	sb.WriteString(fr.Content)
	if fr.Truncated {
		sb.WriteString("\n... (truncated)")
	}
	return dtool.ToolResult{Output: sb.String()}, nil
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

// ---------------------------------------------------------------------------
// DeepResearchTool: one-shot deep research via agent-search /api/search_and_fetch
// ---------------------------------------------------------------------------

// DeepResearchTool provides high-quality, anti-bot-proof deep content via
// the agent-search pipeline: SearXNG → LLM reranker → parallel Camoufox crawl.
type DeepResearchTool struct {
	agentSearchURL string
	client         *http.Client
}

func NewDeepResearchTool(agentSearchEndpoint string) *DeepResearchTool {
	return &DeepResearchTool{
		agentSearchURL: strings.TrimRight(agentSearchEndpoint, "/"),
		client:         &http.Client{Timeout: 120 * time.Second},
	}
}

func (t *DeepResearchTool) Name() string        { return "deep_research" }
func (t *DeepResearchTool) Description() string { return prompttext.ToolDeepResearch }

func (t *DeepResearchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":      map[string]any{"type": "string", "description": "Research question or topic"},
			"categories": map[string]any{"type": "string", "description": "Content category: general|news|images|videos (default: general)"},
			"fetch_top":  map[string]any{"type": "integer", "description": "Number of top results to deep-crawl (default: 3, max: 5)"},
			"limit":      map[string]any{"type": "integer", "description": "Max results to return (default: 5)"},
		},
		"required": []string{"query"},
	}
}

func (t *DeepResearchTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return dtool.ToolResult{Output: "Error: 'query' is required"}, nil
	}

	if t.agentSearchURL == "" {
		return dtool.ToolResult{Output: "Error: agent-search endpoint not configured (set search.endpoint in config)"}, nil
	}

	categories := "general"
	if v, ok := args["categories"].(string); ok && v != "" {
		categories = v
	}

	fetchTop := 3
	if v, ok := args["fetch_top"].(float64); ok && v > 0 {
		fetchTop = min(int(v), 5)
	}

	limit := 5
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	payload := map[string]any{
		"query":              query,
		"categories":         categories,
		"fetch_top":          fetchTop,
		"limit":              limit,
		"max_content_length": 30000,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		t.agentSearchURL+"/api/search_and_fetch",
		bytes.NewReader(body),
	)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error creating request: %v", err)}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error calling deep_research pipeline: %v", err)}, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 500_000))
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error reading response: %v", err)}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return dtool.ToolResult{Output: fmt.Sprintf("agent-search error HTTP %d: %s", resp.StatusCode, string(respBody))}, nil
	}

	var result struct {
		Results []struct {
			Title        string  `json:"title"`
			URL          string  `json:"url"`
			Snippet      string  `json:"snippet"`
			Content      string  `json:"content"`
			RerankScore  float64 `json:"rerank_score"`
			RerankReason string  `json:"rerank_reason"`
			ImageURL     string  `json:"image_url"`
		} `json:"results"`
		Total       int    `json:"total"`
		Fetched     int    `json:"fetched"`
		Intent      string `json:"intent"`
		QueryTimeMs int    `json:"query_time_ms"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error parsing deep_research response: %v", err)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Deep Research Results for: %s\n", query))
	sb.WriteString(fmt.Sprintf("Intent: %s | Results: %d | Deep-crawled: %d | Time: %dms\n\n",
		result.Intent, result.Total, result.Fetched, result.QueryTimeMs))

	for i, r := range result.Results {
		sb.WriteString(fmt.Sprintf("--- [%d] %s ---\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("URL: %s\n", r.URL))
		if r.RerankScore > 0 {
			sb.WriteString(fmt.Sprintf("Relevance: %.1f | Reason: %s\n", r.RerankScore, r.RerankReason))
		}
		if r.ImageURL != "" {
			sb.WriteString(fmt.Sprintf("Image: %s\n", r.ImageURL))
		}
		if r.Content != "" {
			sb.WriteString(fmt.Sprintf("\n%s\n\n", r.Content))
		} else if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("Snippet: %s\n\n", r.Snippet))
		}
	}
	return dtool.ToolResult{Output: sb.String()}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
