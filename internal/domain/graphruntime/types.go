package graphruntime

import (
	"context"
	"fmt"
	"time"
)

type NodeKind string

const (
	NodeKindPrepare     NodeKind = "prepare"
	NodeKindOrchestrate NodeKind = "orchestrate"
	NodeKindSpawn       NodeKind = "spawn"
	NodeKindBarrierWait NodeKind = "barrier_wait"
	NodeKindMerge       NodeKind = "merge"
	NodeKindGenerate    NodeKind = "generate"
	NodeKindToolExec    NodeKind = "tool_exec"
	NodeKindGuard       NodeKind = "guard"
	NodeKindReflect     NodeKind = "reflect"
	NodeKindPlan        NodeKind = "plan"
	NodeKindEvaluate    NodeKind = "evaluate"
	NodeKindRepair      NodeKind = "repair"
	NodeKindCompact     NodeKind = "compact"
	NodeKindDone        NodeKind = "done"
	NodeKindComplete    NodeKind = "complete"
	NodeKindCustom      NodeKind = "custom"
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

type NodeEffectKind string

const (
	NodeEffectNone               NodeEffectKind = ""
	NodeEffectPersistHistory     NodeEffectKind = "persist_history"
	NodeEffectPersistFullHistory NodeEffectKind = "persist_full_history"
	NodeEffectEmitText           NodeEffectKind = "emit_text"
	NodeEffectEmitError          NodeEffectKind = "emit_error"
	NodeEffectEmitAutoWakeStart  NodeEffectKind = "emit_auto_wake_start"
	NodeEffectEmitComplete       NodeEffectKind = "emit_complete"
)

type ToolCallSnapshot struct {
	ID        string
	Type      string
	Name      string
	Arguments string
}

type AttachmentState struct {
	Path string
}

type ConversationMessageState struct {
	Role        string
	Content     string
	Reasoning   string
	ToolCallID  string
	ToolCalls   []ToolCallSnapshot
	Attachments []AttachmentState
}

type NodePatch struct {
	AppendHistory        []ConversationMessageState
	ReplaceHistory       []ConversationMessageState
	HistoryReplaced      bool
	AppendEphemerals     []string
	ReplaceEphemerals    []string
	EphemeralsReplaced   bool
	AppendPendingMedia   []map[string]string
	ReplacePendingMedia  []map[string]string
	PendingMediaReplaced bool
	Task                 *TaskState
	Compact              *CompactState
	Intelligence         *IntelligenceState
	Orchestration        *OrchestrationState
	ActiveSkills         map[string]string
	ForceNextTool        *string
	Execution            *ExecutionState
}

type NodeEffect struct {
	Kind    NodeEffectKind
	Message string
	Error   string
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
	ObservedState    string
	StateMutations   []StateMutation
	Patch            NodePatch
	Effects          []NodeEffect
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
	YieldRequested   bool
	Name             string
	Mode             string
	Status           string
	Summary          string
	StepsSinceUpdate int
	PlanModified     bool
	CurrentStep      int
	ArtifactLastStep map[string]int
	SkillLoaded      string
	SkillPath        string
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

type ReviewDecisionState struct {
	SchemaName string
	Decision   string
	Reason     string
	RawJSON    string
	Valid      bool
}

type PlanningState struct {
	Required         bool
	ReviewRequired   bool
	ReviewDecision   string
	ReviewFeedback   string
	ReviewedAt       time.Time
	Trigger          string
	BoundaryMode     string
	PlanExists       bool
	TaskExists       bool
	ContextTight     bool
	MissingArtifacts []string
}

type HandoffState struct {
	TargetRunID string
	TargetNode  string
	PayloadJSON string
	Kind        string
}

type OrchestrationEventState struct {
	Type         string
	Kind         string
	Source       string
	Trigger      string
	DecisionKind string
	Decision     string
	RunID        string
	SourceRun    string
	BarrierID    string
	At           time.Time
	Summary      string
	PayloadJSON  string
}

type IngressState struct {
	Kind         string
	Source       string
	Trigger      string
	RunID        string
	DecisionKind string
	Decision     string
	At           time.Time
}

type OrchestrationState struct {
	ParentRunID    string
	ChildRunIDs    []string
	ActiveBarrier  *BarrierState
	PendingMerge   bool
	LastWakeSource string
	Ingress        IngressState
	Handoffs       []HandoffState
	Events         []OrchestrationEventState
}

type EvaluationIssueState struct {
	Severity    string
	Description string
}

type EvaluationState struct {
	SchemaName string
	Score      float64
	Passed     bool
	ErrorType  string
	Issues     []EvaluationIssueState
	RawJSON    string
	Valid      bool
}

type RepairStrategy string

type RepairState struct {
	Strategy    RepairStrategy
	Description string
	Ephemeral   string
	Allowed     bool
	Attempted   bool
	Success     bool
	BlockReason string
}

type DecisionKind string

const (
	DecisionKindNone       DecisionKind = ""
	DecisionKindPlanReview DecisionKind = "plan_review"
	DecisionKindReflection DecisionKind = "reflection"
	DecisionKindEvaluation DecisionKind = "evaluation"
)

type DecisionContractState struct {
	Kind         DecisionKind
	SchemaName   string
	Decision     string
	Reason       string
	Feedback     string
	AppliedAt    time.Time
	ResumeAction string
	Valid        bool
}

type IntelligenceState struct {
	Decision   DecisionContractState
	Planning   PlanningState
	Review     ReviewDecisionState
	Evaluation EvaluationState
	Repair     RepairState
}

type LLMResponseState struct {
	Content    string
	Reasoning  string
	StopReason string
	Provider   string
}

type StructuredOutputState struct {
	SchemaName string
	RawJSON    string
	Valid      bool
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
	RunID            string
	UserMessage      string
	History          []ConversationMessageState
	Attachments      []string
	Ephemerals       []string
	PendingMedia     []map[string]string
	Task             TaskState
	Mode             string
	LastLLMResponse  LLMResponseState
	ToolCalls        []ToolCallState
	ToolResults      []ToolResultState
	OutputDraft      string
	StructuredOutput StructuredOutputState
	ForceNextTool    string
	ActiveSkills     map[string]string
	Compact          CompactState
	Reflection       ReflectionState
	Intelligence     IntelligenceState
	Orchestration    OrchestrationState
}

type ApprovalState struct {
	ID          string
	ToolName    string
	Args        map[string]any
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
	Cursor            ExecutionCursor
	StartedAt         time.Time
	UpdatedAt         time.Time
	Status            NodeStatus
	WaitReason        WaitReason
	ObservedState     string
	TurnSteps         int
	MaxTokens         int
	ExcludedProviders []string
	PendingApproval   *ApprovalState
	PendingBarrier    *BarrierState
	PendingWake       bool
	Retry             RetryState
	Continuation      ContinuationState
	OutputSchemaName  string
	LastError         string
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
