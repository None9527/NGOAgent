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

func newTestLegacyAPI(
	loop *service.AgentLoop,
	loopPool *service.LoopPool,
	chatEngine *service.ChatEngine,
	sessMgr *service.SessionManager,
	modelMgr *service.ModelManager,
	toolAdmin *service.ToolAdmin,
	secHook *security.Hook,
	histQuery HistoryQuerier,
	brainDir string,
	router *llm.Router,
) LegacyAPI {
	return NewLegacyAPI(ApplicationDeps{
		Loop:       loop,
		LoopPool:   loopPool,
		ChatEngine: chatEngine,
		SessionMgr: sessMgr,
		ModelMgr:   modelMgr,
		ToolAdmin:  toolAdmin,
		SecHook:    secHook,
		HistQuery:  histQuery,
		BrainDir:   brainDir,
		Router:     router,
	})
}

func newTestLegacyAPIWithWiring(
	loop *service.AgentLoop,
	loopPool *service.LoopPool,
	chatEngine *service.ChatEngine,
	sessMgr *service.SessionManager,
	modelMgr *service.ModelManager,
	toolAdmin *service.ToolAdmin,
	secHook *security.Hook,
	histQuery HistoryQuerier,
	brainDir string,
	router *llm.Router,
	wiring ServiceWiring,
) LegacyAPI {
	return NewLegacyAPI(ApplicationDeps{
		Loop:       loop,
		LoopPool:   loopPool,
		ChatEngine: chatEngine,
		SessionMgr: sessMgr,
		ModelMgr:   modelMgr,
		ToolAdmin:  toolAdmin,
		SecHook:    secHook,
		HistQuery:  histQuery,
		BrainDir:   brainDir,
		Router:     router,
		Wiring:     wiring,
	})
}

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

	api := newTestLegacyAPI(factory("__default__"), loopPool, nil, sessMgr, nil, nil, secHook, nil, brainDir, nil)

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
	api := newTestLegacyAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, secHook, nil, brainDir, nil)

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
	api := newTestLegacyAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, secHook, nil, brainDir, nil)

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
	api := newTestLegacyAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, nil, history, brainDir, nil)

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
	api := newTestLegacyAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, nil, nil, brainDir, nil)

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
	api := newTestLegacyAPIWithWiring(
		factory("__default__"),
		service.NewLoopPool(factory, brainDir),
		nil,
		sessMgr,
		nil,
		nil,
		nil,
		nil,
		brainDir,
		nil,
		ServiceWiring{RuntimeStore: store},
	)

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
	targetedDecision, err := api.PendingDecision(context.Background(), sessionID, decisions[0].RunID)
	if err != nil {
		t.Fatalf("PendingDecision: %v", err)
	}
	if targetedDecision == nil || targetedDecision.RunID != decisions[0].RunID || targetedDecision.PendingDecision == nil {
		t.Fatalf("expected targeted pending decision lookup, got %#v", targetedDecision)
	}

	if err := api.ApplyDecision(context.Background(), sessionID, "", "revise", "narrow scope"); err != nil {
		t.Fatalf("ApplyDecision infer kind: %v", err)
	}

	loaded, err := store.LoadLatestBySession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("load snapshot after inferred decision: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected snapshot after inferred decision")
	}
	if loaded.TurnState.Orchestration.Ingress.DecisionKind != "plan_review" {
		t.Fatalf("expected inferred ingress decision kind, got %#v", loaded.TurnState.Orchestration.Ingress)
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
	api := newTestLegacyAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, nil, nil, brainDir, nil)

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
	api := newTestLegacyAPI(factory("__default__"), service.NewLoopPool(factory, brainDir), nil, sessMgr, nil, nil, nil, nil, brainDir, nil)

	_, err := api.ApplyRuntimeIngress(context.Background(), apitype.RuntimeIngressRequest{
		SessionID: sessionID,
		Ingress: apitype.RuntimeIngressInput{
			Kind: "decision",
		},
	})
	if err == nil {
		t.Fatal("expected validation error for empty decision ingress")
	}

	_, err = api.ApplyRuntimeIngress(context.Background(), apitype.RuntimeIngressRequest{
		SessionID: sessionID,
		Ingress: apitype.RuntimeIngressInput{
			Kind: "message",
		},
	})
	if err == nil {
		t.Fatal("expected validation error for empty message ingress")
	}

	resumeResp, err := api.ApplyRuntimeIngress(context.Background(), apitype.RuntimeIngressRequest{
		SessionID: sessionID,
		Ingress: apitype.RuntimeIngressInput{
			Kind:    "resume",
			Source:  "runtime_ingress",
			Trigger: "manual_resume",
			RunID:   "run-ingress",
		},
	})
	if err != nil {
		t.Fatalf("ApplyRuntimeIngress resume: %v", err)
	}
	if resumeResp.Status != "resumed" || resumeResp.Ingress.RunID != "run-ingress" {
		t.Fatalf("unexpected resume ingress response: %#v", resumeResp)
	}

	loaded, err := store.LoadLatest(context.Background(), "run-ingress")
	if err != nil {
		t.Fatalf("load ingress snapshot: %v", err)
	}
	if loaded == nil || loaded.TurnState.Orchestration.Ingress.Kind != "resume" {
		t.Fatalf("expected resume ingress persisted to snapshot, got %#v", loaded)
	}
	if loaded.TurnState.Orchestration.Ingress.Source != "runtime_ingress" || loaded.TurnState.Orchestration.Ingress.Trigger != "manual_resume" {
		t.Fatalf("unexpected persisted ingress metadata: %#v", loaded.TurnState.Orchestration.Ingress)
	}
}

func TestApplyRuntimeIngress_AcceptsDecisionReasonFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := persistence.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	brainDir := t.TempDir()
	sessionID := "session-runtime-ingress-reason"

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
		RunID:        "run-ingress-reason",
		SessionID:    sessionID,
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-ingress-reason",
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
	api := newTestLegacyAPIWithWiring(
		factory("__default__"),
		service.NewLoopPool(factory, brainDir),
		nil,
		sessMgr,
		nil,
		nil,
		nil,
		nil,
		brainDir,
		nil,
		ServiceWiring{RuntimeStore: store},
	)

	resp, err := api.ApplyRuntimeIngress(context.Background(), apitype.RuntimeIngressRequest{
		SessionID: sessionID,
		Ingress: apitype.RuntimeIngressInput{
			Kind: "decision",
			Decision: apitype.RuntimeDecisionContractInput{
				Kind:     "plan_review",
				Decision: "revise",
				Reason:   "reason-only fallback",
			},
		},
	})
	if err != nil {
		t.Fatalf("ApplyRuntimeIngress decision: %v", err)
	}
	if resp.Ingress.Source != "decision_apply" || resp.Ingress.Trigger != "decision_apply" {
		t.Fatalf("expected default ingress metadata in response, got %#v", resp.Ingress)
	}
	if resp.Ingress.Decision.Kind != "plan_review" || resp.Ingress.Decision.Feedback != "reason-only fallback" {
		t.Fatalf("expected normalized decision contract in ingress response, got %#v", resp.Ingress.Decision)
	}

	loaded, err := store.LoadLatestBySession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("load snapshot after decision ingress: %v", err)
	}
	if loaded == nil || loaded.ExecutionState.WaitReason != graphruntime.WaitReasonUserInput {
		t.Fatalf("expected decision ingress to continue through planning wait, got %#v", loaded)
	}
}

func TestApplyRuntimeIngress_DecisionTargetsExplicitRun(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := persistence.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	brainDir := t.TempDir()
	sessionID := "session-runtime-ingress-targeted"

	factory := func(sessionID string) *service.AgentLoop {
		loop := service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			SnapshotStore: store,
			LLMRouter:     fakeModelRouter{},
		})
		loop.SetPlanMode("plan")
		return loop
	}

	makeWaiting := func(runID string, updatedAt time.Time) *graphruntime.RunSnapshot {
		return &graphruntime.RunSnapshot{
			RunID:        runID,
			SessionID:    sessionID,
			GraphID:      "agent_loop",
			GraphVersion: "v1alpha1",
			Status:       graphruntime.NodeStatusWait,
			TurnState: graphruntime.TurnState{
				RunID: runID,
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
			UpdatedAt: updatedAt,
		}
	}

	targetRunID := "run-ingress-target"
	latestRunID := "run-ingress-latest"
	targetUpdatedAt := time.Now().Add(-time.Minute)
	latestUpdatedAt := time.Now()
	if err := store.Save(context.Background(), makeWaiting(targetRunID, targetUpdatedAt)); err != nil {
		t.Fatalf("save target waiting snapshot: %v", err)
	}
	if err := store.Save(context.Background(), makeWaiting(latestRunID, latestUpdatedAt)); err != nil {
		t.Fatalf("save latest waiting snapshot: %v", err)
	}

	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	api := newTestLegacyAPIWithWiring(
		factory("__default__"),
		service.NewLoopPool(factory, brainDir),
		nil,
		sessMgr,
		nil,
		nil,
		nil,
		nil,
		brainDir,
		nil,
		ServiceWiring{RuntimeStore: store},
	)

	resp, err := api.ApplyRuntimeIngress(context.Background(), apitype.RuntimeIngressRequest{
		SessionID: sessionID,
		Ingress: apitype.RuntimeIngressInput{
			Kind: "decision",
			Run:  apitype.RuntimeRunTarget{RunID: targetRunID},
			Decision: apitype.RuntimeDecisionContractInput{
				Kind:     "plan_review",
				Decision: "revise",
				Feedback: "target this run",
			},
		},
	})
	if err != nil {
		t.Fatalf("ApplyRuntimeIngress targeted decision: %v", err)
	}
	if resp.Status != "applied" || resp.Ingress.Run.RunID != targetRunID {
		t.Fatalf("unexpected targeted decision response: %#v", resp)
	}

	targetSnap, err := store.LoadLatest(context.Background(), targetRunID)
	if err != nil {
		t.Fatalf("load target snapshot: %v", err)
	}
	if targetSnap == nil {
		t.Fatal("expected target snapshot")
	}
	if targetSnap.TurnState.Orchestration.Ingress.RunID != targetRunID {
		t.Fatalf("expected targeted ingress metadata on target run, got %#v", targetSnap.TurnState.Orchestration.Ingress)
	}
	if !targetSnap.UpdatedAt.After(targetUpdatedAt) {
		t.Fatalf("expected target run to be updated by targeted decision, got updated_at=%v", targetSnap.UpdatedAt)
	}

	latestSnap, err := store.LoadLatest(context.Background(), latestRunID)
	if err != nil {
		t.Fatalf("load latest snapshot: %v", err)
	}
	if latestSnap == nil {
		t.Fatal("expected latest snapshot")
	}
	if latestSnap.TurnState.Orchestration.Ingress != (graphruntime.IngressState{}) {
		t.Fatalf("expected untargeted pending run to remain untouched, got ingress %#v", latestSnap.TurnState.Orchestration.Ingress)
	}
	if !latestSnap.UpdatedAt.Equal(latestUpdatedAt) {
		t.Fatalf("expected untargeted pending run updated_at to remain unchanged, got %v", latestSnap.UpdatedAt)
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
	api := newTestLegacyAPI(
		defaultLoop,
		pool,
		nil,
		service.NewSessionManager(&stubSessionRepo{sessions: []service.ConversationInfo{{ID: sessionID}}}),
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

	api := newTestLegacyAPI(
		defaultLoop,
		pool,
		nil,
		service.NewSessionManager(&stubSessionRepo{}),
		nil,
		nil,
		nil,
		nil,
		"",
		llm.NewRouter(nil),
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

	api := newTestLegacyAPI(defaultLoop, pool, nil, sessMgr, nil, nil, nil, nil, "", llm.NewRouter(nil))

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

	api := newTestLegacyAPI(defaultLoop, pool, nil, sessMgr, nil, nil, nil, nil, "", llm.NewRouter(nil))

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

	api := newTestLegacyAPI(defaultLoop, pool, nil, sessMgr, nil, nil, nil, nil, "", llm.NewRouter(nil))
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

	api := newTestLegacyAPI(defaultLoop, pool, nil, sessMgr, nil, nil, nil, nil, "", llm.NewRouter(nil))
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

	api := newTestLegacyAPI(
		defaultLoop,
		pool,
		nil,
		service.NewSessionManager(&stubSessionRepo{sessions: []service.ConversationInfo{{ID: sessionID}}}),
		nil,
		nil,
		nil,
		&stubHistoryQuery{exports: map[string][]service.HistoryExport{
			sessionID: {{Role: "user", Content: "persisted history"}},
		}},
		"",
		llm.NewRouter(nil),
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

	api := newTestLegacyAPIWithWiring(
		defaultLoop,
		pool,
		nil,
		sessMgr,
		nil,
		nil,
		nil,
		nil,
		"",
		llm.NewRouter(nil),
		ServiceWiring{TokenUsageStore: store},
	)

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
				Ingress: graphruntime.IngressState{
					Kind:    "resume",
					Source:  "runtime_ingress",
					Trigger: "barrier_resume",
					RunID:   "run-parent",
					At:      now,
				},
				ChildRunIDs: []string{"run-child"},
				Handoffs: []graphruntime.HandoffState{{
					TargetRunID: "run-child",
					Kind:        "subagent_task",
					PayloadJSON: `{"task_name":"research"}`,
				}},
				Events: []graphruntime.OrchestrationEventState{{
					Type:        "child.spawned",
					RunID:       "run-child",
					SourceRun:   "run-parent",
					At:          now,
					Summary:     "research",
					PayloadJSON: `{"task_name":"research"}`,
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

	api := newTestLegacyAPIWithWiring(
		service.NewAgentLoop(service.Deps{}),
		nil,
		nil,
		service.NewSessionManager(&stubSessionRepo{}),
		nil,
		nil,
		nil,
		nil,
		"",
		llm.NewRouter(nil),
		ServiceWiring{RuntimeStore: store},
	)

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
	if pending[0].Ingress == nil || pending[0].Ingress.Kind != "resume" || pending[0].Ingress.Source != "runtime_ingress" {
		t.Fatalf("expected runtime ingress on pending run, got %#v", pending[0].Ingress)
	}
	if pending[0].Ingress.Category != "runtime_control" || pending[0].Ingress.Phase != "resume" {
		t.Fatalf("expected ingress taxonomy on pending run, got %#v", pending[0].Ingress)
	}

	children, err := api.ListChildRuns(context.Background(), "run-parent")
	if err != nil {
		t.Fatalf("ListChildRuns: %v", err)
	}
	if len(children) != 1 || children[0].RunID != "run-child" || children[0].ParentRunID != "run-parent" {
		t.Fatalf("expected child run listing, got %#v", children)
	}
	if len(runs[1].Events) != 1 || runs[1].Events[0].Type != "child.spawned" || runs[1].Events[0].PayloadJSON != `{"task_name":"research"}` {
		t.Fatalf("expected runtime event payload projection, got %#v", runs[1].Events)
	}

	graph, err := api.ListRuntimeGraph(context.Background(), "session-runtime")
	if err != nil {
		t.Fatalf("ListRuntimeGraph: %v", err)
	}
	if len(graph.Nodes) != 2 || len(graph.RootRunIDs) != 1 || graph.RootRunIDs[0] != "run-parent" {
		t.Fatalf("expected orchestration graph roots/nodes, got %#v", graph)
	}
	if len(graph.PendingRunIDs) != 1 || graph.PendingRunIDs[0] != "run-parent" {
		t.Fatalf("expected pending run grouping, got %#v", graph.PendingRunIDs)
	}
	if len(graph.PendingRuntimeControlRuns) != 1 || graph.PendingRuntimeControlRuns[0] != "run-parent" {
		t.Fatalf("expected runtime-control pending grouping, got %#v", graph.PendingRuntimeControlRuns)
	}
	if len(graph.PendingDecisionRunIDs) != 0 {
		t.Fatalf("expected no pending decision grouping for barrier wait, got %#v", graph.PendingDecisionRunIDs)
	}
	if len(graph.UserTurnRootRunIDs) != 0 {
		t.Fatalf("expected no user-turn roots for runtime-control root, got %#v", graph.UserTurnRootRunIDs)
	}
	if graph.Summary.RunCount != 2 || graph.Summary.IngressNodeCount != 1 || graph.Summary.EventNodeCount != 1 || graph.Summary.EdgeCount != 5 {
		t.Fatalf("expected graph summary counts, got %#v", graph.Summary)
	}
	if len(graph.Summary.PendingRuntimeControlRuns) != 1 || graph.Summary.PendingRuntimeControlRuns[0] != "run-parent" {
		t.Fatalf("expected summary pending runtime-control grouping, got %#v", graph.Summary)
	}
	if len(graph.IngressNodes) != 1 || graph.IngressNodes[0].ID != "ingress:resume:runtime_ingress:barrier_resume" {
		t.Fatalf("expected ingress node layer, got %#v", graph.IngressNodes)
	}
	if len(graph.EventNodes) != 1 || graph.EventNodes[0].ID != "event:run-child:child.spawned:2023-11-14T22:16:40Z" {
		t.Fatalf("expected event node layer, got %#v", graph.EventNodes)
	}
	if graph.EventNodes[0].PayloadJSON != `{"task_name":"research"}` {
		t.Fatalf("expected event payload on graph node, got %#v", graph.EventNodes[0])
	}
	if graph.IngressNodes[0].Category != "runtime_control" || graph.IngressNodes[0].Phase != "resume" {
		t.Fatalf("expected ingress taxonomy on graph node, got %#v", graph.IngressNodes[0])
	}
	if len(graph.Edges) != 5 {
		t.Fatalf("expected ingress, event, event-source, parent-child and handoff edges, got %#v", graph.Edges)
	}
	if graph.Edges[0].Kind != "event" || graph.Edges[0].SourceRunID != "event:run-child:child.spawned:2023-11-14T22:16:40Z" || graph.Edges[0].TargetRunID != "run-parent" {
		t.Fatalf("expected event edge, got %#v", graph.Edges)
	}
	if graph.Edges[1].Kind != "event_source" || graph.Edges[1].SourceRunID != "run-parent" || graph.Edges[1].TargetRunID != "event:run-child:child.spawned:2023-11-14T22:16:40Z" {
		t.Fatalf("expected event-source edge, got %#v", graph.Edges)
	}
	if graph.Edges[2].Kind != "ingress" || graph.Edges[2].SourceRunID != "ingress:resume:runtime_ingress:barrier_resume" || graph.Edges[2].TargetRunID != "run-parent" {
		t.Fatalf("expected ingress edge, got %#v", graph.Edges)
	}
	if graph.Edges[3].Kind != "parent_child" || graph.Edges[3].SourceRunID != "run-parent" || graph.Edges[3].TargetRunID != "run-child" {
		t.Fatalf("expected parent-child edge, got %#v", graph.Edges)
	}
	if graph.Edges[4].Kind != "subagent_task" || graph.Edges[4].SourceRunID != "run-parent" || graph.Edges[4].TargetRunID != "run-child" {
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

	api := newTestLegacyAPIWithWiring(
		service.NewAgentLoop(service.Deps{}),
		nil,
		nil,
		service.NewSessionManager(&stubSessionRepo{}),
		nil,
		nil,
		nil,
		nil,
		"",
		llm.NewRouter(nil),
		ServiceWiring{RuntimeStore: store},
	)

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

func TestListRuntimeGraph_MapsBarrierEventNodes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := persistence.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	now := time.Unix(1700000500, 0)

	snap := &graphruntime.RunSnapshot{
		RunID:        "run-barrier",
		SessionID:    "session-barrier-graph",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-barrier",
			Orchestration: graphruntime.OrchestrationState{
				Events: []graphruntime.OrchestrationEventState{{
					Type:      "barrier.timeout",
					Kind:      "barrier",
					Source:    "barrier",
					Trigger:   "timeout",
					BarrierID: "barrier-9",
					At:        now,
					Summary:   "timed out",
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
	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("save barrier snapshot: %v", err)
	}

	api := newTestLegacyAPIWithWiring(
		service.NewAgentLoop(service.Deps{}),
		nil,
		nil,
		service.NewSessionManager(&stubSessionRepo{}),
		nil,
		nil,
		nil,
		nil,
		"",
		llm.NewRouter(nil),
		ServiceWiring{RuntimeStore: store},
	)

	graph, err := api.ListRuntimeGraph(context.Background(), "session-barrier-graph")
	if err != nil {
		t.Fatalf("ListRuntimeGraph: %v", err)
	}
	if len(graph.EventNodes) != 1 {
		t.Fatalf("expected barrier event node, got %#v", graph.EventNodes)
	}
	if graph.EventNodes[0].Kind != "barrier" || graph.EventNodes[0].Source != "barrier" || graph.EventNodes[0].Trigger != "timeout" {
		t.Fatalf("expected structured barrier event node, got %#v", graph.EventNodes[0])
	}
	if graph.EventNodes[0].BarrierID != "barrier-9" {
		t.Fatalf("expected barrier id on event node, got %#v", graph.EventNodes[0])
	}
	if len(graph.Edges) != 1 || graph.Edges[0].Kind != "event" || graph.Edges[0].BarrierID != "barrier-9" {
		t.Fatalf("expected barrier event edge, got %#v", graph.Edges)
	}
}

func TestListRuntimeRunsByEventAndGraphByEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := persistence.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	store := persistence.NewRunSnapshotStore(db)
	now := time.Unix(1700000600, 0)

	barrierRun := &graphruntime.RunSnapshot{
		RunID:        "run-barrier-event",
		SessionID:    "session-event-filter",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-barrier-event",
			Orchestration: graphruntime.OrchestrationState{
				Events: []graphruntime.OrchestrationEventState{{
					Type:      "barrier.timeout",
					Kind:      "barrier",
					Source:    "barrier",
					Trigger:   "timeout",
					BarrierID: "barrier-x",
					At:        now,
					Summary:   "timed out",
				}},
			},
		},
		ExecutionState: graphruntime.ExecutionState{Status: graphruntime.NodeStatusWait, WaitReason: graphruntime.WaitReasonBarrier},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	triggerRun := &graphruntime.RunSnapshot{
		RunID:        "run-trigger-event",
		SessionID:    "session-event-filter",
		GraphID:      "agent_loop",
		GraphVersion: "v1alpha1",
		Status:       graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			RunID: "run-trigger-event",
			Orchestration: graphruntime.OrchestrationState{
				Events: []graphruntime.OrchestrationEventState{{
					Type:    "trigger.received",
					Kind:    "resume",
					Source:  "resume_run",
					Trigger: "resume_run",
					RunID:   "run-trigger-event",
					At:      now.Add(time.Minute),
					Summary: "resume",
				}},
			},
		},
		ExecutionState: graphruntime.ExecutionState{Status: graphruntime.NodeStatusWait, WaitReason: graphruntime.WaitReasonUserInput},
		CreatedAt:      now.Add(time.Minute),
		UpdatedAt:      now.Add(time.Minute),
	}
	if err := store.Save(context.Background(), barrierRun); err != nil {
		t.Fatalf("save barrier run: %v", err)
	}
	if err := store.Save(context.Background(), triggerRun); err != nil {
		t.Fatalf("save trigger run: %v", err)
	}

	api := newTestLegacyAPIWithWiring(
		service.NewAgentLoop(service.Deps{}),
		nil,
		nil,
		service.NewSessionManager(&stubSessionRepo{}),
		nil,
		nil,
		nil,
		nil,
		"",
		llm.NewRouter(nil),
		ServiceWiring{RuntimeStore: store},
	)

	runs, err := api.ListRuntimeRunsByEvent(context.Background(), "session-event-filter", "barrier.timeout", "timeout", "barrier-x")
	if err != nil {
		t.Fatalf("ListRuntimeRunsByEvent: %v", err)
	}
	if len(runs) != 1 || runs[0].RunID != "run-barrier-event" {
		t.Fatalf("expected barrier-filtered run, got %#v", runs)
	}

	graph, err := api.ListRuntimeGraphByEvent(context.Background(), "session-event-filter", "trigger.received", "resume_run", "")
	if err != nil {
		t.Fatalf("ListRuntimeGraphByEvent: %v", err)
	}
	if len(graph.Nodes) != 1 || graph.Nodes[0].RunID != "run-trigger-event" {
		t.Fatalf("expected trigger-filtered graph, got %#v", graph)
	}
	if len(graph.EventNodes) != 1 || graph.EventNodes[0].Type != "trigger.received" {
		t.Fatalf("expected trigger event node, got %#v", graph.EventNodes)
	}
}
