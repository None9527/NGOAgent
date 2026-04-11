package application

import (
	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
)

// Version is set at build time via -ldflags.
var Version = "0.5.0"

// ErrBusy is returned when the agent loop is already running.
var ErrBusy = agenterr.ErrBusy

// HistoryQuerier loads conversation history from persistence.
type HistoryQuerier interface {
	LoadAll(sessionID string) ([]service.HistoryExport, error)
}
