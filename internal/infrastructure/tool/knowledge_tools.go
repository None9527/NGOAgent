package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
)

// TaskPlanTool manages structured task plans, checklists, and walkthroughs.
type TaskPlanTool struct {
	brainDir string // Root brain dir (fallback if context has no session dir)
}

func NewTaskPlanTool(brainDir string) *TaskPlanTool {
	return &TaskPlanTool{brainDir: brainDir}
}

func (t *TaskPlanTool) Name() string { return "task_plan" }
func (t *TaskPlanTool) Description() string {
	return `Manage artifacts: plan (design), task (checklist), walkthrough (summary).
- action: create/update/get/complete.
- type: plan/task/walkthrough.`
}

func (t *TaskPlanTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":  map[string]any{"type": "string", "enum": []string{"create", "update", "get", "complete"}, "description": "Action to perform"},
			"type":    map[string]any{"type": "string", "enum": []string{"plan", "task", "walkthrough"}, "description": "Artifact type"},
			"content": map[string]any{"type": "string", "description": "Content to write"},
			"summary": map[string]any{"type": "string", "description": "Short summary"},
		},
		"required": []string{"action"},
	}
}

// getStore returns a session-scoped ArtifactStore from context, or a root fallback.
func (t *TaskPlanTool) getStore(ctx context.Context) *brain.ArtifactStore {
	// Preferred: get fully-configured store from context (carries workspaceDir, sessionID, etc.)
	if store := brain.BrainStoreFromContext(ctx); store != nil {
		return store
	}
	// Legacy fallback: reconstruct from brainDir string
	if dir := brain.BrainDirFromContext(ctx); dir != "" {
		return brain.NewArtifactStoreFromDir(dir)
	}
	return brain.NewArtifactStoreFromDir(t.brainDir)
}

func (t *TaskPlanTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	store := t.getStore(ctx)

	action, _ := args["action"].(string)
	artifactType, _ := args["type"].(string)
	summary, _ := args["summary"].(string)
	content, _ := args["content"].(string)

	if artifactType == "" {
		artifactType = "task"
	}

	filename := artifactType + ".md"

	switch action {
	case "create", "update":
		// Ensure session brain directory exists (defense against race conditions)
		os.MkdirAll(store.BaseDir(), 0755)
		if err := store.WriteArtifact(filename, content, summary, artifactType); err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("Error writing %s: %v", filename, err)}, nil
		}
		fullPath := filepath.Join(store.BaseDir(), filename)
		msg := fmt.Sprintf("Successfully %sd %s → %s", action, filename, fullPath)
		// Plan creation: DO NOT yield — instead force the agent to call notify_user next.
		// This ensures the user always reviews the plan before execution proceeds.
		if action == "create" && artifactType == "plan" {
			return dtool.ToolResult{
				Output:  msg + "\n\n⚠️ MANDATORY: You MUST now call notify_user(message=\"...\", paths_to_review=[\"" + fullPath + "\"], blocked_on_user=true) to present this plan for user review. Do NOT proceed with any other tool.",
				Signal:  dtool.SignalProgress,
				Payload: map[string]any{"message": msg, "artifact": filename, "force_next_tool": "notify_user"},
			}, nil
		}
		return dtool.ToolResult{Output: msg}, nil

	case "get":
		data, err := store.Read(filename)
		if err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("No %s found. Use action=create to create one.", artifactType)}, nil
		}
		return dtool.ToolResult{Output: data}, nil

	case "complete":
		// complete only makes sense for task.md (checklist with [ ] markers).
		// walkthrough.md and plan.md are reports — no checkboxes to toggle.
		if artifactType != "task" {
			return dtool.ToolResult{Output: fmt.Sprintf("Error: action=complete is only valid for type=task, not %q. Use action=update to modify %s.", artifactType, filename)}, nil
		}
		data, err := store.Read(filename)
		if err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("No %s found. Create it first with action=create.", filename)}, nil
		}
		// Replace only line-leading checklist items (not inside code blocks)
		lines := strings.Split(data, "\n")
		for i, line := range lines {
			trimmed := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(trimmed, "- [ ]") {
				lines[i] = strings.Replace(line, "- [ ]", "- [x]", 1)
			}
		}
		completed := strings.Join(lines, "\n")
		completed += fmt.Sprintf("\n\n---\nCompleted at: %s\n", time.Now().Format(time.RFC3339))
		if err := store.Write(filename, completed); err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
		}
		return dtool.ToolResult{Output: fmt.Sprintf("Marked all tasks in %s as complete", filename)}, nil

	default:
		return dtool.ToolResult{Output: "Error: action must be one of: create, update, get, complete"}, nil
	}
}

// UpdateProjectContextTool updates the project's persistent knowledge.
type UpdateProjectContextTool struct{}

func (t *UpdateProjectContextTool) Name() string { return "update_project_context" }
func (t *UpdateProjectContextTool) Description() string {
	return `Update the project's persistent knowledge store.
- action: append / replace_section / read
- Information saved here is injected into future sessions for this project
- Use for: tech stack, build commands, architecture conventions, gotchas`
}

func (t *UpdateProjectContextTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":  map[string]any{"type": "string", "enum": []string{"append", "replace_section", "read"}, "description": "Action to perform"},
			"section": map[string]any{"type": "string", "description": "Section name (for replace_section)"},
			"content": map[string]any{"type": "string", "description": "Content to append or replace"},
		},
		"required": []string{"action"},
	}
}

func (t *UpdateProjectContextTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	action, _ := args["action"].(string)
	content, _ := args["content"].(string)

	// Find .ngoagent/context.md from cwd
	cwd, _ := os.Getwd()
	path := filepath.Join(cwd, ".ngoagent", "context.md")

	switch action {
	case "read":
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return dtool.ToolResult{Output: "No project context file found."}, nil
			}
			return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
		}
		return dtool.ToolResult{Output: string(data)}, nil

	case "append":
		os.MkdirAll(filepath.Dir(path), 0755)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
		}
		defer f.Close()
		f.WriteString("\n" + content + "\n")
		return dtool.ToolResult{Output: "Context updated."}, nil

	case "replace_section":
		section, _ := args["section"].(string)
		if section == "" {
			return dtool.ToolResult{Output: "Error: 'section' is required for replace_section"}, nil
		}

		existing, _ := os.ReadFile(path)
		text := string(existing)

		// Find section (## header)
		marker := "## " + section
		idx := strings.Index(text, marker)
		if idx < 0 {
			// Append as new section
			os.MkdirAll(filepath.Dir(path), 0755)
			f, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
			defer f.Close()
			f.WriteString(fmt.Sprintf("\n## %s\n\n%s\n", section, content))
			return dtool.ToolResult{Output: "New section added."}, nil
		}

		// Find next section or end
		rest := text[idx+len(marker):]
		nextIdx := strings.Index(rest, "\n## ")
		if nextIdx < 0 {
			nextIdx = len(rest)
		}

		newText := text[:idx] + fmt.Sprintf("## %s\n\n%s\n", section, content) + rest[nextIdx:]
		os.WriteFile(path, []byte(newText), 0644)
		return dtool.ToolResult{Output: "Section replaced."}, nil

	default:
		return dtool.ToolResult{Output: "Error: action must be one of: append, replace_section, read"}, nil
	}
}

// SaveKnowledgeTool saves knowledge to the cross-session persistent store.
// Supports embedding-based dedup if a retriever is provided.
type SaveKnowledgeTool struct {
	store     *knowledge.Store
	retriever *knowledge.Retriever // optional: for dedup + re-indexing
	threshold float64              // cosine similarity threshold for dedup
}

func NewSaveKnowledgeTool(store *knowledge.Store, retriever *knowledge.Retriever, threshold float64) *SaveKnowledgeTool {
	// Write-time dedup must be aggressive — catch "media_studio CLI usage" vs
	// "media-studio CLI correct invocation" as duplicates to avoid KI sprawl.
	// Config threshold is for retrieval quality; dedup uses a fixed low bar.
	const dedupCeiling = 0.60
	if threshold <= 0 || threshold > dedupCeiling {
		threshold = dedupCeiling
	}
	return &SaveKnowledgeTool{store: store, retriever: retriever, threshold: threshold}
}

func (t *SaveKnowledgeTool) Name() string { return "save_knowledge" }
func (t *SaveKnowledgeTool) Description() string {
	return `Save knowledge to persistent cross-session store. Available across ALL future sessions.
- tags: use "preference" for enforced user preferences
- Similar KIs auto-merged (>0.85 similarity)
- For project-specific info, use update_project_context instead`
}

func (t *SaveKnowledgeTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":     map[string]any{"type": "string", "description": "Descriptive key (title) for this knowledge item. Keep concise and unique."},
			"content": map[string]any{"type": "string", "description": "Knowledge content. Focus on: user preferences, architecture decisions, external system access info (URLs/APIs), root causes of solved problems. Do NOT save: code implementation details (readable from source), git history, temporary task state, installation steps (in docs), or generic how-to knowledge."},
			"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Categorization: 'preference' (user habits/style), 'project' (architecture/decisions), 'reference' (external system pointers), 'feedback' (corrections to agent behavior)"},
		},
		"required": []string{"key", "content"},
	}
}

func (t *SaveKnowledgeTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	key, _ := args["key"].(string)
	content, _ := args["content"].(string)

	if key == "" || content == "" {
		return dtool.ToolResult{Output: "Error: 'key' and 'content' are required"}, nil
	}

	// Parse optional tags
	var tags []string
	if rawTags, ok := args["tags"].([]any); ok {
		for _, rt := range rawTags {
			tags = append(tags, fmt.Sprint(rt))
		}
	}

	// M3b: Dedup check using content for semantic matching (not just key/title).
	// If a similar KI exists, merge new content into it rather than creating a duplicate.
	if t.retriever != nil {
		// Use content as the query for more accurate semantic matching
		dupID, score := t.retriever.FindDuplicate(content, t.threshold)
		if dupID != "" {
			// Append new findings to existing KI under a dated section
			appendContent := fmt.Sprintf("\n\n---\n\n## Update\n\n%s", content)
			mergeSummary := key // Use the new key as updated summary hint
			if err := t.store.UpdateMerge(dupID, appendContent, mergeSummary); err != nil {
				return dtool.ToolResult{Output: fmt.Sprintf("Error merging into existing KI: %v", err)}, nil
			}
			_ = t.retriever.EmbedAndIndexByID(dupID)
			return dtool.ToolResult{Output: fmt.Sprintf("✓ Merged into existing KI %q (similarity=%.2f, threshold=%.2f). Updated in place.", dupID, score, t.threshold)}, nil
		}
	}

	// Build summary: first 200 chars of content
	summary := content
	if len(summary) > 200 {
		summary = summary[:200] + "..."
	}

	item := &knowledge.Item{
		Title:   key,
		Summary: summary,
		Content: content,
		Tags:    tags,
	}
	if err := t.store.Save(item); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error saving knowledge: %v", err)}, nil
	}

	// Index the new KI if retriever is available
	if t.retriever != nil {
		_ = t.retriever.EmbedAndIndex(item)
	}

	return dtool.ToolResult{Output: fmt.Sprintf("Knowledge saved: %s → %s/%s/", key, t.store.BaseDir(), item.ID)}, nil
}
