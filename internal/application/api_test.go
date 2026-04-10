package application

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	dmodel "github.com/ngoclaw/ngoagent/internal/domain/model"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type stubSessionRepo struct {
	sessions []service.ConversationInfo
}

type stubHistoryQuery struct {
	exports map[string][]service.HistoryExport
}

type fakeModelRouter struct{}

func (fakeModelRouter) CurrentModel() string { return "gpt-4.1" }
func (fakeModelRouter) Resolve(string) (dmodel.Provider, error) {
	return nil, nil
}
func (fakeModelRouter) ResolveWithExclusions(string, []string) (dmodel.Provider, string, error) {
	return nil, "", nil
}

func setLoopTokenUsage(loop *service.AgentLoop, byModel map[string]service.ModelUsage) {
	loopValue := reflect.ValueOf(loop).Elem()
	tokenTrackerField := loopValue.FieldByName("tokenTracker")
	tokenTrackerPtr := reflect.NewAt(tokenTrackerField.Type(), unsafe.Pointer(tokenTrackerField.UnsafeAddr())).Elem()
	for model, usage := range byModel {
		priceInput := 0.0
		priceOutput := 0.0
		if usage.PromptTokens > 0 {
			priceInput = usage.CostUSD * 1000 / float64(usage.PromptTokens)
		} else if usage.CompletionTokens > 0 {
			priceOutput = usage.CostUSD * 1000 / float64(usage.CompletionTokens)
		}
		method := tokenTrackerPtr.Addr().MethodByName("RecordAPIUsageWithCost")
		method.Call([]reflect.Value{
			reflect.ValueOf(llm.Usage{
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      usage.TotalTokens,
			}),
			reflect.ValueOf(model),
			reflect.ValueOf(llm.ModelPolicy{
				PriceInput1K:  priceInput,
				PriceOutput1K: priceOutput,
			}),
		})
		for i := 1; i < usage.Calls; i++ {
			method.Call([]reflect.Value{
				reflect.ValueOf(llm.Usage{}),
				reflect.ValueOf(model),
				reflect.ValueOf(llm.ModelPolicy{}),
			})
		}
	}
}

func loopStopRequested(loop *service.AgentLoop) bool {
	loopValue := reflect.ValueOf(loop).Elem()
	stopChField := loopValue.FieldByName("stopCh")
	stopCh := reflect.NewAt(stopChField.Type(), unsafe.Pointer(stopChField.UnsafeAddr())).Elem().Interface().(chan struct{})
	select {
	case <-stopCh:
		return true
	default:
		return false
	}
}

func (s *stubHistoryQuery) LoadAll(sessionID string) ([]service.HistoryExport, error) {
	return append([]service.HistoryExport(nil), s.exports[sessionID]...), nil
}

func (s *stubSessionRepo) CreateConversation(channel, title string) (string, error) { return "", nil }
func (s *stubSessionRepo) ListConversations(limit, offset int) ([]service.ConversationInfo, error) {
	return append([]service.ConversationInfo(nil), s.sessions...), nil
}
func (s *stubSessionRepo) UpdateTitle(id, title string) error { return nil }
func (s *stubSessionRepo) Touch(id string) error              { return nil }
func (s *stubSessionRepo) DeleteConversation(id string) error { return nil }

func TestAgentAPIApprove_RestoresPendingApprovalFromSnapshot(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	secHook := security.NewHook(&config.SecurityConfig{})
	secAdapter := &securityAdapter{hook: secHook}
	brainDir := t.TempDir()

	factory := func(sessionID string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			Security:      secAdapter,
			SnapshotStore: store,
		})
	}

	loopPool := service.NewLoopPool(factory, brainDir)
	sessionID := "session-approval"
	now := time.Unix(1700000000, 0)
	if err := store.Save(context.Background(), &graphruntime.RunSnapshot{
		RunID:     "run-approval",
		SessionID: sessionID,
		Status:    graphruntime.NodeStatusWait,
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:          "approval-1",
				ToolName:    "write_file",
				Args:        map[string]any{"path": "approved.go"},
				Reason:      "needs confirmation",
				RequestedAt: now,
			},
		},
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})

	api := NewAgentAPI(factory("__default__"), loopPool, nil, sessMgr, nil, nil, secHook, nil, nil, nil, nil, nil, nil, brainDir, nil, nil)

	if err := api.Approve("approval-1", true); err != nil {
		t.Fatalf("approve should restore from snapshot: %v", err)
	}

	pending := secHook.ListPending()
	if len(pending) != 0 {
		t.Fatalf("expected pending approval cleanup after approve, got %#v", pending)
	}

	loaded, err := store.LoadLatestBySession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("load snapshot after approve: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected snapshot to remain present after approve")
	}
	if loaded.ExecutionState.PendingApproval != nil {
		t.Fatalf("expected snapshot approval cleared after approve, got %#v", loaded.ExecutionState.PendingApproval)
	}
	if loaded.ExecutionState.WaitReason != graphruntime.WaitReasonNone {
		t.Fatalf("expected wait reason cleared after approve, got %q", loaded.ExecutionState.WaitReason)
	}
}

func TestChatStream_ReplaysPendingApprovalOnReconnect(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	secHook := security.NewHook(&config.SecurityConfig{})
	secAdapter := &securityAdapter{hook: secHook}
	brainDir := t.TempDir()
	sessionID := "session-reconnect"
	now := time.Unix(1700000100, 0)

	factory := func(sessionID string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			Security:      secAdapter,
			SnapshotStore: store,
		})
	}

	if err := store.Save(context.Background(), &graphruntime.RunSnapshot{
		RunID:     "run-reconnect",
		SessionID: sessionID,
		Status:    graphruntime.NodeStatusWait,
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonApproval,
			PendingApproval: &graphruntime.ApprovalState{
				ID:          "approval-reconnect",
				ToolName:    "write_file",
				Args:        map[string]any{"path": "replayed.go"},
				Reason:      "needs confirmation",
				RequestedAt: now,
			},
		},
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	api := NewAgentAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, secHook, nil, nil, nil, nil, nil, nil, brainDir, nil, nil)

	var approvalID, toolName, reason string
	var args map[string]any
	delta := &service.Delta{
		OnApprovalRequestFunc: func(id, tool string, approvalArgs map[string]any, approvalReason string) {
			approvalID = id
			toolName = tool
			args = approvalArgs
			reason = approvalReason
		},
	}

	if err := api.ChatStream(context.Background(), sessionID, "", "", delta); err != nil {
		t.Fatalf("chat reconnect should replay approval request: %v", err)
	}
	if approvalID != "approval-reconnect" || toolName != "write_file" || reason != "needs confirmation" {
		t.Fatalf("unexpected replayed approval event: id=%q tool=%q reason=%q", approvalID, toolName, reason)
	}
	if got := args["path"]; got != "replayed.go" {
		t.Fatalf("unexpected replayed approval args: %#v", args)
	}
}

func TestChatStream_DoesNotReplayClearedApprovalSnapshot(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	secHook := security.NewHook(&config.SecurityConfig{})
	secAdapter := &securityAdapter{hook: secHook}
	brainDir := t.TempDir()
	sessionID := "session-cleared"
	now := time.Unix(1700000200, 0)

	factory := func(sessionID string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			Security:      secAdapter,
			SnapshotStore: store,
		})
	}

	if err := store.Save(context.Background(), &graphruntime.RunSnapshot{
		RunID:     "run-cleared",
		SessionID: sessionID,
		Status:    graphruntime.NodeStatusWait,
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonNone,
		},
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	api := NewAgentAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, secHook, nil, nil, nil, nil, nil, nil, brainDir, nil, nil)

	delta := &service.Delta{
		OnApprovalRequestFunc: func(id, tool string, approvalArgs map[string]any, approvalReason string) {
			t.Fatalf("unexpected approval replay: id=%q tool=%q args=%#v reason=%q", id, tool, approvalArgs, approvalReason)
		},
	}

	if err := api.ChatStream(context.Background(), sessionID, "", "", delta); err == nil {
		t.Fatal("expected cleared approval snapshot to have no pending execution")
	}
}

func TestReviewPlan_RejectsAndResumesPlanningLoop(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	brainDir := t.TempDir()
	sessionID := "session-plan-review"

	factory := func(sessionID string) *service.AgentLoop {
		loop := service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			SnapshotStore: store,
			LLMRouter:     fakeModelRouter{},
		})
		loop.SetPlanMode("plan")
		return loop
	}

	history := &stubHistoryQuery{exports: map[string][]service.HistoryExport{
		sessionID: {{Role: "user", Content: "/plan refactor runtime"}},
	}}

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-plan-review",
		SessionID:    sessionID,
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
			Cursor: graphruntime.ExecutionCursor{
				GraphID:      "agent_loop",
				GraphVersion: "v1alpha1",
				CurrentNode:  "plan",
				Step:         1,
				RouteKey:     "plan",
			},
		},
		UpdatedAt: time.Now(),
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save planning snapshot: %v", err)
	}

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	api := NewAgentAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, nil, history, brainDir, nil, nil)

	if err := api.ReviewPlan(context.Background(), sessionID, false, "split rollout into phases"); err != nil {
		t.Fatalf("ReviewPlan should resume planning loop: %v", err)
	}

	loaded, err := store.LoadLatestBySession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("load snapshot after plan review: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected planning snapshot after review")
	}
	if loaded.Status != graphruntime.NodeStatusWait || loaded.ExecutionState.WaitReason != graphruntime.WaitReasonUserInput {
		t.Fatalf("expected revised plan to re-enter planning wait, got %#v", loaded.ExecutionState)
	}
	planning := loaded.TurnState.Intelligence.Planning
	if planning.ReviewDecision != "" {
		t.Fatalf("expected fresh planning wait after revise cycle, got %#v", planning)
	}
	if !planning.Required || !planning.ReviewRequired {
		t.Fatalf("expected planning to remain required after revise cycle, got %#v", planning)
	}
}

func TestApplyDecision_DelegatesPlanReview(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	brainDir := t.TempDir()
	sessionID := "session-apply-decision"

	factory := func(sessionID string) *service.AgentLoop {
		loop := service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			SnapshotStore: store,
			LLMRouter:     fakeModelRouter{},
		})
		loop.SetPlanMode("plan")
		return loop
	}

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-apply-decision",
		SessionID:    sessionID,
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-apply-decision",
			Intelligence: graphruntime.IntelligenceState{
				Planning: graphruntime.PlanningState{
					Required:       true,
					ReviewRequired: true,
					Trigger:        "mode_force_plan",
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonUserInput,
			Cursor: graphruntime.ExecutionCursor{
				GraphID:      "agent_loop",
				GraphVersion: "v1alpha1",
				CurrentNode:  "plan",
				Step:         1,
				RouteKey:     "plan",
			},
		},
		UpdatedAt: time.Now(),
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save waiting snapshot: %v", err)
	}

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	api := NewAgentAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, nil, nil, brainDir, nil, nil)

	if err := api.ApplyDecision(context.Background(), sessionID, "plan_review", "revise", "needs phasing"); err != nil {
		t.Fatalf("ApplyDecision: %v", err)
	}

	loaded, err := store.LoadLatestBySession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("load snapshot after apply decision: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected snapshot after apply decision")
	}
	if loaded.ExecutionState.WaitReason != graphruntime.WaitReasonUserInput {
		t.Fatalf("expected planning wait after revise cycle, got %#v", loaded.ExecutionState)
	}
}

func TestApplyDecision_InferKindFromPendingDecision(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := persistence.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	brainDir := t.TempDir()
	sessionID := "session-apply-infer"

	factory := func(sessionID string) *service.AgentLoop {
		loop := service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			SnapshotStore: store,
			LLMRouter:     fakeModelRouter{},
		})
		loop.SetPlanMode("plan")
		return loop
	}

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-apply-infer",
		SessionID:    sessionID,
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-apply-infer",
			Intelligence: graphruntime.IntelligenceState{
				Planning: graphruntime.PlanningState{
					Required:       true,
					ReviewRequired: true,
					Trigger:        "mode_force_plan",
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonUserInput,
			Cursor: graphruntime.ExecutionCursor{
				GraphID:      "agent_loop",
				GraphVersion: "v1alpha1",
				CurrentNode:  "plan",
				Step:         1,
				RouteKey:     "plan",
			},
		},
		UpdatedAt: time.Now(),
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save waiting snapshot: %v", err)
	}

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	api := NewAgentAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, nil, nil, brainDir, nil, nil)
	api.SetRuntimeStore(store)

	runs, err := api.ListPendingRuns(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListPendingRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].PendingDecision == nil || runs[0].PendingDecision.Kind != "plan_review" {
		t.Fatalf("expected inferred pending plan decision, got %#v", runs)
	}

	decisions, err := api.ListPendingDecisions(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListPendingDecisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].PendingDecision == nil || decisions[0].PendingDecision.Kind != "plan_review" {
		t.Fatalf("expected pending decision listing, got %#v", decisions)
	}

	if err := api.ApplyDecision(context.Background(), sessionID, "", "revise", "narrow scope"); err != nil {
		t.Fatalf("ApplyDecision infer kind: %v", err)
	}
}

func TestResumeRun_ResumesNamedWaitingRun(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	brainDir := t.TempDir()
	sessionID := "session-resume-run"

	factory := func(sessionID string) *service.AgentLoop {
		loop := service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			SnapshotStore: store,
			LLMRouter:     fakeModelRouter{},
		})
		loop.SetPlanMode("plan")
		return loop
	}

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-resume-explicit",
		SessionID:    sessionID,
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-resume-explicit",
			Intelligence: graphruntime.IntelligenceState{
				Planning: graphruntime.PlanningState{
					Required:       true,
					ReviewRequired: true,
					Trigger:        "mode_force_plan",
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonUserInput,
			Cursor: graphruntime.ExecutionCursor{
				GraphID:      "agent_loop",
				GraphVersion: "v1alpha1",
				CurrentNode:  "plan",
				Step:         1,
				RouteKey:     "plan",
			},
		},
		UpdatedAt: time.Now(),
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save waiting snapshot: %v", err)
	}

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	api := NewAgentAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, nil, nil, brainDir, nil, nil)

	if err := api.ResumeRun(context.Background(), sessionID, "run-resume-explicit"); err != nil {
		t.Fatalf("ResumeRun: %v", err)
	}

	loaded, err := store.LoadLatest(context.Background(), "run-resume-explicit")
	if err != nil {
		t.Fatalf("load resumed snapshot: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected resumed snapshot")
	}
	if loaded.Status != graphruntime.NodeStatusWait || loaded.ExecutionState.WaitReason != graphruntime.WaitReasonUserInput {
		t.Fatalf("expected resumed run to remain in planning wait, got %#v", loaded.ExecutionState)
	}
}

func TestApplyRuntimeIngress_RoutesResumeAndValidatesDecision(t *testing.T) {
	store := graphruntime.NewInMemorySnapshotStore()
	brainDir := t.TempDir()
	sessionID := "session-runtime-ingress"

	factory := func(sessionID string) *service.AgentLoop {
		loop := service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			SnapshotStore: store,
			LLMRouter:     fakeModelRouter{},
		})
		loop.SetPlanMode("plan")
		return loop
	}

	waiting := &graphruntime.RunSnapshot{
		RunID:        "run-ingress",
		SessionID:    sessionID,
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-ingress",
			Intelligence: graphruntime.IntelligenceState{
				Planning: graphruntime.PlanningState{
					Required:       true,
					ReviewRequired: true,
					Trigger:        "mode_force_plan",
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonUserInput,
			Cursor: graphruntime.ExecutionCursor{
				GraphID:      "agent_loop",
				GraphVersion: "v1alpha1",
				CurrentNode:  "plan",
				Step:         1,
				RouteKey:     "plan",
			},
		},
		UpdatedAt: time.Now(),
	}
	if err := store.Save(context.Background(), waiting); err != nil {
		t.Fatalf("save waiting snapshot: %v", err)
	}

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	api := NewAgentAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, nil, nil, brainDir, nil, nil)

	_, err := api.ApplyRuntimeIngress(context.Background(), apitype.RuntimeIngressRequest{
		SessionID: sessionID,
		Ingress: apitype.RuntimeIngressInput{
			Kind: "decision",
		},
	})
	if err == nil {
		t.Fatal("expected validation error for empty decision ingress")
	}

	resumeResp, err := api.ApplyRuntimeIngress(context.Background(), apitype.RuntimeIngressRequest{
		SessionID: sessionID,
		Ingress: apitype.RuntimeIngressInput{
			Kind:  "resume",
			RunID: "run-ingress",
		},
	})
	if err != nil {
		t.Fatalf("ApplyRuntimeIngress resume: %v", err)
	}
	if resumeResp.Status != "accepted" || resumeResp.Ingress.RunID != "run-ingress" {
		t.Fatalf("unexpected resume ingress response: %#v", resumeResp)
	}
}

func TestRetryRun_RestoresPersistedHistoryWithoutCreatingGhostLoop(t *testing.T) {
	sessionID := "session-retry"
	defaultLoop := service.NewAgentLoop(service.Deps{})
	defaultLoop.SetHistory([]llm.Message{
		{Role: "user", Content: "default user"},
		{Role: "assistant", Content: "default answer"},
	})
	pool := service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{})
	}, t.TempDir())
	api := NewAgentAPI(
		defaultLoop,
		pool,
		nil,
		service.NewSessionManager(&stubSessionRepo{sessions: []service.ConversationInfo{{ID: sessionID}}}),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&stubHistoryQuery{exports: map[string][]service.HistoryExport{
			sessionID: {
				{Role: "user", Content: "retry me"},
				{Role: "assistant", Content: "old answer"},
			},
		}},
		"",
		nil,
		nil,
	)

	lastUser, err := api.RetryRun(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("retry run: %v", err)
	}
	if lastUser != "retry me" {
		t.Fatalf("expected restored last user message, got %q", lastUser)
	}
	if got := pool.GetIfExists(sessionID); got != nil {
		t.Fatalf("expected retry to avoid creating a ghost loop, got %#v", got)
	}
	defaultHistory := defaultLoop.GetHistory()
	if len(defaultHistory) != 2 || defaultHistory[0].Content != "default user" || defaultHistory[1].Content != "default answer" {
		t.Fatalf("expected retry not to mutate default loop history, got %#v", defaultHistory)
	}
}

func TestStopRun_DoesNotStopDefaultLoopForMissingSession(t *testing.T) {
	defaultLoop := service.NewAgentLoop(service.Deps{})
	pool := service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{})
	}, t.TempDir())

	api := NewAgentAPI(
		defaultLoop,
		pool,
		nil,
		service.NewSessionManager(&stubSessionRepo{}),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		llm.NewRouter(nil),
		nil,
		"",
		nil,
		nil,
	)

	api.StopRun("missing-session")

	if loopStopRequested(defaultLoop) {
		t.Fatal("expected missing session stop not to signal default loop")
	}
}

func TestGetContextStats_UsesActiveSessionLoop(t *testing.T) {
	sessionID := "session-stats"
	defaultLoop := service.NewAgentLoop(service.Deps{})
	defaultLoop.SetHistory([]llm.Message{{Role: "user", Content: "default"}})

	pool := service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{})
	}, t.TempDir())
	activeLoop := pool.Get(sessionID)
	activeLoop.SetHistory([]llm.Message{{Role: "user", Content: "active session history"}})

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	sessMgr.Activate(sessionID)

	api := NewAgentAPI(defaultLoop, pool, nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, llm.NewRouter(nil), nil, "", nil, nil)

	stats := api.GetContextStats()
	if stats.HistoryCount != 1 {
		t.Fatalf("expected pooled active loop history count, got %d", stats.HistoryCount)
	}
	if stats.TokenEstimate != len("active session history")/4 {
		t.Fatalf("unexpected token estimate from active loop: %d", stats.TokenEstimate)
	}
}

func TestGetContextStats_DoesNotCreateGhostLoopForMissingActiveSession(t *testing.T) {
	sessionID := "missing-active-stats"
	defaultLoop := service.NewAgentLoop(service.Deps{})
	defaultLoop.SetHistory([]llm.Message{{Role: "user", Content: "default only"}})

	pool := service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{})
	}, t.TempDir())
	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	sessMgr.Activate(sessionID)

	api := NewAgentAPI(defaultLoop, pool, nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, llm.NewRouter(nil), nil, "", nil, nil)

	stats := api.GetContextStats()
	if stats.HistoryCount != 1 || stats.TokenEstimate != len("default only")/4 {
		t.Fatalf("expected fallback default loop stats, got %#v", stats)
	}
	if got := pool.GetIfExists(sessionID); got != nil {
		t.Fatalf("expected GetContextStats not to create ghost loop, got %#v", got)
	}
}

func TestClearHistory_DoesNotMutateDefaultLoopForMissingActiveSession(t *testing.T) {
	sessionID := "missing-active-clear"
	defaultLoop := service.NewAgentLoop(service.Deps{})
	defaultLoop.SetHistory([]llm.Message{{Role: "user", Content: "default stays"}})

	pool := service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{})
	}, t.TempDir())
	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	sessMgr.Activate(sessionID)

	api := NewAgentAPI(defaultLoop, pool, nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, llm.NewRouter(nil), nil, "", nil, nil)
	api.ClearHistory()

	history := defaultLoop.GetHistory()
	if len(history) != 1 || history[0].Content != "default stays" {
		t.Fatalf("expected missing active clear not to mutate default loop, got %#v", history)
	}
	if got := pool.GetIfExists(sessionID); got != nil {
		t.Fatalf("expected ClearHistory not to create ghost loop, got %#v", got)
	}
}

func TestCompactContext_DoesNotCreateGhostLoopForMissingActiveSession(t *testing.T) {
	sessionID := "missing-active-compact"
	defaultLoop := service.NewAgentLoop(service.Deps{})
	defaultLoop.SetHistory([]llm.Message{{Role: "user", Content: "default compact"}})

	pool := service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{})
	}, t.TempDir())
	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	sessMgr.Activate(sessionID)

	api := NewAgentAPI(defaultLoop, pool, nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, llm.NewRouter(nil), nil, "", nil, nil)
	api.CompactContext()

	if got := pool.GetIfExists(sessionID); got != nil {
		t.Fatalf("expected CompactContext not to create ghost loop, got %#v", got)
	}
}

func TestGetHistory_PrefersResidentLoopWithoutCreatingGhostLoop(t *testing.T) {
	sessionID := "session-history"
	defaultLoop := service.NewAgentLoop(service.Deps{})
	pool := service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{})
	}, t.TempDir())
	resident := pool.Get(sessionID)
	resident.SetHistory([]llm.Message{{Role: "user", Content: "resident history"}})

	api := NewAgentAPI(
		defaultLoop,
		pool,
		nil,
		service.NewSessionManager(&stubSessionRepo{sessions: []service.ConversationInfo{{ID: sessionID}}}),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		llm.NewRouter(nil),
		&stubHistoryQuery{exports: map[string][]service.HistoryExport{
			sessionID: {{Role: "user", Content: "persisted history"}},
		}},
		"",
		nil,
		nil,
	)

	history, err := api.GetHistory(sessionID)
	if err != nil {
		t.Fatalf("GetHistory error: %v", err)
	}
	if len(history) != 1 || history[0].Content != "resident history" {
		t.Fatalf("expected resident loop history, got %#v", history)
	}

	missingSession := "missing-history"
	history, err = api.GetHistory(missingSession)
	if err != nil {
		t.Fatalf("GetHistory missing session error: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected no history for missing session, got %#v", history)
	}
	if got := pool.GetIfExists(missingSession); got != nil {
		t.Fatalf("expected GetHistory not to create ghost loop, got %#v", got)
	}
}

func TestSaveSessionCost_UsesRequestedSessionInsteadOfActiveSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := persistence.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	store := persistence.NewTokenUsageStore(db)

	defaultLoop := service.NewAgentLoop(service.Deps{})
	pool := service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(service.Deps{})
	}, t.TempDir())

	activeSession := "session-active"
	targetSession := "session-target"
	activeLoop := pool.Get(activeSession)
	targetLoop := pool.Get(targetSession)
	activeLoop.SetHistory([]llm.Message{{Role: "user", Content: "active"}})
	targetLoop.SetHistory([]llm.Message{{Role: "user", Content: "target target"}})

	setLoopTokenUsage(activeLoop, map[string]service.ModelUsage{
		"active-model": {PromptTokens: 10, CompletionTokens: 20, Calls: 1, CostUSD: 1.25},
	})
	setLoopTokenUsage(targetLoop, map[string]service.ModelUsage{
		"target-model": {PromptTokens: 30, CompletionTokens: 40, Calls: 2, CostUSD: 2.50},
	})

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: activeSession}, {ID: targetSession}},
	})
	sessMgr.Activate(activeSession)

	api := NewAgentAPI(defaultLoop, pool, nil, sessMgr, nil, nil, nil, nil, nil, nil, nil, llm.NewRouter(nil), nil, "", nil, nil)
	api.SetTokenUsageStore(store)

	if err := api.SaveSessionCost(targetSession); err != nil {
		t.Fatalf("SaveSessionCost: %v", err)
	}

	usage, err := store.GetSessionUsage(targetSession)
	if err != nil {
		t.Fatalf("GetSessionUsage: %v", err)
	}
	if usage.TotalPromptTok != 30 || usage.TotalCompleteTok != 40 {
		t.Fatalf("expected target session token usage, got prompt=%d complete=%d", usage.TotalPromptTok, usage.TotalCompleteTok)
	}
	if usage.TotalCalls != 2 || math.Abs(usage.TotalCostUSD-2.50) > 1e-9 {
		t.Fatalf("expected target session totals, got calls=%d cost=%v", usage.TotalCalls, usage.TotalCostUSD)
	}
}

func TestListRuntimeRunsAndChildRuns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := persistence.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	now := time.Unix(1700000200, 0)

	parent := &graphruntime.RunSnapshot{
		RunID:        "run-parent",
		SessionID:    "session-runtime",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		Cursor:       graphruntime.ExecutionCursor{CurrentNode: "barrier_wait", RouteKey: "barrier_wait"},
		TurnState: graphruntime.TurnState{
			RunID: "run-parent",
			Orchestration: graphruntime.OrchestrationState{
				ChildRunIDs: []string{"run-child"},
				Handoffs: []graphruntime.HandoffState{{
					TargetRunID: "run-child",
					Kind:        "subagent_task",
					PayloadJSON: `{"task_name":"research"}`,
				}},
				Events: []graphruntime.OrchestrationEventState{{
					Type:      "child.spawned",
					RunID:     "run-child",
					SourceRun: "run-parent",
					At:        now,
					Summary:   "research",
				}},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			Status:     graphruntime.NodeStatusWait,
			WaitReason: graphruntime.WaitReasonBarrier,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	child := &graphruntime.RunSnapshot{
		RunID:        "run-child",
		SessionID:    "session-runtime",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusComplete,
		Cursor:       graphruntime.ExecutionCursor{CurrentNode: "complete", RouteKey: "complete"},
		TurnState: graphruntime.TurnState{
			RunID: "run-child",
			Orchestration: graphruntime.OrchestrationState{
				ParentRunID: "run-parent",
			},
		},
		ExecutionState: graphruntime.ExecutionState{Status: graphruntime.NodeStatusComplete},
		CreatedAt:      now.Add(time.Minute),
		UpdatedAt:      now.Add(time.Minute),
	}
	if err := store.Save(context.Background(), parent); err != nil {
		t.Fatalf("save parent snapshot: %v", err)
	}
	if err := store.Save(context.Background(), child); err != nil {
		t.Fatalf("save child snapshot: %v", err)
	}

	api := NewAgentAPI(service.NewAgentLoop(service.Deps{}), nil, nil, service.NewSessionManager(&stubSessionRepo{}), nil, nil, nil, nil, nil, nil, nil, llm.NewRouter(nil), nil, "", nil, nil)
	api.SetRuntimeStore(store)

	runs, err := api.ListRuntimeRuns(context.Background(), "session-runtime")
	if err != nil {
		t.Fatalf("ListRuntimeRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runtime runs, got %#v", runs)
	}
	if runs[0].RunID != "run-child" || runs[1].RunID != "run-parent" {
		t.Fatalf("expected runs sorted newest-first, got %#v", runs)
	}

	pending, err := api.ListPendingRuns(context.Background(), "session-runtime")
	if err != nil {
		t.Fatalf("ListPendingRuns: %v", err)
	}
	if len(pending) != 1 || pending[0].RunID != "run-parent" || pending[0].WaitReason != string(graphruntime.WaitReasonBarrier) {
		t.Fatalf("expected only pending parent run, got %#v", pending)
	}

	children, err := api.ListChildRuns(context.Background(), "run-parent")
	if err != nil {
		t.Fatalf("ListChildRuns: %v", err)
	}
	if len(children) != 1 || children[0].RunID != "run-child" || children[0].ParentRunID != "run-parent" {
		t.Fatalf("expected child run listing, got %#v", children)
	}

	graph, err := api.ListRuntimeGraph(context.Background(), "session-runtime")
	if err != nil {
		t.Fatalf("ListRuntimeGraph: %v", err)
	}
	if len(graph.Nodes) != 2 || len(graph.RootRunIDs) != 1 || graph.RootRunIDs[0] != "run-parent" {
		t.Fatalf("expected orchestration graph roots/nodes, got %#v", graph)
	}
	if len(graph.Edges) != 2 {
		t.Fatalf("expected parent-child and handoff edges, got %#v", graph.Edges)
	}
	if graph.Edges[0].Kind != "parent_child" || graph.Edges[0].SourceRunID != "run-parent" || graph.Edges[0].TargetRunID != "run-child" {
		t.Fatalf("expected parent-child edge, got %#v", graph.Edges)
	}
	if graph.Edges[1].Kind != "subagent_task" || graph.Edges[1].SourceRunID != "run-parent" || graph.Edges[1].TargetRunID != "run-child" {
		t.Fatalf("expected handoff edge, got %#v", graph.Edges)
	}

	decisions, err := api.ListPendingDecisions(context.Background(), "session-runtime")
	if err != nil {
		t.Fatalf("ListPendingDecisions: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected no pending decisions for barrier wait, got %#v", decisions)
	}
}

func TestListRuntimeRuns_MapsLastDecisionContracts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := persistence.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	now := time.Unix(1700000400, 0)

	reflection := &graphruntime.RunSnapshot{
		RunID:        "run-reflection",
		SessionID:    "session-decisions",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusComplete,
		TurnState: graphruntime.TurnState{
			RunID: "run-reflection",
			Intelligence: graphruntime.IntelligenceState{
				Review: graphruntime.ReviewDecisionState{
					SchemaName: "reflection.review.v1",
					Decision:   "accept",
					Reason:     "answer is coherent",
					RawJSON:    `{"decision":"accept","reason":"answer is coherent"}`,
					Valid:      true,
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{Status: graphruntime.NodeStatusComplete},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	evaluation := &graphruntime.RunSnapshot{
		RunID:        "run-evaluation",
		SessionID:    "session-decisions",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusComplete,
		TurnState: graphruntime.TurnState{
			RunID: "run-evaluation",
			Intelligence: graphruntime.IntelligenceState{
				Evaluation: graphruntime.EvaluationState{
					SchemaName: "evaluation.report.v1",
					Score:      0.42,
					Passed:     false,
					ErrorType:  "tool_misuse",
					RawJSON:    `{"score":0.42,"passed":false,"error_type":"tool_misuse"}`,
					Valid:      true,
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{Status: graphruntime.NodeStatusComplete},
		CreatedAt:      now.Add(time.Minute),
		UpdatedAt:      now.Add(time.Minute),
	}
	if err := store.Save(context.Background(), reflection); err != nil {
		t.Fatalf("save reflection snapshot: %v", err)
	}
	if err := store.Save(context.Background(), evaluation); err != nil {
		t.Fatalf("save evaluation snapshot: %v", err)
	}

	api := NewAgentAPI(service.NewAgentLoop(service.Deps{}), nil, nil, service.NewSessionManager(&stubSessionRepo{}), nil, nil, nil, nil, nil, nil, nil, llm.NewRouter(nil), nil, "", nil, nil)
	api.SetRuntimeStore(store)

	runs, err := api.ListRuntimeRuns(context.Background(), "session-decisions")
	if err != nil {
		t.Fatalf("ListRuntimeRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runtime runs, got %#v", runs)
	}
	if runs[0].RunID != "run-evaluation" || runs[0].LastDecision == nil || runs[0].LastDecision.Kind != "evaluation" {
		t.Fatalf("expected evaluation decision mapping, got %#v", runs[0])
	}
	if runs[0].LastDecision.Decision != "failed" || runs[0].LastDecision.Reason != "tool_misuse" {
		t.Fatalf("unexpected evaluation decision info: %#v", runs[0].LastDecision)
	}
	if runs[1].RunID != "run-reflection" || runs[1].LastDecision == nil || runs[1].LastDecision.Kind != "reflection" {
		t.Fatalf("expected reflection decision mapping, got %#v", runs[1])
	}
	if runs[1].LastDecision.Decision != "accept" || runs[1].LastDecision.Reason != "answer is coherent" {
		t.Fatalf("unexpected reflection decision info: %#v", runs[1].LastDecision)
	}
}
