// Package skill provides skill discovery, loading, and forge lifecycle management.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/entity"
)

// detectSkillType checks skill directory contents to determine type.
//   - "executable": has run.sh or run.py
//   - "workflow": SKILL.md only (guide for LLM)
func detectSkillType(skillDir string) string {
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

			name, desc := parseSkillHeader(string(content))
			if name == "" {
				name = entry.Name()
			}

			cmd := parseSkillCommand(string(content))
			skillDir := filepath.Join(dir, entry.Name())
			skillType := detectSkillType(skillDir)
			triggers := parseTriggers(string(content)) // Use full content, not `desc` (YAML `|` multiline breaks parseSkillHeader)
			weight := parseSkillWeight(string(content))
			// Auto-detect weight: has triggers → heavy, otherwise → light
			if weight == "" {
				if len(triggers) > 0 {
					weight = "heavy"
				} else {
					weight = "light"
				}
			}
			m.skills[name] = &entity.Skill{
				ID:          name,
				Name:        name,
				Description: desc,
				Type:        skillType,
				Weight:      weight,
				Triggers:    triggers,
				Command:     cmd,
				Path:        filepath.Join(dir, entry.Name()),
				Content:     string(content),
				Enabled:     true,
				ForgeStatus: "draft",
				InstalledAt: time.Now(),
			}
		}
	}
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

// SetForgeStatus updates the forge status of a skill.
// Valid transitions: draft→forging→forged, forged→degraded, degraded→reforging→forged
func (m *Manager) SetForgeStatus(name, status string) error {
	s, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill not found: %s", name)
	}

	valid := map[string][]string{
		"draft":     {"forging"},
		"forging":   {"forged", "draft"},
		"forged":    {"degraded"},
		"degraded":  {"reforging"},
		"reforging": {"forged", "degraded"},
	}

	allowed, ok := valid[s.ForgeStatus]
	if !ok {
		return fmt.Errorf("unknown current status: %s", s.ForgeStatus)
	}

	for _, a := range allowed {
		if a == status {
			s.ForgeStatus = status
			return nil
		}
	}
	return fmt.Errorf("invalid transition: %s → %s", s.ForgeStatus, status)
}

// RecordForgeRun stores a forge execution result.
func (m *Manager) RecordForgeRun(name string, run entity.ForgeRun) error {
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
	fmt.Fprintf(f, "[%s] %s retries=%d deps=%v\n",
		run.Timestamp.Format(time.RFC3339), status, run.Retries, run.DepsAdded)
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

// parseSkillHeader extracts name and description from SKILL.md YAML frontmatter.
func parseSkillHeader(content string) (name, desc string) {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if inFrontmatter {
				break
			}
			inFrontmatter = true
			continue
		}
		if !inFrontmatter {
			continue
		}
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			name = strings.Trim(name, "\"'")
		}
		if strings.HasPrefix(line, "description:") {
			desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			desc = strings.Trim(desc, "\"'")
		}
	}
	return
}

// parseTriggers extracts trigger words from the description.
// Looks for "触发词：" or "triggers:" line and splits by Chinese/English comma.
func parseTriggers(desc string) []string {
	for _, line := range strings.Split(desc, "\n") {
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
		// Split by Chinese comma, English comma, or 、
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

// parseSkillWeight extracts weight from SKILL.md frontmatter.
// Returns "light", "heavy", or "" for auto-detect.
func parseSkillWeight(content string) string {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if inFrontmatter {
				break
			}
			inFrontmatter = true
			continue
		}
		if !inFrontmatter {
			continue
		}
		if strings.HasPrefix(line, "weight:") {
			w := strings.TrimSpace(strings.TrimPrefix(line, "weight:"))
			w = strings.Trim(w, "\"'")
			if w == "light" || w == "heavy" {
				return w
			}
		}
	}
	return ""
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
		if s.ForgeStatus == "draft" {
			result = append(result, s)
		}
	}
	return result
}

// ListDegraded returns skills in degraded status.
func (m *Manager) ListDegraded() []*entity.Skill {
	var result []*entity.Skill
	for _, s := range m.skills {
		if s.ForgeStatus == "degraded" {
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
