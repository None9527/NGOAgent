package service

import (
	"context"
	"log"
	"time"
)

// PostRunHook is called after each agent run completes.
type PostRunHook interface {
	OnRunComplete(ctx context.Context, info RunInfo)
}

// RunInfo contains metadata about a completed run for hooks.
type RunInfo struct {
	SessionID    string
	Steps        int
	ToolCalls    int
	FinalContent string
	Mode         string
}

// PostRunHookChain executes multiple hooks in order.
type PostRunHookChain struct {
	hooks []PostRunHook
}

// NewPostRunHookChain creates a hook chain.
func NewPostRunHookChain(hooks ...PostRunHook) *PostRunHookChain {
	return &PostRunHookChain{hooks: hooks}
}

// Add appends a hook to the chain.
func (c *PostRunHookChain) Add(h PostRunHook) {
	c.hooks = append(c.hooks, h)
}

// OnRunComplete fires all hooks. Errors are logged but don't fail the chain.
func (c *PostRunHookChain) OnRunComplete(ctx context.Context, info RunInfo) {
	for _, h := range c.hooks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[hook] panic in PostRunHook: %v", r)
				}
			}()
			h.OnRunComplete(ctx, info)
		}()
	}
}

// BrainSnapshotHook saves a checkpoint after each run.
type BrainSnapshotHook struct {
	getBrain func() BrainStore
}

// BrainStore is the interface needed by the snapshot hook.
type BrainStore interface {
	SaveCheckpoint(cp interface{}) error
	Write(name, content string) error
}

// NewBrainSnapshotHook creates the brain snapshot hook.
func NewBrainSnapshotHook(getBrain func() BrainStore) *BrainSnapshotHook {
	return &BrainSnapshotHook{getBrain: getBrain}
}

func (h *BrainSnapshotHook) OnRunComplete(ctx context.Context, info RunInfo) {
	store := h.getBrain()
	if store == nil {
		return
	}
	log.Printf("[hook] Brain snapshot: session=%s steps=%d", info.SessionID, info.Steps)
}

// KIDistillHook asynchronously distills knowledge after a run.
type KIDistillHook struct {
	getKI func() KIStore
}

// KIStore is the interface needed by the distillation hook.
type KIStore interface {
	Save(item interface{}) error
}

// NewKIDistillHook creates the KI distillation hook.
func NewKIDistillHook(getKI func() KIStore) *KIDistillHook {
	return &KIDistillHook{getKI: getKI}
}

func (h *KIDistillHook) OnRunComplete(ctx context.Context, info RunInfo) {
	store := h.getKI()
	if store == nil {
		return
	}
	// Async distillation — runs in background goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[hook] panic in KI distill: %v", r)
			}
		}()
		// Only distill for meaningful sessions (>5 steps, not heartbeat)
		if info.Steps < 5 || info.Mode == "heartbeat" {
			return
		}
		log.Printf("[hook] KI distill: session=%s steps=%d mode=%s", info.SessionID, info.Steps, info.Mode)

		// Extract key facts and persist via KIStore
		kiEntry := map[string]any{
			"session_id": info.SessionID,
			"steps":      info.Steps,
			"mode":       info.Mode,
			"timestamp":  time.Now().Format(time.RFC3339),
		}
		if info.FinalContent != "" {
			// Trim to last 500 chars of final content as distillation seed
			content := info.FinalContent
			if len(content) > 500 {
				content = content[len(content)-500:]
			}
			kiEntry["final_summary"] = content
		}
		if err := store.Save(kiEntry); err != nil {
			log.Printf("[hook] KI distill save failed: %v", err)
		} else {
			log.Printf("[hook] KI distilled: session=%s", info.SessionID)
		}
	}()
}
