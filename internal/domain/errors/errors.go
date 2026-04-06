// Package errors defines domain error types for the NGOAgent system.
//
// All errors follow a consistent pattern:
//   - Sentinel errors (var ErrXxx) for simple identity checks via errors.Is()
//   - Structured errors (type XxxError) for rich context via errors.As()
//
// Usage in API layers:
//
//	switch {
//	case errors.Is(err, agenterr.ErrNotFound):  → 404
//	case errors.Is(err, agenterr.ErrBusy):      → 429
//	case errors.Is(err, agenterr.ErrDenied):     → 403
//	case errors.Is(err, agenterr.ErrValidation): → 400
//	default:                                     → 500
//	}
package errors

import (
	"errors"
	"fmt"
)

// ──────────────────────────────────────────────
// Sentinel errors — use errors.Is() to check
// ──────────────────────────────────────────────

// ErrNotFound indicates a requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrBusy indicates the system cannot accept the request now (agent running, pool full).
var ErrBusy = errors.New("busy")

// ErrDenied indicates the operation was rejected by security policy.
var ErrDenied = errors.New("permission denied")

// ErrValidation indicates invalid input or parameters.
var ErrValidation = errors.New("validation error")

// ErrTimeout indicates an operation exceeded its time limit.
var ErrTimeout = errors.New("timeout")

// ──────────────────────────────────────────────
// NotFoundError — resource lookup failures
// ──────────────────────────────────────────────

// NotFoundError wraps ErrNotFound with resource context.
type NotFoundError struct {
	Resource string // "session", "tool", "artifact", "conversation"
	ID       string // identifier that was not found
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s %q not found", e.Resource, e.ID)
}

func (e *NotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

// NewNotFound creates a NotFoundError.
func NewNotFound(resource, id string) error {
	return &NotFoundError{Resource: resource, ID: id}
}

// ──────────────────────────────────────────────
// BusyError — capacity / concurrency failures
// ──────────────────────────────────────────────

// BusyError wraps ErrBusy with context about what's busy.
type BusyError struct {
	Reason string // "another run in progress", "pool full", etc.
}

func (e *BusyError) Error() string {
	return fmt.Sprintf("busy: %s", e.Reason)
}

func (e *BusyError) Is(target error) bool {
	return target == ErrBusy
}

// NewBusy creates a BusyError.
func NewBusy(reason string) error {
	return &BusyError{Reason: reason}
}

// ──────────────────────────────────────────────
// DeniedError — security / permission failures
// ──────────────────────────────────────────────

// DeniedError wraps ErrDenied with policy context.
type DeniedError struct {
	Operation string // "tool_call", "file_write", etc.
	Reason    string // human-readable denial reason
}

func (e *DeniedError) Error() string {
	return fmt.Sprintf("denied: %s — %s", e.Operation, e.Reason)
}

func (e *DeniedError) Is(target error) bool {
	return target == ErrDenied
}

// NewDenied creates a DeniedError.
func NewDenied(operation, reason string) error {
	return &DeniedError{Operation: operation, Reason: reason}
}

// ──────────────────────────────────────────────
// ValidationError — input / parameter failures
// ──────────────────────────────────────────────

// ValidationError wraps ErrValidation with field-level detail.
type ValidationError struct {
	Field   string // which field or parameter failed
	Message string // what went wrong
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation: %s — %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation: %s", e.Message)
}

func (e *ValidationError) Is(target error) bool {
	return target == ErrValidation
}

// NewValidation creates a ValidationError.
func NewValidation(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

// ──────────────────────────────────────────────
// LLMError — LLM provider failures
// ──────────────────────────────────────────────

// LLMErrorKind categorizes LLM failures for retry/failover decisions.
type LLMErrorKind int

const (
	LLMTransient    LLMErrorKind = iota // network error, 5xx — retryable
	LLMRateLimit                        // 429 — wait then retry
	LLMContextLimit                     // context too long — compact then retry
	LLMAuth                             // 401/403 — not retryable
	LLMFatal                            // model not found, bad request — not retryable
)

func (k LLMErrorKind) String() string {
	switch k {
	case LLMTransient:
		return "transient"
	case LLMRateLimit:
		return "rate_limit"
	case LLMContextLimit:
		return "context_limit"
	case LLMAuth:
		return "auth"
	case LLMFatal:
		return "fatal"
	default:
		return "unknown"
	}
}

// LLMError represents an LLM provider failure with structured context.
type LLMError struct {
	Kind     LLMErrorKind
	Provider string // "openai", "anthropic", etc.
	Model    string
	Message  string
	Cause    error // original error
}

func (e *LLMError) Error() string {
	return fmt.Sprintf("llm [%s/%s] %s: %s", e.Provider, e.Model, e.Kind, e.Message)
}

func (e *LLMError) Unwrap() error {
	return e.Cause
}

// Retryable returns true if this error class warrants retry.
func (e *LLMError) Retryable() bool {
	return e.Kind == LLMTransient || e.Kind == LLMRateLimit || e.Kind == LLMContextLimit
}

// NewLLMError creates a structured LLM error.
func NewLLMError(kind LLMErrorKind, provider, model, message string, cause error) error {
	return &LLMError{
		Kind:     kind,
		Provider: provider,
		Model:    model,
		Message:  message,
		Cause:    cause,
	}
}

// ──────────────────────────────────────────────
// InternalError — unexpected system state
// ──────────────────────────────────────────────

// InternalError represents an unexpected internal failure.
type InternalError struct {
	Op      string // operation that failed
	Message string
	Cause   error
}

func (e *InternalError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("internal [%s]: %s: %v", e.Op, e.Message, e.Cause)
	}
	return fmt.Sprintf("internal [%s]: %s", e.Op, e.Message)
}

func (e *InternalError) Unwrap() error {
	return e.Cause
}

// NewInternal creates an InternalError.
func NewInternal(op, message string, cause error) error {
	return &InternalError{Op: op, Message: message, Cause: cause}
}

// ──────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────

// Is delegates to errors.Is for convenience.
func Is(err, target error) bool { return errors.Is(err, target) }

// As delegates to errors.As for convenience.
func As(err error, target any) bool { return errors.As(err, target) }

// Wrap annotates an error with an operation context without changing its type.
func Wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
