package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

// ═══════════════════════════════════════════
// ChatEngine — facade for chat operations
// ═══════════════════════════════════════════

// ChatEngine provides the high-level chat API.
type ChatEngine struct {
	loop    *AgentLoop
	sessMgr *SessionManager
}

// NewChatEngine creates a ChatEngine.
func NewChatEngine(loop *AgentLoop, sessMgr *SessionManager) *ChatEngine {
	return &ChatEngine{loop: loop, sessMgr: sessMgr}
}

// Chat sends a user message and runs the agent loop.
func (ce *ChatEngine) Chat(ctx context.Context, sessionID, message string) error {
	// Restore session if needed
	if sessionID != "" {
		ce.sessMgr.Activate(sessionID)
	}
	return ce.loop.Run(ctx, message)
}

// RetryLastRun re-runs the last assistant turn by removing it and re-generating.
func (ce *ChatEngine) RetryLastRun(ctx context.Context) error {
	ce.loop.mu.Lock()
	// Remove last assistant + tool messages
	for len(ce.loop.history) > 0 {
		last := ce.loop.history[len(ce.loop.history)-1]
		if last.Role == "user" {
			break
		}
		ce.loop.history = ce.loop.history[:len(ce.loop.history)-1]
	}
	// Get last user message
	lastUser := ""
	if len(ce.loop.history) > 0 {
		lastUser = ce.loop.history[len(ce.loop.history)-1].Content
		ce.loop.history = ce.loop.history[:len(ce.loop.history)-1]
	}
	ce.loop.mu.Unlock()

	if lastUser == "" {
		return fmt.Errorf("no previous user message to retry")
	}
	return ce.loop.Run(ctx, lastUser)
}

// StopChat signals the agent loop to stop.
func (ce *ChatEngine) StopChat() {
	ce.loop.Stop()
}

// ═══════════════════════════════════════════
// SessionManager — session CRUD
// ═══════════════════════════════════════════

// SessionManager manages conversation sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*SessionState
	active   string
	repo     *persistence.Repository
}

// SessionState holds an in-memory session.
type SessionState struct {
	ID      string
	History []llm.Message
	Title   string
}

// NewSessionManager creates a session manager.
func NewSessionManager(repo *persistence.Repository) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*SessionState),
		repo:     repo,
	}
}

// New creates a new session.
func (sm *SessionManager) New(title string) *SessionState {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	id := fmt.Sprintf("s_%d", len(sm.sessions)+1)
	s := &SessionState{ID: id, Title: title}
	sm.sessions[id] = s
	sm.active = id
	return s
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

// Delete removes a session.
func (sm *SessionManager) Delete(id string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if _, ok := sm.sessions[id]; !ok {
		return fmt.Errorf("session not found: %s", id)
	}
	delete(sm.sessions, id)
	if sm.active == id {
		sm.active = ""
	}
	return nil
}

// Activate sets the active session.
func (sm *SessionManager) Activate(id string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.active = id
}

// Active returns the current active session ID.
func (sm *SessionManager) Active() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.active
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

// ToolAdmin provides tool listing and enable/disable through the registry.
type ToolAdmin struct {
	registry *tool.Registry
}

// NewToolAdmin creates a tool admin.
func NewToolAdmin(registry *tool.Registry) *ToolAdmin {
	return &ToolAdmin{registry: registry}
}

// List returns all tools with their status.
func (ta *ToolAdmin) List() []tool.ToolInfo {
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
