// Package service — Domain-side port interfaces.
//
// These interfaces follow Go's consumer-side declaration principle:
// the consuming package (domain/service) defines the minimum interface
// it needs, and infrastructure types satisfy them implicitly.
//
// This breaks the reverse dependency: domain/service → infrastructure/X.
// Instead: domain/service → ports.go (interfaces) ← infrastructure/X (implementations)
package service

import (
	"context"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/entity"
	dmodel "github.com/ngoclaw/ngoagent/internal/domain/model"
)

// ──────────────────────────────────────────────
// Artifact Storage (breaks import: brain.ArtifactStore)
// ──────────────────────────────────────────────

// ArtifactReader provides read-only access to brain artifacts.
// Implemented by: *brain.ArtifactStore
type ArtifactReader interface {
	BaseDir() string
	Read(name string) (string, error)
	SetWorkspaceDir(dir string)
}

// ──────────────────────────────────────────────
// Knowledge Items (breaks import: knowledge.Store)
// ──────────────────────────────────────────────

// KIIndexer generates the Knowledge Item index for prompt assembly.
// Implemented by: *knowledge.Store
type KIIndexer interface {
	GenerateKIIndex() string
}

// ──────────────────────────────────────────────
// Workspace (breaks import: workspace.Store)
// ──────────────────────────────────────────────

// WorkspaceReader provides workspace file listing and context extraction.
// Implemented by: *workspace.Store
type WorkspaceReader interface {
	WorkDir() string
	RootFiles() []string
	ReadContextWithIncludes() string
}

// ──────────────────────────────────────────────
// Skills (breaks import: skill.Manager)
// ──────────────────────────────────────────────

// SkillLister lists discovered skills for prompt injection.
// Implemented by: *skill.Manager
type SkillLister interface {
	List() []*entity.Skill
}

// ──────────────────────────────────────────────
// File Edit History (breaks import: workspace.FileHistory)
// ──────────────────────────────────────────────

// FileEditTracker tracks pending file edits and creates snapshots.
// Implemented by: *workspace.FileHistory
type FileEditTracker interface {
	HasPendingEdits() bool
	Snapshot(messageID string) error
}

// ──────────────────────────────────────────────
// Security (breaks import: security.Hook)
// ──────────────────────────────────────────────

// SecurityDecision is the domain-side enum for tool security verdicts.
// Mirrors security.Decision but owned by domain, not infrastructure.
type SecurityDecision int

const (
	SecurityAllow SecurityDecision = iota // Proceed with execution
	SecurityDeny                          // Block: tool is forbidden
	SecurityAsk                           // Requires interactive user approval
)

// ApprovalTicket represents a pending user approval request.
// Domain-owned mirror of security.PendingApproval (minus infra fields).
type ApprovalTicket struct {
	ID     string    // Unique approval ID
	Result chan bool // Receives true=approved, false=denied
}

// ApprovalSnapshot is a serializable view of a pending approval.
type ApprovalSnapshot struct {
	ID        string
	ToolName  string
	Args      map[string]any
	Reason    string
	Requested time.Time
}

// SecurityChecker provides the tool security decision chain.
// Implemented by: *security.Hook (via securityAdapter in application/)
type SecurityChecker interface {
	BeforeToolCall(ctx context.Context, toolName string, args map[string]any) (SecurityDecision, string)
	AfterToolCall(ctx context.Context, toolName string, result string, err error)
	RequestApproval(toolName string, args map[string]any, reason string) *ApprovalTicket
	ListPendingApprovals() []ApprovalSnapshot
	CleanupPending(approvalID string)
}

// ──────────────────────────────────────────────
// LLM Routing (breaks import: llm.Router)
// ──────────────────────────────────────────────

// ModelRouter resolves LLM providers by model name.
// Implemented by: *llm.Router
type ModelRouter interface {
	CurrentModel() string
	Resolve(model string) (dmodel.Provider, error)
	ResolveWithExclusions(model string, excluded []string) (dmodel.Provider, string, error)
}
