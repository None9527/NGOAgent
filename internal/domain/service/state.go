package service

// State represents the 10-state agent machine.
type State int

const (
	StateIdle       State = iota // Waiting for input
	StatePrepare                 // Building system prompt + context
	StateGenerate                // Calling LLM
	StateParseReply              // Parsing LLM response
	StateToolExec                // Executing tool calls
	StateGuardCheck              // Behavior guardrails check
	StateCompact                 // Context compaction
	StateWaiting                 // Waiting for user approval
	StateError                   // Recoverable error
	StateFatal                   // Unrecoverable error
	StateAborted                 // User/system abort
	StateDone                    // Turn complete
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
	case StateParseReply:
		return "parse_reply"
	case StateToolExec:
		return "tool_exec"
	case StateGuardCheck:
		return "guard_check"
	case StateCompact:
		return "compact"
	case StateWaiting:
		return "waiting"
	case StateError:
		return "error"
	case StateFatal:
		return "fatal"
	case StateAborted:
		return "aborted"
	case StateDone:
		return "done"
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
	{StateGenerate, StateParseReply},
	{StateParseReply, StateToolExec},
	{StateParseReply, StateDone}, // No tool calls → done
	{StateToolExec, StateGuardCheck},
	{StateToolExec, StateWaiting},    // Tool needs approval
	{StateWaiting, StateToolExec},    // Approved → execute
	{StateWaiting, StateGenerate},    // Denied → skip tool
	{StateWaiting, StateAborted},     // Timeout / cancel
	{StateGuardCheck, StateGenerate}, // Loop back for next turn
	{StateGuardCheck, StateDone},     // Max steps reached
	{StateGuardCheck, StateCompact},  // Context too large
	{StateCompact, StateGenerate},    // Resume after compaction
	{StateGenerate, StateError},      // LLM error
	{StateError, StateGenerate},      // Retry
	{StateError, StateFatal},         // Give up
	{StateFatal, StateIdle},          // Reset
	{StateAborted, StateIdle},        // Reset after abort
	{StateDone, StateIdle},           // Reset for next turn
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
