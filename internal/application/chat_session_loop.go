package application

import (
	"fmt"
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
)

func (c *ChatCommands) withAcquiredSessionLoop(
	sessionID string,
	restore bool,
	restoreReason string,
	fn func(loop *service.AgentLoop) error,
) error {
	loop := service.ResolveSessionLoop(c.loop, c.loopPool, sessionID, true)
	if restore {
		if restored := service.RestoreLoopHistoryIfNeeded(loop, c.histQuery, sessionID, true); restored > 0 {
			slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s (%s)", restored, sessionID, restoreReason))
		}
	}

	if !loop.TryAcquire() {
		return ErrBusy
	}
	defer loop.ReleaseAcquire()

	return fn(loop)
}

func (c *ChatCommands) restoreRetryLoop(sessionID string) *service.AgentLoop {
	loop := service.ResolveRetryLoop(c.loop, c.loopPool, sessionID)
	if restored := service.RestoreLoopHistoryIfNeeded(loop, c.histQuery, sessionID, false); restored > 0 {
		slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s (%s)", restored, sessionID, "retry"))
	}
	return loop
}
