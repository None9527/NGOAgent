package llm

import (
	"context"
	"fmt"
	"strings"
)

// TitleDistiller implements service.TitleLLMCaller using an LLM provider.
// It makes a lightweight, non-streaming call to generate a concise session title.
type TitleDistiller struct {
	router *Router
}

// NewTitleDistiller creates a TitleDistiller backed by the given router.
func NewTitleDistiller(r *Router) *TitleDistiller {
	return &TitleDistiller{router: r}
}

// DistillTitle calls the LLM with a minimal prompt and returns a concise title.
// Implements service.TitleLLMCaller.
func (d *TitleDistiller) DistillTitle(userMessage string) (string, error) {
	prov, model, err := d.router.ResolveWithFallback("")
	if err != nil {
		return "", fmt.Errorf("title distill: no provider: %w", err)
	}

	// Truncate long messages to keep the prompt cheap
	excerpt := userMessage
	if len([]rune(excerpt)) > 300 {
		r := []rune(excerpt)
		excerpt = string(r[:300]) + "..."
	}

	prompt := fmt.Sprintf(
		"Generate a concise, descriptive title (max 8 words, no quotes, no period at end) "+
			"for a conversation that starts with this user message:\n\n%s",
		excerpt,
	)

	req := &Request{
		Model:     model,
		MaxTokens: 40,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}

	// Drain the stream channel — we only need the aggregated Response.
	ch := make(chan StreamChunk, 64)
	resp, err := prov.GenerateStream(context.Background(), req, ch)
	if err != nil {
		return "", fmt.Errorf("title distill: llm error: %w", err)
	}

	title := strings.TrimSpace(resp.Content)
	if title == "" {
		return "", fmt.Errorf("title distill: empty response")
	}
	// Strip surrounding quotes models sometimes add
	title = strings.Trim(title, `"'`)
	return title, nil
}
