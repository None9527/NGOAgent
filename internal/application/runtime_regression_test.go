package application

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
	"github.com/ngoclaw/ngoagent/internal/testing/runtimecases"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type runtimeRegressionDelta struct {
	approvals []runtimecases.ApprovalExpect
}

func TestRuntimeRegressionApplication(t *testing.T) {
	for _, tc := range runtimecases.Load(t) {
		if !tc.HasLayer("application") || tc.Application == nil {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			runApplicationRegressionCase(t, tc)
		})
	}
}

func runApplicationRegressionCase(t *testing.T, tc runtimecases.Case) {
	t.Helper()

	ac := tc.Application
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := persistence.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	runtimeStore := persistence.NewRunSnapshotStore(db)

	brainDir := t.TempDir()
	secHook := security.NewHook(&config.SecurityConfig{})
	secAdapter := &securityAdapter{hook: secHook}
	factory := func(sessionID string) *service.AgentLoop {
		loop := service.NewAgentLoop(service.Deps{
			Brain:         brain.NewArtifactStore(brainDir, sessionID),
			Security:      secAdapter,
			SnapshotStore: runtimeStore,
			LLMRouter:     fakeModelRouter{},
		})
		loop.SetPlanMode("plan")
		return loop
	}

	for i := range ac.Snapshots {
		snap := ac.Snapshots[i]
		if err := runtimeStore.Save(context.Background(), &snap); err != nil {
			t.Fatalf("save snapshot %s: %v", snap.RunID, err)
		}
	}

	sessionID := ac.SessionID
	sessMgr := service.NewSessionManager(&stubSessionRepo{
		sessions: []service.ConversationInfo{{ID: sessionID}},
	})
	if ac.ActivateSession {
		sessMgr.Activate(sessionID)
	}

	persisted := map[string][]service.HistoryExport{}
	if len(ac.PersistedHistory) > 0 {
		exports := make([]service.HistoryExport, 0, len(ac.PersistedHistory))
		for _, msg := range ac.PersistedHistory {
			exports = append(exports, service.HistoryExport{
				Role:        msg.Role,
				Content:     msg.Content,
				Reasoning:   msg.Reasoning,
				ToolCallID:  msg.ToolCallID,
				ToolCalls:   msg.ToolCallsJSON(t),
				Attachments: msg.AttachmentsJSON(t),
			})
		}
		persisted[sessionID] = exports
	}

	pool := service.NewLoopPool(factory, filepath.Join(brainDir, "pool"))
	if len(ac.ResidentHistory) > 0 {
		resident := pool.Get(sessionID)
		resident.SetHistory(fixtureMessagesToLLM(ac.ResidentHistory))
	}

	api := newTestCompatibilityAPIWithWiring(
		factory("__default__"),
		pool,
		nil,
		sessMgr,
		nil,
		nil,
		secHook,
		&stubHistoryQuery{exports: persisted},
		brainDir,
		llm.NewRouter(nil),
		ServiceWiring{RuntimeStore: runtimeStore},
	)

	baselineUpdated := map[string]string{}
	for _, snap := range ac.Snapshots {
		baselineUpdated[snap.RunID] = snap.UpdatedAt.UTC().Format(timeLayout)
	}

	deltaState := &runtimeRegressionDelta{}
	delta := &service.Delta{
		OnApprovalRequestFunc: func(id, tool string, args map[string]any, reason string) {
			deltaState.approvals = append(deltaState.approvals, runtimecases.ApprovalExpect{
				ID:       id,
				ToolName: tool,
				Reason:   reason,
				Args:     cloneAnyMap(args),
			})
		},
	}

	var lastUser string
	var history []apitype.HistoryMessage
	switch ac.Action.Kind {
	case "chat_stream":
		err = api.ChatStream(context.Background(), sessionID, ac.Action.Message, ac.Action.Mode, delta)
	case "retry_run":
		lastUser, err = api.RetryRun(context.Background(), sessionID)
	case "get_history":
		history, err = api.GetHistory(sessionID)
	case "apply_runtime_ingress":
		req := apitype.RuntimeIngressRequest{
			SessionID: sessionID,
			Ingress: apitype.RuntimeIngressInput{
				Kind: "decision",
				Run:  apitype.RuntimeRunTarget{RunID: ac.Action.RunID},
				Decision: apitype.RuntimeDecisionContractInput{
					Kind:     ac.Action.Decision.Kind,
					Decision: ac.Action.Decision.Decision,
					Feedback: ac.Action.Decision.Feedback,
					Reason:   ac.Action.Decision.Reason,
				},
			},
		}
		_, err = api.ApplyRuntimeIngress(context.Background(), req)
	default:
		t.Fatalf("unsupported application action %q", ac.Action.Kind)
	}

	if want := strings.TrimSpace(ac.Expect.ErrorContains); want != "" {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error containing %q, got %v", want, err)
		}
	} else if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if want := ac.Expect.Approval; want != nil {
		if len(deltaState.approvals) != 1 {
			t.Fatalf("expected one approval event, got %#v", deltaState.approvals)
		}
		if !reflect.DeepEqual(deltaState.approvals[0], *want) {
			t.Fatalf("unexpected approval event: got %#v want %#v", deltaState.approvals[0], *want)
		}
	}

	if want := strings.TrimSpace(ac.Expect.RetryLastUser); want != "" && lastUser != want {
		t.Fatalf("unexpected retry last user: got %q want %q", lastUser, want)
	}

	if want := ac.Expect.History; want != nil {
		if len(history) != want.Count {
			t.Fatalf("unexpected history count: got %d want %d", len(history), want.Count)
		}
		if want.LastContent != "" {
			if len(history) == 0 || history[len(history)-1].Content != want.LastContent {
				t.Fatalf("unexpected last history content: got %#v want %q", history, want.LastContent)
			}
		}
	}

	if ac.Expect.NoGhostLoop {
		if got := pool.GetIfExists(sessionID); got != nil {
			t.Fatalf("expected no ghost loop for %s, got %#v", sessionID, got)
		}
	}

	for _, check := range ac.Expect.SnapshotChecks {
		snap, err := runtimeStore.LoadLatest(context.Background(), check.RunID)
		if err != nil {
			t.Fatalf("load snapshot %s: %v", check.RunID, err)
		}
		if snap == nil {
			t.Fatalf("expected snapshot %s", check.RunID)
		}
		if check.Status != "" && string(snap.Status) != check.Status {
			t.Fatalf("snapshot %s status: got %q want %q", check.RunID, snap.Status, check.Status)
		}
		if check.WaitReason != "" && string(snap.ExecutionState.WaitReason) != check.WaitReason {
			t.Fatalf("snapshot %s wait reason: got %q want %q", check.RunID, snap.ExecutionState.WaitReason, check.WaitReason)
		}
		if check.IngressRunID != "" && snap.TurnState.Orchestration.Ingress.RunID != check.IngressRunID {
			t.Fatalf("snapshot %s ingress run id: got %q want %q", check.RunID, snap.TurnState.Orchestration.Ingress.RunID, check.IngressRunID)
		}
		if check.PlanningReviewDecision != "" && snap.TurnState.Intelligence.Planning.ReviewDecision != check.PlanningReviewDecision {
			t.Fatalf("snapshot %s review decision: got %q want %q", check.RunID, snap.TurnState.Intelligence.Planning.ReviewDecision, check.PlanningReviewDecision)
		}
		if baseline, ok := baselineUpdated[check.RunID]; ok && check.Updated != "" {
			got := snap.UpdatedAt.UTC().Format(timeLayout)
			switch check.Updated {
			case "changed":
				if got == baseline {
					t.Fatalf("snapshot %s expected updated_at to change, stayed %s", check.RunID, got)
				}
			case "unchanged":
				if got != baseline {
					t.Fatalf("snapshot %s expected updated_at unchanged, got %s baseline %s", check.RunID, got, baseline)
				}
			}
		}
	}
}

func fixtureMessagesToLLM(in []runtimecases.HistoryMessageFixture) []llm.Message {
	out := make([]llm.Message, 0, len(in))
	for _, msg := range in {
		out = append(out, llm.Message{
			Role:        msg.Role,
			Content:     msg.Content,
			Reasoning:   msg.Reasoning,
			ToolCallID:  msg.ToolCallID,
			ToolCalls:   append([]model.ToolCall(nil), msg.ToolCalls...),
			Attachments: append([]model.Attachment(nil), msg.Attachments...),
		})
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

const timeLayout = "2006-01-02T15:04:05Z"
