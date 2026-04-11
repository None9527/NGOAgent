package apitype

import "strings"

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

func NewRuntimeResumeIngressRequest(req RuntimeResumeRequest) RuntimeIngressRequest {
	return NormalizeRuntimeIngressRequest(RuntimeIngressRequest{
		SessionID: strings.TrimSpace(req.SessionID),
		Ingress: RuntimeIngressInput{
			Kind:    "resume",
			Source:  "resume_run",
			Trigger: "resume_run",
			Run:     NormalizeRuntimeRunTarget(req.Run, req.RunID),
		},
	})
}

func NewRuntimeDecisionIngressRequest(req RuntimeDecisionApplyRequest) RuntimeIngressRequest {
	return NormalizeRuntimeIngressRequest(RuntimeIngressRequest{
		SessionID: strings.TrimSpace(req.SessionID),
		Ingress: RuntimeIngressInput{
			Kind:     "decision",
			Source:   "decision_apply",
			Trigger:  "decision_apply",
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
