package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	dmodel "github.com/ngoclaw/ngoagent/internal/domain/model"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

type fakeSecurityChecker struct {
	pending  []ApprovalSnapshot
	restored []ApprovalSnapshot
	resolved []string
	cleaned  []string
}

type fakeModelRouter struct{}

func (fakeModelRouter) CurrentModel() string { return "gpt-4.1" }
func (fakeModelRouter) Resolve(string) (dmodel.Provider, error) {
	return nil, nil
}
func (fakeModelRouter) ResolveWithExclusions(string, []string) (dmodel.Provider, string, error) {
	return nil, "", nil
}

func (f *fakeSecurityChecker) BeforeToolCall(_ context.Context, _ string, _ map[string]any) (SecurityDecision, string) {
	return SecurityAllow, ""
}
func (f *fakeSecurityChecker) AfterToolCall(_ context.Context, _ string, _ string, _ error) {}
func (f *fakeSecurityChecker) RequestApproval(_ string, _ map[string]any, _ string) *ApprovalTicket {
	return nil
}
func (f *fakeSecurityChecker) RestorePendingApproval(snapshot ApprovalSnapshot) *ApprovalTicket {
	f.restored = append(f.restored, snapshot)
	f.pending = append(f.pending, snapshot)
	return &ApprovalTicket{ID: snapshot.ID, Result: make(chan bool, 1)}
}
func (f *fakeSecurityChecker) ResolvePendingApproval(approvalID string, _ bool) error {
	for _, pending := range f.pending {
		if pending.ID == approvalID {
			f.resolved = append(f.resolved, approvalID)
			return nil
		}
	}
	return fmt.Errorf("approval %s not found", approvalID)
}
func (f *fakeSecurityChecker) ListPendingApprovals() []ApprovalSnapshot { return f.pending }
func (f *fakeSecurityChecker) CleanupPending(approvalID string) {
	f.cleaned = append(f.cleaned, approvalID)
	filtered := f.pending[:0]
	for _, pending := range f.pending {
		if pending.ID != approvalID {
			filtered = append(filtered, pending)
		}
	}
	f.pending = filtered
}

func TestNewAgentLoopGraph_ValidDefinition(t *testing.T) {
	loop := &AgentLoop{}
	graph := NewAgentLoopGraph(loop)

	if err := graph.Validate(); err != nil {
		t.Fatalf("graph should validate: %v", err)
	}
	if graph.ID != "agent_loop" {
		t.Fatalf("unexpected graph id: %s", graph.ID)
	}
	if graph.EntryNode != "prepare" {
		t.Fatalf("unexpected entry node: %s", graph.EntryNode)
	}
	if _, ok := graph.Nodes["reflect"]; !ok {
		t.Fatal("expected reflect node in graph definition")
	}
	if _, ok := graph.Nodes["plan"]; !ok {
		t.Fatal("expected plan node in graph definition")
	}
	if _, ok := graph.Nodes["orchestrate"]; !ok {
		t.Fatal("expected orchestrate node in graph definition")
	}
	if _, ok := graph.Nodes["spawn"]; !ok {
		t.Fatal("expected spawn node in graph definition")
	}
	if _, ok := graph.Nodes["barrier_wait"]; !ok {
		t.Fatal("expected barrier_wait node in graph definition")
	}
	if _, ok := graph.Nodes["merge"]; !ok {
		t.Fatal("expected merge node in graph definition")
	}
	if _, ok := graph.Nodes["evaluate"]; !ok {
		t.Fatal("expected evaluate node in graph definition")
	}
	if _, ok := graph.Nodes["repair"]; !ok {
		t.Fatal("expected repair node in graph definition")
	}
	if _, ok := graph.Nodes["complete"]; !ok {
		t.Fatal("expected complete node in graph definition")
	}
}

func TestPrepareNode_RoutesToPlanWhenPlanningRequired(t *testing.T) {
	loop := NewAgentLoop(Deps{LLMRouter: fakeModelRouter{}})
	loop.mode = ModePermissions{Name: "plan", ForcePlan: true}
	loop.history = []llm.Message{{Role: "user", Content: "/plan refactor this"}}
	node := prepareNode{adapter: newGraphLoopAdapter(loop)}
	state := &graphruntime.TurnState{}

	result, err := node.Execute(context.Background(), &graphruntime.RuntimeContext{
		Execution: &graphruntime.ExecutionState{},
	}, state)
	if err != nil {
		t.Fatalf("prepare execute error: %v", err)
	}
	if result.RouteKey != graphRouteOrchestrate || result.ObservedState != "prepare" {
		t.Fatalf("expected prepare to route orchestrate, got %#v", result)
	}
	if !state.Intelligence.Planning.Required || state.Intelligence.Planning.Trigger == "" {
		t.Fatalf("expected planning decision captured in state, got %#v", state.Intelligence.Planning)
	}
}

func TestOrchestrateNode_RoutesPlanWhenPlanningRequired(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	node := orchestrateNode{adapter: newGraphLoopAdapter(loop)}
	state := &graphruntime.TurnState{
		Intelligence: graphruntime.IntelligenceState{
			Planning: graphruntime.PlanningState{Required: true},
		},
	}

	result, err := node.Execute(context.Background(), &graphruntime.RuntimeContext{
		Execution: &graphruntime.ExecutionState{},
	}, state)
	if err != nil {
		t.Fatalf("orchestrate execute error: %v", err)
	}
	if result.RouteKey != graphRoutePlan || result.ObservedState != "plan" {
		t.Fatalf("expected orchestrate to route plan, got %#v", result)
	}
}

func TestBarrierWaitNode_CheckpointsBarrierWait(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	node := barrierWaitNode{adapter: newGraphLoopAdapter(loop)}
	state := &graphruntime.TurnState{
		Orchestration: graphruntime.OrchestrationState{
			ActiveBarrier: &graphruntime.BarrierState{ID: "bar-1", PendingCount: 2},
		},
	}

	result, err := node.Execute(context.Background(), &graphruntime.RuntimeContext{
		Execution: &graphruntime.ExecutionState{},
	}, state)
	if err != nil {
		t.Fatalf("barrier_wait execute error: %v", err)
	}
	if result.Status != graphruntime.NodeStatusWait || result.WaitReason != graphruntime.WaitReasonBarrier {
		t.Fatalf("expected barrier wait checkpoint, got %#v", result)
	}
}

func TestPlanningNode_EmitsPlanReviewAndRoutesGenerate(t *testing.T) {
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{Delta: delta})
	node := planningNode{adapter: newGraphLoopAdapter(loop)}
	state := &graphruntime.TurnState{
		Intelligence: graphruntime.IntelligenceState{
			Planning: graphruntime.PlanningState{
				Required:         true,
				ReviewRequired:   true,
				Trigger:          "mode_force_plan",
				MissingArtifacts: []string{"plan.md", "task.md"},
			},
		},
	}

	result, err := node.Execute(context.Background(), &graphruntime.RuntimeContext{
		Execution: &graphruntime.ExecutionState{},
	}, state)
	if err != nil {
		t.Fatalf("planning execute error: %v", err)
	}
	if result.Status != graphruntime.NodeStatusWait || result.WaitReason != graphruntime.WaitReasonUserInput {
		t.Fatalf("expected plan node to wait for review, got %#v", result)
	}
	if result.RouteKey != graphRoutePlan || result.ObservedState != "plan" || !result.NeedsCheckpoint {
		t.Fatalf("expected plan node to checkpoint its wait state, got %#v", result)
	}
	if len(delta.reviews) != 1 {
		t.Fatalf("expected plan review event, got %#v", delta.reviews)
	}
	if delta.reviews[0].message != "Planning trigger: mode_force_plan" {
		t.Fatalf("unexpected plan review message: %#v", delta.reviews[0])
	}
	if len(delta.reviews[0].paths) != 2 || delta.reviews[0].paths[0] != "plan.md" {
		t.Fatalf("unexpected plan review paths: %#v", delta.reviews[0].paths)
	}
}

func TestPlanningNode_RoutesGenerateAfterApprovalDecision(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	node := planningNode{adapter: newGraphLoopAdapter(loop)}
	state := &graphruntime.TurnState{
		Intelligence: graphruntime.IntelligenceState{
			Planning: graphruntime.PlanningState{
				Required:       true,
				ReviewRequired: true,
				ReviewDecision: "approved",
			},
		},
	}

	result, err := node.Execute(context.Background(), &graphruntime.RuntimeContext{
		Execution: &graphruntime.ExecutionState{},
	}, state)
	if err != nil {
		t.Fatalf("planning execute error: %v", err)
	}
	if result.RouteKey != graphRouteGenerate || result.Status != "" {
		t.Fatalf("expected approved plan review to continue to generate, got %#v", result)
	}
	if state.Intelligence.Planning.ReviewRequired {
		t.Fatalf("expected review requirement cleared after approval, got %#v", state.Intelligence.Planning)
	}
}

func TestPlanningNode_RoutesPrepareAfterRevisionDecision(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	node := planningNode{adapter: newGraphLoopAdapter(loop)}
	state := &graphruntime.TurnState{
		Intelligence: graphruntime.IntelligenceState{
			Planning: graphruntime.PlanningState{
				Required:       true,
				ReviewRequired: true,
				ReviewDecision: "revise",
				ReviewFeedback: "split the work into milestones",
			},
		},
	}

	result, err := node.Execute(context.Background(), &graphruntime.RuntimeContext{
		Execution: &graphruntime.ExecutionState{},
	}, state)
	if err != nil {
		t.Fatalf("planning execute error: %v", err)
	}
	if result.RouteKey != graphRoutePlanRevise || result.Status != "" {
		t.Fatalf("expected revision decision to route back to prepare, got %#v", result)
	}
	if state.Intelligence.Planning.ReviewRequired {
		t.Fatalf("expected review requirement cleared after revision decision, got %#v", state.Intelligence.Planning)
	}
	loop.mu.Lock()
	got := append([]string(nil), loop.ephemerals...)
	loop.mu.Unlock()
	if len(got) != 1 || got[0] != "Plan review requested revision: split the work into milestones" {
		t.Fatalf("expected revision feedback injected as ephemeral, got %#v", got)
	}
}

func TestHandleReconnect_ReplaysPlanningReviewWait(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{SnapshotStore: store, Delta: delta})

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-plan-review",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-plan-review",
			Intelligence: graphruntime.IntelligenceState{
				Planning: graphruntime.PlanningState{
					Required:         true,
					ReviewRequired:   true,
					Trigger:          "mode_force_plan",
					MissingArtifacts: []string{"plan.md"},
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonUserInput,
		},
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save planning wait snapshot: %v", err)
	}

	handled, err := loop.HandleReconnect(context.Background())
	if err != nil {
		t.Fatalf("HandleReconnect error: %v", err)
	}
	if !handled {
		t.Fatal("expected planning review reconnect to be handled")
	}
	if len(delta.reviews) != 1 {
		t.Fatalf("expected planning review replay, got %#v", delta.reviews)
	}
	if delta.reviews[0].message != "Planning trigger: mode_force_plan" {
		t.Fatalf("unexpected replayed review message: %#v", delta.reviews[0])
	}
}

func TestHandleReconnect_DoesNotReplayReviewedPlanningWait(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{SnapshotStore: store, Delta: delta})

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-plan-reviewed",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-plan-reviewed",
			Intelligence: graphruntime.IntelligenceState{
				Planning: graphruntime.PlanningState{
					Required:       true,
					ReviewRequired: true,
					ReviewDecision: "approved",
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonUserInput,
		},
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save reviewed planning wait snapshot: %v", err)
	}

	handled, err := loop.HandleReconnect(context.Background())
	if err != nil {
		t.Fatalf("HandleReconnect error: %v", err)
	}
	if handled {
		t.Fatal("expected reviewed planning wait not to replay on reconnect")
	}
	if len(delta.reviews) != 0 {
		t.Fatalf("expected no replayed planning review, got %#v", delta.reviews)
	}
}

func TestGraphAdapterSyncsLoopStateIntoGraphState(t *testing.T) {
	loop := NewAgentLoop(Deps{
		Security: &fakeSecurityChecker{
			pending: []ApprovalSnapshot{{
				ID:        "approval-1",
				ToolName:  "write_file",
				Args:      map[string]any{"path": "a.go"},
				Reason:    "needs confirmation",
				Requested: time.Unix(123, 0),
			}},
		},
	})
	loop.mode = ModePermissions{Name: "agentic", SelfReview: true}
	loop.outputContinuations = 2
	loop.compactCount = 1
	loop.ephemerals = []string{"remember this"}
	loop.pendingMedia = []map[string]string{{"type": "image_url", "url": "https://example.com/image.png", "path": "/tmp/image.png"}}
	loop.activeSkills = map[string]string{"git": "skill content"}
	loop.task.RecordBoundary("write feature", "plan", "running", "implement runtime")
	loop.task.PlanModified = true
	loop.task.CurrentStep = 4
	loop.task.ArtifactLastStep["plan.md"] = 2
	loop.task.SkillLoaded = "git"
	loop.task.SkillPath = "/skills/git"
	loop.guard.SetForceToolName("notify_user")
	loop.SetActiveBarrier(NewSubagentBarrierFromState(loop, nil, graphruntime.BarrierState{
		ID:           "barrier-1",
		PendingCount: 1,
		Members: []graphruntime.BarrierMemberState{{
			RunID:    "run-a",
			TaskName: "task a",
			Status:   "running",
		}},
	}))
	loop.history = []llm.Message{{
		Role:      "assistant",
		Content:   "draft answer",
		Reasoning: "analysis",
		Attachments: []llm.Attachment{
			{Path: "/tmp/example.txt"},
		},
		ToolCalls: []llm.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: llm.ToolCallFunc{
				Name:      "write_file",
				Arguments: `{"path":"a.go"}`,
			},
		}},
	}}

	adapter := newGraphLoopAdapter(loop)
	state := &graphruntime.TurnState{}
	exec := &graphruntime.ExecutionState{ObservedState: StateGenerate.String()}

	adapter.syncToGraphState(state, exec)

	if state.Mode != "chat" {
		t.Fatalf("expected loop default mode to sync as chat, got %q", state.Mode)
	}

	state.Mode = "agentic"
	adapter.syncFromGraphState(state, exec)
	adapter.syncToGraphState(state, exec)

	if state.Mode != "agentic" {
		t.Fatalf("expected mode sync, got %q", state.Mode)
	}
	if state.Task.Summary != "implement runtime" {
		t.Fatalf("unexpected task summary: %q", state.Task.Summary)
	}
	if state.Task.Mode != "plan" {
		t.Fatalf("unexpected task mode: %q", state.Task.Mode)
	}
	if !state.Task.PlanModified || state.Task.CurrentStep != 4 {
		t.Fatalf("unexpected task execution state: %#v", state.Task)
	}
	if got := state.Task.ArtifactLastStep["plan.md"]; got != 2 {
		t.Fatalf("unexpected artifact last step: %#v", state.Task.ArtifactLastStep)
	}
	if state.Task.SkillLoaded != "git" || state.Task.SkillPath != "/skills/git" {
		t.Fatalf("unexpected skill state: %#v", state.Task)
	}
	if len(state.Ephemerals) != 1 || state.Ephemerals[0] != "remember this" {
		t.Fatalf("unexpected ephemerals: %#v", state.Ephemerals)
	}
	if len(state.PendingMedia) != 1 || state.PendingMedia[0]["path"] != "/tmp/image.png" {
		t.Fatalf("unexpected pending media: %#v", state.PendingMedia)
	}
	if len(state.Attachments) != 1 || state.Attachments[0] != "/tmp/example.txt" {
		t.Fatalf("unexpected attachments: %#v", state.Attachments)
	}
	if len(state.ToolCalls) != 1 || state.ToolCalls[0].Name != "write_file" {
		t.Fatalf("unexpected tool calls: %#v", state.ToolCalls)
	}
	if state.OutputDraft != "draft answer" {
		t.Fatalf("unexpected output draft: %q", state.OutputDraft)
	}
	if exec.Continuation.Count != 2 {
		t.Fatalf("unexpected continuation count: %d", exec.Continuation.Count)
	}
	if exec.PendingApproval == nil || exec.PendingApproval.ID != "approval-1" {
		t.Fatalf("expected pending approval to sync, got %#v", exec.PendingApproval)
	}
	if got := exec.PendingApproval.Args["path"]; got != "a.go" {
		t.Fatalf("expected approval args to sync, got %#v", exec.PendingApproval.Args)
	}
	if exec.PendingBarrier == nil || exec.PendingBarrier.ID != "barrier-1" {
		t.Fatalf("expected pending barrier to sync, got %#v", exec.PendingBarrier)
	}
	if exec.ObservedState != StateGenerate.String() {
		t.Fatalf("expected observed state to remain runtime-owned, got %q", exec.ObservedState)
	}
	if state.ForceNextTool != "notify_user" {
		t.Fatalf("expected force tool sync, got %q", state.ForceNextTool)
	}
	if got := state.ActiveSkills["git"]; got != "skill content" {
		t.Fatalf("expected active skill sync, got %#v", state.ActiveSkills)
	}
	if !state.Reflection.Required {
		t.Fatal("expected reflection requirement to sync from mode")
	}
	if len(state.Orchestration.Events) != 0 {
		t.Fatalf("expected no orchestration events by default, got %#v", state.Orchestration.Events)
	}
}

func TestGraphAdapterSyncToGraphState_PreservesOrchestrationEvents(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	loop.recordBarrierProgress("run-child", "barrier-1", "completed")

	adapter := newGraphLoopAdapter(loop)
	state := &graphruntime.TurnState{}
	exec := &graphruntime.ExecutionState{}

	adapter.syncToGraphState(state, exec)

	if len(state.Orchestration.Events) != 1 {
		t.Fatalf("expected orchestration events to sync, got %#v", state.Orchestration.Events)
	}
	if state.Orchestration.Events[0].Type != "barrier.member_completed" {
		t.Fatalf("unexpected orchestration event: %#v", state.Orchestration.Events[0])
	}
	if state.Orchestration.Events[0].BarrierID != "barrier-1" {
		t.Fatalf("expected barrier id in orchestration event, got %#v", state.Orchestration.Events[0])
	}
	if state.Orchestration.Events[0].Kind != "barrier" || state.Orchestration.Events[0].Source != "barrier" || state.Orchestration.Events[0].Trigger != "member_completed" {
		t.Fatalf("expected structured barrier event fields, got %#v", state.Orchestration.Events[0])
	}
}

func TestGraphAdapterSyncToGraphState_SelectsLatestPendingApproval(t *testing.T) {
	loop := NewAgentLoop(Deps{
		Security: &fakeSecurityChecker{
			pending: []ApprovalSnapshot{
				{
					ID:        "approval-old",
					ToolName:  "write_file",
					Args:      map[string]any{"path": "old.go"},
					Reason:    "old",
					Requested: time.Unix(100, 0),
				},
				{
					ID:        "approval-new",
					ToolName:  "edit_file",
					Args:      map[string]any{"path": "new.go"},
					Reason:    "new",
					Requested: time.Unix(200, 0),
				},
			},
		},
	})

	adapter := newGraphLoopAdapter(loop)
	state := &graphruntime.TurnState{}
	exec := &graphruntime.ExecutionState{}

	adapter.syncToGraphState(state, exec)

	if exec.PendingApproval == nil {
		t.Fatal("expected pending approval to sync")
	}
	if exec.PendingApproval.ID != "approval-new" {
		t.Fatalf("expected latest approval to sync, got %#v", exec.PendingApproval)
	}
	if got := exec.PendingApproval.Args["path"]; got != "new.go" {
		t.Fatalf("expected latest approval args to sync, got %#v", exec.PendingApproval.Args)
	}
}

func TestGraphAdapterSyncFromGraphState_AppliesExecutionMetadataToRunState(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	adapter := newGraphLoopAdapter(loop)
	state := &graphruntime.TurnState{}
	exec := &graphruntime.ExecutionState{
		ObservedState:     StateCompact.String(),
		TurnSteps:         3,
		MaxTokens:         1024,
		ExcludedProviders: []string{"provider-b"},
		Retry: graphruntime.RetryState{
			Count:        2,
			LastProvider: "provider-a",
		},
	}

	adapter.syncFromGraphState(state, exec)

	if adapter.rs.exec != exec {
		t.Fatal("expected run state to bind execution state directly")
	}
	if adapter.rs.retryCount() != 2 {
		t.Fatalf("expected retries sync from execution state, got %d", adapter.rs.retryCount())
	}
	if adapter.rs.stepCount() != 3 {
		t.Fatalf("expected turn steps sync from execution state, got %d", adapter.rs.stepCount())
	}
	if adapter.rs.maxTokens() != 1024 {
		t.Fatalf("expected max tokens sync from execution state, got %d", adapter.rs.maxTokens())
	}
	if got := adapter.rs.excludedProviderList(); len(got) != 1 || got[0] != "provider-b" {
		t.Fatalf("expected excluded providers sync from execution state, got %#v", got)
	}
	if adapter.rs.lastProvider() != "provider-a" {
		t.Fatalf("expected provider sync from execution state, got %q", adapter.rs.lastProvider())
	}
}

func TestGraphAdapterSyncFromGraphState_ClearsPendingWakeWhenSnapshotIsFalse(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	loop.pendingWake.Store(true)
	adapter := newGraphLoopAdapter(loop)

	adapter.syncFromGraphState(&graphruntime.TurnState{}, &graphruntime.ExecutionState{
		PendingWake: false,
	})

	if loop.pendingWake.Load() {
		t.Fatal("expected pending wake to clear from execution state snapshot")
	}
}

func TestGraphAdapterSyncFromGraphState_RestoresPendingApproval(t *testing.T) {
	sec := &fakeSecurityChecker{}
	loop := NewAgentLoop(Deps{Security: sec})
	adapter := newGraphLoopAdapter(loop)

	adapter.syncFromGraphState(&graphruntime.TurnState{}, &graphruntime.ExecutionState{
		Status:     graphruntime.NodeStatusWait,
		WaitReason: graphruntime.WaitReasonApproval,
		PendingApproval: &graphruntime.ApprovalState{
			ID:          "approval-restore",
			ToolName:    "write_file",
			Args:        map[string]any{"path": "restored.go"},
			Reason:      "needs confirmation",
			RequestedAt: time.Unix(456, 0),
		},
	})

	if len(sec.restored) != 1 || sec.restored[0].ID != "approval-restore" {
		t.Fatalf("expected approval restore from snapshot, got %#v", sec.restored)
	}
	if got := sec.restored[0].Args["path"]; got != "restored.go" {
		t.Fatalf("expected approval args restore, got %#v", sec.restored[0].Args)
	}
}

func TestApprovePending_RestoresResolvesAndClearsSnapshot(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	sec := &fakeSecurityChecker{}
	loop := NewAgentLoop(Deps{Security: sec, SnapshotStore: store})

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-approval-resolve",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState:    graphruntime.TurnState{RunID: "run-approval-resolve"},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:       "approval-resolve",
				ToolName: "write_file",
				Args:     map[string]any{"path": "approved.go"},
				Reason:   "needs confirmation",
			},
		},
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save approval wait snapshot: %v", err)
	}

	handled, err := loop.ApprovePending(context.Background(), "approval-resolve", true)
	if err != nil {
		t.Fatalf("ApprovePending error: %v", err)
	}
	if !handled {
		t.Fatal("expected approval to be handled")
	}
	if len(sec.restored) != 1 || sec.restored[0].ID != "approval-resolve" {
		t.Fatalf("expected approval restore before resolve, got %#v", sec.restored)
	}
	if len(sec.resolved) != 1 || sec.resolved[0] != "approval-resolve" {
		t.Fatalf("expected approval resolve, got %#v", sec.resolved)
	}
	if len(sec.cleaned) != 1 || sec.cleaned[0] != "approval-resolve" {
		t.Fatalf("expected approval cleanup, got %#v", sec.cleaned)
	}

	loaded, err := store.LoadLatestBySession(context.Background(), loop.SessionID())
	if err != nil {
		t.Fatalf("load snapshot after approve: %v", err)
	}
	if loaded.ExecutionState.PendingApproval != nil {
		t.Fatalf("expected snapshot approval cleared, got %#v", loaded.ExecutionState.PendingApproval)
	}
	if loaded.ExecutionState.WaitReason != graphruntime.WaitReasonNone {
		t.Fatalf("expected wait reason cleared, got %q", loaded.ExecutionState.WaitReason)
	}
}

func TestGraphAdapterSyncFromGraphState_UsesBoundaryTaskState(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	adapter := newGraphLoopAdapter(loop)

	adapter.syncFromGraphState(&graphruntime.TurnState{
		PendingMedia:  []map[string]string{{"type": "image_url", "path": "/tmp/restored.png"}},
		ForceNextTool: "notify_user",
		ActiveSkills:  map[string]string{"git": "skill content"},
		Task: graphruntime.TaskState{
			Name:             "task name",
			Mode:             "planning",
			Status:           "running",
			Summary:          "recover plan summary",
			PlanModified:     true,
			CurrentStep:      9,
			ArtifactLastStep: map[string]int{"plan.md": 4},
			SkillLoaded:      "git",
			SkillPath:        "/skills/git",
		},
	}, &graphruntime.ExecutionState{})

	if loop.task.Name != "task name" {
		t.Fatalf("expected task name sync, got %q", loop.task.Name)
	}
	if loop.task.Status != "running" {
		t.Fatalf("expected task status sync, got %q", loop.task.Status)
	}
	if loop.task.Mode != "planning" {
		t.Fatalf("expected task mode sync, got %q", loop.task.Mode)
	}
	if loop.task.Summary != "recover plan summary" {
		t.Fatalf("expected task summary sync, got %q", loop.task.Summary)
	}
	if !loop.task.PlanModified || loop.task.CurrentStep != 9 {
		t.Fatalf("expected task execution metadata sync, got %#v", loop.task)
	}
	if got := loop.task.ArtifactLastStep["plan.md"]; got != 4 {
		t.Fatalf("expected artifact last step sync, got %#v", loop.task.ArtifactLastStep)
	}
	if loop.task.SkillLoaded != "git" || loop.task.SkillPath != "/skills/git" {
		t.Fatalf("expected skill sync, got %#v", loop.task)
	}
	if len(loop.pendingMedia) != 1 || loop.pendingMedia[0]["path"] != "/tmp/restored.png" {
		t.Fatalf("expected pending media sync, got %#v", loop.pendingMedia)
	}
	if loop.guard.PeekForceToolName() != "notify_user" {
		t.Fatalf("expected force tool sync, got %q", loop.guard.PeekForceToolName())
	}
	if got := loop.activeSkills["git"]; got != "skill content" {
		t.Fatalf("expected active skill sync, got %#v", loop.activeSkills)
	}
}

func TestGraphAdapterSyncToGraphState_ClearsStaleResponseArtifacts(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	adapter := newGraphLoopAdapter(loop)
	state := &graphruntime.TurnState{
		LastLLMResponse: graphruntime.LLMResponseState{
			Content:   "stale",
			Reasoning: "stale",
			Provider:  "stale-provider",
		},
		Attachments: []string{"/tmp/stale.txt"},
		ToolCalls: []graphruntime.ToolCallState{{
			ID:   "stale-call",
			Name: "write_file",
		}},
		ToolResults: []graphruntime.ToolResultState{{
			CallID: "stale-call",
			Name:   "write_file",
			Output: "stale",
		}},
		OutputDraft: "stale draft",
	}

	adapter.syncToGraphState(state, &graphruntime.ExecutionState{})

	if state.LastLLMResponse != (graphruntime.LLMResponseState{}) {
		t.Fatalf("expected stale llm response to clear, got %#v", state.LastLLMResponse)
	}
	if len(state.Attachments) != 0 {
		t.Fatalf("expected stale attachments to clear, got %#v", state.Attachments)
	}
	if len(state.ToolCalls) != 0 {
		t.Fatalf("expected stale tool calls to clear, got %#v", state.ToolCalls)
	}
	if len(state.ToolResults) != 0 {
		t.Fatalf("expected stale tool results to clear, got %#v", state.ToolResults)
	}
	if state.OutputDraft != "" {
		t.Fatalf("expected stale output draft to clear, got %q", state.OutputDraft)
	}
}

func TestGraphAdapterSyncToGraphState_MapsTrailingToolMessagesToToolResults(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	loop.history = []llm.Message{
		{
			Role:      "assistant",
			Content:   `{"decision":"tool","reason":"need edit"}`,
			Reasoning: "thinking",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: llm.ToolCallFunc{
					Name:      "write_file",
					Arguments: `{"path":"a.go"}`,
				},
			}},
		},
		{
			Role:       "tool",
			Content:    "ok",
			ToolCallID: "call-1",
		},
	}

	adapter := newGraphLoopAdapter(loop)
	state := &graphruntime.TurnState{}
	adapter.syncToGraphState(state, &graphruntime.ExecutionState{OutputSchemaName: "reflection.review.v1"})

	if len(state.ToolResults) != 1 {
		t.Fatalf("expected one tool result, got %#v", state.ToolResults)
	}
	if state.ToolResults[0].CallID != "call-1" || state.ToolResults[0].Name != "write_file" {
		t.Fatalf("unexpected tool result identity: %#v", state.ToolResults[0])
	}
	if state.ToolResults[0].Output != "ok" {
		t.Fatalf("unexpected tool result output: %#v", state.ToolResults[0])
	}
	if state.OutputDraft != `{"decision":"tool","reason":"need edit"}` {
		t.Fatalf("expected output draft to track assistant draft, got %q", state.OutputDraft)
	}
	if state.StructuredOutput.SchemaName != "reflection.review.v1" {
		t.Fatalf("expected structured output schema to sync, got %#v", state.StructuredOutput)
	}
}

func TestGraphAdapterExecuteNode_SyncsTurnStateBeforeReturningError(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	adapter := newGraphLoopAdapter(loop)
	state := &graphruntime.TurnState{}
	exec := &graphruntime.ExecutionState{}

	wantErr := errors.New("boom")
	result, err := adapter.executeNode(state, exec, func() (graphruntime.NodeResult, error) {
		loop.task.RecordBoundary("write feature", "plan", "running", "latest summary")
		loop.guard.SetForceToolName("notify_user")
		loop.ephemerals = []string{"keep me"}
		return loop.finishWith(StateError, graphruntime.NodeStatusFatal), wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("expected executeNode to return original error, got %v", err)
	}
	if result.ObservedState != StateError.String() {
		t.Fatalf("expected result observed state to stay error, got %#v", result)
	}
	if state.Task.Summary != "latest summary" {
		t.Fatalf("expected task summary to sync on error, got %#v", state.Task)
	}
	if state.ForceNextTool != "notify_user" {
		t.Fatalf("expected force tool to sync on error, got %q", state.ForceNextTool)
	}
	if len(state.Ephemerals) != 1 || state.Ephemerals[0] != "keep me" {
		t.Fatalf("expected ephemerals to sync on error, got %#v", state.Ephemerals)
	}
}

func TestReflectionNode_DefaultsToAcceptAndRoutesDone(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	node := reflectionNode{adapter: newGraphLoopAdapter(loop)}
	state := &graphruntime.TurnState{
		OutputDraft: `plain draft`,
		Reflection: graphruntime.ReflectionState{
			Required: true,
		},
	}

	result, err := node.Execute(context.Background(), &graphruntime.RuntimeContext{
		Execution: &graphruntime.ExecutionState{},
	}, state)
	if err != nil {
		t.Fatalf("reflection execute error: %v", err)
	}
	if result.RouteKey != graphRouteDone {
		t.Fatalf("expected reflect accept to route done, got %#v", result)
	}
	if state.StructuredOutput.SchemaName != graphReflectionSchema || !state.StructuredOutput.Valid {
		t.Fatalf("expected structured reflection output, got %#v", state.StructuredOutput)
	}
	if state.Reflection.LastReview == "" {
		t.Fatal("expected reflection last review to be set")
	}
	if !state.Intelligence.Review.Valid || state.Intelligence.Review.Decision != "accept" {
		t.Fatalf("expected reflection decision to populate intelligence state, got %#v", state.Intelligence.Review)
	}
}

func TestReflectionNode_ReviseRoutesBackToGenerate(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	node := reflectionNode{adapter: newGraphLoopAdapter(loop)}
	state := &graphruntime.TurnState{
		OutputDraft: `draft`,
		StructuredOutput: graphruntime.StructuredOutputState{
			SchemaName: graphReflectionSchema,
			RawJSON:    `{"decision":"revise","reason":"needs tighter answer"}`,
			Valid:      true,
		},
	}

	result, err := node.Execute(context.Background(), &graphruntime.RuntimeContext{
		Execution: &graphruntime.ExecutionState{OutputSchemaName: graphReflectionSchema},
	}, state)
	if err != nil {
		t.Fatalf("reflection execute error: %v", err)
	}
	if result.RouteKey != graphRouteGenerate {
		t.Fatalf("expected reflect revise to route generate, got %#v", result)
	}
	if len(loop.ephemerals) != 1 || loop.ephemerals[0] != "Self-review requested revision: needs tighter answer" {
		t.Fatalf("expected revise reason injected as ephemeral, got %#v", loop.ephemerals)
	}
	if !state.Intelligence.Review.Valid || state.Intelligence.Review.Decision != "revise" {
		t.Fatalf("expected revise decision in intelligence state, got %#v", state.Intelligence.Review)
	}
}

func TestHandleDone_CompletesWithoutPendingWake(t *testing.T) {
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{Delta: delta})
	rs := &runState{}

	result, err := loop.handleDone(context.Background(), rs)
	if err != nil {
		t.Fatalf("handleDone error: %v", err)
	}
	if result.RouteKey != graphRouteComplete || result.ObservedState != StateIdle.String() {
		t.Fatalf("expected done to hand off to complete node, got %#v", result)
	}
	if delta.completed != 0 {
		t.Fatalf("expected done node to defer completion side effects, got %d", delta.completed)
	}
}

func TestHandleDone_RoutesToMergeWhenPendingWakeExists(t *testing.T) {
	loop := NewAgentLoop(Deps{Delta: &mockDeltaSink{}})
	loop.pendingWake.Store(true)

	result, err := loop.handleDone(context.Background(), &runState{})
	if err != nil {
		t.Fatalf("handleDone error: %v", err)
	}
	if result.RouteKey != graphRouteMerge || result.ObservedState != "merge" {
		t.Fatalf("expected done to route merge when pending wake exists, got %#v", result)
	}
	if !loop.pendingWake.Load() {
		t.Fatal("expected pending wake to remain set until merge node consumes it")
	}
}

func TestHandleDone_RoutesToEvaluateWhenEvoEnabled(t *testing.T) {
	loop := NewAgentLoop(Deps{
		Delta:        &mockDeltaSink{},
		EvoEvaluator: &EvoEvaluator{},
	})
	loop.mode = ModePermissions{Name: "agentic-evo", EvoEnabled: true}
	loop.traceCollector = NewTraceCollectorHook(nil)

	result, err := loop.handleDone(context.Background(), &runState{})
	if err != nil {
		t.Fatalf("handleDone error: %v", err)
	}
	if result.RouteKey != graphRouteEvaluate || result.ObservedState != "evaluate" {
		t.Fatalf("expected done to route evaluate, got %#v", result)
	}
}

func TestHandleComplete_FiresCompletionSideEffects(t *testing.T) {
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{Delta: delta})

	result, err := loop.handleComplete(context.Background(), &runState{})
	if err != nil {
		t.Fatalf("handleComplete error: %v", err)
	}
	if result.Status != graphruntime.NodeStatusComplete || result.ObservedState != StateIdle.String() {
		t.Fatalf("expected complete node to finish run, got %#v", result)
	}
	if delta.completed != 1 {
		t.Fatalf("expected complete node to fire completion once, got %d", delta.completed)
	}
}

func TestHandleEvaluate_WithoutTraceRoutesComplete(t *testing.T) {
	loop := NewAgentLoop(Deps{
		Delta:        &mockDeltaSink{},
		EvoEvaluator: &EvoEvaluator{},
	})
	loop.traceCollector = NewTraceCollectorHook(nil)
	loop.mode = ModePermissions{Name: "agentic-evo", EvoEnabled: true}
	loop.history = []llm.Message{{Role: "user", Content: "ship it"}}

	result, err := loop.handleEvaluate(context.Background(), &runState{})
	if err != nil {
		t.Fatalf("handleEvaluate error: %v", err)
	}
	if result.RouteKey != graphRouteComplete {
		t.Fatalf("expected evaluate without trace to route complete, got %#v", result)
	}
}

func TestHandleRepair_ReentersPrepareWithinGraph(t *testing.T) {
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{Delta: delta})
	loop.setRepairDecision(graphruntime.RepairState{
		Strategy:  StrategyToolSwap,
		Allowed:   true,
		Ephemeral: "use a different tool",
	})
	rs := &runState{}

	result, err := loop.handleRepair(context.Background(), rs)
	if err != nil {
		t.Fatalf("handleRepair error: %v", err)
	}
	if result.RouteKey != graphRoutePrepare || result.ObservedState != "repair" {
		t.Fatalf("expected repair to route prepare, got %#v", result)
	}
	if delta.autoWake != 1 {
		t.Fatalf("expected auto wake event, got %d", delta.autoWake)
	}
	if len(loop.ephemerals) != 1 || loop.ephemerals[0] != "use a different tool" {
		t.Fatalf("expected repair ephemeral injected, got %#v", loop.ephemerals)
	}
	if got := loop.intelligenceSnapshot().Repair; !got.Attempted || !got.Allowed {
		t.Fatalf("expected repair state marked attempted, got %#v", got)
	}
}

func TestMergeNode_ConsumesPendingWakeAndReentersPrepare(t *testing.T) {
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{Delta: delta})
	loop.pendingWake.Store(true)
	node := mergeNode{adapter: newGraphLoopAdapter(loop)}
	state := &graphruntime.TurnState{
		Orchestration: graphruntime.OrchestrationState{
			PendingMerge:   true,
			LastWakeSource: "barrier",
			ActiveBarrier:  &graphruntime.BarrierState{ID: "bar-1", Finalized: true, PendingCount: 1},
		},
	}

	result, err := node.Execute(context.Background(), &graphruntime.RuntimeContext{
		Execution: &graphruntime.ExecutionState{},
	}, state)
	if err != nil {
		t.Fatalf("merge execute error: %v", err)
	}
	if result.RouteKey != graphRoutePrepare || result.ObservedState != "merge" {
		t.Fatalf("expected merge to reenter prepare after wake, got %#v", result)
	}
	if delta.autoWake != 1 {
		t.Fatalf("expected auto wake event, got %d", delta.autoWake)
	}
	if loop.pendingWake.Load() {
		t.Fatal("expected merge node to consume pending wake")
	}
	history := loop.GetHistory()
	if len(history) != 1 || history[0].Role != "user" || history[0].Content != "" {
		t.Fatalf("expected merge node to append empty user handoff message, got %#v", history)
	}
	if state.Orchestration.PendingMerge {
		t.Fatalf("expected merge node to clear pending merge state, got %#v", state.Orchestration)
	}
	if state.Orchestration.ActiveBarrier == nil || state.Orchestration.ActiveBarrier.PendingCount != 0 {
		t.Fatalf("expected merge node to mark barrier drained, got %#v", state.Orchestration.ActiveBarrier)
	}
}

func TestHydratePendingBarrier_RestoresFromSnapshot(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	loop := NewAgentLoop(Deps{SnapshotStore: store})

	snap := &graphruntime.RunSnapshot{
		RunID:        "run-1",
		SessionID:    "",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState:    graphruntime.TurnState{RunID: "run-1"},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonBarrier,
			PendingBarrier: &graphruntime.BarrierState{
				ID:           "barrier-restore",
				PendingCount: 1,
				Members: []graphruntime.BarrierMemberState{{
					RunID:    "sub-1",
					TaskName: "research",
					Status:   "running",
				}},
			},
		},
	}
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	if err := loop.hydratePendingBarrier(context.Background(), "run-1"); err != nil {
		t.Fatalf("hydrate pending barrier: %v", err)
	}

	restored := loop.activeBarrierSnapshot()
	if restored == nil {
		t.Fatal("expected active barrier after hydration")
	}
	if restored.ID != "barrier-restore" {
		t.Fatalf("unexpected barrier id: %s", restored.ID)
	}
	if len(restored.Members) != 1 || restored.Members[0].RunID != "sub-1" {
		t.Fatalf("unexpected restored members: %#v", restored.Members)
	}
}

func TestHydratePendingBarrier_SkipsWakeReadyBarrierSnapshot(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	loop := NewAgentLoop(Deps{SnapshotStore: store})
	loop.SetActiveBarrier(NewSubagentBarrierFromState(loop, nil, graphruntime.BarrierState{
		ID:           "stale-barrier",
		PendingCount: 1,
	}))

	snap := &graphruntime.RunSnapshot{
		RunID:        "run-wake",
		SessionID:    "",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState:    graphruntime.TurnState{RunID: "run-wake"},
		ExecutionState: graphruntime.ExecutionState{
			Status:      graphruntime.NodeStatusWait,
			WaitReason:  graphruntime.WaitReasonBarrier,
			PendingWake: true,
			PendingBarrier: &graphruntime.BarrierState{
				ID:           "barrier-should-not-restore",
				PendingCount: 1,
			},
		},
	}
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	if err := loop.hydratePendingBarrier(context.Background(), "run-wake"); err != nil {
		t.Fatalf("hydrate pending barrier: %v", err)
	}

	if restored := loop.activeBarrierSnapshot(); restored != nil {
		t.Fatalf("expected wake-ready barrier snapshot to clear active barrier, got %#v", restored)
	}
}

func TestLatestWaitSnapshotView_ReconnectAction(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	loop := NewAgentLoop(Deps{SnapshotStore: store})

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-wait",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState:    graphruntime.TurnState{RunID: "run-wait"},
		ExecutionState: graphruntime.ExecutionState{
			Status:      graphruntime.NodeStatusWait,
			WaitReason:  graphruntime.WaitReasonApproval,
			PendingWake: false,
		},
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save waiting snapshot: %v", err)
	}

	wait, err := loop.latestWaitSnapshotView(context.Background())
	if err != nil {
		t.Fatalf("latestWaitSnapshotView error: %v", err)
	}
	if action := wait.reconnectAction(); action != reconnectActionNone {
		t.Fatalf("expected approval wait without payload to stay non-reconnectable, got %q", action)
	}

	waiting.ExecutionState.PendingWake = true
	waiting.ExecutionState.WaitReason = graphruntime.WaitReasonApproval
	waiting.ExecutionState.PendingApproval = &graphruntime.ApprovalState{
		ID:       "approval-wait",
		ToolName: "write_file",
		Args:     map[string]any{"path": "replay.go"},
		Reason:   "needs confirmation",
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save mismatched wake snapshot: %v", err)
	}

	wait, err = loop.latestWaitSnapshotView(context.Background())
	if err != nil {
		t.Fatalf("latestWaitSnapshotView error on mismatched wake: %v", err)
	}
	if action := wait.reconnectAction(); action != reconnectActionReplayApproval {
		t.Fatalf("expected approval wake snapshot to replay approval, got %q", action)
	}

	waiting.ExecutionState.WaitReason = graphruntime.WaitReasonUserInput
	waiting.TurnState.Intelligence.Planning = graphruntime.PlanningState{
		Required:       true,
		ReviewRequired: true,
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save user-input wait snapshot: %v", err)
	}

	wait, err = loop.latestWaitSnapshotView(context.Background())
	if err != nil {
		t.Fatalf("latestWaitSnapshotView error on user-input wait: %v", err)
	}
	if action := wait.reconnectAction(); action != reconnectActionReplayPlan {
		t.Fatalf("expected planning user-input wait to replay plan review, got %q", action)
	}

	waiting.ExecutionState.WaitReason = graphruntime.WaitReasonExternal
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save external wait snapshot: %v", err)
	}

	wait, err = loop.latestWaitSnapshotView(context.Background())
	if err != nil {
		t.Fatalf("latestWaitSnapshotView error on external wait: %v", err)
	}
	if action := wait.reconnectAction(); action != reconnectActionNone {
		t.Fatalf("expected external wait snapshot to stay non-reconnectable, got %q", action)
	}

	waiting.ExecutionState.WaitReason = graphruntime.WaitReasonBarrier
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save wake-ready snapshot: %v", err)
	}

	wait, err = loop.latestWaitSnapshotView(context.Background())
	if err != nil {
		t.Fatalf("latestWaitSnapshotView error after wake: %v", err)
	}
	if action := wait.reconnectAction(); action != reconnectActionResumeBarrier {
		t.Fatalf("expected wake-ready barrier snapshot to resume, got %q", action)
	}
	if runID, ok := wait.autoResumeRunID(); !ok || runID != "run-wait" {
		t.Fatalf("expected wake-ready wait snapshot to be resumable, got runID=%q ok=%v", runID, ok)
	}
}

func TestHandleReconnect_ReplaysApprovalWaitWithoutResume(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{SnapshotStore: store, Delta: delta})

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-approval-reconnect",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState:    graphruntime.TurnState{RunID: "run-approval-reconnect"},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:       "approval-reconnect",
				ToolName: "write_file",
				Args:     map[string]any{"path": "reconnect.go"},
				Reason:   "needs confirmation",
			},
		},
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save approval wait snapshot: %v", err)
	}

	handled, err := loop.HandleReconnect(context.Background())
	if err != nil {
		t.Fatalf("HandleReconnect error: %v", err)
	}
	if !handled {
		t.Fatal("expected approval reconnect to be handled")
	}
	if len(delta.approvals) != 1 {
		t.Fatalf("expected one replayed approval, got %#v", delta.approvals)
	}
	if got := delta.approvals[0].ID; got != "approval-reconnect" {
		t.Fatalf("unexpected approval id: %q", got)
	}
	if got := delta.approvals[0].Args["path"]; got != "reconnect.go" {
		t.Fatalf("unexpected approval args: %#v", delta.approvals[0].Args)
	}
}

func TestRunWithoutAcquire_UsesReconnectContractForEmptyMessage(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	delta := &mockDeltaSink{}
	loop := NewAgentLoop(Deps{SnapshotStore: store, Delta: delta})

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-entry-reconnect",
		SessionID:    loop.SessionID(),
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState:    graphruntime.TurnState{RunID: "run-entry-reconnect"},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:       "approval-entry",
				ToolName: "write_file",
				Args:     map[string]any{"path": "entry.go"},
				Reason:   "needs confirmation",
			},
		},
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save approval wait snapshot: %v", err)
	}

	if err := loop.RunWithoutAcquire(context.Background(), ""); err != nil {
		t.Fatalf("RunWithoutAcquire should handle reconnect path: %v", err)
	}
	if len(delta.approvals) != 1 {
		t.Fatalf("expected one approval replay from runEntry, got %#v", delta.approvals)
	}
	if got := delta.approvals[0].ID; got != "approval-entry" {
		t.Fatalf("unexpected approval id from runEntry: %q", got)
	}
}

func TestRunWithoutAcquire_EmptyMessageWithoutPendingExecutionErrors(t *testing.T) {
	loop := NewAgentLoop(Deps{SnapshotStore: graphruntime.NewInMemorySnapshotStore(), Delta: &mockDeltaSink{}})

	err := loop.RunWithoutAcquire(context.Background(), "")
	if err == nil {
		t.Fatal("expected empty reconnect without pending execution to error")
	}
	if got := err.Error(); got != "no pending execution for session "+loop.SessionID() {
		t.Fatalf("unexpected reconnect error: %q", got)
	}
}
