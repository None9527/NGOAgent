package apitype

import "strings"

func ResolveRuntimeRunID(run RuntimeRunTarget, runID string) string {
	if strings.TrimSpace(run.RunID) != "" {
		return strings.TrimSpace(run.RunID)
	}
	return strings.TrimSpace(runID)
}

func NormalizeRuntimeDecisionInput(input RuntimeDecisionContractInput) RuntimeDecisionContractInput {
	input.Kind = strings.TrimSpace(input.Kind)
	input.Decision = strings.TrimSpace(input.Decision)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Feedback = strings.TrimSpace(input.Feedback)
	if input.Feedback == "" {
		input.Feedback = input.Reason
	}
	return input
}
