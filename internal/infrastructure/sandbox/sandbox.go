// Package sandbox provides process isolation for command execution.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Result is the output of a sandboxed command execution.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
	Duration time.Duration
}

// Process tracks a running background command.
type Process struct {
	ID             string
	Cmd            *exec.Cmd
	Stdout         *bytes.Buffer
	Stderr         *bytes.Buffer
	StdinPipe      io.WriteCloser
	Done           chan struct{}
	ExitCode       int
	StartedAt      time.Time
	cancel         context.CancelFunc
	lastReadOffset int // For incremental output
}

// Manager manages sandboxed process execution.
type Manager struct {
	mu        sync.RWMutex
	processes map[string]*Process
	maxOutput int // Max bytes per output buffer
	State     *ShellState
}

// NewManager creates a sandbox process manager with persistent shell state.
// initialCwd sets the starting working directory; if empty, os.Getwd() is used.
func NewManager(initialCwd string) *Manager {
	return &Manager{
		processes: make(map[string]*Process),
		maxOutput: 50 * 1024, // 50KB default
		State:     NewShellState(initialCwd),
	}
}

// Run executes a command synchronously within a timeout.
// If cwd is empty, the persistent shell state cwd is used.
// The command output is automatically parsed to track cwd changes.
func (m *Manager) Run(ctx context.Context, command, cwd string, timeout time.Duration) (*Result, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Wrap command to capture post-execution cwd
	wrapped := m.State.WrapCommand(command)
	cmd := exec.CommandContext(execCtx, "bash", "-c", wrapped)

	// L1 process group isolation: all child processes in a new pgid.
	// On context cancel, Kill entire group so forked children don't
	// hold stdout pipe fds open (which hangs cmd.Wait/awaitGoroutines).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			slog.Info(fmt.Sprintf("[sandbox] killing process group pgid=%d", cmd.Process.Pid))
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second // force-close pipes if children linger

	// Inject persistent env and cwd
	m.State.InjectEnv(cmd, cwd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	rawStdout := stdout.String()

	// Extract and update cwd from the marker in output
	cleanStdout, _ := m.State.ExtractCwdFromOutput(rawStdout)

	result := &Result{
		Stdout:   truncateOutput(cleanStdout, m.maxOutput),
		Stderr:   truncateOutput(stderr.String(), m.maxOutput),
		Duration: dur,
		TimedOut: execCtx.Err() == context.DeadlineExceeded,
	}

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	} else if err != nil {
		result.ExitCode = 1
	}

	return result, nil
}

// RunBackground starts a command in the background and returns its process ID.
// Uses persistent shell state for cwd and env.
func (m *Manager) RunBackground(ctx context.Context, id, command, cwd string) error {
	bgCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(bgCtx, "bash", "-c", command)

	// L1 process group isolation (same as Run)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second

	// Inject persistent env and cwd
	m.State.InjectEnv(cmd, cwd)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Create stdin pipe for interactive input
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("create stdin pipe: %w", err)
	}

	proc := &Process{
		ID:        id,
		Cmd:       cmd,
		Stdout:    stdout,
		Stderr:    stderr,
		StdinPipe: stdinPipe,
		Done:      make(chan struct{}),
		StartedAt: time.Now(),
		cancel:    cancel,
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start command: %w", err)
	}

	m.mu.Lock()
	m.processes[id] = proc
	m.mu.Unlock()

	// Wait in goroutine
	go func() {
		err := cmd.Wait()
		if err != nil {
			if cmd.ProcessState != nil {
				proc.ExitCode = cmd.ProcessState.ExitCode()
			} else {
				proc.ExitCode = 1
			}
		}
		close(proc.Done)
	}()

	return nil
}

// RunDetached launches a command fully detached from the agent process tree.
// The command runs in a new session (setsid), does not inherit pipes, and
// is NOT tracked by the Manager. It survives agent restart/shutdown.
// Use this for persistent services (web servers, daemons) that should outlive the agent.
func (m *Manager) RunDetached(command, cwd string) (int, error) {
	if cwd == "" {
		cwd = m.State.Cwd()
	}

	// setsid creates a new session + process group, fully detaching from agent
	cmd := exec.Command("setsid", "bash", "-c", command)
	cmd.Dir = cwd
	cmd.Env = m.State.BuildEnv()

	// No pipes — /dev/null prevents pipe inheritance hang
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start detached command: %w", err)
	}

	pid := cmd.Process.Pid

	// Release immediately — don't Wait(), don't track
	go cmd.Wait() // reap zombie, but don't block anything

	slog.Info(fmt.Sprintf("[sandbox] detached: pid=%d cmd=%q", pid, command))
	return pid, nil
}

// GetStatus returns the status and output of a background process.
// If maxChars > 0, returns only up to that many characters of output (incremental from last read).
func (m *Manager) GetStatus(id string, waitSeconds int) (*Result, error) {
	return m.GetStatusWithLimit(id, waitSeconds, 0)
}

// GetStatusWithLimit is like GetStatus but supports limiting output size
// and incremental reads (returns only new output since last call).
func (m *Manager) GetStatusWithLimit(id string, waitSeconds int, maxChars int) (*Result, error) {
	m.mu.RLock()
	proc, ok := m.processes[id]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("process %s not found", id)
	}

	// Optionally wait for completion
	if waitSeconds > 0 {
		select {
		case <-proc.Done:
		case <-time.After(time.Duration(waitSeconds) * time.Second):
		}
	}

	// Extract output — incremental if maxChars > 0
	fullStdout := proc.Stdout.String()
	fullStderr := proc.Stderr.String()

	var outStdout, outStderr string
	if maxChars > 0 {
		// Incremental: only return content since last read offset
		if proc.lastReadOffset < len(fullStdout) {
			incremental := fullStdout[proc.lastReadOffset:]
			if len(incremental) > maxChars {
				incremental = incremental[len(incremental)-maxChars:]
			}
			outStdout = incremental
		}
		proc.lastReadOffset = len(fullStdout)
		outStderr = fullStderr // stderr always full (usually small)
	} else {
		outStdout = truncateOutput(fullStdout, m.maxOutput)
		outStderr = truncateOutput(fullStderr, m.maxOutput)
	}

	// Check if done
	select {
	case <-proc.Done:
		return &Result{
			Stdout:   outStdout,
			Stderr:   outStderr,
			ExitCode: proc.ExitCode,
			Duration: time.Since(proc.StartedAt),
		}, nil
	default:
		return &Result{
			Stdout:   outStdout,
			Stderr:   outStderr,
			ExitCode: -1, // Still running
			Duration: time.Since(proc.StartedAt),
		}, nil
	}
}

// Kill terminates a background process group: SIGTERM → 2s → SIGKILL.
func (m *Manager) Kill(id string) error {
	m.mu.RLock()
	proc, ok := m.processes[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process %s not found", id)
	}

	// Try graceful SIGTERM to entire process group first
	if proc.Cmd.Process != nil {
		_ = syscall.Kill(-proc.Cmd.Process.Pid, syscall.SIGTERM)
	}

	// Wait 2s for exit
	select {
	case <-proc.Done:
		return nil
	case <-time.After(2 * time.Second):
		// Force SIGKILL entire process group
		if proc.Cmd.Process != nil {
			slog.Info(fmt.Sprintf("[sandbox] force killing process group pgid=%d", proc.Cmd.Process.Pid))
			return syscall.Kill(-proc.Cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}

// KillAll terminates all active background processes.
// Called on agent stop to prevent orphaned shell processes.
func (m *Manager) KillAll() {
	for _, id := range m.ListActive() {
		m.Kill(id)
	}
}

// SendInput writes data to a background process's stdin.
func (m *Manager) SendInput(id, input string) error {
	m.mu.RLock()
	proc, ok := m.processes[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process %s not found", id)
	}

	select {
	case <-proc.Done:
		return fmt.Errorf("process %s already exited", id)
	default:
	}

	if proc.StdinPipe == nil {
		return fmt.Errorf("process %s has no stdin pipe", id)
	}

	_, err := io.WriteString(proc.StdinPipe, input)
	return err
}

// ListActive returns all active (not done) process IDs.
func (m *Manager) ListActive() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for id, proc := range m.processes {
		select {
		case <-proc.Done:
			continue
		default:
			ids = append(ids, id)
		}
	}
	return ids
}

func truncateOutput(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	half := maxBytes / 2
	lines := strings.Split(s, "\n")

	// Head
	var head strings.Builder
	for _, line := range lines {
		if head.Len()+len(line) > half {
			break
		}
		head.WriteString(line + "\n")
	}

	// Tail
	var tail strings.Builder
	for i := len(lines) - 1; i >= 0; i-- {
		if tail.Len()+len(lines[i]) > half {
			break
		}
		tail.WriteString(lines[i] + "\n")
	}

	return head.String() + "\n... (output truncated) ...\n\n" + tail.String()
}
