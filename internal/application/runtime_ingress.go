package application

import (
	"context"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
)

type runtimeIngressMeta struct {
	kind         string
	source       string
	trigger      string
	runID        string
	decisionKind string
	decision     string
}

func withRuntimeIngress(ctx context.Context, meta runtimeIngressMeta) context.Context {
	if strings.TrimSpace(meta.kind) == "" {
		return ctx
	}
	if existing := ctxutil.RuntimeIngressFromContext(ctx); existing.Kind != "" {
		if strings.TrimSpace(existing.Source) == "" {
			existing.Source = strings.TrimSpace(meta.source)
		}
		if strings.TrimSpace(existing.Trigger) == "" {
			existing.Trigger = strings.TrimSpace(meta.trigger)
		}
		if strings.TrimSpace(existing.RunID) == "" {
			existing.RunID = strings.TrimSpace(meta.runID)
		}
		if strings.TrimSpace(existing.DecisionKind) == "" {
			existing.DecisionKind = strings.TrimSpace(meta.decisionKind)
		}
		if strings.TrimSpace(existing.Decision) == "" {
			existing.Decision = strings.TrimSpace(meta.decision)
		}
		return ctxutil.WithRuntimeIngress(ctx, existing)
	}
	return ctxutil.WithRuntimeIngress(ctx, ctxutil.RuntimeIngressMetadata{
		Kind:         strings.TrimSpace(meta.kind),
		Source:       strings.TrimSpace(meta.source),
		Trigger:      strings.TrimSpace(meta.trigger),
		RunID:        strings.TrimSpace(meta.runID),
		DecisionKind: strings.TrimSpace(meta.decisionKind),
		Decision:     strings.TrimSpace(meta.decision),
	})
}

func resolvedRuntimeRunID(run apitype.RuntimeRunTarget, fallback string) string {
	if strings.TrimSpace(run.RunID) != "" {
		return strings.TrimSpace(run.RunID)
	}
	return strings.TrimSpace(fallback)
}

func runtimeIngressStatus(req apitype.RuntimeIngressRequest) string {
	switch req.Ingress.Kind {
	case "decision":
		return "applied"
	case "resume":
		return "resumed"
	case "message", "reconnect":
		return "completed"
	default:
		return "processed"
	}
}
