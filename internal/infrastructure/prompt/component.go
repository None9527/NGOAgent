package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

// PromptComponent represents a pluggable prompt component loaded from *.md files
// with YAML frontmatter: name, requires clause, priority.
type PromptComponent struct {
	Name     string   // Unique component name
	Content  string   // Markdown body
	Priority int      // Assembly order (lower = earlier)
	Requires []string // RequiresClause: conditions for injection
	Source   string   // File path
}

// RequiresClause checks if all conditions are met.
// Conditions: "planning_mode", "heartbeat_mode", "model:xxx", "channel:xxx"
func (pc *PromptComponent) RequiresClause(ctx map[string]string) bool {
	for _, req := range pc.Requires {
		parts := strings.SplitN(req, ":", 2)
		key := parts[0]
		if len(parts) == 2 {
			// key:value comparison
			if val, ok := ctx[key]; !ok || val != parts[1] {
				return false
			}
		} else {
			// Boolean flag check
			if val, ok := ctx[key]; !ok || val != "true" {
				return false
			}
		}
	}
	return true
}

// LoadComponents discovers and loads *.md prompt components from a directory.
// Components live in ~/.ngoagent/prompts/ or .ngoagent/prompts/
func LoadComponents(dir string) ([]PromptComponent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // No components dir is fine
	}

	var components []PromptComponent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		pc := parseComponent(string(data), path)
		if pc.Name == "" {
			pc.Name = strings.TrimSuffix(entry.Name(), ".md")
		}
		components = append(components, pc)
	}

	return components, nil
}

// parseComponent extracts YAML frontmatter from a markdown file.
func parseComponent(content, source string) PromptComponent {
	pc := PromptComponent{Source: source}

	lines := strings.Split(content, "\n")
	inFrontmatter := false
	bodyStart := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			// End of frontmatter
			bodyStart = i + 1
			break
		}
		if !inFrontmatter {
			// No frontmatter — entire file is body
			pc.Content = content
			return pc
		}
		// Parse YAML-like frontmatter
		if strings.HasPrefix(line, "name:") {
			pc.Name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			pc.Name = strings.Trim(pc.Name, "\"'")
		}
		if strings.HasPrefix(line, "priority:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "priority:"))
			for _, c := range val {
				if c >= '0' && c <= '9' {
					pc.Priority = pc.Priority*10 + int(c-'0')
				}
			}
		}
		if strings.HasPrefix(line, "requires:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "requires:"))
			// Supports comma-separated: "planning_mode, model:claude"
			for _, r := range strings.Split(val, ",") {
				r = strings.TrimSpace(r)
				if r != "" {
					pc.Requires = append(pc.Requires, r)
				}
			}
		}
	}

	if bodyStart < len(lines) {
		pc.Content = strings.Join(lines[bodyStart:], "\n")
	}
	return pc
}
