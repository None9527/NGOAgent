package service

// State is an observational phase label for the graph-backed runtime.
type State int

const (
	StateIdle       State = 0 // Waiting for input
	StatePrepare    State = 1 // Building system prompt + context
	StateGenerate   State = 2 // Calling LLM
	StateToolExec   State = 3 // Executing tool calls
	StateGuardCheck State = 4 // Behavior guardrails check
	StateCompact    State = 5 // Context compaction
	StateError      State = 6 // Recoverable error
	StateFatal      State = 7 // Unrecoverable error
	StateDone       State = 8 // Turn complete
	StateEvaluating State = 9 // Evo mode: evaluating execution quality
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
