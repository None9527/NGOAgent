package graphruntime

import (
	"context"
	"sync"
)

type InMemorySnapshotStore struct {
	mu    sync.RWMutex
	snaps map[string]*RunSnapshot
}

func NewInMemorySnapshotStore() *InMemorySnapshotStore {
	return &InMemorySnapshotStore{
		snaps: map[string]*RunSnapshot{},
	}
}

func (s *InMemorySnapshotStore) Save(_ context.Context, snap *RunSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *snap
	clone.TurnState = cloneTurn(snap.TurnState)
	clone.ExecutionState = *cloneExecution(snap.ExecutionState)
	s.snaps[snap.RunID] = &clone
	return nil
}

func (s *InMemorySnapshotStore) LoadLatest(_ context.Context, runID string) (*RunSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := s.snaps[runID]
	if snap == nil {
		return nil, nil
	}
	clone := *snap
	clone.TurnState = cloneTurn(snap.TurnState)
	clone.ExecutionState = *cloneExecution(snap.ExecutionState)
	return &clone, nil
}

func (s *InMemorySnapshotStore) LoadLatestBySession(_ context.Context, sessionID string) (*RunSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *RunSnapshot
	for _, snap := range s.snaps {
		if snap.SessionID != sessionID {
			continue
		}
		if latest == nil || snap.UpdatedAt.After(latest.UpdatedAt) {
			clone := *snap
			clone.TurnState = cloneTurn(snap.TurnState)
			clone.ExecutionState = *cloneExecution(snap.ExecutionState)
			latest = &clone
		}
	}
	return latest, nil
}

func (s *InMemorySnapshotStore) Delete(_ context.Context, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.snaps, runID)
	return nil
}
