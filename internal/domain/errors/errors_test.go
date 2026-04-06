package errors

import (
	"errors"
	"testing"
)

func TestNotFoundError_Is(t *testing.T) {
	err := NewNotFound("session", "abc-123")
	if !errors.Is(err, ErrNotFound) {
		t.Error("NotFoundError should match ErrNotFound via errors.Is")
	}
	if errors.Is(err, ErrBusy) {
		t.Error("NotFoundError should NOT match ErrBusy")
	}
	if err.Error() != `session "abc-123" not found` {
		t.Errorf("unexpected message: %s", err.Error())
	}
}

func TestBusyError_Is(t *testing.T) {
	err := NewBusy("another run in progress")
	if !errors.Is(err, ErrBusy) {
		t.Error("BusyError should match ErrBusy via errors.Is")
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("BusyError should NOT match ErrNotFound")
	}
}

func TestDeniedError_Is(t *testing.T) {
	err := NewDenied("tool_call", "dangerous operation")
	if !errors.Is(err, ErrDenied) {
		t.Error("DeniedError should match ErrDenied via errors.Is")
	}
}

func TestValidationError_Is(t *testing.T) {
	err := NewValidation("model", "unsupported model name")
	if !errors.Is(err, ErrValidation) {
		t.Error("ValidationError should match ErrValidation via errors.Is")
	}
	// Field should appear in message
	if err.Error() != "validation: model — unsupported model name" {
		t.Errorf("unexpected message: %s", err.Error())
	}
}

func TestValidationError_NoField(t *testing.T) {
	err := NewValidation("", "empty input")
	if err.Error() != "validation: empty input" {
		t.Errorf("unexpected message: %s", err.Error())
	}
}

func TestLLMError_Retryable(t *testing.T) {
	tests := []struct {
		kind      LLMErrorKind
		retryable bool
	}{
		{LLMTransient, true},
		{LLMRateLimit, true},
		{LLMContextLimit, true},
		{LLMAuth, false},
		{LLMFatal, false},
	}
	for _, tt := range tests {
		err := NewLLMError(tt.kind, "openai", "gpt-4", "test", nil).(*LLMError)
		if err.Retryable() != tt.retryable {
			t.Errorf("LLMErrorKind=%s: expected retryable=%v, got %v", tt.kind, tt.retryable, err.Retryable())
		}
	}
}

func TestLLMError_Unwrap(t *testing.T) {
	cause := errors.New("connection reset")
	err := NewLLMError(LLMTransient, "anthropic", "claude", "network fail", cause)
	if !errors.Is(err, cause) {
		t.Error("LLMError should unwrap to its cause")
	}
}

func TestInternalError_Unwrap(t *testing.T) {
	cause := errors.New("nil pointer")
	err := NewInternal("processState", "unexpected nil", cause)
	if !errors.Is(err, cause) {
		t.Error("InternalError should unwrap to its cause")
	}
}

func TestWrap_NilError(t *testing.T) {
	if Wrap("op", nil) != nil {
		t.Error("Wrap(nil) should return nil")
	}
}

func TestWrap_PreservesType(t *testing.T) {
	original := NewNotFound("tool", "search")
	wrapped := Wrap("execTool", original)
	if !errors.Is(wrapped, ErrNotFound) {
		t.Error("Wrap should preserve error type for errors.Is")
	}
}
