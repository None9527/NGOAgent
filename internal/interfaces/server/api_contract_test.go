package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func TestDecodeRuntimeDecisionApplyRequest_NewContractShape(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/runtime/decision/apply", strings.NewReader(`{
		"session_id":"session-1",
		"run":{"run_id":"run-1"},
		"decision":{
			"kind":"plan_review",
			"decision":"revise",
			"reason":"needs staging",
			"feedback":"split rollout"
		}
	}`))

	decoded, err := decodeRuntimeDecisionApplyRequest(req)
	if err != nil {
		t.Fatalf("decodeRuntimeDecisionApplyRequest: %v", err)
	}
	if decoded.SessionID != "session-1" {
		t.Fatalf("expected session_id, got %#v", decoded)
	}
	if decoded.Run.RunID != "run-1" {
		t.Fatalf("expected nested run target, got %#v", decoded)
	}
	if decoded.Decision.Kind != "plan_review" || decoded.Decision.Decision != "revise" {
		t.Fatalf("expected nested decision contract, got %#v", decoded.Decision)
	}
	if decoded.Decision.Reason != "needs staging" || decoded.Decision.Feedback != "split rollout" {
		t.Fatalf("expected nested metadata, got %#v", decoded.Decision)
	}
}

func TestDecodeRuntimeDecisionApplyRequest_LegacyFlatShape(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/runtime/decision/apply", strings.NewReader(`{
		"session_id":"session-2",
		"run_id":"run-flat",
		"kind":"plan_review",
		"decision":"approve",
		"feedback":"ship it"
	}`))

	decoded, err := decodeRuntimeDecisionApplyRequest(req)
	if err != nil {
		t.Fatalf("decodeRuntimeDecisionApplyRequest: %v", err)
	}
	if decoded.Decision.Kind != "plan_review" || decoded.Decision.Decision != "approve" {
		t.Fatalf("expected legacy shape normalization, got %#v", decoded.Decision)
	}
	if decoded.RunID != "run-flat" {
		t.Fatalf("expected legacy run target passthrough, got %#v", decoded)
	}
	if decoded.Decision.Feedback != "ship it" {
		t.Fatalf("expected feedback passthrough, got %#v", decoded.Decision)
	}
}

func TestNormalizeRuntimeDecisionInput_UsesReasonAsFeedbackFallback(t *testing.T) {
	normalized := apitype.NormalizeRuntimeDecisionInput(apitype.RuntimeDecisionContractInput{
		Kind:     "plan_review",
		Decision: "revise",
		Reason:   "needs staging",
	})
	if normalized.Feedback != "needs staging" {
		t.Fatalf("expected reason fallback to populate feedback, got %#v", normalized)
	}
}

func TestDecodeRuntimeResumeRequest_PrefersNestedRun(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/runtime/runs/resume", strings.NewReader(`{
		"session_id":"session-3",
		"run":{"run_id":"run-nested"},
		"run_id":"run-flat"
	}`))

	decoded, err := decodeRuntimeResumeRequest(req)
	if err != nil {
		t.Fatalf("decodeRuntimeResumeRequest: %v", err)
	}
	if resolvedRuntimeRunID(decoded) != "run-nested" {
		t.Fatalf("expected nested run to win, got %#v", decoded)
	}
}

func TestDecodeRuntimeIngressRequest_NestedContractShape(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/runtime/ingress", strings.NewReader(`{
		"session_id":"session-6",
		"ingress":{
			"kind":"decision",
			"source":"web",
			"run":{"run_id":"run-6"},
			"decision":{"kind":"plan_review","decision":"approve","feedback":"go"}
		}
	}`))

	decoded, err := decodeRuntimeIngressRequest(req)
	if err != nil {
		t.Fatalf("decodeRuntimeIngressRequest: %v", err)
	}
	if decoded.SessionID != "session-6" || decoded.Ingress.Kind != "decision" {
		t.Fatalf("unexpected ingress envelope: %#v", decoded)
	}
	if decoded.Ingress.Run.RunID != "run-6" || decoded.Ingress.Decision.Decision != "approve" {
		t.Fatalf("unexpected ingress payload: %#v", decoded.Ingress)
	}
}

func TestRuntimeRunsResponse_MarshalsRunsKey(t *testing.T) {
	body, err := json.Marshal(apitype.RuntimeRunListResponse{})
	if err != nil {
		t.Fatalf("marshal RuntimeRunListResponse: %v", err)
	}
	if string(body) != `{"runs":null}` {
		t.Fatalf("unexpected empty runtime response: %s", string(body))
	}
}

func TestRuntimeGraphResponse_MarshalsTopologyKeys(t *testing.T) {
	body, err := json.Marshal(apitype.OrchestrationGraphInfo{
		SessionID:                 "session-graph",
		RootRunIDs:                []string{"run-parent"},
		PendingRunIDs:             []string{"run-parent"},
		PendingRuntimeControlRuns: []string{"run-parent"},
		Summary: apitype.OrchestrationGraphSummary{
			RootRunIDs:                []string{"run-parent"},
			PendingRunIDs:             []string{"run-parent"},
			PendingRuntimeControlRuns: []string{"run-parent"},
			RunCount:                  1,
			IngressNodeCount:          1,
			EdgeCount:                 1,
		},
		IngressNodes: []apitype.RuntimeIngressNodeInfo{{ID: "ingress:resume:web", Category: "runtime_control", Phase: "resume", Kind: "resume", Source: "web"}},
		Nodes:        []apitype.RuntimeRunInfo{{RunID: "run-parent"}},
		Edges:        []apitype.RuntimeEdgeInfo{{Kind: "parent_child", SourceRunID: "run-parent", TargetRunID: "run-child"}},
	})
	if err != nil {
		t.Fatalf("marshal OrchestrationGraphInfo: %v", err)
	}
	if !strings.Contains(string(body), `"session_id":"session-graph"`) ||
		!strings.Contains(string(body), `"root_run_ids":["run-parent"]`) ||
		!strings.Contains(string(body), `"pending_run_ids":["run-parent"]`) ||
		!strings.Contains(string(body), `"pending_runtime_control_run_ids":["run-parent"]`) ||
		!strings.Contains(string(body), `"summary":{"root_run_ids":["run-parent"],"pending_run_ids":["run-parent"],"pending_runtime_control_run_ids":["run-parent"],"run_count":1,"ingress_node_count":1,"edge_count":1}`) ||
		!strings.Contains(string(body), `"ingress_nodes":[{"id":"ingress:resume:web","category":"runtime_control","phase":"resume","kind":"resume","source":"web"}]`) ||
		!strings.Contains(string(body), `"edges":[{"kind":"parent_child","source_run_id":"run-parent","target_run_id":"run-child"}]`) {
		t.Fatalf("unexpected runtime graph response: %s", string(body))
	}
}

func TestRuntimeDecisionResponses_MarshalNestedShapes(t *testing.T) {
	resumeBody, err := json.Marshal(apitype.RuntimeResumeResponse{
		Status:    "resumed",
		SessionID: "session-4",
		Run:       apitype.RuntimeRunTarget{RunID: "run-4"},
	})
	if err != nil {
		t.Fatalf("marshal runtimeResumeResponse: %v", err)
	}
	if !strings.Contains(string(resumeBody), `"run":{"run_id":"run-4"}`) {
		t.Fatalf("unexpected resume response: %s", string(resumeBody))
	}

	decisionBody, err := json.Marshal(apitype.RuntimeDecisionApplyResponse{
		Status:    "applied",
		SessionID: "session-5",
		Run:       apitype.RuntimeRunTarget{RunID: "run-5"},
		Decision: apitype.RuntimeDecisionContractInput{
			Kind:     "plan_review",
			Decision: "revise",
			Reason:   "needs cleanup",
			Feedback: "split tasks",
		},
	})
	if err != nil {
		t.Fatalf("marshal runtimeDecisionApplyResponse: %v", err)
	}
	if !strings.Contains(string(decisionBody), `"decision":{"kind":"plan_review","decision":"revise"`) {
		t.Fatalf("unexpected decision response: %s", string(decisionBody))
	}
	if !strings.Contains(string(decisionBody), `"run":{"run_id":"run-5"}`) {
		t.Fatalf("expected run target in decision response: %s", string(decisionBody))
	}

	ingressBody, err := json.Marshal(apitype.RuntimeIngressResponse{
		Status:    "resumed",
		SessionID: "session-6",
		Ingress: apitype.RuntimeIngressInput{
			Kind:    "resume",
			Source:  "web",
			Run:     apitype.RuntimeRunTarget{RunID: "run-6"},
			Message: "",
		},
	})
	if err != nil {
		t.Fatalf("marshal RuntimeIngressResponse: %v", err)
	}
	if !strings.Contains(string(ingressBody), `"ingress":{"kind":"resume","source":"web","run":{"run_id":"run-6"},"decision":{}}`) {
		t.Fatalf("unexpected ingress response: %s", string(ingressBody))
	}
}

func TestServerResponseDTOs_MarshalStableKeys(t *testing.T) {
	cases := []struct {
		name string
		got  any
		want string
	}{
		{
			name: "apitype.StatusResponse",
			got:  apitype.StatusResponse{Status: "cleared"},
			want: `{"status":"cleared"}`,
		},
		{
			name: "apitype.MessageListResponse",
			got:  apitype.MessageListResponse{Messages: []apitype.HistoryMessage{{Role: "user", Content: "hi"}}},
			want: `{"messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "apitype.ToolListResponse",
			got:  apitype.ToolListResponse{Tools: []apitype.ToolInfoResponse{{Name: "edit", Enabled: true}}},
			want: `{"tools":[{"name":"edit","enabled":true}]}`,
		},
		{
			name: "apitype.SkillContentResponse",
			got:  apitype.SkillContentResponse{Name: "planner", Content: "use the plan"},
			want: `{"name":"planner","content":"use the plan"}`,
		},
		{
			name: "apitype.ArtifactListResponse",
			got:  apitype.ArtifactListResponse{Artifacts: []apitype.BrainArtifactInfo{{Name: "notes.md", Size: 12, ModTime: "2026-04-10T00:00:00Z"}}},
			want: `{"artifacts":[{"name":"notes.md","size":12,"mod_time":"2026-04-10T00:00:00Z"}]}`,
		},
		{
			name: "keyValueResponse",
			got:  apitype.StatusKeyValueResponse{Status: "ok", Key: "theme", Value: "dark"},
			want: `{"status":"ok","key":"theme","value":"dark"}`,
		},
		{
			name: "apitype.CronLogListResponse",
			got:  apitype.CronLogListResponse{Logs: []apitype.CronLogInfo{{File: "a.log", Time: "now", Size: 1, Success: true}}},
			want: `{"logs":[{"file":"a.log","time":"now","size":1,"success":true}]}`,
		},
		{
			name: "apitype.FileContentResponse",
			got:  apitype.FileContentResponse{File: "x.txt", Content: "hello"},
			want: `{"file":"x.txt","content":"hello"}`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.got)
			if err != nil {
				t.Fatalf("marshal %s: %v", tt.name, err)
			}
			if string(body) != tt.want {
				t.Fatalf("unexpected json for %s: %s", tt.name, string(body))
			}
		})
	}
}
