package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

func TestEventBus_RegisterAndDispatch(t *testing.T) {
	bus := NewEventBus(WithBufferSize(16), WithWorkerCount(2))
	defer bus.Close()

	var received atomic.Int32
	bus.Register(graphruntime.TriggerMessage, func(_ context.Context, event graphruntime.TriggerEvent) error {
		received.Add(1)
		return nil
	})

	ctx := context.Background()
	if err := bus.Dispatch(ctx, graphruntime.TriggerEvent{Kind: graphruntime.TriggerMessage}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if got := received.Load(); got != 1 {
		t.Errorf("expected 1 received event, got %d", got)
	}
}

func TestEventBus_MultipleHandlers(t *testing.T) {
	bus := NewEventBus(WithBufferSize(16), WithWorkerCount(1))
	defer bus.Close()

	var count atomic.Int32
	bus.Register(graphruntime.TriggerBarrier, func(_ context.Context, _ graphruntime.TriggerEvent) error {
		count.Add(1)
		return nil
	})
	bus.Register(graphruntime.TriggerBarrier, func(_ context.Context, _ graphruntime.TriggerEvent) error {
		count.Add(10)
		return nil
	})

	ctx := context.Background()
	_ = bus.Dispatch(ctx, graphruntime.TriggerEvent{Kind: graphruntime.TriggerBarrier})
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 11 {
		t.Errorf("expected 11, got %d", got)
	}
}

func TestEventBus_FilteredSubscription(t *testing.T) {
	bus := NewEventBus(WithBufferSize(16), WithWorkerCount(1))
	defer bus.Close()

	var received atomic.Int32
	bus.Subscribe(graphruntime.TriggerSubscription{
		Kind: graphruntime.TriggerA2A,
		Filter: func(event graphruntime.TriggerEvent) bool {
			return event.Source == "agent-alpha"
		},
		Handler: func(_ context.Context, _ graphruntime.TriggerEvent) error {
			received.Add(1)
			return nil
		},
	})

	ctx := context.Background()
	_ = bus.Dispatch(ctx, graphruntime.TriggerEvent{Kind: graphruntime.TriggerA2A, Source: "agent-beta"})
	_ = bus.Dispatch(ctx, graphruntime.TriggerEvent{Kind: graphruntime.TriggerA2A, Source: "agent-alpha"})
	time.Sleep(50 * time.Millisecond)

	if got := received.Load(); got != 1 {
		t.Errorf("expected 1 filtered event, got %d", got)
	}
}

func TestEventBus_UnknownKindIgnored(t *testing.T) {
	bus := NewEventBus(WithBufferSize(16), WithWorkerCount(1))
	defer bus.Close()

	var received atomic.Int32
	bus.Register(graphruntime.TriggerMessage, func(_ context.Context, _ graphruntime.TriggerEvent) error {
		received.Add(1)
		return nil
	})

	ctx := context.Background()
	_ = bus.Dispatch(ctx, graphruntime.TriggerEvent{Kind: graphruntime.TriggerCron})
	time.Sleep(50 * time.Millisecond)

	if got := received.Load(); got != 0 {
		t.Errorf("expected 0 for unmatched kind, got %d", got)
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus(WithBufferSize(16), WithWorkerCount(1))
	defer bus.Close()

	var received atomic.Int32
	id := bus.Register(graphruntime.TriggerResume, func(_ context.Context, _ graphruntime.TriggerEvent) error {
		received.Add(1)
		return nil
	})

	bus.Unsubscribe(id)

	ctx := context.Background()
	_ = bus.Dispatch(ctx, graphruntime.TriggerEvent{Kind: graphruntime.TriggerResume})
	time.Sleep(50 * time.Millisecond)

	if got := received.Load(); got != 0 {
		t.Errorf("expected 0 after unsubscribe, got %d", got)
	}
}

func TestEventBus_ListRegistered(t *testing.T) {
	bus := NewEventBus(WithBufferSize(8), WithWorkerCount(1))
	defer bus.Close()

	bus.Register(graphruntime.TriggerMessage, func(_ context.Context, _ graphruntime.TriggerEvent) error { return nil })
	bus.Register(graphruntime.TriggerCron, func(_ context.Context, _ graphruntime.TriggerEvent) error { return nil })

	kinds := bus.ListRegistered()
	if len(kinds) != 2 {
		t.Errorf("expected 2 registered kinds, got %d", len(kinds))
	}
}

func TestEventBus_DispatchAfterClose(t *testing.T) {
	bus := NewEventBus(WithBufferSize(8), WithWorkerCount(1))
	bus.Close()

	err := bus.Dispatch(context.Background(), graphruntime.TriggerEvent{Kind: graphruntime.TriggerMessage})
	if err == nil {
		t.Error("expected error dispatching to closed bus")
	}
}

func TestEventBus_ConcurrentDispatch(t *testing.T) {
	bus := NewEventBus(WithBufferSize(128), WithWorkerCount(4))
	defer bus.Close()

	var received atomic.Int32
	bus.Register(graphruntime.TriggerWebhook, func(_ context.Context, _ graphruntime.TriggerEvent) error {
		received.Add(1)
		return nil
	})

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = bus.Dispatch(ctx, graphruntime.TriggerEvent{Kind: graphruntime.TriggerWebhook})
		}()
	}
	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	if got := received.Load(); got != 100 {
		t.Errorf("expected 100 concurrent events, got %d", got)
	}
}

func TestEventBus_TimestampAutoFill(t *testing.T) {
	bus := NewEventBus(WithBufferSize(8), WithWorkerCount(1))
	defer bus.Close()

	var capturedAt time.Time
	bus.Register(graphruntime.TriggerInternal, func(_ context.Context, event graphruntime.TriggerEvent) error {
		capturedAt = event.At
		return nil
	})

	before := time.Now().UTC()
	_ = bus.Dispatch(context.Background(), graphruntime.TriggerEvent{Kind: graphruntime.TriggerInternal})
	time.Sleep(50 * time.Millisecond)

	if capturedAt.Before(before) {
		t.Errorf("auto-filled timestamp %v is before dispatch time %v", capturedAt, before)
	}
}
