package service

import (
	"context"
	"fmt"
	"log"
	"time"

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
	Delta        DeltaSink     // SSE event sink for real-time push (e.g. title_updated)
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
	getKI          func() KIStore
	llm            KILLMDistiller
	dedup          KIDuplicateChecker // optional: embedding-based dedup
	dedupThreshold float64            // cosine similarity threshold for dedup (default 0.60)
}

// KIStore is the interface needed by the distillation hook.
type KIStore interface {
	SaveDistilled(summary, content string, tags, sources []string) error
	UpdateContent(id, newContent string) error // Append/merge content for existing KI
}

// KIDuplicateChecker detects duplicate KIs using embedding similarity.
type KIDuplicateChecker interface {
	FindDuplicate(text string, threshold float64) (string, float64)
	EmbedAndIndexByID(id string) error // Re-index after update
}

// KILLMDistiller abstracts the LLM call for knowledge distillation.
type KILLMDistiller interface {
	DistillKnowledge(messages []llm.Message) (*llm.KIResult, error)
}

// NewKIDistillHook creates the KI distillation hook.
// dedup can be nil if embedding is not configured.
func NewKIDistillHook(getKI func() KIStore, distiller KILLMDistiller, threshold float64, dedup ...KIDuplicateChecker) *KIDistillHook {
	h := &KIDistillHook{getKI: getKI, llm: distiller, dedupThreshold: threshold}
	if h.dedupThreshold <= 0 {
		h.dedupThreshold = 0.60
	}
	if len(dedup) > 0 {
		h.dedup = dedup[0]
	}
	return h
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
		if info.Steps < 5 || len(info.History) < 3 {
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

		// Dedup check: if a similar KI exists, update instead of create
		if h.dedup != nil {
			queryText := result.Title + "\n" + result.Summary
			dupID, score := h.dedup.FindDuplicate(queryText, h.dedupThreshold)
			if dupID != "" {
				log.Printf("[hook] KI dedup: found similar KI %q (score=%.3f), updating", dupID, score)
				appendContent := fmt.Sprintf("\n\n---\n\n## Update from session %s\n\n%s", info.SessionID, result.Content)
				if err := store.UpdateContent(dupID, appendContent); err != nil {
					log.Printf("[hook] KI dedup update failed: %v", err)
				} else {
					_ = h.dedup.EmbedAndIndexByID(dupID)
					log.Printf("[hook] KI dedup: updated existing KI %q", dupID)
				}
				return
			}
		}

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
	Get(id string) (*SessionState, bool)
}

// TitleLLMCaller abstracts the LLM call for title distillation.
type TitleLLMCaller interface {
	DistillTitle(userMessage string) (string, error)
}

// TitleDistillHook distills a short, descriptive session title via LLM
// after the first user message, and persists it to the session manager.
// Runs synchronously so the title SSE event arrives before step_done.
type TitleDistillHook struct {
	llm    TitleLLMCaller
	titler SessionTitler
}

// NewTitleDistillHook creates the title distillation hook.
func NewTitleDistillHook(llm TitleLLMCaller, titler SessionTitler) *TitleDistillHook {
	return &TitleDistillHook{llm: llm, titler: titler}
}

func (h *TitleDistillHook) OnRunComplete(ctx context.Context, info RunInfo) {
	if info.UserMessage == "" || info.SessionID == "" {
		return
	}

	// Dedup guard: skip if session already has a title
	if sess, ok := h.titler.Get(info.SessionID); ok && sess.Title != "" {
		return
	}

	// Timeout guard: title distillation must not block run completion indefinitely
	done := make(chan struct{})
	go func() {
		defer close(done)
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
		if info.Delta != nil {
			info.Delta.OnTitleUpdate(info.SessionID, title)
		}
		log.Printf("[hook] title distilled: session=%s title=%q", info.SessionID, title)
	}()

	// Wait up to 30s — if LLM is slow, let it finish in background
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		log.Printf("[hook] title distill timed out after 30s for session=%s", info.SessionID)
	}
}

