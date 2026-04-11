package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// ═══════════════════════════════════════════
// ChatEngine — facade for chat operations
// ═══════════════════════════════════════════

// ChatEngine provides the high-level chat API.
// Multi-tenant: uses LoopPool to route requests to per-session loops.
type ChatEngine struct {
	pool    *LoopPool
	sessMgr *SessionManager
	history HistoryPersister
}

// NewChatEngine creates a ChatEngine backed by a LoopPool.
func NewChatEngine(pool *LoopPool, sessMgr *SessionManager, history HistoryPersister) *ChatEngine {
	return &ChatEngine{pool: pool, sessMgr: sessMgr, history: history}
}

type HistoryLoader interface {
	LoadAll(sessionID string) ([]HistoryExport, error)
}

type LoopHandler func(loop *AgentLoop) (bool, error)

type LoopContextStats struct {
	HistoryCount  int
	TokenEstimate int
	TotalCostUSD  float64
	TotalCalls    int
	ByModel       map[string]ModelUsage
	CacheHitRate  float64
	CacheBreaks   int
}

func ResolveSessionLoop(defaultLoop *AgentLoop, pool *LoopPool, sessionID string, create bool) *AgentLoop {
	if sessionID == "" || pool == nil {
		return defaultLoop
	}
	if create {
		return pool.Get(sessionID)
	}
	if existing := pool.GetIfExists(sessionID); existing != nil {
		return existing
	}
	return defaultLoop
}

func FindSessionLoop(defaultLoop *AgentLoop, pool *LoopPool, sessionID string) *AgentLoop {
	if sessionID == "" {
		return defaultLoop
	}
	if pool != nil {
		if existing := pool.GetIfExists(sessionID); existing != nil {
			return existing
		}
	}
	if defaultLoop != nil && defaultLoop.SessionID() == sessionID {
		return defaultLoop
	}
	return nil
}

func ResidentSessionLoop(defaultLoop *AgentLoop, pool *LoopPool, sessionID string) *AgentLoop {
	if sessionID == "" || pool == nil {
		return defaultLoop
	}
	return pool.GetIfExists(sessionID)
}

func ResolveActiveManagedLoop(defaultLoop *AgentLoop, pool *LoopPool, sessMgr *SessionManager) *AgentLoop {
	if sessMgr == nil {
		return defaultLoop
	}
	if active := sessMgr.Active(); active != "" {
		return FindSessionLoop(defaultLoop, pool, active)
	}
	return defaultLoop
}

func ResolveRetryLoop(defaultLoop *AgentLoop, pool *LoopPool, sessionID string) *AgentLoop {
	if sessionID == "" {
		return ResolveSessionLoop(defaultLoop, pool, sessionID, false)
	}
	if loop := FindSessionLoop(defaultLoop, pool, sessionID); loop != nil {
		return loop
	}
	// Retry only needs a temporary history container; avoid mutating another
	// session loop or creating a ghost pooled loop for a non-resident session.
	return NewAgentLoop(Deps{})
}

func ResolveStatsLoop(defaultLoop *AgentLoop, pool *LoopPool, sessMgr *SessionManager, sessionID string) *AgentLoop {
	if sessionID == "" {
		return ResolveActiveManagedLoop(defaultLoop, pool, sessMgr)
	}
	return FindSessionLoop(defaultLoop, pool, sessionID)
}

func CollectLoopContextStats(loop *AgentLoop) LoopContextStats {
	history := loop.GetHistory()
	tokenStats := loop.GetTokenStats()
	cacheStats := loop.GetCacheStats()
	tokenEst := 0
	for _, m := range history {
		tokenEst += len(m.Content) / 4
	}

	byModel := make(map[string]ModelUsage, len(tokenStats.ByModel))
	for model, usage := range tokenStats.ByModel {
		byModel[model] = usage
	}

	return LoopContextStats{
		HistoryCount:  len(history),
		TokenEstimate: tokenEst,
		TotalCostUSD:  tokenStats.TotalCostUSD,
		TotalCalls:    tokenStats.TotalCalls,
		ByModel:       byModel,
		CacheHitRate:  cacheStats.HitRate,
		CacheBreaks:   cacheStats.CacheBreaks,
	}
}

func RestoreLoopHistoryIfNeeded(loop *AgentLoop, history HistoryLoader, sessionID string, compact bool) int {
	if loop == nil || history == nil || sessionID == "" || len(loop.GetHistory()) != 0 {
		return 0
	}

	exports, err := history.LoadAll(sessionID)
	if err != nil || len(exports) == 0 {
		return 0
	}

	msgs := RestoreHistory(exports)
	loop.SetHistory(msgs)
	if compact {
		loop.CompactIfNeeded()
	}
	return len(msgs)
}

func StripSessionLastTurn(loop *AgentLoop, history HistoryLoader, sessionID string) (string, error) {
	RestoreLoopHistoryIfNeeded(loop, history, sessionID, false)
	return loop.StripLastTurn()
}

func ForEachCandidateLoop(defaultLoop *AgentLoop, pool *LoopPool, sessMgr *SessionManager, fn LoopHandler) (bool, error) {
	if defaultLoop != nil {
		handled, err := fn(defaultLoop)
		if handled || err != nil {
			return handled, err
		}
	}

	seen := map[string]struct{}{}
	candidates := make([]string, 0, 16)
	addCandidate := func(sessionID string) {
		if sessionID == "" {
			return
		}
		if _, ok := seen[sessionID]; ok {
			return
		}
		seen[sessionID] = struct{}{}
		candidates = append(candidates, sessionID)
	}

	if pool != nil {
		for _, sessionID := range pool.List() {
			addCandidate(sessionID)
		}
	}
	if sessMgr != nil {
		for _, session := range sessMgr.List() {
			addCandidate(session.ID)
		}
		if sessions, err := sessMgr.ListFromRepo(200, 0); err == nil {
			for _, session := range sessions {
				addCandidate(session.ID)
			}
		}
	}

	for _, sessionID := range candidates {
		loop := ResolveSessionLoop(defaultLoop, pool, sessionID, true)
		if loop == nil {
			continue
		}
		if handled, err := fn(loop); handled || err != nil {
			return handled, err
		}
	}
	return false, nil
}

// Chat sends a user message and runs the agent loop.
// If sessionID is provided and the loop has no history, load from DB (session resume).
func (ce *ChatEngine) Chat(ctx context.Context, sessionID, message string) error {
	loop := ResolveSessionLoop(nil, ce.pool, sessionID, true)
	if sessionID != "" {
		ce.sessMgr.Activate(sessionID)
		if restored := RestoreLoopHistoryIfNeeded(loop, ce.history, sessionID, true); restored > 0 {
			slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s", restored, sessionID))
		}
	}
	err := loop.Run(ctx, message)
	// Bump updated_at so sidebar re-sorts by most recent activity
	ce.TouchSession(sessionID)
	return err
}

// TouchSession updates the session's updated_at so it sorts correctly in the sidebar.
func (ce *ChatEngine) TouchSession(id string) {
	if ce.sessMgr.repo != nil {
		_ = ce.sessMgr.repo.Touch(id)
	}
}

// RetryLastRun re-runs the last assistant turn by removing it and re-generating.
func (ce *ChatEngine) RetryLastRun(ctx context.Context, sessionID string) error {
	loop := ResolveRetryLoop(nil, ce.pool, sessionID)
	lastUser, err := StripSessionLastTurn(loop, ce.history, sessionID)
	if err != nil {
		return err
	}
	if lastUser == "" {
		return agenterr.NewValidation("retry", "no previous user message to retry")
	}
	return loop.Run(ctx, lastUser)
}

// StopChat signals the agent loop for a specific session to stop.
func (ce *ChatEngine) StopChat(sessionID string) {
	if loop := ResolveSessionLoop(nil, ce.pool, sessionID, false); loop != nil {
		loop.Stop()
	}
}

// DeleteSession removes a session's history, metadata, and loop from pool.
func (ce *ChatEngine) DeleteSession(id string) error {
	if ce.history != nil {
		if err := ce.history.DeleteSession(id); err != nil {
			slog.Info(fmt.Sprintf("[session] delete history error: %v", err))
		}
	}
	ce.pool.Remove(id)
	return ce.sessMgr.Delete(id)
}

// ═══════════════════════════════════════════
// SessionManager — session CRUD
// ═══════════════════════════════════════════

// SessionRepo is the domain interface for session persistence.
type SessionRepo interface {
	CreateConversation(channel, title string) (id string, err error)
	ListConversations(limit, offset int) ([]ConversationInfo, error)
	UpdateTitle(id, title string) error
	Touch(id string) error
	DeleteConversation(id string) error
}

// ConversationInfo holds minimal conversation metadata.
type ConversationInfo struct {
	ID        string
	Title     string
	Channel   string
	CreatedAt string // RFC3339, session creation time
	UpdatedAt string // RFC3339, last activity time (used for sidebar grouping)
}

// SessionManager manages conversation sessions.
// Multi-tenant: tracks per-user active sessions.
type SessionManager struct {
	mu           sync.RWMutex
	sessions     map[string]*SessionState
	active       string            // backward-compat: global active for single-user mode
	activeByUser map[string]string // userKey → sessionID
	repo         SessionRepo
}

// SessionState holds an in-memory session.
type SessionState struct {
	ID      string
	History []llm.Message
	Title   string
}

// NewSessionManager creates a session manager.
func NewSessionManager(repo SessionRepo) *SessionManager {
	return &SessionManager{
		sessions:     make(map[string]*SessionState),
		activeByUser: make(map[string]string),
		repo:         repo,
	}
}

// New creates a new in-memory session (no DB write).
func (sm *SessionManager) New(title string) *SessionState {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	id := uuid.New().String()
	s := &SessionState{ID: id, Title: title}
	sm.sessions[id] = s
	sm.active = id
	return s
}

// CreatePersisted creates a session in DB and memory, then activates it.
// Returns the DB-generated ID.
func (sm *SessionManager) CreatePersisted(channel, title string) (string, error) {
	if sm.repo == nil {
		return "", agenterr.NewNotFound("session_repo", "")
	}
	id, err := sm.repo.CreateConversation(channel, title)
	if err != nil {
		return "", err
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[id] = &SessionState{ID: id, Title: title}
	sm.active = id
	return id, nil
}

// Get retrieves a session by ID.
func (sm *SessionManager) Get(id string) (*SessionState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	s, ok := sm.sessions[id]
	return s, ok
}

// List returns all sessions.
func (sm *SessionManager) List() []*SessionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make([]*SessionState, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		result = append(result, s)
	}
	return result
}

// ListFromRepo queries the persistent store for all sessions with their titles.
// Returns an empty slice if repo is unavailable.
func (sm *SessionManager) ListFromRepo(limit, offset int) ([]ConversationInfo, error) {
	if sm.repo == nil {
		return nil, nil
	}
	return sm.repo.ListConversations(limit, offset)
}

// Delete removes a session.
func (sm *SessionManager) Delete(id string) error {
	// Remove from persistent store first (works even if not in memory)
	if sm.repo != nil {
		if err := sm.repo.DeleteConversation(id); err != nil {
			return err
		}
	}
	// Remove from in-memory map if present
	sm.mu.Lock()
	delete(sm.sessions, id)
	if sm.active == id {
		sm.active = ""
	}
	sm.mu.Unlock()
	return nil
}

// Activate sets the active session for the user who owns this session.
func (sm *SessionManager) Activate(id string) {
	userKey := ExtractUserKey(id)
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.active = id // backward-compat
	sm.activeByUser[userKey] = id
}

// SetTitle updates the title of an in-memory session and persists it.
func (sm *SessionManager) SetTitle(id, title string) {
	sm.mu.Lock()
	if s, ok := sm.sessions[id]; ok {
		s.Title = title
	}
	sm.mu.Unlock()
	if sm.repo != nil {
		_ = sm.repo.UpdateTitle(id, title)
	}
}

// Active returns the current active session ID (backward-compat: global).
func (sm *SessionManager) Active() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.active
}

// ActiveFor returns the active session for a specific user key.
func (sm *SessionManager) ActiveFor(userKey string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.activeByUser[userKey]
}

// ═══════════════════════════════════════════
// ModelManager — model switching
// ═══════════════════════════════════════════

// ModelManager provides model listing and switching.
type ModelManager struct {
	router *llm.Router
}

// NewModelManager creates a model manager.
func NewModelManager(router *llm.Router) *ModelManager {
	return &ModelManager{router: router}
}

// List returns all available model names.
func (mm *ModelManager) List() []string {
	return mm.router.ListModels()
}

// Switch changes the active model.
func (mm *ModelManager) Switch(model string) error {
	return mm.router.SetDefault(model)
}

// GetCurrent returns the currently active model name.
func (mm *ModelManager) GetCurrent() string {
	return mm.router.CurrentModel()
}

// ═══════════════════════════════════════════
// ToolAdmin — tool administration
// ═══════════════════════════════════════════

// ToolInfo mirrors tool.ToolInfo for the domain boundary.
type ToolInfo struct {
	Name    string
	Enabled bool
}

// ToolRegistry is the domain interface for tool administration.
type ToolRegistry interface {
	List() []ToolInfo
	Enable(name string) error
	Disable(name string) error
}

// ToolAdmin provides tool listing and enable/disable through the registry.
type ToolAdmin struct {
	registry ToolRegistry
}

// NewToolAdmin creates a tool admin.
func NewToolAdmin(registry ToolRegistry) *ToolAdmin {
	return &ToolAdmin{registry: registry}
}

// List returns all tools with their status.
func (ta *ToolAdmin) List() []ToolInfo {
	return ta.registry.List()
}

// Enable re-enables a disabled tool.
func (ta *ToolAdmin) Enable(name string) error {
	return ta.registry.Enable(name)
}

// Disable disables a tool.
func (ta *ToolAdmin) Disable(name string) error {
	return ta.registry.Disable(name)
}
