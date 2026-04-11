package application

import (
	"fmt"
	"time"

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

func (k *ApplicationKernel) saveSessionCost(sessionID string) error {
	if k.tokenUsageStore == nil {
		return fmt.Errorf("token usage store not configured")
	}
	loop := service.ResolveStatsLoop(k.loop, k.loopPool, k.sessMgr, sessionID)
	if loop == nil {
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
