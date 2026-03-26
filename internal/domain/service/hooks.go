package service

import (
	"context"
	"fmt"
	"log"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// ═══════════════════════════════════════════
// Lifecycle Hook Interfaces
// ═══════════════════════════════════════════

// ToolHook is called before/after each tool execution.
// BeforeTool runs in Modifying mode: it can alter args or skip the tool entirely.
type ToolHook interface {
	BeforeTool(ctx context.Context, name string, args map[string]any) (newArgs map[string]any, skip bool)
	AfterTool(ctx context.Context, name string, output string, err error)
}

// CompactHook is called before/after context compaction (Void mode).
// BeforeCompact receives the full history before compression — use it to
// persist important content (e.g., save to vector memory) before it's lost.
type CompactHook interface {
	BeforeCompact(ctx context.Context, history []llm.Message)
	AfterCompact(ctx context.Context, compacted []llm.Message)
}

// MessageHook is called before sending a message to the user (Modifying mode).
// It can modify the text or cancel sending entirely.
type MessageHook interface {
	OnMessageSending(ctx context.Context, text string) (newText string, cancel bool)
}

// HookManager is the centralized hook registry for all lifecycle events.
// It wraps the existing PostRunHookChain and adds tool/compact/message hooks.
type HookManager struct {
	PostRun *PostRunHookChain
	tool    []ToolHook
	compact []CompactHook
	message []MessageHook
}

// NewHookManager creates a HookManager with an empty PostRunHookChain.
func NewHookManager() *HookManager {
	return &HookManager{
		PostRun: NewPostRunHookChain(),
	}
}

// AddToolHook registers a tool lifecycle hook.
func (m *HookManager) AddToolHook(h ToolHook) { m.tool = append(m.tool, h) }

// AddCompactHook registers a compaction lifecycle hook.
func (m *HookManager) AddCompactHook(h CompactHook) { m.compact = append(m.compact, h) }

// AddMessageHook registers an outbound message hook.
func (m *HookManager) AddMessageHook(h MessageHook) { m.message = append(m.message, h) }

// FireBeforeTool executes all ToolHook.BeforeTool in order.
// Returns potentially modified args and skip=true if any hook wants to skip.
func (m *HookManager) FireBeforeTool(ctx context.Context, name string, args map[string]any) (map[string]any, bool) {
	for _, h := range m.tool {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[hook] panic in ToolHook.BeforeTool: %v", r)
				}
			}()
			newArgs, skip := h.BeforeTool(ctx, name, args)
			if skip {
				args = newArgs
				return
			}
			if newArgs != nil {
				args = newArgs
			}
		}()
	}
	return args, false
}

// FireAfterTool executes all ToolHook.AfterTool in order (void, no return).
func (m *HookManager) FireAfterTool(ctx context.Context, name string, output string, err error) {
	for _, h := range m.tool {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[hook] panic in ToolHook.AfterTool: %v", r)
				}
			}()
			h.AfterTool(ctx, name, output, err)
		}()
	}
}

// FireBeforeCompact executes all CompactHook.BeforeCompact.
func (m *HookManager) FireBeforeCompact(ctx context.Context, history []llm.Message) {
	for _, h := range m.compact {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[hook] panic in CompactHook.BeforeCompact: %v", r)
				}
			}()
			h.BeforeCompact(ctx, history)
		}()
	}
}

// FireAfterCompact executes all CompactHook.AfterCompact.
func (m *HookManager) FireAfterCompact(ctx context.Context, compacted []llm.Message) {
	for _, h := range m.compact {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[hook] panic in CompactHook.AfterCompact: %v", r)
				}
			}()
			h.AfterCompact(ctx, compacted)
		}()
	}
}

// FireMessageSending executes all MessageHook.OnMessageSending in order.
// Returns the final text and cancel=true if any hook cancels.
func (m *HookManager) FireMessageSending(ctx context.Context, text string) (string, bool) {
	for _, h := range m.message {
		var cancel bool
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[hook] panic in MessageHook.OnMessageSending: %v", r)
				}
			}()
			text, cancel = h.OnMessageSending(ctx, text)
		}()
		if cancel {
			return text, true
		}
	}
	return text, false
}

// OnRunComplete delegates to the internal PostRunHookChain.
func (m *HookManager) OnRunComplete(ctx context.Context, info RunInfo) {
	if m.PostRun != nil {
		m.PostRun.OnRunComplete(ctx, info)
	}
}

// Add delegates to PostRunHookChain.Add for backward compatibility.
func (m *HookManager) Add(h PostRunHook) {
	if m.PostRun == nil {
		m.PostRun = NewPostRunHookChain()
	}
	m.PostRun.Add(h)
}

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

// KIDistillHook uses LLM to distill knowledge after a run.
type KIDistillHook struct {
	getKI          func() KIStore
	llm            KILLMDistiller
	dedup          KIDuplicateChecker // optional: embedding-based dedup
	dedupThreshold float64            // cosine similarity threshold for dedup (default 0.60)
}

// KIStore is the interface needed by the distillation hook.
type KIStore interface {
	SaveDistilled(title, summary, content string, tags, sources []string) error
	UpdateMerge(id, appendContent, newSummary string) error // Legacy append merge
	ReplaceMerge(id, newContent, newSummary string) error   // Full replacement merge
	GetContent(id string) (string, error)                   // Read existing content
}

// KIDuplicateChecker detects duplicate KIs using embedding similarity.
type KIDuplicateChecker interface {
	FindDuplicate(text string, threshold float64) (string, float64)
	EmbedAndIndexByID(id string) error // Re-index after update
}

// KILLMDistiller abstracts the LLM call for knowledge distillation.
type KILLMDistiller interface {
	DistillKnowledge(messages []llm.Message) (*llm.KIResult, error)
	MergeKnowledge(existingContent, newContent string) (*llm.KIResult, error)
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
		// Gate: only distill conversations with actual tool usage
		// Steps counts generate→tool→guard cycles, so Steps=0 means pure text Q&A.
		// Previously Steps<5 filtered too aggressively — most sessions have 1-3 steps.
		if info.Steps < 2 || len(info.History) < 4 {
			log.Printf("[hook] KI distill: skipped (steps=%d, history=%d) for session=%s",
				info.Steps, len(info.History), info.SessionID)
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

		// Dedup check: if a similar KI exists, merge via LLM instead of concatenation
		if h.dedup != nil {
			queryText := result.Title + "\n" + result.Summary
			dupID, score := h.dedup.FindDuplicate(queryText, h.dedupThreshold)
			if dupID != "" {
				log.Printf("[hook] KI dedup: merging into %q (score=%.3f)", dupID, score)

				// Read existing KI content for LLM merge
				existingContent, err := store.GetContent(dupID)
				if err != nil {
					log.Printf("[hook] KI dedup: read existing failed: %v, falling back to append", err)
					_ = store.UpdateMerge(dupID, "\n\n---\n\n"+result.Content, result.Summary)
					_ = h.dedup.EmbedAndIndexByID(dupID)
					return
				}

				// LLM merge: consolidate old + new into one concise document
				merged, err := h.llm.MergeKnowledge(existingContent, result.Content)
				if err != nil {
					log.Printf("[hook] KI LLM merge failed: %v, falling back to append", err)
					_ = store.UpdateMerge(dupID, "\n\n---\n\n"+result.Content, result.Summary)
					_ = h.dedup.EmbedAndIndexByID(dupID)
					return
				}

				// Replace (not append) with merged content
				mergedContent := fmt.Sprintf("# %s\n\n%s\n\n---\n\n%s", merged.Title, merged.Summary, merged.Content)
				if err := store.ReplaceMerge(dupID, mergedContent, merged.Summary); err != nil {
					log.Printf("[hook] KI replace merge failed: %v", err)
				} else {
					_ = h.dedup.EmbedAndIndexByID(dupID)
					log.Printf("[hook] KI LLM merged into %q: %q", dupID, merged.Title)
				}
				return
			}
		}

		if err := store.SaveDistilled(
			result.Title, result.Summary, content,
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

	// Fire-and-forget: title generation runs in background, never blocks [DONE].
	// 1. Persist to DB via SetTitle (frontend refreshSessions picks it up)
	// 2. Best-effort SSE push via Delta.OnTitleUpdate (may miss if stream closed)
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
		if info.Delta != nil {
			info.Delta.OnTitleUpdate(info.SessionID, title)
		}
		log.Printf("[hook] title distilled: session=%s title=%q", info.SessionID, title)
	}()
}

