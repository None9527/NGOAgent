package graphruntime

import (
	"context"
	"time"
)

// TriggerKind identifies the category of an incoming trigger event.
type TriggerKind string

const (
	TriggerMessage    TriggerKind = "message"
	TriggerDecision   TriggerKind = "decision"
	TriggerResume     TriggerKind = "resume"
	TriggerReconnect  TriggerKind = "reconnect"
	TriggerBarrier    TriggerKind = "barrier"
	TriggerCron       TriggerKind = "cron"
	TriggerA2A        TriggerKind = "a2a"
	TriggerWebhook    TriggerKind = "webhook"
	TriggerInternal   TriggerKind = "internal"
)

// TriggerEvent represents a single inbound event that can wake or initiate a run.
type TriggerEvent struct {
	Kind      TriggerKind    `json:"kind"`
	Source    string         `json:"source,omitempty"`
	SessionID string        `json:"session_id,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	Trigger   string         `json:"trigger,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	At        time.Time      `json:"at"`
}

// TriggerHandler processes a single trigger event.
// Return nil to indicate successful handling.
type TriggerHandler func(ctx context.Context, event TriggerEvent) error

// TriggerFilter determines whether a handler should process a given event.
type TriggerFilter func(event TriggerEvent) bool

// TriggerSubscription pairs a filter with a handler for selective dispatch.
type TriggerSubscription struct {
	ID      string
	Kind    TriggerKind
	Filter  TriggerFilter
	Handler TriggerHandler
}

// TriggerRegistry manages registration and dispatch of trigger events.
type TriggerRegistry interface {
	// Register adds a handler for a specific trigger kind.
	Register(kind TriggerKind, handler TriggerHandler) string

	// Subscribe adds a filtered subscription.
	Subscribe(sub TriggerSubscription) string

	// Unsubscribe removes a subscription by ID.
	Unsubscribe(id string)

	// Dispatch sends an event to all matching handlers.
	// For synchronous registries, handlers execute in registration order.
	// For async registries, handlers execute concurrently via channels.
	Dispatch(ctx context.Context, event TriggerEvent) error

	// ListRegistered returns all trigger kinds that have at least one handler.
	ListRegistered() []TriggerKind

	// Close shuts down the registry and stops async consumers.
	Close()
}
