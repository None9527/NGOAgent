package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

const (
	eventBusDefaultBufferSize  = 256
	eventBusDefaultWorkerCount = 4
)

// EventBus is a channel-based async implementation of graphruntime.TriggerRegistry.
// Events are dispatched to a buffered channel and consumed by a pool of goroutines.
type EventBus struct {
	mu            sync.RWMutex
	subscriptions map[string]*graphruntime.TriggerSubscription
	kindIndex     map[graphruntime.TriggerKind][]string // kind → subscription IDs
	eventCh       chan dispatchItem
	wg            sync.WaitGroup
	closed        bool
}

type dispatchItem struct {
	ctx   context.Context
	event graphruntime.TriggerEvent
}

// EventBusOption configures the EventBus.
type EventBusOption func(*eventBusConfig)

type eventBusConfig struct {
	bufferSize  int
	workerCount int
}

// WithBufferSize sets the event channel buffer size.
func WithBufferSize(size int) EventBusOption {
	return func(c *eventBusConfig) {
		if size > 0 {
			c.bufferSize = size
		}
	}
}

// WithWorkerCount sets the number of consumer goroutines.
func WithWorkerCount(count int) EventBusOption {
	return func(c *eventBusConfig) {
		if count > 0 {
			c.workerCount = count
		}
	}
}

// NewEventBus creates an async event bus with configurable buffer and worker count.
func NewEventBus(opts ...EventBusOption) *EventBus {
	cfg := eventBusConfig{
		bufferSize:  eventBusDefaultBufferSize,
		workerCount: eventBusDefaultWorkerCount,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	bus := &EventBus{
		subscriptions: make(map[string]*graphruntime.TriggerSubscription),
		kindIndex:     make(map[graphruntime.TriggerKind][]string),
		eventCh:       make(chan dispatchItem, cfg.bufferSize),
	}

	bus.wg.Add(cfg.workerCount)
	for i := 0; i < cfg.workerCount; i++ {
		go bus.consumer(i)
	}

	return bus
}

// Register adds a handler for a specific trigger kind and returns the subscription ID.
func (b *EventBus) Register(kind graphruntime.TriggerKind, handler graphruntime.TriggerHandler) string {
	sub := graphruntime.TriggerSubscription{
		ID:      uuid.New().String(),
		Kind:    kind,
		Handler: handler,
	}
	return b.Subscribe(sub)
}

// Subscribe adds a filtered subscription and returns the subscription ID.
func (b *EventBus) Subscribe(sub graphruntime.TriggerSubscription) string {
	if sub.ID == "" {
		sub.ID = uuid.New().String()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscriptions[sub.ID] = &sub
	b.kindIndex[sub.Kind] = append(b.kindIndex[sub.Kind], sub.ID)
	return sub.ID
}

// Unsubscribe removes a subscription by ID.
func (b *EventBus) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sub, ok := b.subscriptions[id]
	if !ok {
		return
	}
	delete(b.subscriptions, id)
	ids := b.kindIndex[sub.Kind]
	for i, sid := range ids {
		if sid == id {
			b.kindIndex[sub.Kind] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(b.kindIndex[sub.Kind]) == 0 {
		delete(b.kindIndex, sub.Kind)
	}
}

// Dispatch sends an event to the async channel for processing.
// If the event timestamp is zero, it is set to now.
func (b *EventBus) Dispatch(ctx context.Context, event graphruntime.TriggerEvent) error {
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return fmt.Errorf("event bus is closed")
	}

	select {
	case b.eventCh <- dispatchItem{ctx: ctx, event: event}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ListRegistered returns all trigger kinds that have at least one handler.
func (b *EventBus) ListRegistered() []graphruntime.TriggerKind {
	b.mu.RLock()
	defer b.mu.RUnlock()
	kinds := make([]graphruntime.TriggerKind, 0, len(b.kindIndex))
	for kind := range b.kindIndex {
		kinds = append(kinds, kind)
	}
	return kinds
}

// Close shuts down the event bus and waits for all consumers to drain.
func (b *EventBus) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()
	close(b.eventCh)
	b.wg.Wait()
}

func (b *EventBus) consumer(workerID int) {
	defer b.wg.Done()
	for item := range b.eventCh {
		b.processEvent(item.ctx, item.event, workerID)
	}
}

func (b *EventBus) processEvent(ctx context.Context, event graphruntime.TriggerEvent, workerID int) {
	b.mu.RLock()
	subIDs := append([]string(nil), b.kindIndex[event.Kind]...)
	b.mu.RUnlock()

	for _, id := range subIDs {
		b.mu.RLock()
		sub, ok := b.subscriptions[id]
		b.mu.RUnlock()
		if !ok {
			continue
		}
		if sub.Filter != nil && !sub.Filter(event) {
			continue
		}
		if sub.Handler == nil {
			continue
		}
		if err := sub.Handler(ctx, event); err != nil {
			slog.Warn("event bus handler error",
				slog.String("trigger_kind", string(event.Kind)),
				slog.String("subscription_id", id),
				slog.Int("worker", workerID),
				slog.String("error", err.Error()),
			)
		}
	}
}

// Compile-time interface check.
var _ graphruntime.TriggerRegistry = (*EventBus)(nil)
