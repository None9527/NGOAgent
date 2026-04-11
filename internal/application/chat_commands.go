package application

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
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
		if loop := service.ResolveSessionLoop(c.loop, c.loopPool, c.sessMgr.Active(), false); loop != nil {
			loop.Stop()
		}
		return
	}
	if loop := service.ResidentSessionLoop(c.loop, c.loopPool, sessionID); loop != nil && loop != c.loop {
		loop.Stop()
		return
	}
	if c.loop != nil && c.loop.SessionID() == sessionID {
		c.loop.Stop()
	}
}

func (c *ChatCommands) RetryRun(_ context.Context, sessionID string) (string, error) {
	loop := c.restoreRetryLoop(sessionID)
	return loop.StripLastTurn()
}

func (c *ChatCommands) Approve(approvalID string, approved bool) error {
	if c.secHook == nil {
		return fmt.Errorf("security hook not configured")
	}
	if err := c.secHook.Resolve(approvalID, approved); err == nil {
		c.secHook.CleanupPending(approvalID)
		_, clearErr := service.ForEachCandidateLoop(c.loop, c.loopPool, c.sessMgr, func(loop *service.AgentLoop) (bool, error) {
			return loop.ClearPendingApprovalSnapshot(context.Background(), approvalID)
		})
		if clearErr != nil {
			return clearErr
		}
		return nil
	}
	handled, err := service.ForEachCandidateLoop(c.loop, c.loopPool, c.sessMgr, func(loop *service.AgentLoop) (bool, error) {
		return loop.ApprovePending(context.Background(), approvalID, approved)
	})
	if handled || err != nil {
		return err
	}
	return c.secHook.Resolve(approvalID, approved)
}
