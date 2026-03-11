package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

// Discovery provides 3-layer prompt file discovery:
// Layer 1: ~/.ngoagent/ (global)
// Layer 2: .ngoagent/ (project)
// Layer 3: ~/.ngoagent/prompts/variants/ (overlay)
type Discovery struct {
	homeDir      string
	workspaceDir string
}

// NewDiscovery creates a file discovery scanner.
func NewDiscovery(homeDir, workspaceDir string) *Discovery {
	return &Discovery{
		homeDir:      homeDir,
		workspaceDir: workspaceDir,
	}
}

// LoadUserRules discovers and concatenates user_rules.md from all layers.
// Project rules override global rules.
func (d *Discovery) LoadUserRules() string {
	files := []string{
		filepath.Join(d.homeDir, "user_rules.md"),
		filepath.Join(d.workspaceDir, ".ngoagent", "user_rules.md"),
	}

	var parts []string
	for _, f := range files {
		if content, err := os.ReadFile(f); err == nil {
			text := strings.TrimSpace(string(content))
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// LoadProjectContext reads context.md from the project's .ngoagent/ directory.
func (d *Discovery) LoadProjectContext() string {
	path := filepath.Join(d.workspaceDir, ".ngoagent", "context.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

// LoadVariants reads all .md files from ~/.ngoagent/prompts/variants/.
func (d *Discovery) LoadVariants() string {
	dir := filepath.Join(d.homeDir, "prompts", "variants")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var parts []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(content))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}
