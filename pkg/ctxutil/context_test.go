package ctxutil

import (
	"context"
	"testing"
)

func TestWithRunIDRoundTrip(t *testing.T) {
	ctx := WithRunID(context.Background(), "run-123")
	if got := RunIDFromContext(ctx); got != "run-123" {
		t.Fatalf("expected run id round-trip, got %q", got)
	}
}

func TestWithRuntimeIngressRoundTrip(t *testing.T) {
	meta := RuntimeIngressMetadata{
		Kind:         "decision",
		Source:       "runtime_ingress",
		Trigger:      "plan_review",
		RunID:        "run-123",
		DecisionKind: "plan_review",
		Decision:     "approved",
	}
	ctx := WithRuntimeIngress(context.Background(), meta)
	if got := RuntimeIngressFromContext(ctx); got != meta {
		t.Fatalf("expected ingress metadata round-trip, got %#v", got)
	}
}
