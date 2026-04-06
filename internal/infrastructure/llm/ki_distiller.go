package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// KIResult holds the structured output from LLM knowledge distillation.
type KIResult struct {
	ShouldSave bool     `json:"should_save"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
}

// KnowledgeDistiller uses an LLM to analyze conversations and extract
// meaningful, reusable knowledge. Implements service.KILLMDistiller.
type KnowledgeDistiller struct {
	router *Router
}

// NewKnowledgeDistiller creates a distiller backed by the given router.
func NewKnowledgeDistiller(r *Router) *KnowledgeDistiller {
	return &KnowledgeDistiller{router: r}
}

const kiDistillPrompt = `You are a knowledge distillation engine. Analyze the following conversation between a user and an AI coding assistant. Extract any reusable, persistent knowledge worth remembering for future sessions.

**What qualifies as worth saving (bias toward saving):**
- Bug fixes: root cause analysis, regex/config/logic errors and their fixes
- Architecture insights: how components connect, data flow, pipeline traces
- Technical decisions and design choices for the project
- Configuration details (API keys setup, env vars, ports, paths)
- Project-specific conventions or constraints
- Important user preferences or requirements
- Non-obvious implementation details or gotchas
- Debugging techniques: how a problem was traced and resolved

**What does NOT qualify (only skip truly trivial):**
- Simple factual Q&A with no project context (e.g., "what does X mean?")
- Conversations that produced no code changes and no insights

**When in doubt, set should_save=true.** It is better to save too much than to lose knowledge.

Respond with a JSON object (no markdown fences):
{
  "should_save": true/false,
  "title": "Concise descriptive title (max 10 words)",
  "summary": "1-2 sentence summary of the knowledge",
  "content": "Detailed knowledge content in markdown format, including key details, code snippets, or configurations worth preserving",
  "tags": ["tag1", "tag2"]
}

If the conversation has no lasting knowledge value, set should_save=false and leave other fields empty.

CONVERSATION:
`

// DistillKnowledge analyzes a conversation and extracts structured knowledge.
func (d *KnowledgeDistiller) DistillKnowledge(messages []Message) (*KIResult, error) {
	prov, model, err := d.router.ResolveWithFallback("")
	if err != nil {
		return nil, fmt.Errorf("ki distill: no provider: %w", err)
	}

	// Build conversation excerpt — keep it within budget
	var b strings.Builder
	for i, msg := range messages {
		// Skip system messages
		if msg.Role == "system" {
			continue
		}
		role := msg.Role
		content := msg.Content
		// Truncate individual messages
		if len([]rune(content)) > 800 {
			r := []rune(content)
			content = string(r[:800]) + "...(truncated)"
		}
		b.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, content))
		// Cap total at ~20 messages
		if i > 20 {
			b.WriteString("...(remaining messages omitted)\n")
			break
		}
	}

	prompt := kiDistillPrompt + b.String()

	req := &Request{
		Model:     model,
		MaxTokens: 1000,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}

	ch := make(chan StreamChunk, 64)
	resp, err := prov.GenerateStream(context.Background(), req, ch)
	if err != nil {
		return nil, fmt.Errorf("ki distill: llm error: %w", err)
	}

	raw := strings.TrimSpace(resp.Content)
	// Strip markdown code fences if present
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var result KIResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("ki distill: parse json: %w (raw: %s)", err, raw[:min(len(raw), 200)])
	}

	return &result, nil
}

const kiMergePrompt = `You are a knowledge consolidation engine. Below are two pieces of knowledge about the SAME topic:

**EXISTING KNOWLEDGE:**
%s

**NEW KNOWLEDGE from latest session:**
%s

Your task: Merge these into ONE consolidated, concise knowledge document.

Rules:
1. REMOVE duplicate information — keep only one version of each fact
2. KEEP all unique details, configurations, and code snippets from both
3. PRIORITIZE newer information when there are conflicts
4. The result should be SHORTER or EQUAL in length to the combined input
5. Use markdown format with clear headers

Respond with JSON (no markdown fences):
{
  "title": "Updated concise title (max 10 words)",
  "summary": "1-2 sentence summary of the merged knowledge",
  "content": "Merged knowledge content in clean markdown"
}
`

// MergeKnowledge consolidates old and new KI content via LLM.
// Returns a unified KIResult that replaces (not appends to) the existing KI.
func (d *KnowledgeDistiller) MergeKnowledge(existingContent, newContent string) (*KIResult, error) {
	prov, model, err := d.router.ResolveWithFallback("")
	if err != nil {
		return nil, fmt.Errorf("ki merge: no provider: %w", err)
	}

	// Truncate inputs to prevent context overflow
	if len([]rune(existingContent)) > 3000 {
		existingContent = string([]rune(existingContent)[:3000]) + "\n...(truncated)"
	}
	if len([]rune(newContent)) > 2000 {
		newContent = string([]rune(newContent)[:2000]) + "\n...(truncated)"
	}

	prompt := fmt.Sprintf(kiMergePrompt, existingContent, newContent)

	req := &Request{
		Model:     model,
		MaxTokens: 1500,
		Messages: []Message{
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}

	ch := make(chan StreamChunk, 64)
	resp, err := prov.GenerateStream(context.Background(), req, ch)
	if err != nil {
		return nil, fmt.Errorf("ki merge: llm error: %w", err)
	}

	raw := strings.TrimSpace(resp.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var result KIResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("ki merge: parse json: %w (raw: %s)", err, raw[:min(len(raw), 200)])
	}
	result.ShouldSave = true
	return &result, nil
}
