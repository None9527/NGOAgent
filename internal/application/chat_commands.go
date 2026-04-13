package application

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
)

func (c *ChatCommands) ChatStream(ctx context.Context, sessionID, message, mode string, delta *service.Delta) error {
	if sessionID != "" {
		c.sessMgr.Activate(sessionID)
	}

	ctx = ctxutil.WithSessionID(ctx, sessionID)
	ctx = withRuntimeIngress(ctx, runtimeIngressMeta{
		kind:    "message",
		source:  "chat_stream",
		trigger: "user_message",
	})
	if strings.TrimSpace(message) == "" {
		ctx = withRuntimeIngress(ctx, runtimeIngressMeta{
			kind:    "reconnect",
			source:  "chat_stream",
			trigger: "reconnect",
		})
	}

	err := c.withAcquiredSessionLoop(sessionID, message != "", "stream", func(loop *service.AgentLoop) error {
		if mode != "" {
			loop.SetPlanMode(mode)
		}
		loop.SetDelta(delta)
		return loop.RunWithoutAcquire(ctx, message)
	})

	if sessionID != "" && c.tokenUsageStore != nil {
		go func() {
			if saveErr := c.saveSessionCost(sessionID); saveErr != nil {
				slog.Info(fmt.Sprintf("[token] Auto-save cost failed for session %s: %v", sessionID, saveErr))
			}
		}()
	}

	return err
}

func (c *ChatCommands) SessionID(sessionID string) string {
	if sessionID != "" {
		return sessionID
	}
	return ""
}

func (c *ChatCommands) StopRun(sessionID string) {
	if sessionID == "" {
		if c.sessMgr != nil {
			sessionID = c.sessMgr.Active()
		}
	}
	if sessionID == "" {
		return
	}
	if stopped, err := c.stopRuntimeSession(context.Background(), sessionID); err != nil {
		slog.Info(fmt.Sprintf("[runtime] stop session %s failed: %v", sessionID, err))
	} else if stopped {
		return
	}
	if loop := service.FindSessionLoop(c.loop, c.loopPool, sessionID); loop != nil {
		loop.Stop()
		return
	}
}

func (c *ChatCommands) RetryRun(_ context.Context, sessionID string) (string, error) {
	if sessionID != "" && c.histQuery != nil {
		if exports, err := c.histQuery.LoadAll(sessionID); err == nil && len(exports) > 0 {
			if loop := service.FindSessionLoop(c.loop, c.loopPool, sessionID); loop != nil {
				loop.SetHistory(service.RestoreHistory(exports))
				return loop.StripLastTurn()
			}
			return stripLastTurnFromExports(exports)
		}
	}
	loop := c.restoreRetryLoop(sessionID)
	return loop.StripLastTurn()
}

func stripLastTurnFromExports(exports []service.HistoryExport) (string, error) {
	msgs := service.RestoreHistory(exports)
	for len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == "user" {
			break
		}
		msgs = msgs[:len(msgs)-1]
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("retry: no previous user message to retry")
	}
	return msgs[len(msgs)-1].Content, nil
}

func (c *ChatCommands) Approve(approvalID string, approved bool) error {
	if c.secHook == nil {
		return fmt.Errorf("security hook not configured")
	}
	if err := c.secHook.Resolve(approvalID, approved); err == nil {
		c.secHook.CleanupPending(approvalID)
		if snap, findErr := c.findApprovalSnapshot(context.Background(), approvalID); findErr != nil {
			return findErr
		} else if snap != nil {
			snap.ExecutionState.PendingApproval = nil
			snap.ExecutionState.WaitReason = ""
			return c.runtimeStore.Save(context.Background(), snap)
		}
		_, clearErr := c.eachResidentLoop(func(loop *service.AgentLoop) (bool, error) {
			return loop.ClearPendingApprovalSnapshot(context.Background(), approvalID)
		})
		return clearErr
	}
	if snap, findErr := c.findApprovalSnapshot(context.Background(), approvalID); findErr != nil {
		return findErr
	} else if snap != nil && snap.ExecutionState.PendingApproval != nil {
		c.secHook.RestorePending(security.PendingApproval{
			ID:       snap.ExecutionState.PendingApproval.ID,
			ToolName: snap.ExecutionState.PendingApproval.ToolName,
			Args:     cloneApprovalArgs(snap.ExecutionState.PendingApproval.Args),
			Reason:   snap.ExecutionState.PendingApproval.Reason,
			Created:  snap.ExecutionState.PendingApproval.RequestedAt,
		})
		if err := c.secHook.Resolve(approvalID, approved); err != nil {
			return err
		}
		c.secHook.CleanupPending(approvalID)
		snap.ExecutionState.PendingApproval = nil
		snap.ExecutionState.WaitReason = ""
		return c.runtimeStore.Save(context.Background(), snap)
	}
	handled, err := c.eachResidentLoop(func(loop *service.AgentLoop) (bool, error) {
		return loop.ApprovePending(context.Background(), approvalID, approved)
	})
	if handled || err != nil {
		return err
	}
	handled, err = service.ForEachCandidateLoop(c.loop, c.loopPool, c.sessMgr, func(loop *service.AgentLoop) (bool, error) {
		return loop.ApprovePending(context.Background(), approvalID, approved)
	})
	if handled || err != nil {
		return err
	}
	return c.secHook.Resolve(approvalID, approved)
}

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
