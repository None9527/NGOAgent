package bot

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/interfaces/grpc/agentpb"
)

// SessionStore maps Telegram user_id → NGOAgent session_id.
// It lazily creates sessions on first use via gRPC NewSession.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[int64]string // userID → sessionID
	client   agentpb.AgentServiceClient
}

func NewSessionStore(client agentpb.AgentServiceClient) *SessionStore {
	return &SessionStore{
		sessions: make(map[int64]string),
		client:   client,
	}
}

// Get returns the session ID for a user, creating one via gRPC if needed.
func (s *SessionStore) Get(userID int64) (string, error) {
	s.mu.RLock()
	sid, ok := s.sessions[userID]
	s.mu.RUnlock()
	if ok {
		return sid, nil
	}
	return s.create(userID)
}

// Reset forcibly creates a new session for the user, replacing the old one.
func (s *SessionStore) Reset(userID int64) (string, error) {
	s.mu.Lock()
	delete(s.sessions, userID)
	s.mu.Unlock()
	return s.create(userID)
}

func (s *SessionStore) create(userID int64) (string, error) {
	sid := fmt.Sprintf("tg-%d-%s", userID, uuid.New().String()[:8])
	_, err := s.client.NewSession(bgCtx(), &agentpb.NewSessionRequest{
		SessionId: sid,
		UserId:    userID,
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	s.mu.Lock()
	s.sessions[userID] = sid
	s.mu.Unlock()
	return sid, nil
}
