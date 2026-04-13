package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

type ephemeralSink interface {
	InjectEphemeral(string)
}

type prepareRuntime interface {
	runMode() string
	prepareSnapshot() prepareTurnSnapshot
	shouldInjectPlanning(string) bool
	artifactExists(string) bool
	setGuardModeState(bool, bool, bool, string)
	Mode() ModePermissions
	estimateTokens() int
	currentModel() string
	phaseEphemeral() string
	stepsSinceBoundary() int
	scratchpadDir() string
	generateKIIndex() string
	setPlanningDecision(graphruntime.PlanningState)
	InjectEphemeral(string)
}

type prepareNodeService struct {
	runtime prepareRuntime
}

func (s prepareNodeService) Execute(ctx context.Context, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	planning := s.prepare(ctx)
	if state != nil {
		state.Intelligence.Planning = planning
	}
	return graphruntime.NodeResult{
		RouteKey:      graphRouteOrchestrate,
		ObservedState: "prepare",
	}, nil
}

type orchestrateNodeService struct{}

func (orchestrateNodeService) Execute(rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	if (state != nil && state.Orchestration.PendingMerge) || (rt != nil && rt.Execution != nil && rt.Execution.PendingWake) {
		return graphruntime.NodeResult{RouteKey: graphRouteMerge, ObservedState: "orchestrate"}, nil
	}
	if state != nil && state.Orchestration.ActiveBarrier != nil && state.Orchestration.ActiveBarrier.PendingCount > 0 {
		return graphruntime.NodeResult{RouteKey: graphRouteBarrierWait, ObservedState: "orchestrate"}, nil
	}
	if state != nil && state.Intelligence.Planning.Required {
		return graphruntime.NodeResult{RouteKey: graphRoutePlan, ObservedState: "plan"}, nil
	}
	return graphruntime.NodeResult{RouteKey: graphRouteOK, ObservedState: "orchestrate"}, nil
}

type spawnNodeService struct{}

func (spawnNodeService) Execute() (graphruntime.NodeResult, error) {
	return graphruntime.NodeResult{RouteKey: graphRouteBarrierWait, ObservedState: "spawn"}, nil
}

type barrierWaitNodeService struct{}

func (barrierWaitNodeService) Execute(state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	if state != nil && state.Orchestration.ActiveBarrier != nil && state.Orchestration.ActiveBarrier.PendingCount > 0 {
		return graphruntime.NodeResult{
			Status:          graphruntime.NodeStatusWait,
			WaitReason:      graphruntime.WaitReasonBarrier,
			RouteKey:        graphRouteMerge,
			ObservedState:   "barrier_wait",
			NeedsCheckpoint: true,
		}, nil
	}
	return graphruntime.NodeResult{RouteKey: graphRouteGenerate, ObservedState: "barrier_wait"}, nil
}

type mergeRuntime interface {
	consumePendingWake() bool
	emitAutoWakeStart()
	appendEmptyUserMessage()
	Mode() ModePermissions
	setOrchestrationState(graphruntime.OrchestrationState)
}

type mergeNodeService struct {
	runtime mergeRuntime
}

func (s mergeNodeService) Execute(state *graphruntime.TurnState, rs *runState) (graphruntime.NodeResult, error) {
	pendingWake := s.runtime.consumePendingWake()
	pendingMerge := state != nil && state.Orchestration.PendingMerge
	if pendingWake || pendingMerge {
		s.runtime.emitAutoWakeStart()
		rs.setStepCount(0)
		rs.setRetryCount(0)
		s.runtime.appendEmptyUserMessage()
		if state != nil {
			state.Orchestration.PendingMerge = false
			state.Orchestration.LastWakeSource = "barrier"
			if state.Orchestration.ActiveBarrier != nil && state.Orchestration.ActiveBarrier.Finalized {
				state.Orchestration.ActiveBarrier.PendingCount = 0
			}
			s.runtime.setOrchestrationState(state.Orchestration)
		}
		return graphruntime.NodeResult{RouteKey: graphRoutePrepare, ObservedState: "merge"}, nil
	}
	if s.runtime.Mode().EvoEnabled {
		return graphruntime.NodeResult{RouteKey: graphRouteEvaluate, ObservedState: "merge"}, nil
	}
	return graphruntime.NodeResult{RouteKey: graphRouteComplete, ObservedState: "merge"}, nil
}

type planningDecisionSink interface {
	setPlanningDecision(graphruntime.PlanningState)
}

type planReviewEmitter interface {
	OnPlanReview(message string, paths []string)
}

type planningNodeService struct {
	ephemeral ephemeralSink
	decision  planningDecisionSink
	review    planReviewEmitter
}

func (s planningNodeService) Execute(state *graphruntime.TurnState) graphruntime.NodeResult {
	if state == nil {
		return graphruntime.NodeResult{
			RouteKey:      graphRouteGenerate,
			ObservedState: "plan",
		}
	}
	planning := state.Intelligence.Planning
	switch planning.ReviewDecision {
	case "approved":
		planning.ReviewRequired = false
		state.Intelligence.Planning = planning
		setDecisionContract(state, planningAppliedDecisionContract(planning))
		if s.decision != nil {
			s.decision.setPlanningDecision(planning)
		}
		return graphruntime.NodeResult{
			RouteKey:      graphRouteGenerate,
			ObservedState: "plan",
		}
	case "revise":
		planning.ReviewRequired = false
		state.Intelligence.Planning = planning
		setDecisionContract(state, planningAppliedDecisionContract(planning))
		if s.decision != nil {
			s.decision.setPlanningDecision(planning)
		}
		message := "Plan review requested revision before execution."
		if feedback := strings.TrimSpace(planning.ReviewFeedback); feedback != "" {
			message = "Plan review requested revision: " + feedback
		}
		if s.ephemeral != nil {
			s.ephemeral.InjectEphemeral(message)
		}
		return graphruntime.NodeResult{
			RouteKey:      graphRoutePlanRevise,
			ObservedState: "plan",
		}
	}
	if planning.Required && planning.ReviewRequired {
		message := "Planning review required before execution"
		if planning.Trigger != "" {
			message = "Planning trigger: " + planning.Trigger
		}
		setDecisionContract(state, planningPendingDecisionContract(planning))
		if s.review != nil {
			s.review.OnPlanReview(message, append([]string(nil), planning.MissingArtifacts...))
		}
		return graphruntime.NodeResult{
			Status:          graphruntime.NodeStatusWait,
			WaitReason:      graphruntime.WaitReasonUserInput,
			RouteKey:        graphRoutePlan,
			ObservedState:   "plan",
			NeedsCheckpoint: true,
		}
	}
	return graphruntime.NodeResult{
		RouteKey:      graphRouteGenerate,
		ObservedState: "plan",
	}
}

type reflectionNodeService struct {
	ephemeral ephemeralSink
}

func (s reflectionNodeService) Execute(state *graphruntime.TurnState) graphruntime.NodeResult {
	review := resolveReflectionReview(state)
	if state != nil {
		state.Reflection.LastReview = review.RawJSON
		state.Intelligence.Review = graphruntime.ReviewDecisionState{
			SchemaName: graphReflectionSchema,
			Decision:   review.Decision,
			Reason:     review.Reason,
			RawJSON:    review.RawJSON,
			Valid:      review.RawJSON != "",
		}
		state.StructuredOutput = graphruntime.StructuredOutputState{
			SchemaName: graphReflectionSchema,
			RawJSON:    review.RawJSON,
			Valid:      review.RawJSON != "",
		}
		setDecisionContract(state, reflectionDecisionContract(review))
	}
	if review.Decision == "revise" {
		if review.Reason != "" && s.ephemeral != nil {
			s.ephemeral.InjectEphemeral("Self-review requested revision: " + review.Reason)
		}
		return graphruntime.NodeResult{
			RouteKey:         graphRouteGenerate,
			ObservedState:    "reflect",
			OutputSchemaName: graphReflectionSchema,
		}
	}
	return graphruntime.NodeResult{
		RouteKey:         graphRouteDone,
		ObservedState:    "reflect",
		OutputSchemaName: graphReflectionSchema,
	}
}

type reflectionReview struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	RawJSON  string `json:"-"`
}

func resolveReflectionReview(state *graphruntime.TurnState) reflectionReview {
	if state != nil &&
		state.Intelligence.Review.SchemaName == graphReflectionSchema &&
		state.Intelligence.Review.Valid &&
		state.Intelligence.Review.Decision != "" {
		return reflectionReview{
			Decision: state.Intelligence.Review.Decision,
			Reason:   state.Intelligence.Review.Reason,
			RawJSON:  state.Intelligence.Review.RawJSON,
		}
	}
	if state != nil &&
		state.StructuredOutput.SchemaName == graphReflectionSchema &&
		state.StructuredOutput.Valid &&
		state.StructuredOutput.RawJSON != "" {
		var parsed reflectionReview
		if err := json.Unmarshal([]byte(state.StructuredOutput.RawJSON), &parsed); err == nil && parsed.Decision != "" {
			parsed.RawJSON = state.StructuredOutput.RawJSON
			return parsed
		}
	}
	return reflectionReview{
		Decision: "accept",
		Reason:   "self-review accepted current draft",
		RawJSON:  `{"decision":"accept","reason":"self-review accepted current draft"}`,
	}
}

type evaluationRuntime interface {
	evaluationContextTimeout() (context.Context, context.CancelFunc)
	intelligenceSnapshot() graphruntime.IntelligenceState
	evaluateCurrentRun(context.Context, graphruntime.EvaluationState) (bool, error)
	logInlineEvaluationFailure(error)
}

type generateRuntime interface {
	microCompact()
	doGenerate(context.Context, RunOptions, []string) (*llm.Response, string, error)
	guardCheck(string, int, int) GuardVerdict
	recordFinalResponse(string)
	selfReviewEnabled() bool
	appendMessage(llm.Message)
	incrementOutputContinuation() int
	resetOutputContinuations()
	emitText(string)
	emitError(error)
	forceTruncate(int)
	transitionTo(State, string) graphruntime.NodeResult
	finishWith(State, graphruntime.NodeStatus) graphruntime.NodeResult
}

type generateNodeService struct {
	runtime generateRuntime
}

func (s generateNodeService) Execute(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	s.runtime.microCompact()

	opts := rs.opts
	opts.MaxTokens = rs.maxTokens()
	resp, provName, err := s.runtime.doGenerate(ctx, opts, rs.excludedProviderList())
	rs.setLastProvider(provName)

	if err != nil {
		return s.handleError(ctx, rs, err)
	}
	rs.setRetryCount(0)

	if resp.StopReason == "length" && len(resp.ToolCalls) == 0 {
		if cont := s.runtime.incrementOutputContinuation(); cont <= 3 {
			slog.Info(fmt.Sprintf("[max-output] continuation %d/3 — output truncated, auto-resuming", cont))
			s.runtime.appendMessage(llm.Message{
				Role:      "assistant",
				Content:   resp.Content,
				Reasoning: resp.Reasoning,
			})
			s.runtime.appendMessage(llm.Message{
				Role:    "user",
				Content: "Your previous output was truncated due to length. Continue EXACTLY from where you left off. Do NOT repeat any content. Do NOT add preamble.",
			})
			return s.runtime.transitionTo(StateGenerate, graphRouteGenerate), nil
		}
		slog.Info("[max-output] exceeded 3 continuations, stopping")
		s.runtime.emitText("\n\n[Output continuation limit reached (3/3)]\n")
	}
	if resp.StopReason != "length" {
		s.runtime.resetOutputContinuations()
	}

	s.runtime.appendMessage(llm.Message{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
		Reasoning: resp.Reasoning,
	})

	verdict := s.runtime.guardCheck(resp.Content, len(resp.ToolCalls), rs.stepCount())
	switch verdict.Action {
	case "terminate":
		s.runtime.emitText("\n\n[" + verdict.Message + "]")
		return s.runtime.transitionTo(StateDone, graphRouteDone), nil
	case "warn":
		if verdict.Message != "" {
			if sink, ok := s.runtime.(ephemeralSink); ok {
				sink.InjectEphemeral(verdict.Message)
			}
		}
	}

	if len(resp.ToolCalls) == 0 {
		if resp.Content != "" {
			s.runtime.recordFinalResponse(resp.Content)
		}
		if s.runtime.selfReviewEnabled() && resp.Content != "" {
			return graphruntime.NodeResult{
				RouteKey:         graphRouteReflect,
				ObservedState:    "reflect",
				OutputSchemaName: graphReflectionSchema,
			}, nil
		}
		return s.runtime.transitionTo(StateDone, graphRouteDone), nil
	}
	return s.runtime.transitionTo(StateToolExec, graphRouteToolExec), nil
}

type toolExecRuntime interface {
	lastHistoryMessage() llm.Message
	execToolsConcurrent(context.Context, []llm.ToolCall)
	execToolsSerial(context.Context, []llm.ToolCall) bool
	consumeYieldRequested() bool
	shouldRouteSpawn([]llm.ToolCall) bool
	transitionTo(State, string) graphruntime.NodeResult
}

type toolExecNodeService struct {
	runtime toolExecRuntime
}

func (s toolExecNodeService) Execute(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	lastMsg := s.runtime.lastHistoryMessage()
	if len(lastMsg.ToolCalls) > 1 {
		readOnly, write := splitToolCalls(lastMsg.ToolCalls)
		if len(readOnly) > 0 && len(write) == 0 {
			s.runtime.execToolsConcurrent(ctx, lastMsg.ToolCalls)
		} else if len(readOnly) > 1 && len(write) > 0 {
			s.runtime.execToolsConcurrent(ctx, readOnly)
			if s.runtime.execToolsSerial(ctx, write) {
				return s.runtime.transitionTo(StateDone, graphRouteDone), nil
			}
		} else if s.runtime.execToolsSerial(ctx, lastMsg.ToolCalls) {
			return s.runtime.transitionTo(StateDone, graphRouteDone), nil
		}
	} else if s.runtime.execToolsSerial(ctx, lastMsg.ToolCalls) {
		return s.runtime.transitionTo(StateDone, graphRouteDone), nil
	}

	if s.runtime.consumeYieldRequested() {
		if s.runtime.shouldRouteSpawn(lastMsg.ToolCalls) {
			return graphruntime.NodeResult{
				RouteKey:      graphRouteSpawn,
				ObservedState: "spawn",
			}, nil
		}
		return s.runtime.transitionTo(StateDone, graphRouteDone), nil
	}

	return s.runtime.transitionTo(StateGuardCheck, graphRouteGuardCheck), nil
}

type guardCheckRuntime interface {
	estimateTokens() int
	currentModel() string
	forceTruncate(int)
	InjectEphemeral(string)
	transitionTo(State, string) graphruntime.NodeResult
}

type guardCheckNodeService struct {
	runtime guardCheckRuntime
}

func (s guardCheckNodeService) Execute(rs *runState) (graphruntime.NodeResult, error) {
	rs.incStep()

	tokenEstimate := s.runtime.estimateTokens()
	policy := llm.GetPolicy(s.runtime.currentModel())
	usage := float64(tokenEstimate) / float64(policy.ContextWindow)

	if usage > 0.95 {
		s.runtime.forceTruncate(8)
		s.runtime.InjectEphemeral(prompttext.EphCompactionNotice)
		return s.runtime.transitionTo(StateGenerate, graphRouteGenerate), nil
	} else if usage > 0.70 {
		return s.runtime.transitionTo(StateCompact, graphRouteCompact), nil
	}
	return s.runtime.transitionTo(StateGenerate, graphRouteGenerate), nil
}

type compactRuntime interface {
	toolHeavyCompact() bool
	estimateTokens() int
	currentModel() string
	doCompact(context.Context)
	persistFullHistory()
	InjectEphemeral(string)
	transitionTo(State, string) graphruntime.NodeResult
}

type compactNodeService struct {
	runtime compactRuntime
}

func (s compactNodeService) Execute(ctx context.Context) (graphruntime.NodeResult, error) {
	if s.runtime.toolHeavyCompact() {
		newEst := s.runtime.estimateTokens()
		newPolicy := llm.GetPolicy(s.runtime.currentModel())
		newUsage := float64(newEst) / float64(newPolicy.ContextWindow)
		if newUsage <= 0.70 {
			s.runtime.InjectEphemeral(prompttext.EphCompactionNotice)
			return s.runtime.transitionTo(StateGenerate, graphRouteGenerate), nil
		}
	}
	s.runtime.doCompact(ctx)
	s.runtime.InjectEphemeral(prompttext.EphCompactionNotice)
	s.runtime.persistFullHistory()
	return s.runtime.transitionTo(StateGenerate, graphRouteGenerate), nil
}

type evaluationNodeService struct {
	runtime evaluationRuntime
}

func (s evaluationNodeService) Execute() (graphruntime.NodeResult, error) {
	evalCtx, cancel := s.runtime.evaluationContextTimeout()
	defer cancel()

	previous := s.runtime.intelligenceSnapshot().Evaluation
	shouldRepair, err := s.runtime.evaluateCurrentRun(evalCtx, previous)
	if err != nil {
		s.runtime.logInlineEvaluationFailure(err)
		return graphruntime.NodeResult{
			RouteKey:      graphRouteComplete,
			ObservedState: "evaluate",
		}, nil
	}
	if shouldRepair {
		return graphruntime.NodeResult{
			RouteKey:         graphRouteRepair,
			ObservedState:    "evaluate",
			OutputSchemaName: graphEvaluationSchema,
		}, nil
	}
	return graphruntime.NodeResult{
		RouteKey:         graphRouteComplete,
		ObservedState:    "evaluate",
		OutputSchemaName: graphEvaluationSchema,
	}, nil
}

type repairRuntime interface {
	repairSnapshot() graphruntime.RepairState
	setRepairDecision(graphruntime.RepairState)
	recordRepair(graphruntime.RepairState)
	emitRepairWake()
	InjectEphemeral(string)
	appendEmptyUserMessage()
	transitionTo(State, string) graphruntime.NodeResult
}

type repairNodeService struct {
	runtime repairRuntime
}

func (s repairNodeService) Execute(rs *runState) (graphruntime.NodeResult, error) {
	repair := s.runtime.repairSnapshot()
	if !repair.Allowed || repair.Strategy == "" {
		return graphruntime.NodeResult{
			RouteKey:      graphRouteComplete,
			ObservedState: "repair",
		}, nil
	}

	repair.Attempted = true
	s.runtime.setRepairDecision(repair)
	s.runtime.recordRepair(repair)
	s.runtime.emitRepairWake()
	if repair.Ephemeral != "" {
		s.runtime.InjectEphemeral(repair.Ephemeral)
	}
	rs.setStepCount(0)
	rs.setRetryCount(0)
	s.runtime.appendEmptyUserMessage()
	return graphruntime.NodeResult{
		RouteKey:      graphRoutePrepare,
		ObservedState: "repair",
	}, nil
}

type doneRuntime interface {
	snapshotPendingFileEdits(step int)
	persistHistory()
	hasPendingWake() bool
	shouldRunEvoEvaluation() bool
	transitionTo(State, string) graphruntime.NodeResult
}

type doneNodeService struct {
	runtime doneRuntime
}

func (s doneNodeService) Execute(_ context.Context, rs *runState) (graphruntime.NodeResult, error) {
	s.runtime.snapshotPendingFileEdits(rs.stepCount())
	s.runtime.persistHistory()

	if s.runtime.hasPendingWake() {
		slog.Info(fmt.Sprintf("[loop] pendingWake detected, routing through merge node"))
		return graphruntime.NodeResult{
			RouteKey:      graphRouteMerge,
			ObservedState: "merge",
		}, nil
	}

	if s.runtime.shouldRunEvoEvaluation() {
		return graphruntime.NodeResult{
			RouteKey:      graphRouteEvaluate,
			ObservedState: "evaluate",
		}, nil
	}
	return s.runtime.transitionTo(StateIdle, graphRouteComplete), nil
}

type completeRuntime interface {
	emitComplete()
	fireHooks(context.Context, int)
	markDreamIdle()
	finishWith(State, graphruntime.NodeStatus) graphruntime.NodeResult
}

type completeNodeService struct {
	runtime completeRuntime
}

func (s completeNodeService) Execute(ctx context.Context, rs *runState) (graphruntime.NodeResult, error) {
	s.runtime.emitComplete()
	go s.runtime.fireHooks(ctx, rs.stepCount())
	s.runtime.markDreamIdle()
	return s.runtime.finishWith(StateIdle, graphruntime.NodeStatusComplete), nil
}
