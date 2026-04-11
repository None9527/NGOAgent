package application

import (
	"fmt"
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
)

func (a *RuntimeCommands) withAcquiredSessionLoop(
	sessionID string,
	restoreReason string,
	fn func(loop *service.AgentLoop) (bool, error),
) (bool, error) {
	loop := service.ResolveSessionLoop(a.loop, a.loopPool, sessionID, true)
	if restored := service.RestoreLoopHistoryIfNeeded(loop, a.histQuery, sessionID, true); restored > 0 {
		slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s (%s)", restored, sessionID, restoreReason))
	}

	if !loop.TryAcquire() {
		return false, ErrBusy
	}
	defer loop.ReleaseAcquire()

	return fn(loop)
}
