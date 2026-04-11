package service

import "testing"

func TestParseEvalResult_ProducesStructuredEvaluationContract(t *testing.T) {
	result, err := parseEvalResult(`{"score":0.42,"error_type":"tool_wrong","issues":[{"severity":"warning","description":"wrong tool"}]}`, 0.7)
	if err != nil {
		t.Fatalf("parseEvalResult error: %v", err)
	}
	if !result.Valid || result.SchemaName != graphEvaluationSchema {
		t.Fatalf("expected structured evaluation contract metadata, got %#v", result)
	}
	if result.Passed {
		t.Fatalf("expected low score to fail threshold, got %#v", result)
	}
	if len(result.Issues) != 1 || result.Issues[0].Description != "wrong tool" {
		t.Fatalf("unexpected issues payload: %#v", result.Issues)
	}
	if result.RawJSON == "" {
		t.Fatal("expected raw json to be preserved")
	}
}

func TestRepairRouterRoute_ProducesExecutableRepairDecision(t *testing.T) {
	router := &RepairRouter{}
	plan := router.Route(&EvalResult{
		Score:     0.33,
		ErrorType: "tool_wrong",
		Issues: []EvalIssue{{
			Severity:    "warning",
			Description: "picked the wrong tool",
		}},
	})

	if !plan.Allowed {
		t.Fatalf("expected routed repair to be executable, got %#v", plan)
	}
	if plan.Strategy != StrategyToolSwap {
		t.Fatalf("expected tool swap strategy, got %#v", plan)
	}
	if plan.Ephemeral == "" || plan.Description == "" {
		t.Fatalf("expected repair decision to include operator guidance, got %#v", plan)
	}
}
