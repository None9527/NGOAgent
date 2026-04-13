package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

type graphNodeRuntime struct {
	*AgentLoop
	state *graphruntime.TurnState
	exec  *graphruntime.ExecutionState
}

type loopSideEffectBaseline struct {
	ephemerals          int
	pendingMedia        int
	activeSkills        map[string]string
	orchestrationEvents int
}

func newGraphNodeRuntime(loop *AgentLoop, state *graphruntime.TurnState, exec *graphruntime.ExecutionState) *graphNodeRuntime {
	return &graphNodeRuntime{
		AgentLoop: loop,
		state:     state,
		exec:      exec,
	}
}

func (r *graphNodeRuntime) appendMessage(msg llm.Message) {
	if r.state == nil {
		r.AgentLoop.appendMessage(msg)
		return
	}
	r.state.History = append(r.state.History, messageToGraphHistory(msg))
}

func (r *graphNodeRuntime) captureLoopSideEffectBaseline() loopSideEffectBaseline {
	r.AgentLoop.mu.Lock()
	defer r.AgentLoop.mu.Unlock()
	base := loopSideEffectBaseline{
		ephemerals:          len(r.AgentLoop.ephemerals),
		pendingMedia:        len(r.AgentLoop.pendingMedia),
		activeSkills:        cloneStringMap(r.AgentLoop.activeSkills),
		orchestrationEvents: len(r.AgentLoop.orchestration.Events),
	}
	return base
}

func (r *graphNodeRuntime) syncLoopSideEffectsSince(base loopSideEffectBaseline) {
	if r.state == nil {
		r.syncStateHistoryFromLoop()
		return
	}

	r.AgentLoop.mu.Lock()
	if base.ephemerals < len(r.AgentLoop.ephemerals) {
		r.state.Ephemerals = append(r.state.Ephemerals, r.AgentLoop.ephemerals[base.ephemerals:]...)
		r.AgentLoop.ephemerals = r.AgentLoop.ephemerals[:base.ephemerals]
	}
	if base.pendingMedia < len(r.AgentLoop.pendingMedia) {
		r.state.PendingMedia = append(r.state.PendingMedia, cloneMediaItems(r.AgentLoop.pendingMedia[base.pendingMedia:])...)
		r.AgentLoop.pendingMedia = r.AgentLoop.pendingMedia[:base.pendingMedia]
	}
	for name, content := range r.AgentLoop.activeSkills {
		if base.activeSkills == nil || base.activeSkills[name] != content {
			if r.state.ActiveSkills == nil {
				r.state.ActiveSkills = make(map[string]string)
			}
			r.state.ActiveSkills[name] = content
		}
	}
	if base.orchestrationEvents < len(r.AgentLoop.orchestration.Events) {
		r.state.Orchestration.Events = append(
			r.state.Orchestration.Events,
			r.AgentLoop.orchestration.Events[base.orchestrationEvents:]...,
		)
	}
	r.state.Task = graphruntime.TaskState{
		YieldRequested:   r.AgentLoop.task.YieldRequested,
		Name:             r.AgentLoop.task.Name,
		Mode:             r.AgentLoop.task.Mode,
		Status:           r.AgentLoop.task.Status,
		Summary:          r.AgentLoop.task.Summary,
		StepsSinceUpdate: r.AgentLoop.task.StepsSinceUpdate,
		PlanModified:     r.AgentLoop.task.PlanModified,
		CurrentStep:      r.AgentLoop.task.CurrentStep,
		ArtifactLastStep: cloneIntMap(r.AgentLoop.task.ArtifactLastStep),
		SkillLoaded:      r.AgentLoop.task.SkillLoaded,
		SkillPath:        r.AgentLoop.task.SkillPath,
	}
	r.state.ForceNextTool = r.AgentLoop.guard.PeekForceToolName()
	r.AgentLoop.mu.Unlock()

	if r.exec != nil {
		r.exec.PendingWake = r.AgentLoop.pendingWake.Load()
		if r.AgentLoop.barrier != nil {
			barrier := r.AgentLoop.barrier.Snapshot()
			r.exec.PendingBarrier = &barrier
		}
	}
}

func (r *graphNodeRuntime) syncStateHistoryFromLoop() {
	if r.state == nil {
		return
	}
	r.AgentLoop.mu.Lock()
	defer r.AgentLoop.mu.Unlock()
	r.state.History = messagesToGraphHistory(r.AgentLoop.history)
	r.state.Ephemerals = append([]string(nil), r.AgentLoop.ephemerals...)
	r.state.PendingMedia = cloneMediaItems(r.AgentLoop.pendingMedia)
	r.state.Compact = graphruntime.CompactState{
		CompactCount:        r.AgentLoop.compactCount,
		OutputContinuations: r.AgentLoop.outputContinuations,
		HistoryDirty:        r.AgentLoop.historyDirty,
	}
}

func (r *graphNodeRuntime) microCompact() {
	if r.state != nil {
		r.microCompactState()
		return
	}
	r.AgentLoop.microCompact()
	r.syncStateHistoryFromLoop()
}

func (r *graphNodeRuntime) forceTruncate(keep int) {
	if r.state != nil {
		r.forceTruncateState(keep)
		return
	}
	r.AgentLoop.forceTruncate(keep)
	r.syncStateHistoryFromLoop()
}

func (r *graphNodeRuntime) toolHeavyCompact() bool {
	if r.state != nil {
		return r.toolHeavyCompactState()
	}
	ok := r.AgentLoop.toolHeavyCompact()
	r.syncStateHistoryFromLoop()
	return ok
}

func (r *graphNodeRuntime) microCompactState() {
	if len(r.state.History) < 10 {
		return
	}
	assistantCount := 0
	digestBoundary := len(r.state.History)
	for i := len(r.state.History) - 1; i >= 0; i-- {
		if r.state.History[i].Role == "assistant" {
			assistantCount++
			if assistantCount >= 2 {
				digestBoundary = i
				break
			}
		}
	}
	for i := 0; i < digestBoundary; i++ {
		if r.state.History[i].Role == "tool" && len(r.state.History[i].Content) > 200 {
			firstLine := r.state.History[i].Content
			if idx := strings.IndexByte(firstLine, '\n'); idx > 0 {
				firstLine = firstLine[:idx]
			}
			if len(firstLine) > 100 {
				firstLine = firstLine[:100]
			}
			r.state.History[i].Content = fmt.Sprintf("[cleared: %s]", firstLine)
		}
	}
}

func (r *graphNodeRuntime) forceTruncateState(keep int) {
	if len(r.state.History) <= keep+1 {
		return
	}
	safeCut := len(r.state.History) - keep
	if safeCut < 1 {
		safeCut = 1
	}
	for safeCut > 1 && r.state.History[safeCut].Role == "tool" {
		safeCut--
	}
	discarded := graphHistoryToMessages(r.state.History[1:safeCut])
	if r.AgentLoop.deps.Hooks != nil && len(discarded) > 0 {
		r.AgentLoop.deps.Hooks.FireBeforeCompact(context.Background(), discarded)
	}
	truncated := []graphruntime.ConversationMessageState{r.state.History[0]}
	truncated = append(truncated, r.state.History[safeCut:]...)
	r.state.History = truncated
	r.state.Compact.HistoryDirty = true
	r.AgentLoop.mu.Lock()
	r.AgentLoop.persistedCount = 0
	r.AgentLoop.mu.Unlock()
}

func (r *graphNodeRuntime) toolHeavyCompactState() bool {
	if len(r.state.History) < 8 {
		return false
	}
	totalChars := 0
	toolChars := 0
	for _, msg := range r.state.History {
		totalChars += len(msg.Content)
		if msg.Role == "tool" {
			toolChars += len(msg.Content)
		}
	}
	if totalChars == 0 || float64(toolChars)/float64(totalChars) < 0.60 {
		return false
	}

	const threshold = 10 * 1024
	compressed := 0
	for i := range r.state.History {
		if r.state.History[i].Role != "tool" || len(r.state.History[i].Content) <= threshold {
			continue
		}
		content := r.state.History[i].Content
		originalLen := len(content)
		headSize := 500
		tailSize := 1500
		if originalLen < headSize+tailSize+100 {
			continue
		}
		r.state.History[i].Content = content[:headSize] +
			fmt.Sprintf("\n\n[... tool output compressed: %d -> %d bytes, %.0f%% reduction ...]\n\n",
				originalLen, headSize+tailSize, float64(originalLen-headSize-tailSize)/float64(originalLen)*100) +
			content[originalLen-tailSize:]
		compressed++
	}
	return compressed > 0
}

func (r *graphNodeRuntime) doCompactState(runCtx context.Context) {
	if len(r.state.History) <= 6 {
		return
	}
	history := graphHistoryToMessages(r.state.History)

	type turnInfo struct {
		start   int
		density int
	}
	var turns []turnInfo
	for i := 1; i < len(history); i++ {
		if history[i].Role == "user" {
			turns = append(turns, turnInfo{start: i})
		}
	}
	for idx := range turns {
		end := len(history)
		if idx+1 < len(turns) {
			end = turns[idx+1].start
		}
		for j := turns[idx].start; j < end; j++ {
			turns[idx].density += len(history[j].Content)
			turns[idx].density += len(history[j].ToolCalls) * 200
		}
	}

	safeCut := 1
	if len(turns) >= 2 {
		safeCut = turns[len(turns)-2].start
	} else if len(turns) >= 1 {
		safeCut = turns[len(turns)-1].start
	} else {
		safeCut = len(history) / 2
	}

	middle := history[1:safeCut]
	tail := append([]llm.Message(nil), history[safeCut:]...)
	firstMsg := history[0]

	if r.AgentLoop.deps.Hooks != nil {
		r.AgentLoop.deps.Hooks.FireBeforeCompact(context.Background(), middle)
	}

	var content strings.Builder
	for _, msg := range middle {
		if msg.Content != "" {
			content.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
		}
	}

	summaryMessages := []llm.Message{
		{Role: "system", Content: `You are a conversation summarizer. Produce a concise structured summary preserving user intent, code changes, learned facts, current work, errors/fixes, and all user messages. If preference_knowledge or semantic_knowledge tags appear, preserve them fully.`},
		{Role: "user", Content: content.String()},
	}

	model := r.AgentLoop.deps.LLMRouter.CurrentModel()
	provider, _ := r.AgentLoop.deps.LLMRouter.Resolve(model)

	ctx, cancel := context.WithTimeout(runCtx, 30*time.Second)
	defer cancel()

	summary := ""
	r.state.Compact.CompactCount++
	if r.state.Compact.CompactCount > 3 {
		for _, msg := range middle {
			if msg.Role == "assistant" && msg.Content != "" {
				summary += msg.Content[:min(300, len(msg.Content))] + "... "
			}
		}
		if summary != "" {
			summary = "[Compact limit reached — raw extraction] " + summary
		}
	} else if provider != nil {
		req := &llm.Request{
			Model:       model,
			Messages:    summaryMessages,
			Temperature: 0.3,
			MaxTokens:   1024,
			Stream:      false,
		}
		ch := make(chan llm.StreamChunk, 32)
		resp, err := provider.GenerateStream(ctx, req, ch)
		for range ch {
		}
		if err == nil && resp != nil && resp.Content != "" {
			summary = resp.Content
		}
	}

	if summary == "" {
		for _, msg := range middle {
			if msg.Role == "assistant" && msg.Content != "" {
				summary += msg.Content[:min(200, len(msg.Content))] + "... "
			}
		}
	}

	compacted := []llm.Message{firstMsg}
	if summary != "" {
		if strings.HasPrefix(firstMsg.Content, "[COMPACT_SUMMARY]") {
			compacted = []llm.Message{{Role: "assistant", Content: "[COMPACT_SUMMARY] " + summary}}
		} else {
			compacted = append(compacted, llm.Message{Role: "assistant", Content: "[COMPACT_SUMMARY] " + summary})
		}
	}
	compacted = append(compacted, tail...)

	if recentFiles := extractRecentFiles(middle); len(recentFiles) > 0 {
		var fileBuf strings.Builder
		fileBuf.WriteString("Files recently accessed before context compaction (may need re-reading):\n")
		for _, f := range recentFiles {
			fileBuf.WriteString("- " + f + "\n")
		}
		compacted = append(compacted, llm.Message{Role: "user", Content: fileBuf.String()})
	}

	if len(r.state.ActiveSkills) > 0 {
		var skillBuf strings.Builder
		skillBuf.WriteString("[SKILL_RECOVERY] The following skills were active before context compaction. Resume execution:\n\n")
		for name, content := range r.state.ActiveSkills {
			skillBuf.WriteString(fmt.Sprintf("<skill_execution name=\"%s\">\n", name))
			skillBuf.WriteString(content)
			skillBuf.WriteString("\n</skill_execution>\n\n")
		}
		compacted = append(compacted, llm.Message{Role: "user", Content: skillBuf.String()})
		slog.Info(fmt.Sprintf("[compact] re-injected %d active skill(s) after graph compaction", len(r.state.ActiveSkills)))
	}

	r.state.History = messagesToGraphHistory(compacted)
	r.state.Compact.HistoryDirty = true
	r.AgentLoop.tokenTracker.Reset()

	if r.AgentLoop.deps.Hooks != nil {
		go r.AgentLoop.deps.Hooks.FireAfterCompact(context.Background(), compacted)
	}
}

func (r *graphNodeRuntime) doCompact(ctx context.Context) {
	if r.state != nil {
		r.doCompactState(ctx)
		return
	}
	r.AgentLoop.doCompact(ctx)
	r.syncStateHistoryFromLoop()
}

func (r *graphNodeRuntime) doGenerate(ctx context.Context, opts RunOptions, excluded []string) (*llm.Response, string, error) {
	if r.state == nil {
		resp, provider, err := r.AgentLoop.doGenerate(ctx, opts, excluded)
		r.syncStateHistoryFromLoop()
		return resp, provider, err
	}
	input := generateInput{
		History:      graphHistoryToMessages(r.state.History),
		Ephemerals:   append([]string(nil), r.state.Ephemerals...),
		PendingMedia: cloneMediaItems(r.state.PendingMedia),
		HistoryDirty: r.state.Compact.HistoryDirty,
		SubagentMode: opts.Mode == "subagent",
	}
	resp, provider, err := r.AgentLoop.doGenerateWithInput(ctx, opts, excluded, input)
	r.state.Ephemerals = nil
	r.state.PendingMedia = nil
	r.state.Compact.HistoryDirty = false
	r.AgentLoop.mu.Lock()
	r.AgentLoop.ephemerals = nil
	r.AgentLoop.pendingMedia = nil
	r.AgentLoop.historyDirty = false
	r.AgentLoop.mu.Unlock()
	return resp, provider, err
}

func (r *graphNodeRuntime) execToolsConcurrent(ctx context.Context, calls []llm.ToolCall) {
	type toolOutput struct {
		result string
		err    error
	}

	results := make([]toolOutput, len(calls))
	for i, tc := range calls {
		base := r.captureLoopSideEffectBaseline()
		result, err := r.AgentLoop.doToolExec(ctx, tc)
		results[i] = toolOutput{result: result, err: err}
		r.syncLoopSideEffectsSince(base)
	}

	for _, result := range results {
		if result.err != nil && errors.Is(result.err, ErrApprovalDenied) {
			for j, tc := range calls {
				content := "Cancelled due to tool denial."
				if results[j].result != "" {
					content = results[j].result
				}
				r.appendMessage(llm.Message{
					Role:       "tool",
					Content:    content,
					ToolCallID: tc.ID,
				})
			}
			return
		}
	}

	for i, result := range results {
		content := result.result
		if result.err != nil {
			content = fmt.Sprintf("Error: %v", result.err)
		}
		r.appendMessage(llm.Message{
			Role:       "tool",
			Content:    content,
			ToolCallID: calls[i].ID,
		})
	}
}

func (r *graphNodeRuntime) execToolsSerial(ctx context.Context, calls []llm.ToolCall) bool {
	for i, tc := range calls {
		base := r.captureLoopSideEffectBaseline()
		result, err := r.AgentLoop.doToolExec(ctx, tc)
		r.syncLoopSideEffectsSince(base)

		if errors.Is(err, ErrApprovalDenied) {
			r.appendMessage(llm.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
			for j := i + 1; j < len(calls); j++ {
				r.appendMessage(llm.Message{
					Role:       "tool",
					Content:    "Cancelled due to previous tool denial.",
					ToolCallID: calls[j].ID,
				})
			}
			r.emitText("\n" + result + "\n")
			return true
		}

		content := result
		if err != nil {
			content = fmt.Sprintf("Error: %v", err)
		}
		r.appendMessage(llm.Message{
			Role:       "tool",
			Content:    content,
			ToolCallID: tc.ID,
		})
	}
	return false
}

func (r *graphNodeRuntime) InjectEphemeral(msg string) {
	if msg == "" {
		return
	}
	if r.state == nil {
		r.AgentLoop.InjectEphemeral(msg)
		return
	}
	r.state.Ephemerals = append(r.state.Ephemerals, msg)
}

func (r *graphNodeRuntime) persistHistory() {
	if r.state == nil {
		r.AgentLoop.persistHistory()
		return
	}
	if r.AgentLoop.deps.HistoryStore == nil {
		return
	}
	sid := r.AgentLoop.SessionID()
	if sid == "" {
		return
	}
	r.AgentLoop.mu.Lock()
	baseline := r.AgentLoop.persistedCount
	r.AgentLoop.mu.Unlock()
	if baseline >= len(r.state.History) {
		return
	}
	newMsgs := graphHistoryToMessages(r.state.History[baseline:])
	exports := make([]HistoryExport, len(newMsgs))
	for i, msg := range newMsgs {
		exports[i] = msgToExport(msg)
	}
	if err := r.AgentLoop.deps.HistoryStore.AppendAll(sid, exports); err != nil {
		slog.Info(fmt.Sprintf("[history] graph incremental persist failed: %v", err))
		return
	}
	r.AgentLoop.mu.Lock()
	r.AgentLoop.persistedCount = len(r.state.History)
	r.AgentLoop.mu.Unlock()
}

func (r *graphNodeRuntime) persistFullHistory() {
	if r.state == nil {
		r.AgentLoop.persistFullHistory()
		return
	}
	if r.AgentLoop.deps.HistoryStore == nil {
		return
	}
	sid := r.AgentLoop.SessionID()
	if sid == "" {
		return
	}
	msgs := graphHistoryToMessages(r.state.History)
	exports := make([]HistoryExport, len(msgs))
	for i, msg := range msgs {
		exports[i] = msgToExport(msg)
	}
	if err := r.AgentLoop.deps.HistoryStore.SaveAll(sid, exports); err != nil {
		slog.Info(fmt.Sprintf("[history] graph full persist failed: %v", err))
		return
	}
	r.AgentLoop.mu.Lock()
	r.AgentLoop.persistedCount = len(r.state.History)
	r.AgentLoop.mu.Unlock()
}

func (r *graphNodeRuntime) appendEmptyUserMessage() {
	if r.state == nil {
		r.AgentLoop.appendEmptyUserMessage()
		r.syncStateHistoryFromLoop()
		return
	}
	r.appendMessage(r.AgentLoop.buildUserMessage(""))
}

func (r *graphNodeRuntime) lastHistoryMessage() llm.Message {
	if r.state == nil || len(r.state.History) == 0 {
		return r.AgentLoop.lastHistoryMessage()
	}
	return graphHistoryToMessage(r.state.History[len(r.state.History)-1])
}

func (r *graphNodeRuntime) incrementOutputContinuation() int {
	if r.state != nil {
		cont := 1
		if r.exec != nil {
			cont = r.exec.Continuation.Count + 1
		} else {
			cont = r.state.Compact.OutputContinuations + 1
		}
		if r.exec != nil {
			r.exec.Continuation.Count = cont
		}
		r.state.Compact.OutputContinuations = cont
		return cont
	}
	cont := r.AgentLoop.incrementOutputContinuation()
	if r.exec != nil {
		r.exec.Continuation.Count = cont
	}
	return cont
}

func (r *graphNodeRuntime) resetOutputContinuations() {
	if r.state != nil {
		r.state.Compact.OutputContinuations = 0
		if r.exec != nil {
			r.exec.Continuation.Count = 0
		}
		return
	}
	r.AgentLoop.resetOutputContinuations()
	if r.exec != nil {
		r.exec.Continuation.Count = 0
	}
}

func (r *graphNodeRuntime) consumePendingWake() bool {
	pending := r.AgentLoop.consumePendingWake()
	if r.exec != nil && r.exec.PendingWake {
		pending = true
		r.exec.PendingWake = false
	}
	if r.state != nil && r.state.Orchestration.PendingMerge {
		pending = true
		r.state.Orchestration.PendingMerge = false
	}
	return pending
}

func (r *graphNodeRuntime) hasPendingWake() bool {
	if r.AgentLoop.hasPendingWake() {
		return true
	}
	if r.exec != nil && r.exec.PendingWake {
		return true
	}
	if r.state != nil && r.state.Orchestration.PendingMerge {
		return r.state.Orchestration.PendingMerge
	}
	return false
}

func (r *graphNodeRuntime) setPlanningDecision(planning graphruntime.PlanningState) {
	if r.state == nil {
		r.AgentLoop.setPlanningDecision(planning)
		return
	}
	intelligence := r.state.Intelligence
	intelligence.Planning = planning
	if planning.ReviewRequired && planning.ReviewDecision == "" {
		intelligence.Decision = planningPendingDecisionContract(planning)
	} else if planning.ReviewDecision != "" {
		intelligence.Decision = planningAppliedDecisionContract(planning)
	}
	r.state.Intelligence = intelligence
}

func (r *graphNodeRuntime) setRepairDecision(repair graphruntime.RepairState) {
	if r.state == nil {
		r.AgentLoop.setRepairDecision(repair)
		return
	}
	r.state.Intelligence.Repair = repair
}

func (r *graphNodeRuntime) repairSnapshot() graphruntime.RepairState {
	if r.state == nil {
		return r.AgentLoop.repairSnapshot()
	}
	return r.state.Intelligence.Repair
}

func (r *graphNodeRuntime) intelligenceSnapshot() graphruntime.IntelligenceState {
	if r.state == nil {
		return r.AgentLoop.intelligenceSnapshot()
	}
	return cloneIntelligenceState(r.state.Intelligence)
}

func (r *graphNodeRuntime) setOrchestrationState(state graphruntime.OrchestrationState) {
	if r.state == nil {
		r.AgentLoop.setOrchestrationState(state)
		return
	}
	r.state.Orchestration = cloneOrchestrationState(state)
	if r.exec != nil {
		r.exec.PendingWake = state.PendingMerge
		if state.ActiveBarrier != nil {
			barrier := *state.ActiveBarrier
			barrier.Members = append([]graphruntime.BarrierMemberState(nil), state.ActiveBarrier.Members...)
			r.exec.PendingBarrier = &barrier
		}
	}
}
