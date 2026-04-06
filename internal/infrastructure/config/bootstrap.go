package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// GenerateAuthToken creates a cryptographically secure token:
// 32 random bytes → SHA-256 → 64-char hex string.
func GenerateAuthToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		// Fallback: use crypto/rand is virtually never failing,
		// but if it does, panic is acceptable for a security-critical path.
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// Bootstrap creates the directory structure and default files if they don't exist.
// Called on first startup. Auto-generates auth token if config.yaml is new.
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
		filepath.Join(home, "mcp"),
		filepath.Join(home, "workspace"),
		filepath.Join(home, "cron"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Auto-generate auth token and inject into default config
	configPath := filepath.Join(home, "config.yaml")
	configIsNew := false
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configIsNew = true
		token := GenerateAuthToken()
		configContent := strings.Replace(DefaultConfigYAML,
			`auth_token: ""`, fmt.Sprintf(`auth_token: "%s"`, token), 1)
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			return fmt.Errorf("write %s: %w", configPath, err)
		}
		slog.Info("╔══════════════════════════════════════════════════════════════╗")
		slog.Info("║  AUTH TOKEN GENERATED (save this for frontend connection):   ║")
		slog.Info(fmt.Sprintf("║  %s  ║", token))
		slog.Info("╚══════════════════════════════════════════════════════════════╝")
	}

	// Write other default files (only if missing)
	defaults := map[string]string{
		filepath.Join(home, "user_rules.md"): DefaultUserRules,
		filepath.Join(home, "mcp.json"):      DefaultMCPJSON,
	}
	// config.yaml is handled above with token injection
	if !configIsNew {
		// If config.yaml already exists, don't touch it
		_ = configIsNew
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
