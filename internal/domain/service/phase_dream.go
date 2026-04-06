package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ═══════════════════════════════════════════
// PhaseDetector — Coordinator 4-Phase Pipeline (P3 I1)
// ═══════════════════════════════════════════

// ExecutionPhase represents the current phase of agent execution.
type ExecutionPhase int

const (
	PhaseUnknown    ExecutionPhase = iota
	PhaseResearch                  // reading files, searching, gathering context
	PhaseSynthesize                // analyzing, no tool calls → planning response
	PhaseImplement                 // writing/editing files
	PhaseVerify                    // testing, running commands to validate
)

func (p ExecutionPhase) String() string {
	switch p {
	case PhaseResearch:
		return "Research"
	case PhaseSynthesize:
		return "Synthesize"
	case PhaseImplement:
		return "Implement"
	case PhaseVerify:
		return "Verify"
	default:
		return "Unknown"
	}
}

// PhaseDetector tracks the agent's current execution phase based on tool call patterns.
// Only active in agentic mode — planning/regular modes skip phase injection.
type PhaseDetector struct {
	mu         sync.Mutex
	current    ExecutionPhase
	readCount  int // consecutive read/search calls → Research
	writeCount int // consecutive write/edit calls → Implement
	testCount  int // run_command with test keywords → Verify
	idleCount  int // turns with no tool → Synthesize
	totalCalls int
}

// ReadTools are classified as Research-phase signals.
var readTools = map[string]bool{
	"read_file": true, "grep_search": true, "glob": true,
	"web_search": true, "list_dir": true, "view_file": true,
	"find_files": true, "count_lines": true, "tree": true,
}

// WriteTools are classified as Implement-phase signals.
var writeTools = map[string]bool{
	"write_file": true, "edit_file": true, "multi_edit_file": true,
	"diff_files": true, "clipboard": true,
}

// testKeywords trigger Verify phase when present in run_command.
var testKeywords = []string{"test", "spec", "check", "lint", "vet", "build"}

// RecordTool updates phase state based on the called tool name.
func (pd *PhaseDetector) RecordTool(toolName, toolArgs string) ExecutionPhase {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	pd.totalCalls++

	if readTools[toolName] {
		pd.readCount++
		pd.writeCount = 0
		pd.testCount = 0
		pd.idleCount = 0
	} else if writeTools[toolName] {
		pd.writeCount++
		pd.readCount = 0
		pd.testCount = 0
		pd.idleCount = 0
	} else if toolName == "run_command" {
		isTest := false
		for _, kw := range testKeywords {
			if containsKeyword(toolArgs, kw) {
				isTest = true
				break
			}
		}
		if isTest {
			pd.testCount++
			pd.readCount = 0
			pd.writeCount = 0
			pd.idleCount = 0
		}
	}

	// Phase transitions (hysteresis: require 2+ consecutive signals)
	switch {
	case pd.testCount >= 1:
		pd.current = PhaseVerify
	case pd.writeCount >= 2:
		pd.current = PhaseImplement
	case pd.readCount >= 2:
		pd.current = PhaseResearch
	default:
		// Keep current phase on single-signal turns (stability)
	}

	return pd.current
}

// RecordNoTool is called when an assistant turn produces no tool call (synthesis).
func (pd *PhaseDetector) RecordNoTool() {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.idleCount++
	if pd.idleCount >= 1 {
		pd.current = PhaseSynthesize
	}
}

// Current returns the current detected phase.
func (pd *PhaseDetector) Current() ExecutionPhase {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	return pd.current
}

// Reset clears phase state (called at start of each new Run).
func (pd *PhaseDetector) Reset() {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.current = PhaseUnknown
	pd.readCount = 0
	pd.writeCount = 0
	pd.testCount = 0
	pd.idleCount = 0
}

// PhaseEphemeral returns the ephemeral hint text for the current phase.
func (pd *PhaseDetector) PhaseEphemeral() string {
	pd.mu.Lock()
	phase := pd.current
	pd.mu.Unlock()

	switch phase {
	case PhaseResearch:
		return "<ephemeral_message>[Phase: Research] Gather full context before making changes. " +
			"Read all relevant files, grep for patterns, understand dependencies. " +
			"Do NOT write files yet.</ephemeral_message>"
	case PhaseSynthesize:
		return "<ephemeral_message>[Phase: Synthesize] Analyze your findings and form a clear approach. " +
			"Identify the root cause, decide on the implementation strategy.</ephemeral_message>"
	case PhaseImplement:
		return "<ephemeral_message>[Phase: Implement] Making changes incrementally. " +
			"Ensure each edit is complete and correct before proceeding. " +
			"Update task.md as you complete sub-tasks.</ephemeral_message>"
	case PhaseVerify:
		return "<ephemeral_message>[Phase: Verify] Validate your changes. " +
			"Run tests, check compilation, verify edge cases. " +
			"Fix any errors found before declaring completion.</ephemeral_message>"
	default:
		return ""
	}
}

func containsKeyword(s, kw string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, kw)
}

// ═══════════════════════════════════════════
// DreamTask — Idle-time pre-indexing (P3 I2)
// ═══════════════════════════════════════════

// DreamJob is a background pre-indexing task run during agent idle time.
type DreamJob struct {
	Name string
	Fn   func(ctx context.Context)
}

// DreamTask manages background pre-indexing during session idle time.
// All jobs are cancelled immediately when the agent wakes (new user input).
type DreamTask struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	running  bool
	jobs     []DreamJob
	idleWait time.Duration // default 30s
}

// NewDreamTask creates a DreamTask with a 30s default idle threshold.
func NewDreamTask(idleWait time.Duration) *DreamTask {
	if idleWait <= 0 {
		idleWait = 30 * time.Second
	}
	return &DreamTask{idleWait: idleWait}
}

// AddJob registers a background job to run during idle periods.
func (dt *DreamTask) AddJob(job DreamJob) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.jobs = append(dt.jobs, job)
}

// OnIdle starts background jobs after the idle threshold is reached.
// Called after Run() completes. Non-blocking.
func (dt *DreamTask) OnIdle() {
	dt.mu.Lock()
	if dt.running || len(dt.jobs) == 0 {
		dt.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	dt.cancel = cancel
	dt.running = true
	jobs := make([]DreamJob, len(dt.jobs))
	copy(jobs, dt.jobs)
	dt.mu.Unlock()

	go func() {
		// Delay before starting to avoid triggering on quick back-to-back messages
		select {
		case <-time.After(dt.idleWait):
		case <-ctx.Done():
			return
		}

		slog.Info(fmt.Sprintf("[dream] Starting %d background jobs", len(jobs)))
		var wg sync.WaitGroup
		for _, job := range jobs {
			wg.Add(1)
			j := job
			go func() {
				defer wg.Done()
				slog.Info(fmt.Sprintf("[dream] Running job: %s", j.Name))
				j.Fn(ctx)
			}()
		}
		wg.Wait()
		slog.Info("[dream] All background jobs complete")

		dt.mu.Lock()
		dt.running = false
		dt.mu.Unlock()
	}()
}

// OnWake cancels all running background jobs. Called at the start of Run().
func (dt *DreamTask) OnWake() {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if dt.cancel != nil {
		dt.cancel()
		dt.cancel = nil
	}
	dt.running = false
}
