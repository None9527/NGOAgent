package apitype

import (
	"strings"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
)

func NormalizeRuntimeRunTarget(run RuntimeRunTarget, runID string) RuntimeRunTarget {
	resolved := ResolveRuntimeRunID(run, runID)
	if resolved == "" {
		return RuntimeRunTarget{}
	}
	return RuntimeRunTarget{RunID: resolved}
}

func NormalizeRuntimeIngressInput(input RuntimeIngressInput) RuntimeIngressInput {
	input.Kind = strings.TrimSpace(input.Kind)
	input.Source = strings.TrimSpace(input.Source)
	input.Trigger = strings.TrimSpace(input.Trigger)
	input.Message = strings.TrimSpace(input.Message)
	input.Mode = strings.TrimSpace(input.Mode)
	input.Run = NormalizeRuntimeRunTarget(input.Run, input.RunID)
	input.RunID = input.Run.RunID
	input.Decision = NormalizeRuntimeDecisionInput(input.Decision)
	return input
}

func NormalizeRuntimeIngressRequest(req RuntimeIngressRequest) RuntimeIngressRequest {
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Ingress = NormalizeRuntimeIngressInput(req.Ingress)
	return req
}

func CompleteRuntimeIngressRequest(req RuntimeIngressRequest) RuntimeIngressRequest {
	req = NormalizeRuntimeIngressRequest(req)
	if req.Ingress.Source == "" {
		req.Ingress.Source = defaultRuntimeIngressSource(req.Ingress.Kind)
	}
	if req.Ingress.Trigger == "" {
		req.Ingress.Trigger = defaultRuntimeIngressTrigger(req.Ingress.Kind)
	}
	return req
}

func ValidateRuntimeIngressRequest(req RuntimeIngressRequest) error {
	if req.SessionID == "" {
		return agenterr.NewValidation("session_id", "is required")
	}
	switch req.Ingress.Kind {
	case "message":
		if req.Ingress.Message == "" {
			return agenterr.NewValidation("ingress.message", "is required")
		}
	case "decision":
		if req.Ingress.Decision.Decision == "" {
			return agenterr.NewValidation("ingress.decision.decision", "is required")
		}
	case "resume":
		return nil
	case "reconnect":
		return nil
	default:
		return agenterr.NewValidation("ingress.kind", "unsupported ingress kind")
	}
	return nil
}

func NewRuntimeResumeIngressRequest(req RuntimeResumeRequest) RuntimeIngressRequest {
	return CompleteRuntimeIngressRequest(RuntimeIngressRequest{
		SessionID: strings.TrimSpace(req.SessionID),
		Ingress: RuntimeIngressInput{
			Kind: "resume",
			Run:  NormalizeRuntimeRunTarget(req.Run, req.RunID),
		},
	})
}

func NewRuntimeDecisionIngressRequest(req RuntimeDecisionApplyRequest) RuntimeIngressRequest {
	return CompleteRuntimeIngressRequest(RuntimeIngressRequest{
		SessionID: strings.TrimSpace(req.SessionID),
		Ingress: RuntimeIngressInput{
			Kind:     "decision",
			Run:      NormalizeRuntimeRunTarget(req.Run, req.RunID),
			Decision: NormalizeRuntimeDecisionInput(req.Decision),
		},
	})
}

func RuntimeResumeResponseFromIngress(resp RuntimeIngressResponse) RuntimeResumeResponse {
	return RuntimeResumeResponse{
		Status:    resp.Status,
		SessionID: resp.SessionID,
		Run:       NormalizeRuntimeRunTarget(resp.Ingress.Run, resp.Ingress.RunID),
	}
}

func RuntimeDecisionApplyResponseFromIngress(resp RuntimeIngressResponse) RuntimeDecisionApplyResponse {
	return RuntimeDecisionApplyResponse{
		Status:    resp.Status,
		SessionID: resp.SessionID,
		Run:       NormalizeRuntimeRunTarget(resp.Ingress.Run, resp.Ingress.RunID),
		Decision:  NormalizeRuntimeDecisionInput(resp.Ingress.Decision),
	}
}

func defaultRuntimeIngressSource(kind string) string {
	switch kind {
	case "message":
		return "runtime_ingress"
	case "decision":
		return "decision_apply"
	case "resume":
		return "resume_run"
	case "reconnect":
		return "runtime_ingress"
	default:
		return ""
	}
}

func defaultRuntimeIngressTrigger(kind string) string {
	switch kind {
	case "message":
		return "message"
	case "decision":
		return "decision_apply"
	case "resume":
		return "resume_run"
	case "reconnect":
		return "reconnect"
	default:
		return ""
	}
}
