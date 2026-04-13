package service

import (
	"testing"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

func TestPendingDecisionFromSnapshot_DetectsPlanReview(t *testing.T) {
	snap := &graphruntime.RunSnapshot{
		Status: graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			Intelligence: graphruntime.IntelligenceState{
				Planning: graphruntime.PlanningState{
					Required:       true,
					ReviewRequired: true,
					ReviewFeedback: "tighten scope",
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			WaitReason: graphruntime.WaitReasonUserInput,
		},
	}

	decision := PendingDecisionFromSnapshot(snap)
	if decision == nil {
		t.Fatal("expected pending decision")
	}
	if decision.Kind != graphruntime.DecisionKindPlanReview {
		t.Fatalf("expected plan review kind, got %q", decision.Kind)
	}
	if decision.SchemaName != planningReviewSchema {
		t.Fatalf("expected planning schema, got %q", decision.SchemaName)
	}
	if decision.ResumeAction != "resume_run" {
		t.Fatalf("expected resume action, got %q", decision.ResumeAction)
	}
	if decision.Feedback != "tighten scope" {
		t.Fatalf("expected feedback passthrough, got %q", decision.Feedback)
	}
}

func TestNewDecisionContract_NormalizesPlanAliasesAndApplies(t *testing.T) {
	snap := &graphruntime.RunSnapshot{
		Status: graphruntime.NodeStatusWait,
		TurnState: graphruntime.TurnState{
			Intelligence: graphruntime.IntelligenceState{
				Planning: graphruntime.PlanningState{
					Required:       true,
					ReviewRequired: true,
				},
			},
		},
		ExecutionState: graphruntime.ExecutionState{
			WaitReason: graphruntime.WaitReasonUserInput,
		},
	}

	contract, err := NewDecisionContract("plan", "reject", "break the work into phases")
	if err != nil {
		t.Fatalf("NewDecisionContract: %v", err)
	}
	if contract.Kind != graphruntime.DecisionKindPlanReview {
		t.Fatalf("expected normalized kind, got %q", contract.Kind)
	}
	if contract.Decision != "revise" {
		t.Fatalf("expected normalized decision, got %q", contract.Decision)
	}

	if err := applyDecisionToSnapshot(snap, contract); err != nil {
		t.Fatalf("applyDecisionToSnapshot: %v", err)
	}

	planning := snap.TurnState.Intelligence.Planning
	if planning.ReviewDecision != "revise" {
		t.Fatalf("expected applied review decision, got %q", planning.ReviewDecision)
	}
	if planning.ReviewFeedback != "break the work into phases" {
		t.Fatalf("expected applied review feedback, got %q", planning.ReviewFeedback)
	}
	if planning.ReviewedAt.IsZero() {
		t.Fatal("expected reviewed timestamp")
	}
	if snap.UpdatedAt.IsZero() {
		t.Fatal("expected snapshot updated timestamp")
	}
}

func TestDecisionFromSnapshot_PrefersReflectionThenEvaluation(t *testing.T) {
	snap := &graphruntime.RunSnapshot{
		TurnState: graphruntime.TurnState{
			Intelligence: graphruntime.IntelligenceState{
				Review: graphruntime.ReviewDecisionState{
					SchemaName: "reflection.review.v1",
					Decision:   "accept",
					Reason:     "looks good",
					Valid:      true,
				},
				Evaluation: graphruntime.EvaluationState{
					SchemaName: "evaluation.report.v1",
					Passed:     false,
					ErrorType:  "coverage_gap",
					Valid:      true,
				},
			},
		},
	}

	decision := DecisionFromSnapshot(snap)
	if decision == nil {
		t.Fatal("expected extracted decision")
	}
	if decision.Kind != graphruntime.DecisionKindReflection {
		t.Fatalf("expected reflection to win, got %q", decision.Kind)
	}
	if decision.Decision != "accept" || decision.Reason != "looks good" {
		t.Fatalf("unexpected reflection decision: %#v", decision)
	}

	snap.TurnState.Intelligence.Review = graphruntime.ReviewDecisionState{}
	decision = DecisionFromSnapshot(snap)
	if decision == nil {
		t.Fatal("expected evaluation decision")
	}
	if decision.Kind != graphruntime.DecisionKindEvaluation {
		t.Fatalf("expected evaluation kind, got %q", decision.Kind)
	}
	if decision.Decision != "failed" || decision.Reason != "coverage_gap" {
		t.Fatalf("unexpected evaluation decision: %#v", decision)
	}
}

func TestDecisionFromSnapshot_PrefersExplicitDecisionContract(t *testing.T) {
	snap := &graphruntime.RunSnapshot{
		TurnState: graphruntime.TurnState{
			Intelligence: graphruntime.IntelligenceState{
				Decision: graphruntime.DecisionContractState{
					Kind:       graphruntime.DecisionKindPlanReview,
					SchemaName: planningReviewSchema,
					Decision:   "approved",
					Feedback:   "ship it",
					Valid:      true,
				},
				Review: graphruntime.ReviewDecisionState{
					SchemaName: graphReflectionSchema,
					Decision:   "revise",
					Reason:     "older fallback",
					Valid:      true,
				},
			},
		},
	}

	decision := DecisionFromSnapshot(snap)
	if decision == nil {
		t.Fatal("expected explicit decision contract")
	}
	if decision.Kind != graphruntime.DecisionKindPlanReview ||
		decision.SchemaName != planningReviewSchema ||
		decision.Decision != "approved" ||
		decision.Feedback != "ship it" {
		t.Fatalf("unexpected explicit decision contract: %#v", decision)
	}
}
