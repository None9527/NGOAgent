// Package application provides the unified AgentAPI facade.
// All protocol adapters (HTTP, gRPC, CLI) call this layer.
package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	agenterr "github.com/ngoclaw/ngoagent/internal/domain/errors"
	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
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
	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
)

// Version is set at build time via -ldflags.
var Version = "0.5.0"

// HistoryQuerier loads conversation history from persistence.
type HistoryQuerier interface {
	LoadAll(sessionID string) ([]service.HistoryExport, error)
}

// ═══════════════════════════════════════════
// AgentAPI — unified facade
// ═══════════════════════════════════════════

// AgentAPI is the protocol-agnostic API layer.
// All HTTP/gRPC/CLI adapters call these methods.
type AgentAPI struct {
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
	brainDir        string // base brain directory for session-scoped artifact access
	kiStore         *knowledge.Store
	sandboxMgr      *sandbox.Manager // for process cleanup on stop
	startedAt       time.Time
	tokenUsageStore *persistence.TokenUsageStore // P2 H2: session token usage persistence (nil = disabled)
	runtimeStore    *persistence.RunSnapshotStore
}

// NewAgentAPI creates a unified API facade.
func NewAgentAPI(
	loop *service.AgentLoop,
	loopPool *service.LoopPool,
	chatEngine *service.ChatEngine,
	sessMgr *service.SessionManager,
	modelMgr *service.ModelManager,
	toolAdmin *service.ToolAdmin,
	secHook *security.Hook,
	skillMgr *skill.Manager,
	cronMgr *cron.Manager,
	mcpMgr *mcp.Manager,
	cfg *config.Manager,
	router *llm.Router,
	histQuery HistoryQuerier,
	brainDir string,
	kiStore *knowledge.Store,
	sbMgr *sandbox.Manager,
) *AgentAPI {
	return &AgentAPI{
		loop:       loop,
		loopPool:   loopPool,
		chatEngine: chatEngine,
		sessMgr:    sessMgr,
		modelMgr:   modelMgr,
		toolAdmin:  toolAdmin,
		secHook:    secHook,
		skillMgr:   skillMgr,
		cronMgr:    cronMgr,
		mcpMgr:     mcpMgr,
		cfg:        cfg,
		router:     router,
		histQuery:  histQuery,
		brainDir:   brainDir,
		kiStore:    kiStore,
		sandboxMgr: sbMgr,
		startedAt:  time.Now(),
	}
}

// ErrBusy is returned when the agent loop is already running.
var ErrBusy = agenterr.ErrBusy

// ─── Chat ───

// ChatStream runs the agent loop with a user message, streaming events
// through the provided delta sink. This is the unified entry point for
// all transport layers (HTTP/SSE, gRPC, etc.).
//
// Kernel operations encapsulated:
//   - Loop resolution (per-session via LoopPool, or default loop)
//   - Concurrency guard (TryAcquire / ReleaseAcquire)
//   - Delta sink binding
//   - Agent loop execution
func (a *AgentAPI) ChatStream(ctx context.Context, sessionID, message, mode string, delta *service.Delta) error {
	// D1 fix: Synchronize Active session pointer to prevent ghost sessions on reconnect.
	// Any chat interaction proves the frontend is using this session.
	if sessionID != "" {
		a.sessMgr.Activate(sessionID)
	}

	// Resolve loop: per-session if LoopPool available
	loop := service.ResolveSessionLoop(a.loop, a.loopPool, sessionID, true)

	// Set per-request plan mode ("auto" | "plan" | "agentic")
	if mode != "" {
		loop.SetPlanMode(mode)
	}

	if message != "" {
		if restored := service.RestoreLoopHistoryIfNeeded(loop, a.histQuery, sessionID, true); restored > 0 {
			slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s (%s)", restored, sessionID, "stream"))
		}
	}

	// Concurrency guard
	if !loop.TryAcquire() {
		return ErrBusy
	}
	defer loop.ReleaseAcquire()

	// Bind protocol-specific event sink
	loop.SetDelta(delta)

	// Inject session ID into context so SpawnFunc can retrieve runtime session ID
	ctx = ctxutil.WithSessionID(ctx, sessionID)

	// Execute agent loop — title distillation is handled by TitleDistillHook post-run
	err := loop.RunWithoutAcquire(ctx, message)

	// P2 H2: Auto-persist token usage after each run
	if sessionID != "" && a.tokenUsageStore != nil {
		go func() {
			if saveErr := a.SaveSessionCost(sessionID); saveErr != nil {
				slog.Info(fmt.Sprintf("[token] Auto-save cost failed for session %s: %v", sessionID, saveErr))
			}
		}()
	}

	return err
}

// SessionID is DEPRECATED — callers should use the frontend-provided session_id directly.
// The previous implementation fell back to the default loop's startup UUID when
// LoopPool had no entry for a new session, creating "ghost sessions" whose history
// was invisible to the frontend. Retained only for compile-time interface compliance.
func (a *AgentAPI) SessionID(sessionID string) string {
	if sessionID != "" {
		return sessionID
	}
	// Defensive: return empty to surface bugs early rather than silently
	// routing to the default loop's random UUID.
	return ""
}

// StopRun signals the correct agent loop to stop.
// Uses sessionID to find the pool loop that is actually running.
// Uses GetIfExists to avoid creating a ghost loop for evicted sessions.
func (a *AgentAPI) StopRun(sessionID string) {
	if sessionID == "" {
		if loop := service.ResolveSessionLoop(a.loop, a.loopPool, a.sessMgr.Active(), false); loop != nil {
			loop.Stop()
		}
		return
	}
	if loop := service.ResidentSessionLoop(a.loop, a.loopPool, sessionID); loop != nil && loop != a.loop {
		loop.Stop()
		return
	}
	if a.loop != nil && a.loop.SessionID() == sessionID {
		a.loop.Stop()
	}
}

// RetryRun strips the last assistant turn from the agent loop and returns
// the last user message text. The frontend re-sends it via normal ChatStream.
func (a *AgentAPI) RetryRun(_ context.Context, sessionID string) (string, error) {
	loop := service.ResolveRetryLoop(a.loop, a.loopPool, sessionID)
	if restored := service.RestoreLoopHistoryIfNeeded(loop, a.histQuery, sessionID, false); restored > 0 {
		slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s (%s)", restored, sessionID, "retry"))
	}
	return loop.StripLastTurn()
}

// Approve resolves a pending tool approval.
func (a *AgentAPI) Approve(approvalID string, approved bool) error {
	if a.secHook == nil {
		return fmt.Errorf("security hook not configured")
	}
	if err := a.secHook.Resolve(approvalID, approved); err == nil {
		a.secHook.CleanupPending(approvalID)
		_, clearErr := service.ForEachCandidateLoop(a.loop, a.loopPool, a.sessMgr, func(loop *service.AgentLoop) (bool, error) {
			return loop.ClearPendingApprovalSnapshot(context.Background(), approvalID)
		})
		if clearErr != nil {
			return clearErr
		}
		return nil
	}
	handled, err := service.ForEachCandidateLoop(a.loop, a.loopPool, a.sessMgr, func(loop *service.AgentLoop) (bool, error) {
		return loop.ApprovePending(context.Background(), approvalID, approved)
	})
	if handled || err != nil {
		return err
	}
	return a.secHook.Resolve(approvalID, approved)
}

func (a *AgentAPI) ReviewPlan(ctx context.Context, sessionID string, approved bool, feedback string) error {
	loop := service.ResolveSessionLoop(a.loop, a.loopPool, sessionID, true)
	if restored := service.RestoreLoopHistoryIfNeeded(loop, a.histQuery, sessionID, true); restored > 0 {
		slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s (%s)", restored, sessionID, "plan_review"))
	}

	if !loop.TryAcquire() {
		return ErrBusy
	}
	defer loop.ReleaseAcquire()

	handled, err := loop.ReviewPendingPlan(ctx, approved, feedback)
	if handled || err != nil {
		return err
	}
	return fmt.Errorf("no pending planning review for session %s", sessionID)
}

func (a *AgentAPI) ApplyDecision(ctx context.Context, sessionID, kind, decision, feedback string) error {
	loop := service.ResolveSessionLoop(a.loop, a.loopPool, sessionID, true)
	if restored := service.RestoreLoopHistoryIfNeeded(loop, a.histQuery, sessionID, true); restored > 0 {
		slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s (%s)", restored, sessionID, "decision_apply"))
	}

	if !loop.TryAcquire() {
		return ErrBusy
	}
	defer loop.ReleaseAcquire()

	handled, err := loop.ApplyPendingDecision(ctx, kind, decision, feedback)
	if handled || err != nil {
		return err
	}
	return agenterr.NewNotFound("decision", "pending")
}

func (a *AgentAPI) ResumeRun(ctx context.Context, sessionID, runID string) error {
	loop := service.ResolveSessionLoop(a.loop, a.loopPool, sessionID, true)
	if restored := service.RestoreLoopHistoryIfNeeded(loop, a.histQuery, sessionID, true); restored > 0 {
		slog.Info(fmt.Sprintf("[session] Resumed %d messages for session %s (%s)", restored, sessionID, "resume_run"))
	}

	if !loop.TryAcquire() {
		return ErrBusy
	}
	defer loop.ReleaseAcquire()

	handled, err := loop.ResumeRun(ctx, runID)
	if handled || err != nil {
		return err
	}
	if runID != "" {
		return agenterr.NewNotFound("run", runID)
	}
	return agenterr.NewNotFound("run", "pending")
}

func (a *AgentAPI) ApplyRuntimeIngress(ctx context.Context, req apitype.RuntimeIngressRequest) (apitype.RuntimeIngressResponse, error) {
	resp := apitype.RuntimeIngressResponse{
		Status:    "accepted",
		SessionID: req.SessionID,
		Ingress:   req.Ingress,
	}
	if req.SessionID == "" {
		return resp, agenterr.NewValidation("session_id", "is required")
	}

	switch req.Ingress.Kind {
	case "message":
		if strings.TrimSpace(req.Ingress.Message) == "" {
			return resp, agenterr.NewValidation("ingress.message", "is required")
		}
		if err := a.ChatStream(ctx, req.SessionID, req.Ingress.Message, req.Ingress.Mode, &service.Delta{}); err != nil {
			return resp, err
		}
	case "decision":
		if strings.TrimSpace(req.Ingress.Decision.Decision) == "" {
			return resp, agenterr.NewValidation("ingress.decision.decision", "is required")
		}
		if err := a.ApplyDecision(ctx, req.SessionID, req.Ingress.Decision.Kind, req.Ingress.Decision.Decision, req.Ingress.Decision.Feedback); err != nil {
			return resp, err
		}
	case "resume":
		runID := req.Ingress.Run.RunID
		if runID == "" {
			runID = req.Ingress.RunID
		}
		if err := a.ResumeRun(ctx, req.SessionID, runID); err != nil {
			return resp, err
		}
	case "reconnect":
		if err := a.ChatStream(ctx, req.SessionID, "", req.Ingress.Mode, &service.Delta{}); err != nil {
			return resp, err
		}
	default:
		return resp, agenterr.NewValidation("ingress.kind", "unsupported ingress kind")
	}

	return resp, nil
}

func (a *AgentAPI) contextStatsForLoop(loop *service.AgentLoop) apitype.ContextStats {
	stats := service.CollectLoopContextStats(loop)
	byModel := make(map[string]any)
	for model, mu := range stats.ByModel {
		byModel[model] = mu
	}

	return apitype.ContextStats{
		Model:         a.router.CurrentModel(),
		HistoryCount:  stats.HistoryCount,
		TokenEstimate: stats.TokenEstimate,
		TotalCostUSD:  stats.TotalCostUSD,
		TotalCalls:    stats.TotalCalls,
		ByModel:       byModel,
		CacheHitRate:  stats.CacheHitRate,
		CacheBreaks:   stats.CacheBreaks,
	}
}

func (a *AgentAPI) ListRuntimeRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	if a.runtimeStore == nil {
		return nil, nil
	}
	snaps, err := a.runtimeStore.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return a.runtimeSnapshotsToInfo(snaps), nil
}

func (a *AgentAPI) ListPendingRuns(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	runs, err := a.ListRuntimeRuns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	pending := make([]apitype.RuntimeRunInfo, 0, len(runs))
	for _, run := range runs {
		if run.Status == "wait" || run.WaitReason != "" {
			pending = append(pending, run)
		}
	}
	return pending, nil
}

func (a *AgentAPI) ListPendingDecisions(ctx context.Context, sessionID string) ([]apitype.RuntimeRunInfo, error) {
	runs, err := a.ListPendingRuns(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	decisions := make([]apitype.RuntimeRunInfo, 0, len(runs))
	for _, run := range runs {
		if run.PendingDecision != nil {
			decisions = append(decisions, run)
		}
	}
	return decisions, nil
}

func (a *AgentAPI) ListRuntimeGraph(ctx context.Context, sessionID string) (apitype.OrchestrationGraphInfo, error) {
	graph := apitype.OrchestrationGraphInfo{SessionID: sessionID}
	runs, err := a.ListRuntimeRuns(ctx, sessionID)
	if err != nil {
		return graph, err
	}
	graph.Nodes = runs
	graph.RootRunIDs = rootRunIDs(runs)
	graph.Edges = runtimeEdges(runs)
	return graph, nil
}

func (a *AgentAPI) ListChildRuns(ctx context.Context, parentRunID string) ([]apitype.RuntimeRunInfo, error) {
	if a.runtimeStore == nil {
		return nil, nil
	}
	snaps, err := a.runtimeStore.ListByParentRun(ctx, parentRunID)
	if err != nil {
		return nil, err
	}
	return a.runtimeSnapshotsToInfo(snaps), nil
}

func rootRunIDs(runs []apitype.RuntimeRunInfo) []string {
	roots := make([]string, 0, len(runs))
	for _, run := range runs {
		if run.RunID == "" || run.ParentRunID != "" {
			continue
		}
		roots = append(roots, run.RunID)
	}
	sort.Strings(roots)
	return roots
}

func runtimeEdges(runs []apitype.RuntimeRunInfo) []apitype.RuntimeEdgeInfo {
	edges := make([]apitype.RuntimeEdgeInfo, 0, len(runs)*2)
	seen := make(map[string]struct{}, len(runs)*4)
	addEdge := func(edge apitype.RuntimeEdgeInfo) {
		key := edge.Kind + "|" + edge.SourceRunID + "|" + edge.TargetRunID + "|" + edge.BarrierID + "|" + edge.Summary
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		edges = append(edges, edge)
	}

	for _, run := range runs {
		if run.ParentRunID != "" {
			addEdge(apitype.RuntimeEdgeInfo{
				Kind:        "parent_child",
				SourceRunID: run.ParentRunID,
				TargetRunID: run.RunID,
			})
		}
		for _, handoff := range run.Handoffs {
			kind := handoff.Kind
			if kind == "" {
				kind = "handoff"
			}
			addEdge(apitype.RuntimeEdgeInfo{
				Kind:        kind,
				SourceRunID: run.RunID,
				TargetRunID: handoff.TargetRunID,
				Summary:     handoff.TargetNode,
			})
		}
		if barrier := run.ActiveBarrier; barrier != nil {
			addEdge(apitype.RuntimeEdgeInfo{
				Kind:        "barrier_wait",
				SourceRunID: run.RunID,
				BarrierID:   barrier.ID,
				Summary:     run.WaitReason,
			})
			for _, member := range barrier.Members {
				if member.RunID == "" {
					continue
				}
				addEdge(apitype.RuntimeEdgeInfo{
					Kind:        "barrier_member",
					SourceRunID: member.RunID,
					BarrierID:   barrier.ID,
					Summary:     member.TaskName,
				})
			}
		}
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Kind != edges[j].Kind {
			return edges[i].Kind < edges[j].Kind
		}
		if edges[i].SourceRunID != edges[j].SourceRunID {
			return edges[i].SourceRunID < edges[j].SourceRunID
		}
		if edges[i].TargetRunID != edges[j].TargetRunID {
			return edges[i].TargetRunID < edges[j].TargetRunID
		}
		if edges[i].BarrierID != edges[j].BarrierID {
			return edges[i].BarrierID < edges[j].BarrierID
		}
		return edges[i].Summary < edges[j].Summary
	})
	return edges
}

func (a *AgentAPI) runtimeSnapshotsToInfo(snaps []*graphruntime.RunSnapshot) []apitype.RuntimeRunInfo {
	out := make([]apitype.RuntimeRunInfo, 0, len(snaps))
	for _, snap := range snaps {
		if snap == nil {
			continue
		}
		out = append(out, runtimeSnapshotToInfo(snap))
	}
	return out
}

func runtimeSnapshotToInfo(snap *graphruntime.RunSnapshot) apitype.RuntimeRunInfo {
	info := apitype.RuntimeRunInfo{
		RunID:           snap.RunID,
		ParentRunID:     snap.TurnState.Orchestration.ParentRunID,
		Status:          string(snap.Status),
		CurrentNode:     snap.Cursor.CurrentNode,
		CurrentRoute:    snap.Cursor.RouteKey,
		WaitReason:      string(snap.ExecutionState.WaitReason),
		UpdatedAt:       snap.UpdatedAt.UTC().Format(time.RFC3339),
		PendingMerge:    snap.TurnState.Orchestration.PendingMerge,
		LastWakeSource:  snap.TurnState.Orchestration.LastWakeSource,
		ChildRunIDs:     append([]string(nil), snap.TurnState.Orchestration.ChildRunIDs...),
		PendingDecision: pendingDecisionInfo(snap),
		LastDecision:    lastDecisionInfo(snap),
	}
	if len(snap.TurnState.Orchestration.Handoffs) > 0 {
		info.Handoffs = make([]apitype.RuntimeHandoffInfo, 0, len(snap.TurnState.Orchestration.Handoffs))
		for _, handoff := range snap.TurnState.Orchestration.Handoffs {
			info.Handoffs = append(info.Handoffs, apitype.RuntimeHandoffInfo{
				TargetRunID: handoff.TargetRunID,
				TargetNode:  handoff.TargetNode,
				Kind:        handoff.Kind,
				PayloadJSON: handoff.PayloadJSON,
			})
		}
	}
	if len(snap.TurnState.Orchestration.Events) > 0 {
		info.Events = make([]apitype.RuntimeEventInfo, 0, len(snap.TurnState.Orchestration.Events))
		for _, event := range snap.TurnState.Orchestration.Events {
			eventAt := ""
			if !event.At.IsZero() {
				eventAt = event.At.UTC().Format(time.RFC3339)
			}
			info.Events = append(info.Events, apitype.RuntimeEventInfo{
				Type:      event.Type,
				RunID:     event.RunID,
				SourceRun: event.SourceRun,
				BarrierID: event.BarrierID,
				At:        eventAt,
				Summary:   event.Summary,
			})
		}
	}
	return info
}

func pendingDecisionInfo(snap *graphruntime.RunSnapshot) *apitype.RuntimeDecisionInfo {
	contract := service.PendingDecisionFromSnapshot(snap)
	if contract == nil {
		return nil
	}
	return decisionContractInfo(contract)
}

func lastDecisionInfo(snap *graphruntime.RunSnapshot) *apitype.RuntimeDecisionInfo {
	contract := service.DecisionFromSnapshot(snap)
	if contract == nil {
		return nil
	}
	return decisionContractInfo(contract)
}

func decisionContractInfo(contract *graphruntime.DecisionContractState) *apitype.RuntimeDecisionInfo {
	if contract == nil {
		return nil
	}
	info := &apitype.RuntimeDecisionInfo{
		Kind:         string(contract.Kind),
		Schema:       contract.SchemaName,
		Decision:     contract.Decision,
		Reason:       contract.Reason,
		Feedback:     contract.Feedback,
		ResumeAction: contract.ResumeAction,
	}
	if !contract.AppliedAt.IsZero() {
		info.AppliedAt = contract.AppliedAt.UTC().Format(time.RFC3339)
	}
	return info
}

// ─── Session ───

// NewSession creates a new conversation session (persisted in DB immediately).
func (a *AgentAPI) NewSession(title string) apitype.SessionResponse {
	// Create in DB first — this ensures the session is durable and visible in ListSessions
	dbID, err := a.sessMgr.CreatePersisted("web", title)
	if err != nil {
		// Fallback to in-memory if DB unavailable
		slog.Info(fmt.Sprintf("[NewSession] DB create failed, falling back to memory: %v", err))
		sess := a.sessMgr.New(title)
		return apitype.SessionResponse{SessionID: sess.ID, Title: sess.Title}
	}
	return apitype.SessionResponse{SessionID: dbID, Title: title}
}

// ListSessions returns all sessions ordered by recency, with titles from DB.
func (a *AgentAPI) ListSessions() apitype.SessionListResponse {
	// Read from DB first — this has LLM-distilled titles
	dbSessions, err := a.sessMgr.ListFromRepo(200, 0)
	if err == nil && len(dbSessions) > 0 {
		// Build DB set for dedup
		inDB := make(map[string]bool, len(dbSessions))
		infos := make([]apitype.SessionInfo, 0, len(dbSessions))
		for _, s := range dbSessions {
			inDB[s.ID] = true
			title := s.Title
			if mem, ok := a.sessMgr.Get(s.ID); ok && mem.Title != "" {
				title = mem.Title
			}
			infos = append(infos, apitype.SessionInfo{ID: s.ID, Title: title, Channel: s.Channel, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt})
		}
		return apitype.SessionListResponse{Sessions: infos, Active: a.sessMgr.Active()}
	}
	// Fallback to in-memory only
	sessions := a.sessMgr.List()
	infos := make([]apitype.SessionInfo, len(sessions))
	for i, s := range sessions {
		infos[i] = apitype.SessionInfo{ID: s.ID, Title: s.Title}
	}
	return apitype.SessionListResponse{Sessions: infos, Active: a.sessMgr.Active()}
}

// DeleteSession removes a session and all its history via the kernel.
func (a *AgentAPI) DeleteSession(id string) error {
	return a.chatEngine.DeleteSession(id)
}

// SetSessionTitle sets a display title for a session.
func (a *AgentAPI) SetSessionTitle(id, title string) {
	a.sessMgr.SetTitle(id, title)
}

// ─── History ───

// GetHistory returns conversation history for a session.
func (a *AgentAPI) GetHistory(sessionID string) ([]apitype.HistoryMessage, error) {
	if a.histQuery == nil {
		return nil, fmt.Errorf("history store not configured")
	}

	// D1 fix: Loading history means the frontend is viewing this session.
	// Sync Active pointer so ListSessions returns the correct active ID.
	if sessionID != "" {
		a.sessMgr.Activate(sessionID)
	}

	// First Principles: The Active AgentLoop memory is the absolute Ground Truth.
	// If the loop is actively running (or recently used and in memory), it has
	// messages that are not yet persisted to the database.
	if loop := service.ResidentSessionLoop(a.loop, a.loopPool, sessionID); loop != nil {
		msgs := loop.GetHistory()
		if len(msgs) > 0 {
			return a.convertLLMToHistory(msgs), nil
		}
	}

	// Fallback to database
	exports, err := a.histQuery.LoadAll(sessionID)
	if err != nil {
		return nil, err
	}
	return a.convertExportsToHistory(exports), nil
}

// convertLLMToHistory converts the agent's internal memory format to the API format.
func (a *AgentAPI) convertLLMToHistory(msgs []llm.Message) []apitype.HistoryMessage {
	nameMap := make(map[string]string)
	argsMap := make(map[string]string)
	for _, m := range msgs {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				nameMap[tc.ID] = tc.Function.Name
				argsMap[tc.ID] = string(tc.Function.Arguments)
			}
		}
	}

	apiMsgs := make([]apitype.HistoryMessage, len(msgs))
	for i, m := range msgs {
		hm := apitype.HistoryMessage{
			Role:      m.Role,
			Content:   m.Content,
			Reasoning: m.Reasoning,
		}
		if m.Role == "tool" && m.ToolCallID != "" {
			hm.ToolName = nameMap[m.ToolCallID]
			hm.ToolArgs = argsMap[m.ToolCallID]
		}
		apiMsgs[i] = hm
	}
	return apiMsgs
}

// convertExportsToHistory converts DB format to API format.
func (a *AgentAPI) convertExportsToHistory(exports []service.HistoryExport) []apitype.HistoryMessage {
	nameMap := make(map[string]string)
	argsMap := make(map[string]string)
	for _, e := range exports {
		if e.Role == "assistant" && e.ToolCalls != "" {
			var calls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			}
			if json.Unmarshal([]byte(e.ToolCalls), &calls) == nil {
				for _, c := range calls {
					if c.ID != "" {
						if c.Function.Name != "" {
							nameMap[c.ID] = c.Function.Name
						}
						if c.Function.Arguments != "" {
							argsMap[c.ID] = c.Function.Arguments
						}
					}
				}
			}
		}
	}

	msgs := make([]apitype.HistoryMessage, len(exports))
	for i, e := range exports {
		m := apitype.HistoryMessage{
			Role:      e.Role,
			Content:   e.Content,
			Reasoning: e.Reasoning,
		}
		if e.Role == "tool" && e.ToolCallID != "" {
			m.ToolName = nameMap[e.ToolCallID]
			m.ToolArgs = argsMap[e.ToolCallID]
		}
		msgs[i] = m
	}
	return msgs
}

// ClearHistory resets the conversation history for the active session.
func (a *AgentAPI) ClearHistory() {
	if loop := service.ResolveActiveManagedLoop(a.loop, a.loopPool, a.sessMgr); loop != nil {
		loop.ClearHistory()
	}
}

// CompactContext triggers context compaction for the active session.
func (a *AgentAPI) CompactContext() {
	if loop := service.ResolveActiveManagedLoop(a.loop, a.loopPool, a.sessMgr); loop != nil {
		loop.Compact()
	}
}

// ─── Model ───

// ListModels returns available models.
func (a *AgentAPI) ListModels() apitype.ModelListResponse {
	return apitype.ModelListResponse{
		Models:  a.router.ListModels(),
		Current: a.router.CurrentModel(),
	}
}

// SwitchModel changes the active model.
func (a *AgentAPI) SwitchModel(name string) error {
	return a.router.SwitchModel(name)
}

// CurrentModel returns the current model name.
func (a *AgentAPI) CurrentModel() string {
	return a.router.CurrentModel()
}

// ─── Config ───

// GetConfig returns the full sanitized configuration.
func (a *AgentAPI) GetConfig() map[string]any {
	return a.cfg.Get().Sanitized()
}

// SetConfig sets a configuration value by dot-key.
func (a *AgentAPI) SetConfig(key string, value any) error {
	return a.cfg.Set(key, value)
}

// AddProvider adds an LLM provider.
func (a *AgentAPI) AddProvider(p config.ProviderDef) error {
	return a.cfg.AddProvider(p)
}

// RemoveProvider removes an LLM provider.
func (a *AgentAPI) RemoveProvider(name string) error {
	return a.cfg.RemoveProvider(name)
}

// AddMCPServer adds an MCP server.
func (a *AgentAPI) AddMCPServer(s config.MCPServerDef) error {
	return a.cfg.AddMCPServer(s)
}

// RemoveMCPServer removes an MCP server.
func (a *AgentAPI) RemoveMCPServer(name string) error {
	return a.cfg.RemoveMCPServer(name)
}

// ─── Tools & Skills ───

// ListTools returns all registered tools.
func (a *AgentAPI) ListTools() []apitype.ToolInfoResponse {
	tools := a.toolAdmin.List()
	result := make([]apitype.ToolInfoResponse, len(tools))
	for i, t := range tools {
		result[i] = apitype.ToolInfoResponse{Name: t.Name, Enabled: t.Enabled}
	}
	return result
}

// EnableTool enables a tool by name.
func (a *AgentAPI) EnableTool(name string) error {
	return a.toolAdmin.Enable(name)
}

// DisableTool disables a tool by name.
func (a *AgentAPI) DisableTool(name string) error {
	return a.toolAdmin.Disable(name)
}

// ─── Status & Info ───

// Health returns system health info.
func (a *AgentAPI) Health() apitype.HealthResponse {
	return apitype.HealthResponse{
		Status:  "ok",
		Version: Version,
		Model:   a.router.CurrentModel(),
		Tools:   len(a.toolAdmin.List()),
	}
}

// GetSecurity returns security policy and recent audit log.
func (a *AgentAPI) GetSecurity() apitype.SecurityResponse {
	c := a.cfg.Get()
	resp := apitype.SecurityResponse{
		Mode:         c.Security.Mode,
		BlockList:    c.Security.BlockList,
		SafeCommands: c.Security.SafeCommands,
	}
	if a.secHook != nil {
		entries := a.secHook.GetAuditLog(50)
		resp.AuditEntries = make([]apitype.AuditEntry, len(entries))
		for i, e := range entries {
			dec := "allow"
			switch e.Decision {
			case security.Deny:
				dec = "deny"
			case security.Ask:
				dec = "ask"
			}
			resp.AuditEntries[i] = apitype.AuditEntry{
				Time:     e.Timestamp.Format("15:04:05"),
				Tool:     e.ToolName,
				Decision: dec,
				Reason:   e.Reason,
			}
		}
	}
	return resp
}

// GetContextStats returns context usage stats for the active session.
func (a *AgentAPI) GetContextStats() apitype.ContextStats {
	return a.contextStatsForLoop(service.ResolveSessionLoop(a.loop, a.loopPool, a.sessMgr.Active(), false))
}

// GetSystemInfo returns runtime system information.
func (a *AgentAPI) GetSystemInfo() apitype.SystemInfoResponse {
	return apitype.SystemInfoResponse{
		Version:   Version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		UptimeMs:  time.Since(a.startedAt).Milliseconds(),
		Models:    len(a.router.ListModels()),
		Tools:     len(a.toolAdmin.List()),
		Skills:    len(a.skillMgr.List()),
	}
}

// CronStatus returns cron job summary.
func (a *AgentAPI) CronStatus() map[string]any {
	if a.cronMgr == nil {
		return map[string]any{"enabled": false, "jobs": 0}
	}
	jobs, _ := a.cronMgr.List()
	active := 0
	for _, j := range jobs {
		if j.Enabled {
			active++
		}
	}
	return map[string]any{
		"enabled": true,
		"total":   len(jobs),
		"active":  active,
	}
}

// ListCronJobs returns all cron jobs.
func (a *AgentAPI) ListCronJobs() ([]apitype.CronJobInfo, error) {
	if a.cronMgr == nil {
		return nil, fmt.Errorf("cron not enabled")
	}
	jobs, err := a.cronMgr.List()
	if err != nil {
		return nil, err
	}
	result := make([]apitype.CronJobInfo, len(jobs))
	for i, job := range jobs {
		result[i] = cronJobToInfo(job)
	}
	return result, nil
}

// CreateCronJob creates a new cron job.
func (a *AgentAPI) CreateCronJob(name, schedule, prompt string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Create(name, schedule, prompt)
}

// DeleteCronJob removes a cron job.
func (a *AgentAPI) DeleteCronJob(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Delete(name)
}

// EnableCronJob activates a job.
func (a *AgentAPI) EnableCronJob(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Enable(name)
}

// DisableCronJob deactivates a job.
func (a *AgentAPI) DisableCronJob(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.Disable(name)
}

// RunCronJobNow triggers a job immediately.
func (a *AgentAPI) RunCronJobNow(name string) error {
	if a.cronMgr == nil {
		return fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.RunNow(name)
}

// ListCronLogs returns log entries for a specific cron job.
func (a *AgentAPI) ListCronLogs(jobName string) ([]apitype.CronLogInfo, error) {
	if a.cronMgr == nil {
		return nil, fmt.Errorf("cron not enabled")
	}
	logs, err := a.cronMgr.ListLogs(jobName)
	if err != nil {
		return nil, err
	}
	result := make([]apitype.CronLogInfo, len(logs))
	for i, entry := range logs {
		result[i] = apitype.CronLogInfo{
			File:    entry.File,
			Time:    entry.Time,
			Size:    entry.Size,
			Success: entry.Success,
		}
	}
	return result, nil
}

// ReadCronLog reads a specific cron job log file.
func (a *AgentAPI) ReadCronLog(jobName, logFile string) (string, error) {
	if a.cronMgr == nil {
		return "", fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.ReadLog(jobName, logFile)
}

// ═══════════════════════════════════════════
// Skills
// ═══════════════════════════════════════════

// ListSkills returns all discovered skills.
func (a *AgentAPI) ListSkills() ([]apitype.SkillInfoResponse, error) {
	if a.skillMgr == nil {
		return nil, fmt.Errorf("skill manager not configured")
	}
	skills := a.skillMgr.List()
	var result []apitype.SkillInfoResponse
	for _, s := range skills {
		result = append(result, apitype.SkillInfoResponse{
			Name:        s.Name,
			Description: s.Description,
			Path:        s.Path,
			Type:        s.Type,
			Enabled:     s.Enabled,
			Status:      s.EvoStatus,
		})
	}
	return result, nil
}

// ReadSkillContent reads SKILL.md content for a named skill.
func (a *AgentAPI) ReadSkillContent(name string) (string, error) {
	if a.skillMgr == nil {
		return "", fmt.Errorf("skill manager not configured")
	}
	s, ok := a.skillMgr.Get(name)
	if !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}
	return s.Content, nil
}

// RefreshSkills re-discovers all skills from disk.
func (a *AgentAPI) RefreshSkills() error {
	if a.skillMgr == nil {
		return fmt.Errorf("skill manager not configured")
	}
	a.skillMgr.Discover()
	return nil
}

// DeleteSkill removes a skill by name.
func (a *AgentAPI) DeleteSkill(name string) error {
	if a.skillMgr == nil {
		return fmt.Errorf("skill manager not configured")
	}
	return a.skillMgr.Delete(name)
}

// ═══════════════════════════════════════════
// MCP Servers
// ═══════════════════════════════════════════

// ListMCPServers returns all MCP server names and their running status.
func (a *AgentAPI) ListMCPServers() ([]apitype.MCPServerInfo, error) {
	if a.mcpMgr == nil {
		return nil, fmt.Errorf("MCP not configured")
	}
	servers := a.mcpMgr.ListServers()
	var result []apitype.MCPServerInfo
	for name, running := range servers {
		result = append(result, apitype.MCPServerInfo{Name: name, Running: running})
	}
	return result, nil
}

// ListMCPTools returns all tools exposed by MCP servers.
func (a *AgentAPI) ListMCPTools() ([]apitype.MCPToolInfo, error) {
	if a.mcpMgr == nil {
		return nil, fmt.Errorf("MCP not configured")
	}
	tools := a.mcpMgr.ListTools()
	var result []apitype.MCPToolInfo
	for _, t := range tools {
		result = append(result, apitype.MCPToolInfo{
			Name:        t.Name,
			Description: t.Description,
			Server:      t.ServerName,
		})
	}
	return result, nil
}

// ═══════════════════════════════════════════
// Brain artifacts
// ═══════════════════════════════════════════

// ListBrainArtifacts returns all artifacts for a given session.
func (a *AgentAPI) ListBrainArtifacts(sessionID string) ([]apitype.BrainArtifactInfo, error) {
	if sessionID == "" || a.brainDir == "" {
		return nil, fmt.Errorf("session_id required")
	}
	store := brain.NewArtifactStoreFromDir(filepath.Join(a.brainDir, sessionID))
	files, err := store.List()
	if err != nil {
		return nil, err
	}
	var result []apitype.BrainArtifactInfo
	for _, f := range files {
		// Filter out metadata/resolved files for clean display
		if strings.HasSuffix(f, ".metadata.json") || strings.Contains(f, ".resolved") {
			continue
		}
		info, err := os.Stat(filepath.Join(a.brainDir, sessionID, f))
		if err != nil {
			continue
		}
		result = append(result, apitype.BrainArtifactInfo{
			Name:    f,
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}
	return result, nil
}

// ReadBrainArtifact reads a single brain artifact by name.
func (a *AgentAPI) ReadBrainArtifact(sessionID, name string) (string, error) {
	if sessionID == "" || name == "" || a.brainDir == "" {
		return "", fmt.Errorf("session_id and name required")
	}
	store := brain.NewArtifactStoreFromDir(filepath.Join(a.brainDir, sessionID))
	return store.Read(name)
}

// ═══════════════════════════════════════════
// KI (Knowledge Items) management
// ═══════════════════════════════════════════

func kiToInfo(item *knowledge.Item) apitype.KIInfo {
	return apitype.KIInfo{
		ID:        item.ID,
		Title:     item.Title,
		Summary:   item.Summary,
		Tags:      item.Tags,
		Sources:   item.Sources,
		CreatedAt: item.CreatedAt.Format("2006-01-02 15:04"),
		UpdatedAt: item.UpdatedAt.Format("2006-01-02 15:04"),
	}
}

func kiToDetail(item *knowledge.Item) apitype.KIDetailResponse {
	return apitype.KIDetailResponse{
		ID:           item.ID,
		Title:        item.Title,
		Summary:      item.Summary,
		Content:      item.Content,
		Tags:         item.Tags,
		Sources:      item.Sources,
		Scope:        item.Scope,
		Deprecated:   item.Deprecated,
		SupersededBy: item.SupersededBy,
		ValidFrom:    formatOptionalTime(item.ValidFrom),
		ValidUntil:   formatOptionalTime(item.ValidUntil),
		CreatedAt:    item.CreatedAt.Format("2006-01-02 15:04"),
		UpdatedAt:    item.UpdatedAt.Format("2006-01-02 15:04"),
	}
}

func cronJobToInfo(job cron.Job) apitype.CronJobInfo {
	return apitype.CronJobInfo{
		Name:      job.Name,
		Schedule:  job.Schedule,
		Prompt:    job.Prompt,
		Enabled:   job.Enabled,
		Internal:  job.Internal,
		RunCount:  job.RunCount,
		FailCount: job.FailCount,
		LastRun:   formatOptionalTime(job.LastRun),
		CreatedAt: formatTime(job.CreatedAt),
		UpdatedAt: formatTime(job.UpdatedAt),
	}
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(*t)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

// ListKI returns all Knowledge Items.
func (a *AgentAPI) ListKI() ([]apitype.KIInfo, error) {
	if a.kiStore == nil {
		return nil, fmt.Errorf("KI store not configured")
	}
	items, err := a.kiStore.List()
	if err != nil {
		return nil, err
	}
	result := make([]apitype.KIInfo, len(items))
	for i, item := range items {
		result[i] = kiToInfo(item)
	}
	return result, nil
}

// GetKI returns a single Knowledge Item with full content.
func (a *AgentAPI) GetKI(id string) (apitype.KIDetailResponse, error) {
	if a.kiStore == nil {
		return apitype.KIDetailResponse{}, fmt.Errorf("KI store not configured")
	}
	item, err := a.kiStore.GetWithContent(id)
	if err != nil {
		return apitype.KIDetailResponse{}, err
	}
	return kiToDetail(item), nil
}

// DeleteKI removes a Knowledge Item directory.
func (a *AgentAPI) DeleteKI(id string) error {
	if a.kiStore == nil || id == "" {
		return fmt.Errorf("KI store not configured or id empty")
	}
	dir := filepath.Join(a.kiStore.BaseDir(), id)
	return os.RemoveAll(dir)
}

// ListKIArtifacts lists artifact files in a KI.
func (a *AgentAPI) ListKIArtifacts(id string) ([]apitype.BrainArtifactInfo, error) {
	if a.kiStore == nil || id == "" {
		return nil, fmt.Errorf("id required")
	}
	artDir := filepath.Join(a.kiStore.BaseDir(), id, "artifacts")
	var result []apitype.BrainArtifactInfo
	filepath.Walk(artDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(artDir, path)
		result = append(result, apitype.BrainArtifactInfo{
			Name:    rel,
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
		return nil
	})
	return result, nil
}

// ReadKIArtifact reads a single artifact file from a KI.
func (a *AgentAPI) ReadKIArtifact(id, name string) (string, error) {
	if a.kiStore == nil || id == "" || name == "" {
		return "", fmt.Errorf("id and name required")
	}
	data, err := os.ReadFile(filepath.Join(a.kiStore.BaseDir(), id, "artifacts", name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ═══════════════════════════════════════════
// P2 H2: Token usage persistence
// ═══════════════════════════════════════════

// SetTokenUsageStore injects the token usage persistence store.
func (a *AgentAPI) SetTokenUsageStore(store *persistence.TokenUsageStore) {
	a.tokenUsageStore = store
}

func (a *AgentAPI) SetRuntimeStore(store *persistence.RunSnapshotStore) {
	a.runtimeStore = store
}

// SaveSessionCost persists the current session's token usage to the database.
func (a *AgentAPI) SaveSessionCost(sessionID string) error {
	if a.tokenUsageStore == nil {
		return fmt.Errorf("token usage store not configured")
	}
	loop := service.ResolveStatsLoop(a.loop, a.loopPool, a.sessMgr, sessionID)
	if loop == nil {
		return fmt.Errorf("session %s not found in memory", sessionID)
	}
	stats := a.contextStatsForLoop(loop)
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
	return a.tokenUsageStore.SaveSessionUsage(
		sessionID, stats.Model,
		promptTok, completeTok,
		stats.TotalCalls, stats.TotalCostUSD,
		stats.ByModel,
	)
}

// GetSessionCost retrieves stored token usage for a given session.
func (a *AgentAPI) GetSessionCost(sessionID string) (map[string]any, error) {
	if a.tokenUsageStore == nil {
		return nil, fmt.Errorf("token usage store not configured")
	}
	usage, err := a.tokenUsageStore.GetSessionUsage(sessionID)
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
