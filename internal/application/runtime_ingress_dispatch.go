package application

import (
	"context"
	"strings"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func (a *RuntimeCommands) runtimeIngressContext(ctx context.Context, req apitype.RuntimeIngressRequest) context.Context {
	return withRuntimeIngress(ctx, runtimeIngressMeta{
		kind:         req.Ingress.Kind,
		source:       req.Ingress.Source,
		trigger:      req.Ingress.Trigger,
		runID:        resolvedRuntimeRunID(req.Ingress.Run, req.Ingress.RunID),
		decisionKind: req.Ingress.Decision.Kind,
		decision:     req.Ingress.Decision.Decision,
	})
}

func (a *RuntimeCommands) dispatchRuntimeIngress(
	ctx context.Context,
	req apitype.RuntimeIngressRequest,
	facades *applicationFacades,
) (apitype.RuntimeIngressInput, error) {
	switch req.Ingress.Kind {
	case "message":
		return a.applyRuntimeIngressMessage(ctx, req, facades)
	case "decision":
		return a.applyRuntimeIngressDecision(ctx, req)
	case "resume":
		return a.applyRuntimeIngressResume(ctx, req)
	case "reconnect":
		return a.applyRuntimeIngressReconnect(ctx, req, facades)
	default:
		return apitype.RuntimeIngressInput{}, agenterr.NewValidation("ingress.kind", "unsupported ingress kind")
	}
}

func (a *RuntimeCommands) applyRuntimeIngressMessage(
	ctx context.Context,
	req apitype.RuntimeIngressRequest,
	facades *applicationFacades,
) (apitype.RuntimeIngressInput, error) {
	if strings.TrimSpace(req.Ingress.Message) == "" {
		return apitype.RuntimeIngressInput{}, agenterr.NewValidation("ingress.message", "is required")
	}
	if err := facades.chatCommands.ChatStream(ctx, req.SessionID, req.Ingress.Message, req.Ingress.Mode, &service.Delta{}); err != nil {
		return apitype.RuntimeIngressInput{}, err
	}
	return req.Ingress, nil
}

func (a *RuntimeCommands) applyRuntimeIngressDecision(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressInput, error) {
	decision := apitype.NormalizeRuntimeDecisionInput(req.Ingress.Decision)
	if strings.TrimSpace(decision.Decision) == "" {
		return apitype.RuntimeIngressInput{}, agenterr.NewValidation("ingress.decision.decision", "is required")
	}
	runID := apitype.ResolveRuntimeRunID(req.Ingress.Run, req.Ingress.RunID)
	resolvedKind, err := a.applyDecisionToRun(
		ctx,
		req.SessionID,
		runID,
		decision.Kind,
		decision.Decision,
		decision.Feedback,
	)
	if err != nil {
		return apitype.RuntimeIngressInput{}, err
	}
	normalized := req.Ingress
	normalized.Decision = decision
	normalized.Decision.Kind = resolvedKind
	if strings.TrimSpace(normalized.Run.RunID) == "" && runID != "" {
		normalized.Run = apitype.RuntimeRunTarget{RunID: runID}
	}
	return normalized, nil
}

func (a *RuntimeCommands) applyRuntimeIngressResume(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressInput, error) {
	runID := req.Ingress.Run.RunID
	if runID == "" {
		runID = req.Ingress.RunID
	}
	if err := a.ResumeRun(ctx, req.SessionID, runID); err != nil {
		return apitype.RuntimeIngressInput{}, err
	}
	return req.Ingress, nil
}

func (a *RuntimeCommands) applyRuntimeIngressReconnect(
	ctx context.Context,
	req apitype.RuntimeIngressRequest,
	facades *applicationFacades,
) (apitype.RuntimeIngressInput, error) {
	if err := facades.chatCommands.ChatStream(ctx, req.SessionID, "", req.Ingress.Mode, &service.Delta{}); err != nil {
		return apitype.RuntimeIngressInput{}, err
	}
	return req.Ingress, nil
}
