package service

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/testing/runtimecases"
)

func TestRuntimeRegressionService(t *testing.T) {
	for _, tc := range runtimecases.Load(t) {
		if !tc.HasLayer("service") || tc.Service == nil {
			continue
		}
		t.Run(tc.ID, func(t *testing.T) {
			runServiceRegressionCase(t, tc)
		})
	}
}

func runServiceRegressionCase(t *testing.T, tc runtimecases.Case) {
	t.Helper()

	sc := tc.Service
	delta := &mockDeltaSink{}
	store := newServiceRegressionStore(t, sc.Store)
	loop := NewAgentLoop(Deps{
		Brain:         brain.NewArtifactStore(t.TempDir(), sc.SessionID),
		SnapshotStore: store,
		Delta:         delta,
	})

	if sc.SeedBarrier != nil {
		loop.SetActiveBarrier(NewSubagentBarrierFromState(loop, nil, *sc.SeedBarrier))
	}

	for i := range sc.Snapshots {
		snap := sc.Snapshots[i]
		if err := store.Save(context.Background(), &snap); err != nil {
			t.Fatalf("save snapshot %s: %v", snap.RunID, err)
		}
	}

	switch sc.Action.Kind {
	case "handle_reconnect":
		handled, err := loop.HandleReconnect(context.Background())
		assertServiceHandled(t, handled, err, sc.Expect)
	case "hydrate_pending_barrier":
		if err := loop.hydratePendingBarrier(context.Background(), sc.Action.RunID); err != nil {
			t.Fatalf("hydrate pending barrier: %v", err)
		}
	case "latest_wait_snapshot_view":
		wait, err := loop.latestWaitSnapshotView(context.Background())
		if err != nil {
			t.Fatalf("latestWaitSnapshotView: %v", err)
		}
		if want := sc.Expect.ReconnectAction; want != "" {
			if got := string(wait.reconnectAction()); got != want {
				t.Fatalf("unexpected reconnect action: got %q want %q", got, want)
			}
		}
		if want := sc.Expect.AutoResumeRunID; want != "" {
			got, ok := wait.autoResumeRunID()
			if !ok || got != want {
				t.Fatalf("unexpected auto-resume run id: got %q ok=%v want %q", got, ok, want)
			}
		}
	default:
		t.Fatalf("unsupported service action %q", sc.Action.Kind)
	}

	if want := sc.Expect.Approval; want != nil {
		if len(delta.approvals) != 1 {
			t.Fatalf("expected one approval replay, got %#v", delta.approvals)
		}
		got := runtimecases.ApprovalExpect{
			ID:       delta.approvals[0].ID,
			ToolName: delta.approvals[0].ToolName,
			Reason:   delta.approvals[0].Reason,
			Args:     cloneRuntimeArgs(delta.approvals[0].Args),
		}
		if !reflect.DeepEqual(got, *want) {
			t.Fatalf("unexpected approval replay: got %#v want %#v", got, *want)
		}
	}
	if want := sc.Expect.PlanReview; want != nil {
		if len(delta.reviews) != 1 {
			t.Fatalf("expected one plan review replay, got %#v", delta.reviews)
		}
		got := runtimecases.PlanReviewExpect{
			Message: delta.reviews[0].message,
			Paths:   append([]string(nil), delta.reviews[0].paths...),
		}
		if !reflect.DeepEqual(got, *want) {
			t.Fatalf("unexpected plan review replay: got %#v want %#v", got, *want)
		}
	}
	if sc.Expect.NoActiveBarrier {
		if restored := loop.activeBarrierSnapshot(); restored != nil {
			t.Fatalf("expected no active barrier, got %#v", restored)
		}
	}
	if want := sc.Expect.ActiveBarrier; want != nil {
		restored := loop.activeBarrierSnapshot()
		if restored == nil {
			t.Fatal("expected active barrier")
		}
		if restored.ID != want.ID || restored.PendingCount != want.PendingCount {
			t.Fatalf("unexpected active barrier: got %#v want %#v", restored, *want)
		}
		if want.FirstRunID != "" {
			if len(restored.Members) == 0 || restored.Members[0].RunID != want.FirstRunID {
				t.Fatalf("unexpected active barrier members: %#v", restored.Members)
			}
		}
	}
}

func newServiceRegressionStore(t *testing.T, kind string) graphruntime.SnapshotStore {
	t.Helper()

	if kind == "persistence" {
		db, err := persistence.Open(filepath.Join(t.TempDir(), "runtime-regression.db"))
		if err != nil {
			t.Fatalf("open persistence store: %v", err)
		}
		return persistence.NewRunSnapshotStore(db)
	}
	return graphruntime.NewInMemorySnapshotStore()
}

func assertServiceHandled(t *testing.T, handled bool, err error, expect runtimecases.ServiceExpect) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expect.Handled != nil && handled != *expect.Handled {
		t.Fatalf("unexpected handled state: got %v want %v", handled, *expect.Handled)
	}
}

func cloneRuntimeArgs(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
