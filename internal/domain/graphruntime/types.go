package graphruntime

import (
	"context"
	"fmt"
	"time"
)

type NodeKind string

const (
	NodeKindPrepare  NodeKind = "prepare"
	NodeKindGenerate NodeKind = "generate"
	NodeKindToolExec NodeKind = "tool_exec"
	NodeKindGuard    NodeKind = "guard"
	NodeKindCompact  NodeKind = "compact"
	NodeKindDone     NodeKind = "done"
	NodeKindCustom   NodeKind = "custom"
)

type NodeStatus string

const (
	NodeStatusContinue NodeStatus = "continue"
	NodeStatusWait     NodeStatus = "wait"
	NodeStatusComplete NodeStatus = "complete"
	NodeStatusFatal    NodeStatus = "fatal"
)

type WaitReason string

const (
	WaitReasonNone      WaitReason = ""
	WaitReasonApproval  WaitReason = "approval"
	WaitReasonBarrier   WaitReason = "barrier"
	WaitReasonExternal  WaitReason = "external"
	WaitReasonUserInput WaitReason = "user_input"
)

type StateMutation struct {
	Key   string
	Value any
}

type Edge struct {
	From      string
	To        string
	Condition string
	Priority  int
}

type ExecutionCursor struct {
	GraphID      string
	GraphVersion string
	CurrentNode  string
	Step         int
	RouteKey     string
}

type NodeResult struct {
	RouteKey         string
	Status           NodeStatus
	StateMutations   []StateMutation
	NeedsCheckpoint  bool
	WaitReason       WaitReason
	OutputSchemaName string
}

func (r NodeResult) normalize() NodeResult {
	if r.Status == "" {
		r.Status = NodeStatusContinue
	}
	return r
}

type Node interface {
	Name() string
	Kind() NodeKind
	Execute(ctx context.Context, rt *RuntimeContext, state *TurnState) (NodeResult, error)
}

type GraphDefinition struct {
	ID        string
	Version   string
	EntryNode string
	Nodes     map[string]Node
	Edges     []Edge
}

func (g GraphDefinition) Validate() error {
	if g.ID == "" {
		return fmt.Errorf("graph id is required")
	}
	if g.Version == "" {
		return fmt.Errorf("graph version is required")
	}
	if g.EntryNode == "" {
		return fmt.Errorf("graph entry node is required")
	}
	if len(g.Nodes) == 0 {
		return fmt.Errorf("graph must define at least one node")
	}
	if _, ok := g.Nodes[g.EntryNode]; !ok {
		return fmt.Errorf("entry node %q not found", g.EntryNode)
	}
	for _, e := range g.Edges {
		if _, ok := g.Nodes[e.From]; !ok {
			return fmt.Errorf("edge from unknown node %q", e.From)
		}
		if _, ok := g.Nodes[e.To]; !ok {
			return fmt.Errorf("edge to unknown node %q", e.To)
		}
	}
	return nil
}

type SessionState struct {
	SessionID         string
	ConversationTurns []string
	SessionMetadata   map[string]string
	ActiveOverlays    []string
	MemoryPointers    []string
	TokenUsageSummary map[string]int64
}

type TaskState struct {
	Name             string
	Status           string
	Summary          string
	YieldRequested   bool
	BoundaryTaskName string
	BoundaryStatus   string
	BoundarySummary  string
	StepsSinceUpdate int
}

type CompactState struct {
	CompactCount        int
	OutputContinuations int
	HistoryDirty        bool
}

type ReflectionState struct {
	LastReview string
	Required   bool
}

type LLMResponseState struct {
	Content    string
	Reasoning  string
	StopReason string
	Provider   string
}

type ToolCallState struct {
	ID   string
	Name string
	Args map[string]any
}

type ToolResultState struct {
	CallID string
	Name   string
	Output string
	Error  string
}

type TurnState struct {
	RunID           string
	UserMessage     string
	Attachments     []string
	Ephemerals      []string
	Task            TaskState
	Mode            string
	CurrentPlan     string
	LastLLMResponse LLMResponseState
	ToolCalls       []ToolCallState
	ToolResults     []ToolResultState
	OutputDraft     string
	Compact         CompactState
	Reflection      ReflectionState
}

type ApprovalState struct {
	ID          string
	ToolName    string
	Reason      string
	RequestedAt time.Time
}

type BarrierMemberState struct {
	RunID    string
	TaskName string
	Status   string
	Output   string
	Error    string
	DoneAt   time.Time
}

type BarrierState struct {
	ID             string
	TotalCount     int
	PendingCount   int
	CompletedCount int
	Finalized      bool
	Members        []BarrierMemberState
}

type RetryState struct {
	Count         int
	LastErrorCode string
	LastProvider  string
}

type ContinuationState struct {
	Count int
}

type ExecutionState struct {
	Cursor          ExecutionCursor
	StartedAt       time.Time
	UpdatedAt       time.Time
	Status          NodeStatus
	PendingApproval *ApprovalState
	PendingBarrier  *BarrierState
	PendingWake     bool
	Retry           RetryState
	Continuation    ContinuationState
	LastError       string
}

type RuntimeContext struct {
	Graph         GraphDefinition
	Session       *SessionState
	Execution     *ExecutionState
	SnapshotStore SnapshotStore
	Values        map[string]any
}

type RunRequest struct {
	RunID    string
	Session  *SessionState
	Turn     TurnState
	ResumeAt *ExecutionCursor
}

type RunSnapshot struct {
	RunID          string
	SessionID      string
	GraphID        string
	GraphVersion   string
	Status         NodeStatus
	Cursor         ExecutionCursor
	TurnState      TurnState
	ExecutionState ExecutionState
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SnapshotStore interface {
	Save(ctx context.Context, snap *RunSnapshot) error
	LoadLatest(ctx context.Context, runID string) (*RunSnapshot, error)
	LoadLatestBySession(ctx context.Context, sessionID string) (*RunSnapshot, error)
	Delete(ctx context.Context, runID string) error
}
