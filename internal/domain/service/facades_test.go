package service

import (
	"context"
	"testing"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

type stubSessionRepo struct {
	sessions []ConversationInfo
}

func (s *stubSessionRepo) CreateConversation(channel, title string) (string, error) { return "", nil }
func (s *stubSessionRepo) ListConversations(limit, offset int) ([]ConversationInfo, error) {
	return append([]ConversationInfo(nil), s.sessions...), nil
}
func (s *stubSessionRepo) UpdateTitle(id, title string) error { return nil }
func (s *stubSessionRepo) Touch(id string) error              { return nil }
func (s *stubSessionRepo) DeleteConversation(id string) error { return nil }

func TestResolveSessionLoop(t *testing.T) {
	factory := func(sessionID string) *AgentLoop { return NewAgentLoop(Deps{}) }
	pool := NewLoopPool(factory, t.TempDir())
	defaultLoop := NewAgentLoop(Deps{})

	if got := ResolveSessionLoop(defaultLoop, pool, "missing", false); got != defaultLoop {
		t.Fatalf("expected fallback loop when create=false and session is missing")
	}

	created := ResolveSessionLoop(defaultLoop, pool, "session-created", true)
	if created == nil || created == defaultLoop {
		t.Fatalf("expected pool loop when create=true")
	}

	if got := ResolveSessionLoop(defaultLoop, pool, "session-created", false); got != created {
		t.Fatalf("expected existing pool loop when session already exists")
	}
}

func TestFindSessionLoop(t *testing.T) {
	factory := func(sessionID string) *AgentLoop { return NewAgentLoop(Deps{}) }
	pool := NewLoopPool(factory, t.TempDir())
	defaultLoop := NewAgentLoop(Deps{})

	if got := FindSessionLoop(defaultLoop, pool, "missing"); got != nil {
		t.Fatalf("expected no loop for missing session, got %#v", got)
	}

	created := pool.Get("session-created")
	if got := FindSessionLoop(defaultLoop, pool, "session-created"); got != created {
		t.Fatalf("expected resident loop for created session")
	}
}

func TestResidentSessionLoop(t *testing.T) {
	factory := func(sessionID string) *AgentLoop { return NewAgentLoop(Deps{}) }
	pool := NewLoopPool(factory, t.TempDir())
	defaultLoop := NewAgentLoop(Deps{})

	if got := ResidentSessionLoop(defaultLoop, pool, "missing"); got != nil {
		t.Fatalf("expected no resident loop for missing session, got %#v", got)
	}
	if got := ResidentSessionLoop(defaultLoop, nil, "anything"); got != defaultLoop {
		t.Fatalf("expected default loop when pool is nil")
	}
	if got := ResidentSessionLoop(defaultLoop, pool, ""); got != defaultLoop {
		t.Fatalf("expected default loop for empty session")
	}

	created := pool.Get("resident-session")
	if got := ResidentSessionLoop(defaultLoop, pool, "resident-session"); got != created {
		t.Fatalf("expected resident pooled loop, got %#v", got)
	}
}

func TestResolveActiveManagedLoop(t *testing.T) {
	factory := func(sessionID string) *AgentLoop { return NewAgentLoop(Deps{}) }
	pool := NewLoopPool(factory, t.TempDir())
	defaultLoop := NewAgentLoop(Deps{})
	sessMgr := NewSessionManager(&stubSessionRepo{
		sessions: []ConversationInfo{{ID: "active-session"}},
	})

	sessMgr.Activate("active-session")
	if got := ResolveActiveManagedLoop(defaultLoop, pool, sessMgr); got != nil {
		t.Fatalf("expected nil for non-resident active session, got %#v", got)
	}

	activeLoop := pool.Get("active-session")
	if got := ResolveActiveManagedLoop(defaultLoop, pool, sessMgr); got != activeLoop {
		t.Fatalf("expected resident active loop, got %#v", got)
	}
}

func TestResolveRetryLoop(t *testing.T) {
	factory := func(sessionID string) *AgentLoop { return NewAgentLoop(Deps{}) }
	pool := NewLoopPool(factory, t.TempDir())
	defaultLoop := NewAgentLoop(Deps{})

	if got := ResolveRetryLoop(defaultLoop, pool, "missing"); got == nil || got == defaultLoop {
		t.Fatalf("expected scratch retry loop for missing session, got %#v", got)
	}

	resident := pool.Get("retry-session")
	if got := ResolveRetryLoop(defaultLoop, pool, "retry-session"); got != resident {
		t.Fatalf("expected resident retry loop, got %#v", got)
	}
}

func TestResolveStatsLoop(t *testing.T) {
	factory := func(sessionID string) *AgentLoop { return NewAgentLoop(Deps{}) }
	pool := NewLoopPool(factory, t.TempDir())
	defaultLoop := NewAgentLoop(Deps{})
	sessMgr := NewSessionManager(&stubSessionRepo{
		sessions: []ConversationInfo{{ID: "active-stats"}},
	})

	sessMgr.Activate("active-stats")
	if got := ResolveStatsLoop(defaultLoop, pool, sessMgr, ""); got != nil {
		t.Fatalf("expected nil for non-resident active stats loop, got %#v", got)
	}

	activeLoop := pool.Get("active-stats")
	if got := ResolveStatsLoop(defaultLoop, pool, sessMgr, ""); got != activeLoop {
		t.Fatalf("expected resident active stats loop, got %#v", got)
	}
	if got := ResolveStatsLoop(defaultLoop, pool, sessMgr, "missing"); got != nil {
		t.Fatalf("expected nil for missing explicit stats session, got %#v", got)
	}
}

func TestRetryLastRun_DoesNotCreateGhostLoopForMissingSession(t *testing.T) {
	pool := NewLoopPool(func(sessionID string) *AgentLoop {
		return NewAgentLoop(Deps{})
	}, t.TempDir())
	engine := NewChatEngine(pool, NewSessionManager(&stubSessionRepo{}), nil)

	err := engine.RetryLastRun(context.Background(), "missing-session")
	if err == nil {
		t.Fatal("expected retry validation error for missing session history")
	}
	if got := pool.GetIfExists("missing-session"); got != nil {
		t.Fatalf("expected retry to avoid creating a ghost loop, got %#v", got)
	}
}

func TestCollectLoopContextStats(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	loop.SetHistory([]llm.Message{
		{Role: "user", Content: "abcd"},
		{Role: "assistant", Content: "abcdefgh"},
	})

	stats := CollectLoopContextStats(loop)
	if stats.HistoryCount != 2 {
		t.Fatalf("expected 2 history messages, got %d", stats.HistoryCount)
	}
	if stats.TokenEstimate != 3 {
		t.Fatalf("expected token estimate 3, got %d", stats.TokenEstimate)
	}
	if stats.TotalCalls != 0 || stats.TotalCostUSD != 0 {
		t.Fatalf("expected zero token usage stats, got %#v", stats)
	}
	if len(stats.ByModel) != 0 {
		t.Fatalf("expected empty by-model stats, got %#v", stats.ByModel)
	}
}

func TestForEachCandidateLoop_CreatesLoopForPersistedSessionCandidate(t *testing.T) {
	defaultLoop := NewAgentLoop(Deps{})
	pool := NewLoopPool(func(sessionID string) *AgentLoop {
		return NewAgentLoop(Deps{})
	}, t.TempDir())
	sessMgr := NewSessionManager(&stubSessionRepo{
		sessions: []ConversationInfo{{ID: "persisted-session"}},
	})

	var visited []string
	handled, err := ForEachCandidateLoop(defaultLoop, pool, sessMgr, func(loop *AgentLoop) (bool, error) {
		visited = append(visited, loop.SessionID())
		return loop.SessionID() == "persisted-session", nil
	})
	if err != nil {
		t.Fatalf("ForEachCandidateLoop error: %v", err)
	}
	if !handled {
		t.Fatal("expected persisted session candidate to be handled")
	}
	found := false
	for _, sessionID := range visited {
		if sessionID == "persisted-session" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected persisted session candidate in visited set, got %#v", visited)
	}
	if got := pool.GetIfExists("persisted-session"); got == nil {
		t.Fatal("expected persisted session candidate to create a loop")
	}
}
