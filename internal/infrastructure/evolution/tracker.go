package evolution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EvoRun records one evolution execution (repair or quality iteration).
type EvoRun struct {
	At         time.Time     `json:"at"`
	Passed     bool          `json:"passed"`
	Retries    int           `json:"retries"`
	Strategy   string        `json:"strategy,omitempty"` // param_fix | tool_swap | re_route | iterate | escalate
	FailReason string        `json:"fail_reason,omitempty"`
	Duration   time.Duration `json:"duration"`
}

// RunTracker persists evolution history to disk.
type RunTracker struct {
	dir string
}

// NewRunTracker creates an evolution history tracker.
func NewRunTracker(evoDir string) *RunTracker {
	os.MkdirAll(evoDir, 0755)
	return &RunTracker{dir: evoDir}
}

// RecordRun appends an evolution run to the skill's history file.
func (t *RunTracker) RecordRun(skillName string, run EvoRun) error {
	historyPath := t.historyPath(skillName)
	os.MkdirAll(filepath.Dir(historyPath), 0755)

	history, _ := t.GetHistory(skillName)
	history = append(history, run)

	// Cap history at 20 entries per skill
	if len(history) > 20 {
		history = history[len(history)-20:]
	}

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal evo history: %w", err)
	}
	return os.WriteFile(historyPath, data, 0644)
}

// GetHistory returns all evolution runs for a skill.
func (t *RunTracker) GetHistory(skillName string) ([]EvoRun, error) {
	data, err := os.ReadFile(t.historyPath(skillName))
	if err != nil {
		return nil, nil // No history yet
	}
	var history []EvoRun
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("parse evo history: %w", err)
	}
	return history, nil
}

// GetSuccessRate calculates the success rate from history.
func (t *RunTracker) GetSuccessRate(skillName string) (float64, error) {
	history, err := t.GetHistory(skillName)
	if err != nil || len(history) == 0 {
		return 0, err
	}
	passed := 0
	for _, run := range history {
		if run.Passed {
			passed++
		}
	}
	return float64(passed) / float64(len(history)), nil
}

func (t *RunTracker) historyPath(skillName string) string {
	return filepath.Join(t.dir, skillName, "history.json")
}
