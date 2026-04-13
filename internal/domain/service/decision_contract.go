package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

const planningReviewSchema = "planning.review.v1"

func NormalizeDecisionKind(kind string) graphruntime.DecisionKind {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "", "none":
		return graphruntime.DecisionKindNone
	case "plan_review", "planning", "plan":
		return graphruntime.DecisionKindPlanReview
	case "reflection", "reflect":
		return graphruntime.DecisionKindReflection
	case "evaluation", "evaluate":
		return graphruntime.DecisionKindEvaluation
	default:
		return graphruntime.DecisionKind(strings.TrimSpace(strings.ToLower(kind)))
	}
}

func DecisionFromSnapshot(snap *graphruntime.RunSnapshot) *graphruntime.DecisionContractState {
	if pending := PendingDecisionFromSnapshot(snap); pending != nil {
		return pending
	}
	if snap == nil {
		return nil
	}
	if decision := explicitDecisionFromSnapshot(snap); decision != nil {
		return decision
	}
	if review := reflectionDecisionFromSnapshot(snap); review != nil {
		return review
	}
	if eval := evaluationDecisionFromSnapshot(snap); eval != nil {
		return eval
	}
	return nil
}

func explicitDecisionFromSnapshot(snap *graphruntime.RunSnapshot) *graphruntime.DecisionContractState {
	if snap == nil {
		return nil
	}
	decision := snap.TurnState.Intelligence.Decision
	if !decision.Valid || decision.Kind == graphruntime.DecisionKindNone {
		return nil
	}
	decision.Decision = strings.TrimSpace(strings.ToLower(decision.Decision))
	decision.Reason = strings.TrimSpace(decision.Reason)
	decision.Feedback = strings.TrimSpace(decision.Feedback)
	return &decision
}

func setDecisionContract(state *graphruntime.TurnState, contract graphruntime.DecisionContractState) {
	if state != nil {
		state.Intelligence.Decision = contract
	}
}

func planningPendingDecisionContract(planning graphruntime.PlanningState) graphruntime.DecisionContractState {
	return graphruntime.DecisionContractState{
		Kind:         graphruntime.DecisionKindPlanReview,
		SchemaName:   planningReviewSchema,
		Feedback:     strings.TrimSpace(planning.ReviewFeedback),
		ResumeAction: "resume_run",
		Valid:        true,
	}
}

func planningAppliedDecisionContract(planning graphruntime.PlanningState) graphruntime.DecisionContractState {
	return graphruntime.DecisionContractState{
		Kind:       graphruntime.DecisionKindPlanReview,
		SchemaName: planningReviewSchema,
		Decision:   strings.TrimSpace(strings.ToLower(planning.ReviewDecision)),
		Feedback:   strings.TrimSpace(planning.ReviewFeedback),
		AppliedAt:  planning.ReviewedAt,
		Valid:      strings.TrimSpace(planning.ReviewDecision) != "",
	}
}

func reflectionDecisionContract(review reflectionReview) graphruntime.DecisionContractState {
	return graphruntime.DecisionContractState{
		Kind:       graphruntime.DecisionKindReflection,
		SchemaName: graphReflectionSchema,
		Decision:   strings.TrimSpace(strings.ToLower(review.Decision)),
		Reason:     strings.TrimSpace(review.Reason),
		Valid:      strings.TrimSpace(review.Decision) != "",
	}
}

func evaluationDecisionContract(eval graphruntime.EvaluationState) graphruntime.DecisionContractState {
	if !eval.Valid {
		return graphruntime.DecisionContractState{}
	}
	schemaName := strings.TrimSpace(eval.SchemaName)
	if schemaName == "" {
		schemaName = graphEvaluationSchema
	}
	decision := "failed"
	if eval.Passed {
		decision = "passed"
	}
	reason := strings.TrimSpace(eval.ErrorType)
	if reason == "" && len(eval.Issues) > 0 {
		reason = strings.TrimSpace(eval.Issues[0].Description)
	}
	return graphruntime.DecisionContractState{
		Kind:       graphruntime.DecisionKindEvaluation,
		SchemaName: schemaName,
		Decision:   decision,
		Reason:     reason,
		Valid:      true,
	}
}

func NewDecisionContract(kind, decision, feedback string) (*graphruntime.DecisionContractState, error) {
	normalizedKind := NormalizeDecisionKind(kind)
	contract := &graphruntime.DecisionContractState{
		Kind:      normalizedKind,
		Feedback:  strings.TrimSpace(feedback),
		AppliedAt: time.Now().UTC(),
	}

	switch normalizedKind {
	case graphruntime.DecisionKindNone:
		return contract, nil
	case graphruntime.DecisionKindPlanReview:
		switch strings.TrimSpace(strings.ToLower(decision)) {
		case "approve", "approved", "accept", "accepted":
			contract.Decision = "approved"
			contract.Valid = true
		case "revise", "reject", "rejected":
			contract.Decision = "revise"
			contract.Valid = true
		default:
			return nil, agenterr.NewValidation("decision", "unsupported plan review decision")
		}
	case graphruntime.DecisionKindReflection:
		switch strings.TrimSpace(strings.ToLower(decision)) {
		case "accept", "accepted", "approve", "approved":
			contract.Decision = "accept"
			contract.Valid = true
		case "revise", "reject", "rejected":
			contract.Decision = "revise"
			contract.Valid = true
		default:
			return nil, agenterr.NewValidation("decision", "unsupported reflection decision")
		}
	case graphruntime.DecisionKindEvaluation:
		switch strings.TrimSpace(strings.ToLower(decision)) {
		case "pass", "passed":
			contract.Decision = "passed"
			contract.Valid = true
		case "fail", "failed":
			contract.Decision = "failed"
			contract.Valid = true
		default:
			return nil, agenterr.NewValidation("decision", "unsupported evaluation decision")
		}
	default:
		return nil, agenterr.NewValidation("kind", "unsupported decision kind")
	}

	return contract, nil
}

func PendingDecisionFromSnapshot(snap *graphruntime.RunSnapshot) *graphruntime.DecisionContractState {
	return newWaitSnapshotView(snap).pendingDecision()
}

func reflectionDecisionFromSnapshot(snap *graphruntime.RunSnapshot) *graphruntime.DecisionContractState {
	if snap == nil {
		return nil
	}
	review := snap.TurnState.Intelligence.Review
	if !review.Valid || strings.TrimSpace(review.Decision) == "" {
		return nil
	}
	return &graphruntime.DecisionContractState{
		Kind:       graphruntime.DecisionKindReflection,
		SchemaName: review.SchemaName,
		Decision:   strings.TrimSpace(strings.ToLower(review.Decision)),
		Reason:     strings.TrimSpace(review.Reason),
		Valid:      true,
	}
}

func evaluationDecisionFromSnapshot(snap *graphruntime.RunSnapshot) *graphruntime.DecisionContractState {
	if snap == nil {
		return nil
	}
	eval := snap.TurnState.Intelligence.Evaluation
	if !eval.Valid || strings.TrimSpace(eval.SchemaName) == "" {
		return nil
	}
	decision := "failed"
	if eval.Passed {
		decision = "passed"
	}
	reason := strings.TrimSpace(eval.ErrorType)
	if reason == "" && len(eval.Issues) > 0 {
		reason = strings.TrimSpace(eval.Issues[0].Description)
	}
	return &graphruntime.DecisionContractState{
		Kind:       graphruntime.DecisionKindEvaluation,
		SchemaName: eval.SchemaName,
		Decision:   decision,
		Reason:     reason,
		Valid:      true,
	}
}

func applyDecisionToSnapshot(snap *graphruntime.RunSnapshot, contract *graphruntime.DecisionContractState) error {
	if snap == nil || contract == nil {
		return agenterr.NewValidation("decision", "invalid decision contract")
	}
	pending := PendingDecisionFromSnapshot(snap)
	if pending == nil {
		return agenterr.NewValidation("decision", "no pending decision")
	}
	if contract.Kind == graphruntime.DecisionKindNone {
		contract.Kind = pending.Kind
	}
	if !contract.Valid {
		return agenterr.NewValidation("decision", "invalid decision contract")
	}
	if contract.Kind != pending.Kind {
		return agenterr.NewValidation("kind", fmt.Sprintf("pending decision kind is %q", pending.Kind))
	}

	switch contract.Kind {
	case graphruntime.DecisionKindPlanReview:
		planning := snap.TurnState.Intelligence.Planning
		if !planning.Required || !planning.ReviewRequired {
			return agenterr.NewValidation("decision", "planning review is not pending")
		}
		switch contract.Decision {
		case "approved":
			planning.ReviewDecision = "approved"
		case "revise":
			planning.ReviewDecision = "revise"
		default:
			return agenterr.NewValidation("decision", fmt.Sprintf("unsupported planning decision %q", contract.Decision))
		}
		planning.ReviewFeedback = strings.TrimSpace(contract.Feedback)
		planning.ReviewedAt = contract.AppliedAt
		snap.TurnState.Intelligence.Planning = planning
		snap.TurnState.Intelligence.Decision = planningAppliedDecisionContract(planning)
		snap.UpdatedAt = contract.AppliedAt
		return nil
	case graphruntime.DecisionKindReflection:
		review := snap.TurnState.Intelligence.Review
		switch contract.Decision {
		case "accept":
			review.Decision = "accept"
		case "revise":
			review.Decision = "revise"
		default:
			return agenterr.NewValidation("decision", fmt.Sprintf("unsupported reflection decision %q", contract.Decision))
		}
		review.Reason = strings.TrimSpace(contract.Feedback)
		if strings.TrimSpace(review.SchemaName) == "" {
			review.SchemaName = graphReflectionSchema
		}
		review.Valid = true
		snap.TurnState.Intelligence.Review = review
		snap.TurnState.Intelligence.Decision = graphruntime.DecisionContractState{
			Kind:       graphruntime.DecisionKindReflection,
			SchemaName: review.SchemaName,
			Decision:   review.Decision,
			Reason:     review.Reason,
			Feedback:   strings.TrimSpace(contract.Feedback),
			AppliedAt:  contract.AppliedAt,
			Valid:      true,
		}
		snap.UpdatedAt = contract.AppliedAt
		return nil
	case graphruntime.DecisionKindEvaluation:
		eval := snap.TurnState.Intelligence.Evaluation
		switch contract.Decision {
		case "passed":
			eval.Passed = true
		case "failed":
			eval.Passed = false
			eval.ErrorType = strings.TrimSpace(contract.Feedback)
		default:
			return agenterr.NewValidation("decision", fmt.Sprintf("unsupported evaluation decision %q", contract.Decision))
		}
		eval.Valid = true
		snap.TurnState.Intelligence.Evaluation = eval
		snap.TurnState.Intelligence.Decision = evaluationDecisionContract(eval)
		snap.UpdatedAt = contract.AppliedAt
		return nil
	default:
		return agenterr.NewValidation("kind", "unsupported decision kind")
	}
}

func (w waitSnapshotView) pendingDecision() *graphruntime.DecisionContractState {
	if w.snapshot == nil || w.snapshot.Status != graphruntime.NodeStatusWait {
		return nil
	}

	planning := w.snapshot.TurnState.Intelligence.Planning
	if w.snapshot.ExecutionState.WaitReason == graphruntime.WaitReasonUserInput &&
		planning.Required &&
		planning.ReviewRequired &&
		strings.TrimSpace(planning.ReviewDecision) == "" {
		contract := planningPendingDecisionContract(planning)
		return &contract
	}

	return nil
}

func (a *AgentLoop) PendingDecision(ctx context.Context) (*graphruntime.DecisionContractState, error) {
	return a.PendingDecisionForRun(ctx, "")
}

func (a *AgentLoop) PendingDecisionForRun(ctx context.Context, runID string) (*graphruntime.DecisionContractState, error) {
	wait, err := a.latestWaitSnapshotView(ctx)
	if strings.TrimSpace(runID) != "" {
		wait, err = a.waitSnapshotView(ctx, runID)
	}
	if err != nil {
		return nil, err
	}
	return wait.pendingDecision(), nil
}

func (a *AgentLoop) ApplyPendingDecision(ctx context.Context, kind, decision, feedback string) (bool, error) {
	return a.ApplyPendingDecisionToRun(ctx, "", kind, decision, feedback)
}

func (a *AgentLoop) ApplyPendingDecisionToRun(ctx context.Context, runID, kind, decision, feedback string) (bool, error) {
	if a.deps.SnapshotStore == nil {
		return false, nil
	}

	wait, err := a.waitSnapshotView(ctx, runID)
	if err != nil {
		return false, err
	}
	snap := wait.snapshot
	if wait.pendingDecision() == nil || snap == nil {
		return false, nil
	}
	resolvedKind := NormalizeDecisionKind(kind)
	if resolvedKind == graphruntime.DecisionKindNone {
		resolvedKind = wait.pendingDecision().Kind
	}

	contract, err := NewDecisionContract(string(resolvedKind), decision, feedback)
	if err != nil {
		return false, err
	}
	if err := applyDecisionToSnapshot(snap, contract); err != nil {
		return false, err
	}
	if err := a.deps.SnapshotStore.Save(ctx, snap); err != nil {
		return false, err
	}

	return true, a.resumeGraph(ctx, snap.RunID)
}
