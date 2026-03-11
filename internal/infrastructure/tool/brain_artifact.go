package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// BrainArtifactTool lets the agent read/write versioned brain artifacts.
// Mirrors Antigravity's pattern: source.md + .metadata.json + .resolved.N.
type BrainArtifactTool struct {
	brain *brain.ArtifactStore
}

// NewBrainArtifactTool creates a brain artifact tool.
func NewBrainArtifactTool(b *brain.ArtifactStore) *BrainArtifactTool {
	return &BrainArtifactTool{brain: b}
}

func (t *BrainArtifactTool) Name() string { return "brain_artifact" }

func (t *BrainArtifactTool) Description() string {
	return "Manage versioned brain artifacts (task plans, walkthroughs, notes). " +
		"Actions: write (create/update with version history), read (with metadata), " +
		"list (all artifacts), versions (history), read_version (specific version)."
}

func (t *BrainArtifactTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"write", "read", "list", "versions", "read_version"},
				"description": "Action to perform",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Artifact filename (e.g. task.md, plan.md, walkthrough.md)",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to write (for write action)",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Brief summary of the artifact (for write action)",
			},
			"artifact_type": map[string]any{
				"type":        "string",
				"enum":        []string{"task", "plan", "walkthrough", "other"},
				"description": "Type of artifact (for write action)",
			},
			"version": map[string]any{
				"type":        "integer",
				"description": "Version number (for read_version action)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *BrainArtifactTool) Execute(ctx context.Context, params map[string]any) (dtool.ToolResult, error) {
	action, _ := params["action"].(string)

	switch action {
	case "write":
		return t.doWrite(params)
	case "read":
		return t.doRead(params)
	case "list":
		return t.doList()
	case "versions":
		return t.doVersions(params)
	case "read_version":
		return t.doReadVersion(params)
	default:
		return dtool.ToolResult{}, fmt.Errorf("brain_artifact: unknown action %q (use write/read/list/versions/read_version)", action)
	}
}

func (t *BrainArtifactTool) doWrite(params map[string]any) (dtool.ToolResult, error) {
	if t.brain == nil {
		return dtool.ToolResult{}, fmt.Errorf("brain not available")
	}
	name, _ := params["name"].(string)
	content, _ := params["content"].(string)
	summary, _ := params["summary"].(string)
	artifactType, _ := params["artifact_type"].(string)

	if name == "" || content == "" {
		return dtool.ToolResult{}, fmt.Errorf("name and content required")
	}
	if artifactType == "" {
		artifactType = "other"
	}

	if err := t.brain.WriteArtifact(name, content, summary, artifactType); err != nil {
		return dtool.ToolResult{}, fmt.Errorf("write artifact: %w", err)
	}

	meta, _ := t.brain.ReadMetadata(name)
	verCount := len(t.brain.ListVersions(name))
	return dtool.ToolResult{Output: fmt.Sprintf("Wrote brain artifact: %s (type=%s, version=%d, history=%d versions)\nSummary: %s",
		name, artifactType, meta.Version, verCount, summary)}, nil
}

func (t *BrainArtifactTool) doRead(params map[string]any) (dtool.ToolResult, error) {
	if t.brain == nil {
		return dtool.ToolResult{}, fmt.Errorf("brain not available")
	}
	name, _ := params["name"].(string)
	if name == "" {
		return dtool.ToolResult{}, fmt.Errorf("name required")
	}
	content, err := t.brain.Read(name)
	if err != nil {
		return dtool.ToolResult{}, fmt.Errorf("read artifact: %w", err)
	}

	// Include metadata if available
	meta, _ := t.brain.ReadMetadata(name)
	if meta != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("[metadata: type=%s, version=%d, updated=%s]\n\n%s",
			meta.ArtifactType, meta.Version, meta.UpdatedAt.Format("2006-01-02 15:04:05"), content)}, nil
	}
	return dtool.ToolResult{Output: content}, nil
}

func (t *BrainArtifactTool) doList() (dtool.ToolResult, error) {
	if t.brain == nil {
		return dtool.ToolResult{}, fmt.Errorf("brain not available")
	}
	files, err := t.brain.List()
	if err != nil {
		return dtool.ToolResult{}, fmt.Errorf("list artifacts: %w", err)
	}

	// Filter: only show source files (not .resolved.N or .metadata.json)
	var artifacts []string
	for _, f := range files {
		if strings.Contains(f, ".resolved") || strings.Contains(f, ".metadata.json") {
			continue
		}
		meta, _ := t.brain.ReadMetadata(f)
		verCount := len(t.brain.ListVersions(f))
		if meta != nil {
			artifacts = append(artifacts, fmt.Sprintf("  %s [%s] v%d (%d history) — %s",
				f, meta.ArtifactType, meta.Version, verCount, meta.Summary))
		} else {
			artifacts = append(artifacts, fmt.Sprintf("  %s (no metadata)", f))
		}
	}

	if len(artifacts) == 0 {
		return dtool.ToolResult{Output: "No brain artifacts found."}, nil
	}
	return dtool.ToolResult{Output: fmt.Sprintf("Brain artifacts (%d):\n%s", len(artifacts), strings.Join(artifacts, "\n"))}, nil
}

func (t *BrainArtifactTool) doVersions(params map[string]any) (dtool.ToolResult, error) {
	if t.brain == nil {
		return dtool.ToolResult{}, fmt.Errorf("brain not available")
	}
	name, _ := params["name"].(string)
	if name == "" {
		return dtool.ToolResult{}, fmt.Errorf("name required")
	}
	versions := t.brain.ListVersions(name)
	if len(versions) == 0 {
		return dtool.ToolResult{Output: fmt.Sprintf("No version history for %s", name)}, nil
	}

	data, _ := json.Marshal(versions)
	return dtool.ToolResult{Output: fmt.Sprintf("Version history for %s: %s (%d versions)", name, string(data), len(versions))}, nil
}

func (t *BrainArtifactTool) doReadVersion(params map[string]any) (dtool.ToolResult, error) {
	if t.brain == nil {
		return dtool.ToolResult{}, fmt.Errorf("brain not available")
	}
	name, _ := params["name"].(string)
	verF, _ := params["version"].(float64)
	ver := int(verF)

	content, err := t.brain.ReadVersion(name, ver)
	if err != nil {
		return dtool.ToolResult{}, fmt.Errorf("read version %d of %s: %w", ver, name, err)
	}
	return dtool.ToolResult{Output: fmt.Sprintf("[version %d of %s]\n\n%s", ver, name, content)}, nil
}

// SetBrain updates the brain store (for per-session injection).
func (t *BrainArtifactTool) SetBrain(b *brain.ArtifactStore) {
	t.brain = b
}
