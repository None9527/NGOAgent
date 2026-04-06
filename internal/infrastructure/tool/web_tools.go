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

// WebSearchTool performs web searches via agent-search /api/search (depth=standard).
type WebSearchTool struct {
	agentSearchURL string
	client         *http.Client
}

// NewWebSearchTool creates a web search tool.
// agentSearchURL should point to the agent-search service (e.g. http://localhost:8889).
func NewWebSearchTool(agentSearchURL string) *WebSearchTool {
	return &WebSearchTool{
		agentSearchURL: strings.TrimRight(agentSearchURL, "/"),
		client:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return `Search the web (text/news/images).
- categories: general (default), news, images, videos.
- Image search: Embed URLs directly via ![desc](url).`
}

func (t *WebSearchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":      map[string]any{"type": "string", "description": "Search query"},
			"limit":      map[string]any{"type": "integer", "description": "Max results (default: 5)"},
			"categories": map[string]any{"type": "string", "description": "Category: general, news, images, videos"},
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

	if t.agentSearchURL == "" {
		return dtool.ToolResult{Output: "Error: agent-search endpoint not configured (set search.endpoint in config)"}, nil
	}

	categories := "general"
	if v, ok := args["categories"].(string); ok && v != "" {
		categories = v
	}

	// Route through agent-search /api/search with depth=standard
	payload := map[string]any{
		"query":       query,
		"depth":       "standard",
		"max_results": limit,
		"categories":  categories,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		t.agentSearchURL+"/api/search",
		bytes.NewReader(body),
	)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error creating request: %v", err)}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error searching: %v", err)}, nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 100_000))
	if resp.StatusCode != http.StatusOK {
		return dtool.ToolResult{Output: fmt.Sprintf("Search API error HTTP %d: %s", resp.StatusCode, string(respBody))}, nil
	}

	var result struct {
		Results []struct {
			Title      string  `json:"title"`
			URL        string  `json:"url"`
			Domain     string  `json:"domain"`
			Score      float64 `json:"score"`
			Reason     string  `json:"reason"`
			ResultType string  `json:"result_type"`
			Snippet    string  `json:"snippet"`
			Images     []struct {
				URL         string `json:"url"`
				LocalPath   string `json:"local_path"`
				Description string `json:"description"`
			} `json:"images"`
		} `json:"results"`
		Images []struct {
			URL         string `json:"url"`
			LocalPath   string `json:"local_path"`
			Description string `json:"description"`
		} `json:"images"`
		Intent      string `json:"intent"`
		QueryTimeMs int    `json:"query_time_ms"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error parsing search results: %v", err)}, nil
	}

	if len(result.Results) == 0 {
		return dtool.ToolResult{Output: fmt.Sprintf("No results found for: %s", query)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s (intent: %s, %dms)\n\n", query, result.Intent, result.QueryTimeMs))

	// If top-level images exist (image category search), show them prominently
	if len(result.Images) > 0 {
		sb.WriteString(fmt.Sprintf("Found %d images:\n\n", len(result.Images)))
		for i, img := range result.Images {
			desc := img.Description
			if desc == "" {
				desc = "image"
			}
			if img.LocalPath != "" {
				sb.WriteString(fmt.Sprintf("%d. ![%s](%s)\n   Local: %s\n\n", i+1, desc, img.URL, img.LocalPath))
			} else {
				sb.WriteString(fmt.Sprintf("%d. ![%s](%s)\n\n", i+1, desc, img.URL))
			}
		}
		sb.WriteString("---\n\n")
	}

	for i, r := range result.Results {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n   %s\n", i+1, r.ResultType, r.Title, r.URL))
		if r.Reason != "" {
			sb.WriteString(fmt.Sprintf("   Relevance: %.0f%% — %s\n", r.Score*100, r.Reason))
		}
		if len(r.Images) > 0 {
			sb.WriteString(fmt.Sprintf("   Image URL: %s\n", r.Images[0].URL))
		}
		sb.WriteString(fmt.Sprintf("   %s\n\n", r.Snippet))
	}
	return dtool.ToolResult{Output: sb.String()}, nil
}

// WebFetchTool fetches content from a URL via agent-search /api/extract.
type WebFetchTool struct {
	agentSearchURL string
	client         *http.Client
}

func NewWebFetchTool(agentSearchEndpoint string) *WebFetchTool {
	return &WebFetchTool{
		agentSearchURL: strings.TrimRight(agentSearchEndpoint, "/"),
		client:         &http.Client{Timeout: 60 * time.Second},
	}
}

func (t *WebFetchTool) Name() string { return "web_fetch" }
func (t *WebFetchTool) Description() string {
	return `Fetch full content of a URL. Auto-escalates through captcha/anti-bot.
- force_stealth=true for JS-heavy sites returning partial content.
- Use web_search to find URLs; use this to read a known URL.`
}

func (t *WebFetchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":           map[string]any{"type": "string", "description": "URL to fetch (http/https only)"},
			"max_length":    map[string]any{"type": "integer", "description": "Max response length in bytes (default: 50000)"},
			"force_stealth": map[string]any{"type": "boolean", "description": "Forcibly use stealth browser for JS-heavy or stubborn sites (default: false)"},
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

	if t.agentSearchURL == "" {
		return dtool.ToolResult{Output: "Error: agent-search endpoint not configured (set search.endpoint in config)"}, nil
	}

	forceStealth := false
	if v, ok := args["force_stealth"].(bool); ok {
		forceStealth = v
	}

	// Route through agent-search /api/extract
	payload := map[string]any{
		"urls":          []string{targetURL},
		"max_length":    maxLength,
		"force_stealth": forceStealth,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		t.agentSearchURL+"/api/extract",
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
		return dtool.ToolResult{Output: fmt.Sprintf("agent-search /api/extract error HTTP %d: %s", resp.StatusCode, string(respBody))}, nil
	}

	var extractResp struct {
		Results []struct {
			URL         string `json:"url"`
			Title       string `json:"title"`
			Content     string `json:"content"`
			ContentType string `json:"content_type"`
			Truncated   bool   `json:"truncated"`
			FetchTimeMs int    `json:"fetch_time_ms"`
			FetchMethod string `json:"fetch_method"`
			LocalPath   string `json:"local_path"`
			ByteSize    int    `json:"byte_size"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &extractResp); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error parsing agent-search response: %v\nRaw: %s", err, string(respBody))}, nil
	}

	if len(extractResp.Results) == 0 {
		return dtool.ToolResult{Output: fmt.Sprintf("No content extracted from: %s", targetURL)}, nil
	}

	fr := extractResp.Results[0]

	if fr.ContentType == "error" || strings.Contains(fr.Content, "[Fetch error") {
		return dtool.ToolResult{Output: fmt.Sprintf("Fetch failed for %s: %s", fr.URL, fr.Content)}, nil
	}

	var sb strings.Builder
	if fr.LocalPath != "" {
		sb.WriteString(fmt.Sprintf("Type: %s\n", fr.ContentType))
		sb.WriteString(fmt.Sprintf("URL: %s\n", fr.URL))
		sb.WriteString(fmt.Sprintf("Local path: %s\n", fr.LocalPath))
		sb.WriteString(fmt.Sprintf("Size: %d bytes\n", fr.ByteSize))
		sb.WriteString(fmt.Sprintf("Fetched in: %dms (method: %s)\n", fr.FetchTimeMs, fr.FetchMethod))
	} else {
		if fr.Title != "" {
			sb.WriteString(fmt.Sprintf("Title: %s\n", fr.Title))
		}
		sb.WriteString(fmt.Sprintf("URL: %s\n", fr.URL))
		sb.WriteString(fmt.Sprintf("Fetched in: %dms (method: %s)\n\n", fr.FetchTimeMs, fr.FetchMethod))
		sb.WriteString(fr.Content)
		if fr.Truncated {
			sb.WriteString("\n... (truncated)")
		}
	}
	return dtool.ToolResult{Output: sb.String()}, nil
}

// ---------------------------------------------------------------------------
// DeepResearchTool: one-shot deep research via agent-search /api/search depth=deep
// ---------------------------------------------------------------------------

// DeepResearchTool provides high-quality, anti-bot-proof deep content via
// the agent-search pipeline: SearXNG → LLM reranker → parallel Camoufox crawl → de-noise.
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

func (t *DeepResearchTool) Name() string { return "deep_research" }
func (t *DeepResearchTool) Description() string {
	return `Deep research: search + re-rank + deep-crawl top results in parallel.
Slower than web_search (~5-15s) but richer content with anti-bot bypass.
Use for complex research where quality > speed.`
}

func (t *DeepResearchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":         map[string]any{"type": "string", "description": "Research question or topic"},
			"categories":    map[string]any{"type": "string", "description": "Content category: general|news|images|videos (default: general)"},
			"fetch_top":     map[string]any{"type": "integer", "description": "Number of top results to deep-crawl (default: 3, max: 5)"},
			"limit":         map[string]any{"type": "integer", "description": "Max results to return (default: 5)"},
			"force_stealth": map[string]any{"type": "boolean", "description": "Force stealth browser for crawling stubborn anti-bot targets (default: false)"},
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

	forceStealth := false
	if v, ok := args["force_stealth"].(bool); ok {
		forceStealth = v
	}

	// Route through agent-search /api/search with depth=deep
	payload := map[string]any{
		"query":              query,
		"depth":              "deep",
		"categories":         categories,
		"fetch_top":          fetchTop,
		"max_results":        limit,
		"max_content_length": 8000,
		"force_stealth":      forceStealth,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST",
		t.agentSearchURL+"/api/search",
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
			Title       string  `json:"title"`
			URL         string  `json:"url"`
			Domain      string  `json:"domain"`
			Score       float64 `json:"score"`
			Reason      string  `json:"reason"`
			ResultType  string  `json:"result_type"`
			Snippet     string  `json:"snippet"`
			Content     string  `json:"content"`
			FetchMethod string  `json:"fetch_method"`
			LocalPath   string  `json:"local_path"`
			Truncated   bool    `json:"truncated"`
		} `json:"results"`
		Total       int    `json:"total"`
		Intent      string `json:"intent"`
		QueryTimeMs int    `json:"query_time_ms"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error parsing deep_research response: %v", err)}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Deep Research Results for: %s\n", query))
	sb.WriteString(fmt.Sprintf("Intent: %s | Results: %d | Time: %dms\n\n",
		result.Intent, result.Total, result.QueryTimeMs))

	for i, r := range result.Results {
		sb.WriteString(fmt.Sprintf("--- [%d] [%s] %s ---\n", i+1, r.ResultType, r.Title))
		sb.WriteString(fmt.Sprintf("URL: %s\n", r.URL))
		if r.Score > 0 {
			sb.WriteString(fmt.Sprintf("Relevance: %.0f%% — %s\n", r.Score*100, r.Reason))
		}
		if r.FetchMethod != "" {
			sb.WriteString(fmt.Sprintf("Fetch: %s\n", r.FetchMethod))
		}
		if r.LocalPath != "" {
			sb.WriteString(fmt.Sprintf("Local path: %s\n", r.LocalPath))
		}
		if r.Content != "" {
			sb.WriteString(fmt.Sprintf("\n%s\n", r.Content))
			if r.Truncated {
				sb.WriteString("... (truncated)\n")
			}
			sb.WriteString("\n")
		} else if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("Snippet: %s\n\n", r.Snippet))
		}
	}
	return dtool.ToolResult{Output: sb.String()}, nil
}
