package service

import dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"

// TaskTracker manages task boundary state for the agent loop.
// Extracted from AgentLoop to reduce God Object field count.
// These fields are written by the task_boundary tool intercept in doToolExec
// and read by the prepare node service for ephemeral injection decisions.
type TaskTracker struct {
	dtool.BoundaryState // Shared with LoopState — eliminates protoState/syncLoopState copy

	PlanModified bool   // true if plan.md was written/updated this session
	SkillLoaded  string // L2: skill name loaded via SKILL.md read (one-shot)
	SkillPath    string // L2: skill directory path

	// Artifact staleness tracking (Anti-style: steps since last interaction)
	ArtifactLastStep map[string]int // artifact name → last step that touched it
	CurrentStep      int            // global step counter across tool calls
}

// NewTaskTracker creates an initialized TaskTracker.
func NewTaskTracker() *TaskTracker {
	return &TaskTracker{
		ArtifactLastStep: make(map[string]int),
	}
}

// RecordToolCall increments step counters after each tool execution.
func (t *TaskTracker) RecordToolCall() {
	t.StepsSinceUpdate++
	t.CurrentStep++
}

// RecordBoundary records a task_boundary tool call.
func (t *TaskTracker) RecordBoundary(taskName, mode, status, summary string) {
	t.PreviousMode = t.Mode
	t.Name = taskName
	t.Mode = mode
	t.Status = status
	t.Summary = summary
	t.StepsSinceUpdate = 0
}

// RecordArtifactTouch marks an artifact as touched at the current step.
func (t *TaskTracker) RecordArtifactTouch(name string) {
	t.ArtifactLastStep[name] = t.CurrentStep
}

// ConsumeSkill returns and clears the loaded skill info (one-shot consumption).
func (t *TaskTracker) ConsumeSkill() (name, path string) {
	name = t.SkillLoaded
	path = t.SkillPath
	t.SkillLoaded = ""
	t.SkillPath = ""
	return
}

// ConsumeYield returns and clears the yield request flag.
func (t *TaskTracker) ConsumeYield() bool {
	if t.YieldRequested {
		t.YieldRequested = false
		return true
	}
	return false
}
