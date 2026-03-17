// Package sandbox — ShellState manages persistent shell context across commands.
// Tracks cwd and env diff so each new bash process inherits
// the accumulated state from previous commands.
package sandbox

import (
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Markers injected into command output for state extraction.
const (
	cwdMarker = "__NGOAGENT_CWD_MARKER__"
	envMarker = "__NGOAGENT_ENV_MARKER__"
	envEnd    = "__NGOAGENT_ENV_END__"
)

// envBlacklist contains env vars that should never be captured from command output.
// These are transient, session-specific, or security-sensitive.
var envBlacklist = map[string]bool{
	"_":            true, // last command
	"SHLVL":        true, // shell nesting level
	"OLDPWD":       true, // tracked via cwd
	"PWD":          true, // tracked via cwd
	"BASH_FUNC_%":  true, // shell functions (prefix match handled separately)
	"BASHOPTS":     true,
	"BASH_VERSINFO": true,
	"BASH_VERSION": true,
	"SHELLOPTS":    true,
	"RANDOM":       true,
	"LINENO":       true,
	"SECONDS":      true,
	"EPOCHSECONDS": true,
	"EPOCHREALTIME": true,
	"BASHPID":      true,
	"PPID":         true,
}

// ShellState tracks persistent shell context across independent bash processes.
type ShellState struct {
	mu          sync.RWMutex
	cwd         string            // Current working directory (persists between commands)
	envSnapshot map[string]string // Base env captured at startup (key→value)
	userEnv     map[string]string // Extra vars the user exported during the session
}

// NewShellState creates a ShellState with the given initial working directory.
func NewShellState(initialCwd string) *ShellState {
	if initialCwd == "" {
		initialCwd, _ = os.Getwd()
	}
	s := &ShellState{
		cwd:         initialCwd,
		envSnapshot: make(map[string]string),
		userEnv:     make(map[string]string),
	}
	s.CaptureSnapshot()
	return s
}

// CaptureSnapshot records the current process environment as the baseline.
func (s *ShellState) CaptureSnapshot() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.envSnapshot = make(map[string]string)
	for _, e := range os.Environ() {
		k, v := splitEnvVar(e)
		if k != "" {
			s.envSnapshot[k] = v
		}
	}
}

// Cwd returns the current persistent working directory.
func (s *ShellState) Cwd() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cwd
}

// SetCwd updates the persistent working directory.
func (s *ShellState) SetCwd(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cwd = path
}

// SetEnv sets a user-level environment variable that persists across commands.
func (s *ShellState) SetEnv(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userEnv[key] = value
}

// WrapCommand appends state-tracking suffixes to capture post-execution cwd and env.
func (s *ShellState) WrapCommand(command string) string {
	// Chain: command → empty line → cwd marker → pwd → env marker → env → env end
	return command +
		` ; echo ""` +
		` ; echo "` + cwdMarker + `"` +
		` ; pwd` +
		` ; echo "` + envMarker + `"` +
		` ; env` +
		` ; echo "` + envEnd + `"`
}

// ExtractStateFromOutput parses the command output to extract both cwd and env
// changes. Updates internal state and returns cleaned output.
func (s *ShellState) ExtractStateFromOutput(output string) (cleanOutput string, newCwd string) {
	// Find cwd marker first — everything before it is user output
	cwdIdx := strings.LastIndex(output, cwdMarker)
	if cwdIdx < 0 {
		return output, ""
	}

	// Clean user output (everything before the marker block)
	cleanPart := output[:cwdIdx]
	cleanPart = strings.TrimRight(cleanPart, "\n")

	// Parse the trailer: CWD_MARKER\npwd\nENV_MARKER\nenv...\nENV_END
	trailer := output[cwdIdx:]

	// Extract cwd (between CWD_MARKER and ENV_MARKER)
	envIdx := strings.Index(trailer, envMarker)
	if envIdx < 0 {
		// Fallback: only cwd tracking (no env section)
		afterCwd := trailer[len(cwdMarker):]
		lines := strings.Split(strings.TrimSpace(afterCwd), "\n")
		if len(lines) > 0 {
			newCwd = strings.TrimSpace(lines[0])
			if newCwd != "" {
				s.SetCwd(newCwd)
			}
		}
		return cleanPart, newCwd
	}

	// Extract pwd between CWD_MARKER and ENV_MARKER
	cwdSection := trailer[len(cwdMarker):envIdx]
	cwdLines := strings.Split(strings.TrimSpace(cwdSection), "\n")
	if len(cwdLines) > 0 {
		newCwd = strings.TrimSpace(cwdLines[0])
		if newCwd != "" {
			s.SetCwd(newCwd)
		}
	}

	// Extract env between ENV_MARKER and ENV_END
	envSection := trailer[envIdx+len(envMarker):]
	envEndIdx := strings.LastIndex(envSection, envEnd)
	if envEndIdx > 0 {
		envSection = envSection[:envEndIdx]
	}

	// Parse env diff
	s.applyEnvDiff(envSection)

	return cleanPart, newCwd
}

// ExtractCwdFromOutput is the legacy API — now delegates to ExtractStateFromOutput.
func (s *ShellState) ExtractCwdFromOutput(output string) (cleanOutput string, newCwd string) {
	return s.ExtractStateFromOutput(output)
}

// applyEnvDiff compares command env output against baseline snapshot,
// and persists any new or changed variables into userEnv.
func (s *ShellState) applyEnvDiff(envOutput string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Parse env output into key→value map
	// Handle multi-line values (value continues until next KEY= pattern)
	currentEnv := parseEnvOutput(envOutput)

	// Diff against snapshot: only store vars that are new or changed
	for key, newVal := range currentEnv {
		if isBlacklisted(key) {
			continue
		}
		origVal, existed := s.envSnapshot[key]
		if !existed || origVal != newVal {
			s.userEnv[key] = newVal
		}
	}
}

// parseEnvOutput parses `env` command output into a key→value map.
// Handles multi-line values correctly (values containing newlines).
func parseEnvOutput(output string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(output, "\n")

	var currentKey string
	var currentVal strings.Builder

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			if currentKey != "" {
				currentVal.WriteString("\n")
			}
			continue
		}

		// Check if this line starts a new KEY=value pair
		eqIdx := strings.Index(line, "=")
		if eqIdx > 0 && isValidEnvKey(line[:eqIdx]) {
			// Save previous key
			if currentKey != "" {
				result[currentKey] = currentVal.String()
			}
			currentKey = line[:eqIdx]
			currentVal.Reset()
			currentVal.WriteString(line[eqIdx+1:])
		} else if currentKey != "" {
			// Continuation of multi-line value
			currentVal.WriteString("\n")
			currentVal.WriteString(line)
		}
	}

	// Save last key
	if currentKey != "" {
		result[currentKey] = strings.TrimRight(currentVal.String(), "\n")
	}

	return result
}

// isValidEnvKey checks if a string is a valid environment variable name.
func isValidEnvKey(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if i == 0 && c >= '0' && c <= '9' {
			return false
		}
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// isBlacklisted checks if an env var key should be ignored.
func isBlacklisted(key string) bool {
	if envBlacklist[key] {
		return true
	}
	// Prefix match for BASH_FUNC_*
	if strings.HasPrefix(key, "BASH_FUNC_") {
		return true
	}
	return false
}

// BuildEnv constructs the full environment for a new bash process by merging
// the startup snapshot with any user-set variables.
func (s *ShellState) BuildEnv() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build merged map: snapshot + userEnv overlay
	merged := make(map[string]string, len(s.envSnapshot)+len(s.userEnv))
	for k, v := range s.envSnapshot {
		merged[k] = v
	}
	for k, v := range s.userEnv {
		merged[k] = v
	}

	// Convert to []string
	env := make([]string, 0, len(merged))
	for k, v := range merged {
		env = append(env, k+"="+v)
	}
	return env
}

// InjectEnv sets the Cmd's environment and working directory from the
// persistent shell state. If explicitCwd is non-empty, it takes priority.
func (s *ShellState) InjectEnv(cmd *exec.Cmd, explicitCwd string) {
	cmd.Env = s.BuildEnv()
	if explicitCwd != "" {
		cmd.Dir = explicitCwd
	} else {
		cmd.Dir = s.Cwd()
	}
}

// splitEnvVar splits "KEY=VALUE" into key and value.
func splitEnvVar(entry string) (string, string) {
	idx := strings.Index(entry, "=")
	if idx < 0 {
		return entry, ""
	}
	return entry[:idx], entry[idx+1:]
}
