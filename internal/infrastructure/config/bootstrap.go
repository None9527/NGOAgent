package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BootstrapPhase represents the initialization state.
type BootstrapPhase string

const (
	PhaseNew   BootstrapPhase = "new"   // Just installed
	PhaseReady BootstrapPhase = "ready" // Initialized
)

type bootstrapState struct {
	Phase BootstrapPhase `json:"phase"`
}

// Bootstrap creates the directory structure and default files if they don't exist.
// Called on first startup.
func Bootstrap() error {
	home := HomeDir()

	// Create directory structure
	dirs := []string{
		home,
		filepath.Join(home, "data"),
		filepath.Join(home, "brain"),
		filepath.Join(home, "knowledge"),
		filepath.Join(home, "skills"),
		filepath.Join(home, "forge"),
		filepath.Join(home, "logs"),
		filepath.Join(home, "prompts"),
		filepath.Join(home, "prompts", "variants"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Write default files (only if missing)
	defaults := map[string]string{
		filepath.Join(home, "config.yaml"):   DefaultConfigYAML,
		filepath.Join(home, "user_rules.md"): DefaultUserRules,
	}

	for path, content := range defaults {
		if err := writeIfMissing(path, content); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}

	// Write bootstrap state
	statePath := filepath.Join(home, ".state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		state := bootstrapState{Phase: PhaseNew}
		data, _ := json.MarshalIndent(state, "", "  ")
		if err := os.WriteFile(statePath, data, 0644); err != nil {
			return fmt.Errorf("write state: %w", err)
		}
	}

	return nil
}

// MarkReady transitions the bootstrap state from "new" to "ready".
// Called after the first conversation completes.
func MarkReady() error {
	statePath := filepath.Join(HomeDir(), ".state.json")
	state := bootstrapState{Phase: PhaseReady}
	data, _ := json.MarshalIndent(state, "", "  ")
	return os.WriteFile(statePath, data, 0644)
}

// IsBootstrapped returns true if the system has been initialized.
func IsBootstrapped() bool {
	statePath := filepath.Join(HomeDir(), ".state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return false
	}
	var state bootstrapState
	if err := json.Unmarshal(data, &state); err != nil {
		return false
	}
	return state.Phase == PhaseReady
}

func writeIfMissing(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // File exists, don't overwrite
	}
	return os.WriteFile(path, []byte(content), 0644)
}
