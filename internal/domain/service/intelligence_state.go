package service

import "github.com/ngoclaw/ngoagent/internal/domain/graphruntime"

func (a *AgentLoop) intelligenceSnapshot() graphruntime.IntelligenceState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return cloneIntelligenceState(a.intelligence)
}

func (a *AgentLoop) consumeIntelligenceSnapshot() graphruntime.IntelligenceState {
	a.mu.Lock()
	defer a.mu.Unlock()
	snapshot := cloneIntelligenceState(a.intelligence)
	a.intelligence = graphruntime.IntelligenceState{}
	return snapshot
}

func (a *AgentLoop) setReviewDecision(review graphruntime.ReviewDecisionState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intelligence.Review = review
	a.intelligence.Decision = graphruntime.DecisionContractState{
		Kind:       graphruntime.DecisionKindReflection,
		SchemaName: review.SchemaName,
		Decision:   review.Decision,
		Reason:     review.Reason,
		Valid:      review.Valid && review.Decision != "",
	}
}

func (a *AgentLoop) setPlanningDecision(planning graphruntime.PlanningState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intelligence.Planning = planning
	if planning.ReviewRequired && planning.ReviewDecision == "" {
		a.intelligence.Decision = planningPendingDecisionContract(planning)
		return
	}
	if planning.ReviewDecision != "" {
		a.intelligence.Decision = planningAppliedDecisionContract(planning)
	}
}

func (a *AgentLoop) setEvaluationDecision(eval graphruntime.EvaluationState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intelligence.Evaluation = eval
	a.intelligence.Decision = evaluationDecisionContract(eval)
}

func (a *AgentLoop) setRepairDecision(repair graphruntime.RepairState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intelligence.Repair = repair
}

func cloneIntelligenceState(in graphruntime.IntelligenceState) graphruntime.IntelligenceState {
	out := in
	out.Planning.MissingArtifacts = append([]string(nil), in.Planning.MissingArtifacts...)
	out.Evaluation.Issues = append([]graphruntime.EvaluationIssueState(nil), in.Evaluation.Issues...)
	return out
}

func intelligenceStateEmpty(in graphruntime.IntelligenceState) bool {
	return !in.Decision.Valid &&
		in.Decision.Kind == graphruntime.DecisionKindNone &&
		!in.Planning.Required &&
		!in.Planning.ReviewRequired &&
		in.Planning.ReviewDecision == "" &&
		in.Planning.ReviewFeedback == "" &&
		in.Planning.ReviewedAt.IsZero() &&
		in.Planning.Trigger == "" &&
		in.Planning.BoundaryMode == "" &&
		!in.Planning.PlanExists &&
		!in.Planning.TaskExists &&
		!in.Planning.ContextTight &&
		len(in.Planning.MissingArtifacts) == 0 &&
		!in.Review.Valid &&
		!in.Evaluation.Valid &&
		!in.Repair.Allowed &&
		!in.Repair.Attempted &&
		!in.Repair.Success &&
		in.Repair.BlockReason == "" &&
		in.Repair.Strategy == "" &&
		in.Repair.Description == "" &&
		in.Repair.Ephemeral == ""
}
