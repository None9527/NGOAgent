package forge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ForgeRun records one forge execution.
type ForgeRun struct {
	At         time.Time     `json:"at"`
	Passed     bool          `json:"passed"`
	Retries    int           `json:"retries"`
	FailReason string        `json:"fail_reason,omitempty"`
	DepsAdded  []string      `json:"deps_added,omitempty"`
	Duration   time.Duration `json:"duration"`
}

// Tracker persists forge history to ~/.ngoagent/forge/.
type Tracker struct {
	dir string // ~/.ngoagent/forge/
}

// NewTracker creates a forge history tracker.
func NewTracker(forgeDir string) *Tracker {
	os.MkdirAll(forgeDir, 0755)
	return &Tracker{dir: forgeDir}
}

// RecordRun appends a forge run to the skill's history file.
func (t *Tracker) RecordRun(skillName string, run ForgeRun) error {
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
		return fmt.Errorf("marshal forge history: %w", err)
	}
	return os.WriteFile(historyPath, data, 0644)
}

// GetHistory returns all forge runs for a skill.
func (t *Tracker) GetHistory(skillName string) ([]ForgeRun, error) {
	data, err := os.ReadFile(t.historyPath(skillName))
	if err != nil {
		return nil, nil // No history yet
	}
	var history []ForgeRun
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("parse forge history: %w", err)
	}
	return history, nil
}

// GetSuccessRate calculates the success rate from history.
func (t *Tracker) GetSuccessRate(skillName string) (float64, error) {
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

func (t *Tracker) historyPath(skillName string) string {
	return filepath.Join(t.dir, skillName, "history.json")
}
