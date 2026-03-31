package service

// State represents the agent state machine.
type State int

const (
	StateIdle       State = 0  // Waiting for input
	StatePrepare    State = 1  // Building system prompt + context
	StateGenerate   State = 2  // Calling LLM
	StateToolExec   State = 3  // Executing tool calls
	StateGuardCheck State = 4  // Behavior guardrails check
	StateCompact    State = 5  // Context compaction
	StateError      State = 6  // Recoverable error
	StateFatal      State = 7  // Unrecoverable error
	StateDone       State = 8  // Turn complete
	StateEvaluating State = 9  // Evo mode: evaluating execution quality
)

// String returns the human-readable state name.
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StatePrepare:
		return "prepare"
	case StateGenerate:
		return "generate"
	case StateToolExec:
		return "tool_exec"
	case StateGuardCheck:
		return "guard_check"
	case StateCompact:
		return "compact"
	case StateError:
		return "error"
	case StateFatal:
		return "fatal"
	case StateDone:
		return "done"
	case StateEvaluating:
		return "evaluating"
	default:
		return "unknown"
	}
}

// Transition defines a valid state transition.
type Transition struct {
	From State
	To   State
}

// ValidTransitions defines the state machine transition rules.
var ValidTransitions = []Transition{
	{StateIdle, StatePrepare},
	{StatePrepare, StateGenerate},
	{StateGenerate, StateToolExec},   // Tool calls present
	{StateGenerate, StateDone},       // No tool calls → done
	{StateToolExec, StateGuardCheck},
	{StateGuardCheck, StateGenerate}, // Loop back for next turn
	{StateGuardCheck, StateDone},     // Max steps reached
	{StateGuardCheck, StateCompact},  // Context too large
	{StateCompact, StateGenerate},    // Resume after compaction
	{StateGenerate, StateError},      // LLM error
	{StateError, StateGenerate},      // Retry
	{StateError, StateFatal},         // Give up
	{StateFatal, StateIdle},          // Reset
	{StateDone, StateIdle},           // Reset for next turn
	{StateDone, StatePrepare},        // PendingWake: subagent results continuation
	{StateDone, StateEvaluating},     // Reserved: evo mode (currently unused, evaluation is async)
	// Evaluating transitions removed: evaluation now runs async in fireHooks goroutine
}

// CanTransition checks if a state transition is valid.
func CanTransition(from, to State) bool {
	for _, t := range ValidTransitions {
		if t.From == from && t.To == to {
			return true
		}
	}
	return false
}
