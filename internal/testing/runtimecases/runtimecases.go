package runtimecases

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/model"
)

type Case struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Category    string           `json:"category"`
	Layers      []string         `json:"layers"`
	Application *ApplicationCase `json:"application,omitempty"`
	Service     *ServiceCase     `json:"service,omitempty"`
}

type ApplicationCase struct {
	SessionID        string                     `json:"session_id"`
	ActivateSession  bool                       `json:"activate_session,omitempty"`
	PersistedHistory []HistoryMessageFixture    `json:"persisted_history,omitempty"`
	ResidentHistory  []HistoryMessageFixture    `json:"resident_history,omitempty"`
	Snapshots        []graphruntime.RunSnapshot `json:"snapshots,omitempty"`
	Action           ApplicationAction          `json:"action"`
	Expect           ApplicationExpect          `json:"expect"`
}

type ServiceCase struct {
	SessionID   string                     `json:"session_id"`
	Store       string                     `json:"store,omitempty"`
	SeedBarrier *graphruntime.BarrierState `json:"seed_barrier,omitempty"`
	Snapshots   []graphruntime.RunSnapshot `json:"snapshots,omitempty"`
	Action      ServiceAction              `json:"action"`
	Expect      ServiceExpect              `json:"expect"`
}

type HistoryMessageFixture struct {
	Role        string             `json:"role"`
	Content     string             `json:"content"`
	Reasoning   string             `json:"reasoning,omitempty"`
	ToolCallID  string             `json:"tool_call_id,omitempty"`
	ToolCalls   []model.ToolCall   `json:"tool_calls,omitempty"`
	Attachments []model.Attachment `json:"attachments,omitempty"`
}

type ApplicationAction struct {
	Kind     string           `json:"kind"`
	Message  string           `json:"message,omitempty"`
	Mode     string           `json:"mode,omitempty"`
	RunID    string           `json:"run_id,omitempty"`
	Decision *DecisionFixture `json:"decision,omitempty"`
}

type ServiceAction struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id,omitempty"`
}

type DecisionFixture struct {
	Kind     string `json:"kind"`
	Decision string `json:"decision"`
	Feedback string `json:"feedback,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type ApplicationExpect struct {
	ErrorContains   string          `json:"error_contains,omitempty"`
	RetryLastUser   string          `json:"retry_last_user,omitempty"`
	Approval        *ApprovalExpect `json:"approval,omitempty"`
	History         *HistoryExpect  `json:"history,omitempty"`
	NoGhostLoop     bool            `json:"no_ghost_loop,omitempty"`
	SnapshotChecks  []SnapshotCheck `json:"snapshot_checks,omitempty"`
	UntouchedRunIDs []string        `json:"untouched_run_ids,omitempty"`
}

type ServiceExpect struct {
	Handled         *bool             `json:"handled,omitempty"`
	Approval        *ApprovalExpect   `json:"approval,omitempty"`
	PlanReview      *PlanReviewExpect `json:"plan_review,omitempty"`
	ReconnectAction string            `json:"reconnect_action,omitempty"`
	AutoResumeRunID string            `json:"auto_resume_run_id,omitempty"`
	ActiveBarrier   *BarrierExpect    `json:"active_barrier,omitempty"`
	NoActiveBarrier bool              `json:"no_active_barrier,omitempty"`
}

type ApprovalExpect struct {
	ID       string         `json:"id"`
	ToolName string         `json:"tool_name"`
	Reason   string         `json:"reason"`
	Args     map[string]any `json:"args,omitempty"`
}

type PlanReviewExpect struct {
	Message string   `json:"message"`
	Paths   []string `json:"paths,omitempty"`
}

type HistoryExpect struct {
	Count       int    `json:"count"`
	LastContent string `json:"last_content,omitempty"`
}

type SnapshotCheck struct {
	RunID                  string `json:"run_id"`
	Status                 string `json:"status,omitempty"`
	WaitReason             string `json:"wait_reason,omitempty"`
	IngressRunID           string `json:"ingress_run_id,omitempty"`
	PlanningReviewDecision string `json:"planning_review_decision,omitempty"`
	Updated                string `json:"updated,omitempty"`
}

type BarrierExpect struct {
	ID           string `json:"id"`
	PendingCount int    `json:"pending_count"`
	FirstRunID   string `json:"first_run_id,omitempty"`
}

func Load(tb testing.TB) []Case {
	tb.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("runtimecases: unable to resolve caller path")
	}
	root := filepath.Join(filepath.Dir(filename), "testdata", "runtime_regressions")
	entries, err := os.ReadDir(root)
	if err != nil {
		tb.Fatalf("runtimecases: read fixtures: %v", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		files = append(files, filepath.Join(root, entry.Name()))
	}
	sort.Strings(files)

	cases := make([]Case, 0, len(files))
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			tb.Fatalf("runtimecases: read %s: %v", filepath.Base(name), err)
		}
		var tc Case
		if err := json.Unmarshal(raw, &tc); err != nil {
			tb.Fatalf("runtimecases: decode %s: %v", filepath.Base(name), err)
		}
		cases = append(cases, tc)
	}
	return cases
}

func (c Case) HasLayer(layer string) bool {
	for _, candidate := range c.Layers {
		if candidate == layer {
			return true
		}
	}
	return false
}

func (m HistoryMessageFixture) ToolCallsJSON(tb testing.TB) string {
	tb.Helper()

	if len(m.ToolCalls) > 0 {
		raw, err := json.Marshal(m.ToolCalls)
		if err != nil {
			tb.Fatalf("runtimecases: marshal tool calls: %v", err)
		}
		return string(raw)
	}
	return ""
}

func (m HistoryMessageFixture) AttachmentsJSON(tb testing.TB) string {
	tb.Helper()

	if len(m.Attachments) > 0 {
		raw, err := json.Marshal(m.Attachments)
		if err != nil {
			tb.Fatalf("runtimecases: marshal attachments: %v", err)
		}
		return string(raw)
	}
	return ""
}

func Bool(v bool) *bool {
	return &v
}
