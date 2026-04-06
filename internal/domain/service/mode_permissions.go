// Package service — ModePermissions defines execution mode as a permission matrix.
// Replaces string-based PlanMode branching with structured capability flags.
//
// Two orthogonal axes:
//   - Execution mode (auto|plan|agentic): mutually exclusive, controls planning + approval
//   - Evo layer (on|off): orthogonal toggle, controls trace collection + evaluation
package service

// ModePermissions defines execution mode capabilities as permission bits.
// Not mutually exclusive branches — capability composition.
type ModePermissions struct {
	Name        string // Execution mode: "auto" | "plan" | "agentic"
	ForcePlan   bool   // Force planning prompt injection (plan, agentic)
	AutoApprove bool   // Skip human approval for tool execution (agentic)
	SelfReview  bool   // Self-review plans instead of yielding to user (agentic)
	PhaseDetect bool   // Enable 4-phase execution detection (agentic)
	EvoEnabled  bool   // Orthogonal: trace collection + evaluation + repair
}

// modeRegistry defines the base permission set for each execution mode.
var modeRegistry = map[string]ModePermissions{
	"auto": {Name: "auto"},
	"plan": {Name: "plan", ForcePlan: true},
	"agentic": {
		Name:        "agentic",
		ForcePlan:   true,
		AutoApprove: true,
		SelfReview:  true,
		PhaseDetect: true,
	},
}

// ModeFromString parses an API mode string into structured permissions.
// "evo" is backward-compatible shorthand for auto+evo.
func ModeFromString(mode string, evo bool) ModePermissions {
	if mode == "evo" {
		// Backward compat: legacy "evo" string → auto execution + evo layer
		m := modeRegistry["auto"]
		m.EvoEnabled = true
		return m
	}
	m, ok := modeRegistry[mode]
	if !ok {
		m = modeRegistry["auto"]
	}
	if evo {
		m.EvoEnabled = true
	}
	return m
}

// String returns a human-readable mode descriptor (e.g., "agentic+evo").
func (m ModePermissions) String() string {
	s := m.Name
	if s == "" {
		s = "auto"
	}
	if m.EvoEnabled {
		s += "+evo"
	}
	return s
}
