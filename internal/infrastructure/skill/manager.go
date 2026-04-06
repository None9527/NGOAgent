// Package skill provides skill discovery, loading, and forge lifecycle management.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/entity"
	"gopkg.in/yaml.v3"
)

// skillFrontmatter maps directly to SKILL.md YAML frontmatter.
type skillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Weight      string   `yaml:"weight"` // deprecated: auto-derived, kept for backward compat
	Rules       []string `yaml:"rules"`
	WhenToUse   string   `yaml:"when_to_use"` // precise trigger condition for listing
	Context     string   `yaml:"context"`     // "inline" (default) | "fork" (spawn sub-agent)
	Args        string   `yaml:"args"`        // parameter hint (e.g. "[topic] [style?]")
}

// extractFrontmatter parses the YAML frontmatter block between --- delimiters.
func extractFrontmatter(content string) skillFrontmatter {
	var fm skillFrontmatter
	lines := strings.Split(content, "\n")
	var start, end int
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if start == 0 {
				start = i + 1
			} else {
				end = i
				break
			}
		}
	}
	if start == 0 || end == 0 {
		return fm
	}
	block := strings.Join(lines[start:end], "\n")
	_ = yaml.Unmarshal([]byte(block), &fm)
	return fm
}

// detectSkillType checks skill directory contents to determine type.
//   - "pipeline": has workflow.yaml (executed by WorkflowRunner, code-enforced steps)
//   - "executable": has run.sh or run.py (direct script execution)
//   - "workflow": SKILL.md only (guide injected into LLM prompt)
func detectSkillType(skillDir string) string {
	if HasWorkflow(skillDir) {
		return "pipeline"
	}
	for _, script := range []string{"run.sh", "run.py"} {
		if _, err := os.Stat(filepath.Join(skillDir, script)); err == nil {
			return "executable"
		}
	}
	return "workflow"
}

// Manager handles skill discovery, loading, and lifecycle.
type Manager struct {
	skillDirs []string // Ordered: global, project
	skills    map[string]*entity.Skill
}

// NewManager creates a skill manager scanning the given directories.
func NewManager(skillDirs ...string) *Manager {
	m := &Manager{
		skillDirs: skillDirs,
		skills:    make(map[string]*entity.Skill),
	}
	m.Discover()
	return m
}

// Discover scans all skill directories for SKILL.md files.
func (m *Manager) Discover() {
	// Clear stale entries so removed skills don't linger in memory.
	m.skills = make(map[string]*entity.Skill)
	for _, dir := range m.skillDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillFile); err != nil {
				continue
			}

			content, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}

			raw := string(content)
			fm := extractFrontmatter(raw)
			name := fm.Name
			if name == "" {
				name = entry.Name()
			}

			cmd := parseSkillCommand(raw)
			skillDir := filepath.Join(dir, entry.Name())
			skillType := detectSkillType(skillDir)
			triggers := parseTriggers(raw)
			// Auto-derive weight: explicit frontmatter > trigger heuristic > default
			weight := fm.Weight
			if weight == "" {
				switch {
				case skillType == "pipeline" || skillType == "executable":
					weight = "heavy" // code-enforced execution, always prominent
				case fm.Context == "fork":
					weight = "heavy" // fork skills are important enough for full listing
				case len(triggers) > 0:
					weight = "heavy"
				default:
					weight = "light"
				}
			}
			m.skills[name] = &entity.Skill{
				ID:          name,
				Name:        name,
				Description: fm.Description,
				Type:        skillType,
				Weight:      weight,
				Triggers:    triggers,
				Rules:       fm.Rules,
				Command:     cmd,
				Path:        filepath.Join(dir, entry.Name()),
				Content:     raw,
				Enabled:     true,
				EvoStatus:   "draft",
				InstalledAt: time.Now(),
				WhenToUse:   fm.WhenToUse,
				Context:     fm.Context,
				Args:        fm.Args,
				Category:    KICategory(fm.Description + " " + fm.Name),
				KIRef:       scanKIRefs(filepath.Join(dir, entry.Name())),
			}
		}
	}
}

// ═══════════════════════════════════════════
// P3 L3: KI Categorization
// ═══════════════════════════════════════════

// kiCategoryKeywords maps category → keyword signals.
var kiCategoryKeywords = map[string][]string{
	"ai":     {"llm", "model", "ai", "gpt", "claude", "embedding", "vector", "rag", "prompt", "agent", "openai", "anthropic"},
	"web":    {"http", "fetch", "browser", "html", "css", "react", "frontend", "api", "rest", "graphql", "web", "url", "scrape"},
	"data":   {"csv", "excel", "xlsx", "database", "sql", "pandas", "dataframe", "spreadsheet", "json", "xml", "etl", "postgres"},
	"devops": {"docker", "k8s", "kubernetes", "deploy", "ci", "pipeline", "terraform", "ansible", "helm", "cloud", "aws", "gcp"},
	"infra":  {"git", "github", "pr", "commit", "branch", "repo", "build", "test", "make", "compile", "lint", "format"},
	"media":  {"image", "video", "audio", "pdf", "docx", "pptx", "slide", "photo", "resize", "convert", "ffmpeg"},
}

// KICategory auto-classifies a skill by keyword matching on its description.
// Returns the best-matching category, or "util" if nothing matches.
func KICategory(text string) string {
	ltext := strings.ToLower(text)
	bestCat := "util"
	bestScore := 0
	for cat, keywords := range kiCategoryKeywords {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(ltext, kw) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestCat = cat
		}
	}
	return bestCat
}

// scanKIRefs looks for a ki/ or knowledge/ subdirectory inside the skill dir
// and returns all .md artifact paths found there.
func scanKIRefs(skillDir string) []string {
	var refs []string
	for _, subdir := range []string{"ki", "knowledge", "artifacts"} {
		kiDir := filepath.Join(skillDir, subdir)
		entries, err := os.ReadDir(kiDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				refs = append(refs, filepath.Join(kiDir, e.Name()))
			}
		}
	}
	return refs
}

// ListByCategory returns all skills in a given category.
func (m *Manager) ListByCategory(category string) []*entity.Skill {
	var result []*entity.Skill
	for _, s := range m.skills {
		if s.Category == category {
			result = append(result, s)
		}
	}
	return result
}

// Get returns a skill by name.
func (m *Manager) Get(name string) (*entity.Skill, bool) {
	s, ok := m.skills[name]
	return s, ok
}

// Delete removes a skill by name (from memory and disk).
func (m *Manager) Delete(name string) error {
	s, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill not found: %s", name)
	}
	if err := os.RemoveAll(s.Path); err != nil {
		return fmt.Errorf("delete skill dir: %w", err)
	}
	delete(m.skills, name)
	return nil
}

// List returns all discovered skills.
func (m *Manager) List() []*entity.Skill {
	result := make([]*entity.Skill, 0, len(m.skills))
	for _, s := range m.skills {
		result = append(result, s)
	}
	return result
}

// ListSummary returns a string summary suitable for prompt injection.
func (m *Manager) ListSummary() string {
	skills := m.List()
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range skills {
		b.WriteString(fmt.Sprintf("- %s (%s): %s\n", s.Name, s.Path, s.Description))
	}
	return b.String()
}

// --- Forge Lifecycle ---

// SetEvoStatus updates the forge status of a skill.
// Valid transitions: draft→forging→forged, forged→degraded, degraded→reforging→forged
func (m *Manager) SetEvoStatus(name, status string) error {
	s, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill not found: %s", name)
	}

	valid := map[string][]string{
		"draft":       {"evolving"},
		"evolving":    {"evolved", "draft"},
		"evolved":     {"degraded"},
		"degraded":    {"re-evolving"},
		"re-evolving": {"evolved", "degraded"},
	}

	allowed, ok := valid[s.EvoStatus]
	if !ok {
		return fmt.Errorf("unknown current status: %s", s.EvoStatus)
	}

	for _, a := range allowed {
		if a == status {
			s.EvoStatus = status
			return nil
		}
	}
	return fmt.Errorf("invalid transition: %s → %s", s.EvoStatus, status)
}

// RecordEvoRun stores a forge execution result.
func (m *Manager) RecordEvoRun(name string, run entity.EvoRun) error {
	s, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill not found: %s", name)
	}

	// Write to skill directory
	logPath := filepath.Join(s.Path, "forge_history.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	status := "SUCCESS"
	if !run.Success {
		status = "FAIL: " + run.FailureReason
	}
	fmt.Fprintf(f, "[%s] %s retries=%d strategy=%s\n",
		run.Timestamp.Format(time.RFC3339), status, run.Retries, run.Strategy)
	return nil
}

// parseSkillCommand extracts the first ```bash code block from the SKILL.md body
// as the quick-run command. This lets the prompt inject a ready-to-use command
// so the agent doesn't need to read SKILL.md just to find the execution path.
func parseSkillCommand(content string) string {
	lines := strings.Split(content, "\n")
	inBlock := false
	var cmd strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock && (trimmed == "```bash" || trimmed == "```shell") {
			inBlock = true
			continue
		}
		if inBlock {
			if trimmed == "```" {
				break
			}
			if cmd.Len() > 0 {
				cmd.WriteString(" ")
			}
			cmd.WriteString(strings.TrimSpace(line))
		}
	}
	return cmd.String()
}

// parseTriggers extracts trigger words from the full file content.
// Looks for "触发词：" or "triggers:" line and splits by Chinese/English comma.
func parseTriggers(content string) []string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		var triggerPart string
		if strings.HasPrefix(trimmed, "触发词：") || strings.HasPrefix(trimmed, "触发词:") {
			triggerPart = strings.TrimPrefix(trimmed, "触发词：")
			triggerPart = strings.TrimPrefix(triggerPart, "触发词:")
		} else if strings.HasPrefix(strings.ToLower(trimmed), "triggers:") {
			triggerPart = trimmed[len("triggers:"):]
		} else {
			continue
		}
		var triggers []string
		for _, sep := range []string{"、", "，", ","} {
			triggerPart = strings.ReplaceAll(triggerPart, sep, "|")
		}
		for _, t := range strings.Split(triggerPart, "|") {
			t = strings.TrimSpace(t)
			if t != "" && t != "。" {
				triggers = append(triggers, strings.ToLower(t))
			}
		}
		return triggers
	}
	return nil
}

// MatchTriggers checks user message against all skill triggers.
// Returns matched heavy skills with their quick usage hint.
func (m *Manager) MatchTriggers(userMsg string) []SkillTriggerMatch {
	lowerMsg := strings.ToLower(userMsg)
	var matches []SkillTriggerMatch
	seen := make(map[string]bool)
	for _, s := range m.skills {
		if s.Weight != "heavy" || !s.Enabled {
			continue
		}
		for _, trigger := range s.Triggers {
			if strings.Contains(lowerMsg, trigger) && !seen[s.Name] {
				seen[s.Name] = true
				matches = append(matches, SkillTriggerMatch{
					Skill:   s,
					Trigger: trigger,
				})
				break
			}
		}
	}
	return matches
}

// SkillTriggerMatch represents a skill matched by trigger word.
type SkillTriggerMatch struct {
	Skill   *entity.Skill
	Trigger string
}

// HasCommand checks if any skill defines the given slash command name.
func (m *Manager) HasCommand(name string) bool {
	for _, s := range m.skills {
		if s.Name == name || strings.HasPrefix(s.Name, name) {
			return true
		}
	}
	return false
}

// AutoPromote returns all skills for tool registry registration.
// Each skill's Type determines which adapter to use:
//   - "executable"/"hybrid": ScriptTool (runs run.sh/run.py)
//   - "workflow": SkillGuideTool (returns SKILL.md content)
func (m *Manager) AutoPromote() []*entity.Skill {
	result := make([]*entity.Skill, 0, len(m.skills))
	for _, s := range m.skills {
		result = append(result, s)
	}
	return result
}

// ListUnforged returns skills in draft status.
func (m *Manager) ListUnforged() []*entity.Skill {
	var result []*entity.Skill
	for _, s := range m.skills {
		if s.EvoStatus == "draft" {
			result = append(result, s)
		}
	}
	return result
}

// ListDegraded returns skills in degraded status.
func (m *Manager) ListDegraded() []*entity.Skill {
	var result []*entity.Skill
	for _, s := range m.skills {
		if s.EvoStatus == "degraded" {
			result = append(result, s)
		}
	}
	return result
}

// StartWatcher starts an fsnotify watcher on all skill directories.
// When a SKILL.md is created/modified, skills are re-discovered.
func (m *Manager) StartWatcher(stopCh <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				m.Discover() // Re-scan periodically as fallback
			}
		}
	}()
}
