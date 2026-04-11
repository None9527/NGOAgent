// Package ctxutil provides cross-layer context metadata propagation.
// Used by server, engine, security, and tool layers.
package ctxutil

import "context"

type contextKey int

const (
	keySessionID contextKey = iota
	keyChannel
	keyTraceID
	keyMode
	keyWorkspaceDir
	keyActiveForgeID
	keyRunID
	keyRuntimeIngress
)

type RuntimeIngressMetadata struct {
	Kind         string
	Source       string
	Trigger      string
	RunID        string
	DecisionKind string
	Decision     string
}

// --- Setters (Server layer entry point) ---

func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keySessionID, id)
}

func WithChannel(ctx context.Context, ch string) context.Context {
	return context.WithValue(ctx, keyChannel, ch)
}

func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyTraceID, id)
}

func WithMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, keyMode, mode)
}

func WithWorkspaceDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, keyWorkspaceDir, dir)
}

func WithRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRunID, id)
}

func WithRuntimeIngress(ctx context.Context, meta RuntimeIngressMetadata) context.Context {
	return context.WithValue(ctx, keyRuntimeIngress, meta)
}

// WithActiveForgeID sets the forge signal. Security uses this to route forge policy.
// Set by forge tool on setup, cleared on cleanup.
func WithActiveForgeID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyActiveForgeID, id)
}

// --- Getters (any downstream layer) ---

func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(keySessionID).(string); ok {
		return v
	}
	return ""
}

func ChannelFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(keyChannel).(string); ok {
		return v
	}
	return ""
}

func TraceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(keyTraceID).(string); ok {
		return v
	}
	return ""
}

func ModeFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(keyMode).(string); ok {
		return v
	}
	return "chat"
}

func WorkspaceDirFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(keyWorkspaceDir).(string); ok {
		return v
	}
	return ""
}

func RunIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(keyRunID).(string); ok {
		return v
	}
	return ""
}

func RuntimeIngressFromContext(ctx context.Context) RuntimeIngressMetadata {
	if v, ok := ctx.Value(keyRuntimeIngress).(RuntimeIngressMetadata); ok {
		return v
	}
	return RuntimeIngressMetadata{}
}

// ActiveEvoIDFromContext returns the active evo ID. Empty means no evo in progress.
func ActiveEvoIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(keyActiveForgeID).(string); ok {
		return v
	}
	return ""
}
