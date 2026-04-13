package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

// ApplicationKernel is the shared dependency graph used by application facades.
type ApplicationKernel struct {
	loop            *service.AgentLoop
	loopPool        *service.LoopPool
	chatEngine      *service.ChatEngine
	sessMgr         *service.SessionManager
	modelMgr        *service.ModelManager
	toolAdmin       *service.ToolAdmin
	secHook         *security.Hook
	skillMgr        *skill.Manager
	cronMgr         *cron.Manager
	mcpMgr          *mcp.Manager
	discovery       service.ToolDiscovery
	cfg             *config.Manager
	router          *llm.Router
	histQuery       HistoryQuerier
	brainDir        string
	kiStore         *knowledge.Store
	sandboxMgr      *sandbox.Manager
	startedAt       time.Time
	tokenUsageStore *persistence.TokenUsageStore
	runtimeStore    *persistence.RunSnapshotStore
}

func (k *ApplicationKernel) contextStatsForLoop(loop *service.AgentLoop) apitype.ContextStats {
	stats := service.CollectLoopContextStats(loop)
	byModel := make(map[string]any)
	for model, usage := range stats.ByModel {
		byModel[model] = usage
	}

	return apitype.ContextStats{
		Model:         k.router.CurrentModel(),
		HistoryCount:  stats.HistoryCount,
		TokenEstimate: stats.TokenEstimate,
		TotalCostUSD:  stats.TotalCostUSD,
		TotalCalls:    stats.TotalCalls,
		ByModel:       byModel,
		CacheHitRate:  stats.CacheHitRate,
		CacheBreaks:   stats.CacheBreaks,
	}
}

func (k *ApplicationKernel) contextStatsForSession(sessionID string) apitype.ContextStats {
	base := apitype.ContextStats{}
	if k.router != nil {
		base.Model = k.router.CurrentModel()
	}
	if sessionID == "" {
		return k.contextStatsForLoop(service.ResolveSessionLoop(k.loop, k.loopPool, sessionID, false))
	}

	havePersisted := false
	if k.histQuery != nil {
		if exports, err := k.histQuery.LoadAll(sessionID); err == nil && len(exports) > 0 {
			havePersisted = true
			base.HistoryCount = len(exports)
			tokenEstimate := 0
			for _, msg := range exports {
				tokenEstimate += len(msg.Content) / 4
			}
			base.TokenEstimate = tokenEstimate
		}
	}

	if k.tokenUsageStore != nil {
		if usage, err := k.tokenUsageStore.GetSessionUsage(sessionID); err == nil && usage != nil {
			base.TotalCostUSD = usage.TotalCostUSD
			base.TotalCalls = usage.TotalCalls
			if usage.ByModelJSON != "" {
				var byModel map[string]any
				if json.Unmarshal([]byte(usage.ByModelJSON), &byModel) == nil && len(byModel) > 0 {
					base.ByModel = byModel
				}
			}
			if base.ByModel == nil {
				base.ByModel = map[string]any{}
			}
			return base
		}
	}

	if havePersisted {
		if base.ByModel == nil {
			base.ByModel = map[string]any{}
		}
		return base
	}

	return k.contextStatsForLoop(service.ResolveSessionLoop(k.loop, k.loopPool, sessionID, false))
}

func (k *ApplicationKernel) saveSessionCost(sessionID string) error {
	if k.tokenUsageStore == nil {
		return fmt.Errorf("token usage store not configured")
	}
	if sessionID == "" && k.sessMgr != nil {
		sessionID = k.sessMgr.Active()
	}
	if sessionID == "" {
		return fmt.Errorf("session id required")
	}
	loop := service.ResolveStatsLoop(k.loop, k.loopPool, k.sessMgr, sessionID)
	if loop == nil {
		if usage, err := k.tokenUsageStore.GetSessionUsage(sessionID); err == nil && usage != nil {
			return nil
		}
		return fmt.Errorf("session %s not found in memory", sessionID)
	}
	stats := k.contextStatsForLoop(loop)
	var promptTok, completeTok int
	for _, usage := range stats.ByModel {
		switch u := usage.(type) {
		case service.ModelUsage:
			promptTok += u.PromptTokens
			completeTok += u.CompletionTokens
		case map[string]any:
			if pt, ok := u["prompt_tokens"].(float64); ok {
				promptTok += int(pt)
			}
			if ct, ok := u["completion_tokens"].(float64); ok {
				completeTok += int(ct)
			}
		}
	}
	return k.tokenUsageStore.SaveSessionUsage(
		sessionID, stats.Model,
		promptTok, completeTok,
		stats.TotalCalls, stats.TotalCostUSD,
		stats.ByModel,
	)
}

func (k *ApplicationKernel) sessionCost(sessionID string) (map[string]any, error) {
	if k.tokenUsageStore == nil {
		return nil, fmt.Errorf("token usage store not configured")
	}
	usage, err := k.tokenUsageStore.GetSessionUsage(sessionID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id":   usage.SessionID,
		"model":        usage.Model,
		"prompt_tok":   usage.TotalPromptTok,
		"complete_tok": usage.TotalCompleteTok,
		"total_calls":  usage.TotalCalls,
		"total_cost":   usage.TotalCostUSD,
		"updated_at":   usage.UpdatedAt.Format("2006-01-02 15:04:05"),
	}, nil
}

func (k *ApplicationKernel) knownSessionIDs() []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, 16)
	add := func(sessionID string) {
		if sessionID == "" {
			return
		}
		if _, ok := seen[sessionID]; ok {
			return
		}
		seen[sessionID] = struct{}{}
		ids = append(ids, sessionID)
	}

	if k.loopPool != nil {
		for _, sessionID := range k.loopPool.List() {
			add(sessionID)
		}
	}
	if k.sessMgr != nil {
		if active := k.sessMgr.Active(); active != "" {
			add(active)
		}
		for _, session := range k.sessMgr.List() {
			add(session.ID)
		}
		if sessions, err := k.sessMgr.ListFromRepo(200, 0); err == nil {
			for _, session := range sessions {
				add(session.ID)
			}
		}
	}
	return ids
}

func (k *ApplicationKernel) eachResidentLoop(fn func(*service.AgentLoop) (bool, error)) (bool, error) {
	if k.loop != nil {
		handled, err := fn(k.loop)
		if handled || err != nil {
			return handled, err
		}
	}

	for _, sessionID := range k.knownSessionIDs() {
		loop := service.FindSessionLoop(k.loop, k.loopPool, sessionID)
		if loop == nil || loop == k.loop {
			continue
		}
		handled, err := fn(loop)
		if handled || err != nil {
			return handled, err
		}
	}
	return false, nil
}

func (k *ApplicationKernel) findApprovalSnapshot(ctx context.Context, approvalID string) (*graphruntime.RunSnapshot, error) {
	if approvalID == "" || k.runtimeStore == nil {
		return nil, nil
	}
	for _, sessionID := range k.knownSessionIDs() {
		snap, err := k.runtimeStore.LoadLatestBySession(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if snap == nil || snap.ExecutionState.PendingApproval == nil {
			continue
		}
		if snap.ExecutionState.PendingApproval.ID == approvalID {
			return snap, nil
		}
	}
	return nil, nil
}

func (k *ApplicationKernel) stopRuntimeSession(ctx context.Context, sessionID string) (bool, error) {
	if sessionID == "" || k.runtimeStore == nil {
		return false, nil
	}
	snap, err := k.runtimeStore.LoadLatestBySession(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if snap == nil || snap.Status != graphruntime.NodeStatusWait {
		return false, nil
	}
	if pending := snap.ExecutionState.PendingApproval; pending != nil && k.secHook != nil {
		k.secHook.CleanupPending(pending.ID)
	}
	snap.Status = graphruntime.NodeStatusFatal
	snap.ExecutionState.Status = graphruntime.NodeStatusFatal
	snap.ExecutionState.WaitReason = graphruntime.WaitReasonNone
	snap.ExecutionState.PendingApproval = nil
	snap.ExecutionState.PendingBarrier = nil
	snap.ExecutionState.PendingWake = false
	snap.ExecutionState.LastError = "stopped by user"
	snap.UpdatedAt = time.Now().UTC()
	return true, k.runtimeStore.Save(ctx, snap)
}
