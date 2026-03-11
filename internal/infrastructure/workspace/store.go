// Package workspace provides project-level knowledge and heartbeat state storage.
package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store manages the .ngoagent/ directory within a project workspace.
type Store struct {
	workDir  string // Project root
	agentDir string // .ngoagent/
}

// NewStore creates a workspace store for a project directory.
func NewStore(workDir string) *Store {
	agentDir := filepath.Join(workDir, ".ngoagent")
	return &Store{
		workDir:  workDir,
		agentDir: agentDir,
	}
}

// Init creates the .ngoagent/ project structure if it doesn't exist.
func (s *Store) Init() error {
	dirs := []string{
		s.agentDir,
		filepath.Join(s.agentDir, "skills"),
		filepath.Join(s.agentDir, "workflows"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

// ReadContext reads .ngoagent/context.md
func (s *Store) ReadContext() string {
	data, err := os.ReadFile(filepath.Join(s.agentDir, "context.md"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// WriteContext writes to .ngoagent/context.md
func (s *Store) WriteContext(content string) error {
	os.MkdirAll(s.agentDir, 0755)
	return os.WriteFile(filepath.Join(s.agentDir, "context.md"), []byte(content), 0644)
}

// ReadHeartbeat reads the heartbeat task list.
// Project-level heartbeat overrides global if present.
func (s *Store) ReadHeartbeat(globalHeartbeatPath string) string {
	// Try project first
	projectPath := filepath.Join(s.agentDir, "heartbeat.md")
	if data, err := os.ReadFile(projectPath); err == nil {
		return strings.TrimSpace(string(data))
	}
	// Fall back to global
	if data, err := os.ReadFile(globalHeartbeatPath); err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

// SkillsDir returns the project-level skills directory.
func (s *Store) SkillsDir() string {
	return filepath.Join(s.agentDir, "skills")
}

// WorkflowsDir returns the project-level workflows directory.
func (s *Store) WorkflowsDir() string {
	return filepath.Join(s.agentDir, "workflows")
}

// WorkDir returns the project root.
func (s *Store) WorkDir() string { return s.workDir }

// Exists checks if .ngoagent/ exists in the project.
func (s *Store) Exists() bool {
	_, err := os.Stat(s.agentDir)
	return err == nil
}

// HeartbeatState tracks heartbeat execution status.
type HeartbeatState struct {
	LastRun    string `json:"last_run"`
	NextRun    string `json:"next_run"`
	TasksDone  int    `json:"tasks_done"`
	TasksTotal int    `json:"tasks_total"`
	Status     string `json:"status"` // idle / running / error
}

// ReadHeartbeatState loads heartbeat-state.json.
func (s *Store) ReadHeartbeatState() *HeartbeatState {
	data, err := os.ReadFile(filepath.Join(s.agentDir, "heartbeat-state.json"))
	if err != nil {
		return &HeartbeatState{Status: "idle"}
	}
	var state HeartbeatState
	if err := json.Unmarshal(data, &state); err != nil {
		return &HeartbeatState{Status: "idle"}
	}
	return &state
}

// WriteHeartbeatState saves heartbeat-state.json.
func (s *Store) WriteHeartbeatState(state *HeartbeatState) error {
	os.MkdirAll(s.agentDir, 0755)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.agentDir, "heartbeat-state.json"), data, 0644)
}

// AppendContext appends to context.md with dedup and 5KB max.
func (s *Store) AppendContext(entry string) error {
	current := s.ReadContext()

	// Dedup: skip if entry already exists (similarity >0.9 → exact match for now)
	if strings.Contains(current, strings.TrimSpace(entry)) {
		return nil // Already exists
	}

	newContent := current
	if newContent != "" {
		newContent += "\n"
	}
	newContent += strings.TrimSpace(entry)

	// 5KB max — truncate from the beginning
	const maxSize = 5 * 1024
	if len(newContent) > maxSize {
		// Keep the tail
		newContent = newContent[len(newContent)-maxSize:]
		// Trim to nearest newline
		if idx := strings.Index(newContent, "\n"); idx >= 0 {
			newContent = newContent[idx+1:]
		}
	}

	return s.WriteContext(newContent)
}

// Analyze generates initial context.md by scanning the project structure.
func (s *Store) Analyze() string {
	var b strings.Builder
	b.WriteString("# Project: " + filepath.Base(s.workDir) + "\n\n")

	// Detect language by file extensions
	langs := map[string]int{}
	filepath.Walk(s.workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			// Skip .git, node_modules, vendor
			if info != nil && info.IsDir() {
				name := info.Name()
				if name == ".git" || name == "node_modules" || name == "vendor" || name == "__pycache__" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		ext := filepath.Ext(info.Name())
		if ext != "" {
			langs[ext]++
		}
		return nil
	})

	b.WriteString("## Languages\n")
	for ext, count := range langs {
		if count >= 3 {
			b.WriteString(fmt.Sprintf("- %s: %d files\n", ext, count))
		}
	}

	// Check for common config files
	markers := map[string]string{
		"go.mod":         "Go module",
		"package.json":   "Node.js project",
		"Cargo.toml":     "Rust project",
		"pom.xml":        "Java/Maven project",
		"pyproject.toml": "Python project",
		"Makefile":       "Makefile present",
		"Dockerfile":     "Docker containerized",
	}

	b.WriteString("\n## Project Markers\n")
	for file, label := range markers {
		if _, err := os.Stat(filepath.Join(s.workDir, file)); err == nil {
			b.WriteString("- " + label + "\n")
		}
	}

	return b.String()
}
