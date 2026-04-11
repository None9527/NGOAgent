package application

import (
	"context"
	"fmt"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func (a *RuntimeCommands) ReviewPlan(ctx context.Context, sessionID string, approved bool, feedback string) error {
	decision := "revise"
	if approved {
		decision = "approved"
	}
	ctx = withRuntimeIngress(ctx, runtimeIngressMeta{
		kind:         "decision",
		source:       "review_plan",
		trigger:      "plan_review",
		decisionKind: string(graphruntime.DecisionKindPlanReview),
		decision:     decision,
	})
	handled, err := a.withAcquiredSessionLoop(sessionID, "plan_review", func(loop *service.AgentLoop) (bool, error) {
		return loop.ReviewPendingPlan(ctx, approved, feedback)
	})
	if handled || err != nil {
		return err
	}
	return fmt.Errorf("no pending planning review for session %s", sessionID)
}

func (a *RuntimeCommands) ApplyDecision(ctx context.Context, sessionID, kind, decision, feedback string) error {
	_, err := a.applyDecisionToRun(ctx, sessionID, "", kind, decision, feedback)
	return err
}

func (a *RuntimeCommands) ApplyDecisionToRun(ctx context.Context, sessionID, runID, kind, decision, feedback string) error {
	_, err := a.applyDecisionToRun(ctx, sessionID, runID, kind, decision, feedback)
	return err
}

func (a *RuntimeCommands) applyDecisionToRun(ctx context.Context, sessionID, runID, kind, decision, feedback string) (string, error) {
	resolvedKind := kind
	handled, err := a.withAcquiredSessionLoop(sessionID, "decision_apply", func(loop *service.AgentLoop) (bool, error) {
		if service.NormalizeDecisionKind(kind) == graphruntime.DecisionKindNone {
			pending, err := loop.PendingDecisionForRun(ctx, runID)
			if err != nil {
				return false, err
			}
			if pending != nil {
				resolvedKind = string(pending.Kind)
			}
		}
		decisionCtx := withRuntimeIngress(ctx, runtimeIngressMeta{
			kind:         "decision",
			source:       "apply_decision",
			trigger:      "decision_apply",
			runID:        runID,
			decisionKind: resolvedKind,
			decision:     decision,
		})
		return loop.ApplyPendingDecisionToRun(decisionCtx, runID, resolvedKind, decision, feedback)
	})
	if handled || err != nil {
		return resolvedKind, err
	}
	if runID != "" {
		return resolvedKind, agenterr.NewNotFound("pending_decision", runID)
	}
	return resolvedKind, agenterr.NewNotFound("decision", "pending")
}

func (a *RuntimeCommands) ResumeRun(ctx context.Context, sessionID, runID string) error {
	ctx = withRuntimeIngress(ctx, runtimeIngressMeta{
		kind:   "resume",
		source: "resume_run",
		runID:  runID,
	})
	handled, err := a.withAcquiredSessionLoop(sessionID, "resume_run", func(loop *service.AgentLoop) (bool, error) {
		return loop.ResumeRun(ctx, runID)
	})
	if handled || err != nil {
		return err
	}
	if runID != "" {
		return agenterr.NewNotFound("run", runID)
	}
	return agenterr.NewNotFound("run", "pending")
}

func (a *RuntimeCommands) ApplyRuntimeIngress(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressResponse, error) {
	req = apitype.NormalizeRuntimeIngressRequest(req)
	resp := apitype.RuntimeIngressResponse{
		Status:    runtimeIngressStatus(req),
		SessionID: req.SessionID,
		Ingress:   req.Ingress,
	}
	if req.SessionID == "" {
		return resp, agenterr.NewValidation("session_id", "is required")
	}
	ctx = a.runtimeIngressContext(ctx, req)
	facades := newApplicationFacades(a.ApplicationKernel)
	normalizedIngress, err := a.dispatchRuntimeIngress(ctx, req, facades)
	if err != nil {
		return resp, err
	}
	resp.Ingress = normalizedIngress

	return resp, nil
}
