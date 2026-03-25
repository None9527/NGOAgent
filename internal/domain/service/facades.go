package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/google/uuid"
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

// Chat sends a user message and runs the agent loop.
// If sessionID is provided and the loop has no history, load from DB (session resume).
func (ce *ChatEngine) Chat(ctx context.Context, sessionID, message string) error {
	loop := ce.pool.Get(sessionID)
	if sessionID != "" {
		ce.sessMgr.Activate(sessionID)
		// Session resume: load history from DB if loop is empty
		if ce.history != nil && len(loop.GetHistory()) == 0 {
			exports, err := ce.history.LoadAll(sessionID)
			if err == nil && len(exports) > 0 {
				msgs := make([]llm.Message, len(exports))
				for i, e := range exports {
					msgs[i] = llm.Message{
						Role:       e.Role,
						Content:    e.Content,
						ToolCallID: e.ToolCallID,
					}
					if e.ToolCalls != "" {
						json.Unmarshal([]byte(e.ToolCalls), &msgs[i].ToolCalls)
					}
				}
				loop.SetHistory(msgs)
				// Auto-compact on resume: prevent full-history overload
				loop.CompactIfNeeded()
				log.Printf("[session] Resumed %d messages for session %s", len(msgs), sessionID)
			}
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
	loop := ce.pool.Get(sessionID)
	loop.mu.Lock()
	// Remove last assistant + tool messages
	for len(loop.history) > 0 {
		last := loop.history[len(loop.history)-1]
		if last.Role == "user" {
			break
		}
		loop.history = loop.history[:len(loop.history)-1]
	}
	// Get last user message
	lastUser := ""
	if len(loop.history) > 0 {
		lastUser = loop.history[len(loop.history)-1].Content
		loop.history = loop.history[:len(loop.history)-1]
	}
	loop.mu.Unlock()

	if lastUser == "" {
		return fmt.Errorf("no previous user message to retry")
	}
	return loop.Run(ctx, lastUser)
}

// StopChat signals the agent loop for a specific session to stop.
func (ce *ChatEngine) StopChat(sessionID string) {
	loop := ce.pool.Get(sessionID)
	loop.Stop()
}

// DeleteSession removes a session's history, metadata, and loop from pool.
func (ce *ChatEngine) DeleteSession(id string) error {
	if ce.history != nil {
		if err := ce.history.DeleteSession(id); err != nil {
			log.Printf("[session] delete history error: %v", err)
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
	activeByUser map[string]string  // userKey → sessionID
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
		return "", fmt.Errorf("no session repo")
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
