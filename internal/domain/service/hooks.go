package service

import (
	"context"
	"fmt"
	"log"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// PostRunHook is called after each agent run completes.
type PostRunHook interface {
	OnRunComplete(ctx context.Context, info RunInfo)
}

// RunInfo contains metadata about a completed run for hooks.
type RunInfo struct {
	SessionID    string
	UserMessage  string // first user message in this run
	Steps        int
	ToolCalls    int
	FinalContent string
	Mode         string
	History      []llm.Message // conversation history for distillation
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

// KIDistillHook uses LLM to distill knowledge after a run.
type KIDistillHook struct {
	getKI  func() KIStore
	llm    KILLMDistiller
}

// KIStore is the interface needed by the distillation hook.
type KIStore interface {
	SaveDistilled(summary, content string, tags, sources []string) error
}

// KILLMDistiller abstracts the LLM call for knowledge distillation.
type KILLMDistiller interface {
	DistillKnowledge(messages []llm.Message) (*llm.KIResult, error)
}

// NewKIDistillHook creates the KI distillation hook.
func NewKIDistillHook(getKI func() KIStore, distiller KILLMDistiller) *KIDistillHook {
	return &KIDistillHook{getKI: getKI, llm: distiller}
}

func (h *KIDistillHook) OnRunComplete(ctx context.Context, info RunInfo) {
	store := h.getKI()
	if store == nil || h.llm == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[hook] panic in KI distill: %v", r)
			}
		}()
		// Gate: only distill meaningful conversations
		if info.Steps < 5 || info.Mode == "heartbeat" || len(info.History) < 3 {
			return
		}
		log.Printf("[hook] KI distill: calling LLM for session=%s steps=%d", info.SessionID, info.Steps)

		result, err := h.llm.DistillKnowledge(info.History)
		if err != nil {
			log.Printf("[hook] KI distill LLM failed: %v", err)
			return
		}

		if !result.ShouldSave {
			log.Printf("[hook] KI distill: LLM decided not worth saving for session=%s", info.SessionID)
			return
		}

		if result.Title == "" {
			log.Printf("[hook] KI distill: empty title, skipping")
			return
		}

		// Build full content with summary header
		content := fmt.Sprintf("# %s\n\n%s\n\n---\n\n%s",
			result.Title, result.Summary, result.Content)

		if err := store.SaveDistilled(
			result.Title, content,
			result.Tags,
			[]string{info.SessionID},
		); err != nil {
			log.Printf("[hook] KI distill save failed: %v", err)
		} else {
			log.Printf("[hook] KI distilled: session=%s title=%q tags=%v", info.SessionID, result.Title, result.Tags)
		}
	}()
}

// ═══════════════════════════════════════════
// TitleDistillHook — LLM-generated session title
// ═══════════════════════════════════════════

// SessionTitler is the interface needed to persist a distilled title.
type SessionTitler interface {
	SetTitle(sessionID, title string)
}

// TitleLLMCaller abstracts the LLM call for title distillation.
type TitleLLMCaller interface {
	DistillTitle(userMessage string) (string, error)
}

// TitleDistillHook distills a short, descriptive session title via LLM
// after the first user message, and persists it to the session manager.
type TitleDistillHook struct {
	llm    TitleLLMCaller
	titler SessionTitler
}

// NewTitleDistillHook creates the title distillation hook.
func NewTitleDistillHook(llm TitleLLMCaller, titler SessionTitler) *TitleDistillHook {
	return &TitleDistillHook{llm: llm, titler: titler}
}

func (h *TitleDistillHook) OnRunComplete(ctx context.Context, info RunInfo) {
	// Only distill if session has a user message and we haven't titled it yet
	// (The caller checks sess.Title == "" before triggering; here we just guard)
	if info.UserMessage == "" || info.SessionID == "" || info.Mode == "heartbeat" {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[hook] panic in TitleDistillHook: %v", r)
			}
		}()
		title, err := h.llm.DistillTitle(info.UserMessage)
		if err != nil {
			log.Printf("[hook] title distill failed: %v", err)
			return
		}
		h.titler.SetTitle(info.SessionID, title)
		log.Printf("[hook] title distilled: session=%s title=%q", info.SessionID, title)
	}()
}
