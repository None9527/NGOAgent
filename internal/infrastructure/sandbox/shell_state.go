// Package sandbox — ShellState manages persistent shell context across commands.
// Tracks cwd and env snapshot so each new bash -lc process inherits
// the accumulated state from previous commands (like CC's approach).
package sandbox

import (
	"os"
	"os/exec"
	"strings"
	"sync"
)

// cwdMarker is the sentinel injected into command output to extract
// the post-execution working directory.
const cwdMarker = "__NGOAGENT_CWD_MARKER__"

// ShellState tracks persistent shell context across independent bash processes.
type ShellState struct {
	mu          sync.RWMutex
	cwd         string            // Current working directory (persists between commands)
	envSnapshot []string          // Base env captured at startup (os.Environ() format)
	userEnv     map[string]string // Extra vars the user exported during the session
}

// NewShellState creates a ShellState with the given initial working directory.
// Calls CaptureSnapshot automatically to grab the current process env.
func NewShellState(initialCwd string) *ShellState {
	if initialCwd == "" {
		initialCwd, _ = os.Getwd()
	}
	s := &ShellState{
		cwd:     initialCwd,
		userEnv: make(map[string]string),
	}
	s.CaptureSnapshot()
	return s
}

// CaptureSnapshot records the current process environment as the baseline.
// This is called once at startup and provides the env vars for all future
// bash processes, removing the need for `bash -l` (login shell) on every command.
func (s *ShellState) CaptureSnapshot() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.envSnapshot = os.Environ()
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

// WrapCommand appends a cwd-tracking suffix to the command so we can
// extract the actual post-execution directory from stdout.
// Returns the wrapped command string.
func (s *ShellState) WrapCommand(command string) string {
	return command + " ; echo ''" + ` ; echo "` + cwdMarker + `" ; pwd`
}

// ExtractCwdFromOutput parses the command output to find the __CWD_MARKER__
// sentinel and extracts the pwd that follows it. It updates the internal cwd
// and returns the cleaned output (with marker + pwd lines removed).
func (s *ShellState) ExtractCwdFromOutput(output string) (cleanOutput string, newCwd string) {
	idx := strings.LastIndex(output, cwdMarker)
	if idx < 0 {
		return output, ""
	}

	// Everything before the empty line + marker is the real command output
	cleanPart := output[:idx]
	// Remove the trailing empty line we injected
	cleanPart = strings.TrimRight(cleanPart, "\n")

	// Everything after the marker is the pwd
	afterMarker := output[idx+len(cwdMarker):]
	lines := strings.Split(strings.TrimSpace(afterMarker), "\n")
	if len(lines) > 0 {
		newCwd = strings.TrimSpace(lines[0])
		if newCwd != "" {
			s.SetCwd(newCwd)
		}
	}

	return cleanPart, newCwd
}

// BuildEnv constructs the full environment for a new bash process by merging
// the startup snapshot with any user-set variables.
func (s *ShellState) BuildEnv() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Start with the snapshot
	env := make([]string, len(s.envSnapshot))
	copy(env, s.envSnapshot)

	// Overlay user vars (overwrite any matching keys in snapshot)
	for key, val := range s.userEnv {
		found := false
		prefix := key + "="
		for i, e := range env {
			if strings.HasPrefix(e, prefix) {
				env[i] = prefix + val
				found = true
				break
			}
		}
		if !found {
			env = append(env, prefix+val)
		}
	}

	return env
}

// InjectEnv sets the Cmd's environment and working directory from the
// persistent shell state. If explicitCwd is non-empty, it takes priority
// over the persisted cwd.
func (s *ShellState) InjectEnv(cmd *exec.Cmd, explicitCwd string) {
	cmd.Env = s.BuildEnv()
	if explicitCwd != "" {
		cmd.Dir = explicitCwd
	} else {
		cmd.Dir = s.Cwd()
	}
}
