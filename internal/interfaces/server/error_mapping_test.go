package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
)

func TestErrorToHTTPStatus(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{"NotFound", agenterr.NewNotFound("session", "abc"), http.StatusNotFound},
		{"Busy", agenterr.NewBusy("another run"), http.StatusTooManyRequests},
		{"Denied", agenterr.NewDenied("tool", "dangerous"), http.StatusForbidden},
		{"Validation", agenterr.NewValidation("model", "invalid"), http.StatusBadRequest},
		{"Sentinel_NotFound", agenterr.ErrNotFound, http.StatusNotFound},
		{"Sentinel_Busy", agenterr.ErrBusy, http.StatusTooManyRequests},
		{"Sentinel_Denied", agenterr.ErrDenied, http.StatusForbidden},
		{"Sentinel_Validation", agenterr.ErrValidation, http.StatusBadRequest},
		{"Sentinel_Timeout", agenterr.ErrTimeout, http.StatusGatewayTimeout},
		{"UnknownError", errors.New("some unknown error"), http.StatusInternalServerError},
		{"WrappedNotFound", agenterr.Wrap("lookup", agenterr.NewNotFound("tool", "xyz")), http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorToHTTPStatus(tt.err)
			if got != tt.expected {
				t.Errorf("errorToHTTPStatus(%v) = %d, want %d", tt.err, got, tt.expected)
			}
		})
	}
}

func TestWriteJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	err := agenterr.NewNotFound("session", "test-123")
	writeJSONError(w, err)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
	body := w.Body.String()
	if body == "" {
		t.Error("expected JSON body, got empty")
	}
}
