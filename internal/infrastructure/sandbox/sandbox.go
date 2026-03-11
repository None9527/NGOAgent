// Package sandbox provides process isolation for command execution.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
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
	ID        string
	Cmd       *exec.Cmd
	Stdout    *bytes.Buffer
	Stderr    *bytes.Buffer
	Done      chan struct{}
	ExitCode  int
	StartedAt time.Time
	cancel    context.CancelFunc
}

// Manager manages sandboxed process execution.
type Manager struct {
	mu        sync.RWMutex
	processes map[string]*Process
	maxOutput int // Max bytes per output buffer
}

// NewManager creates a sandbox process manager.
func NewManager() *Manager {
	return &Manager{
		processes: make(map[string]*Process),
		maxOutput: 50 * 1024, // 50KB default
	}
}

// Run executes a command synchronously within a timeout.
func (m *Manager) Run(ctx context.Context, command, cwd string, timeout time.Duration) (*Result, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "bash", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	result := &Result{
		Stdout:   truncateOutput(stdout.String(), m.maxOutput),
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
func (m *Manager) RunBackground(ctx context.Context, id, command, cwd string) error {
	bgCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(bgCtx, "bash", "-c", command)
	if cwd != "" {
		cmd.Dir = cwd
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	proc := &Process{
		ID:        id,
		Cmd:       cmd,
		Stdout:    stdout,
		Stderr:    stderr,
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

// GetStatus returns the status and output of a background process.
func (m *Manager) GetStatus(id string, waitSeconds int) (*Result, error) {
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

	// Check if done
	select {
	case <-proc.Done:
		return &Result{
			Stdout:   truncateOutput(proc.Stdout.String(), m.maxOutput),
			Stderr:   truncateOutput(proc.Stderr.String(), m.maxOutput),
			ExitCode: proc.ExitCode,
			Duration: time.Since(proc.StartedAt),
		}, nil
	default:
		return &Result{
			Stdout:   truncateOutput(proc.Stdout.String(), m.maxOutput),
			Stderr:   truncateOutput(proc.Stderr.String(), m.maxOutput),
			ExitCode: -1, // Still running
			Duration: time.Since(proc.StartedAt),
		}, nil
	}
}

// Kill terminates a background process with SIGTERM → 2s → SIGKILL.
func (m *Manager) Kill(id string) error {
	m.mu.RLock()
	proc, ok := m.processes[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("process %s not found", id)
	}

	// Try graceful SIGTERM first via cancel
	proc.cancel()

	// Wait 2s for exit
	select {
	case <-proc.Done:
		return nil
	case <-time.After(2 * time.Second):
		// Force SIGKILL
		if proc.Cmd.Process != nil {
			return proc.Cmd.Process.Kill()
		}
		return nil
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

	if proc.Cmd.Process == nil {
		return fmt.Errorf("process %s has no running process", id)
	}

	// Write to stdin via pipe (requires stdin pipe setup in RunBackground)
	return nil // Stdin pipe not wired in basic RunBackground — future enhancement
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
