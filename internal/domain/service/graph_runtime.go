package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

const (
	graphRouteOK          = "ok"
	graphRouteOrchestrate = "orchestrate"
	graphRouteSpawn       = "spawn"
	graphRouteBarrierWait = "barrier_wait"
	graphRouteMerge       = "merge"
	graphRoutePrepare     = "prepare"
	graphRoutePlan        = "plan"
	graphRoutePlanRevise  = "plan_revise"
	graphRouteGenerate    = "generate"
	graphRouteToolExec    = "tool_exec"
	graphRouteGuardCheck  = "guard_check"
	graphRouteReflect     = "reflect"
	graphRouteEvaluate    = "evaluate"
	graphRouteRepair      = "repair"
	graphRouteCompact     = "compact"
	graphRouteDone        = "done"
	graphRouteComplete    = "complete"
	graphReflectionSchema = "reflection.review.v1"
)

type graphLoopAdapter struct {
	loop *AgentLoop
	rs   *runState
}

func newGraphLoopAdapter(loop *AgentLoop) *graphLoopAdapter {
	return &graphLoopAdapter{
		loop: loop,
		rs:   &runState{opts: loop.options},
	}
}

func NewAgentLoopGraph(loop *AgentLoop) graphruntime.GraphDefinition {
	adapter := newGraphLoopAdapter(loop)

	return graphruntime.GraphDefinition{
		ID:        "agent_loop",
		Version:   "v1alpha1",
		EntryNode: "prepare",
		Nodes: map[string]graphruntime.Node{
			"prepare":      prepareNode{adapter: adapter},
			"orchestrate":  orchestrateNode{adapter: adapter},
			"spawn":        spawnNode{adapter: adapter},
			"barrier_wait": barrierWaitNode{adapter: adapter},
			"merge":        mergeNode{adapter: adapter},
			"plan":         planningNode{adapter: adapter},
			"generate":     generateNode{adapter: adapter},
			"tool_exec":    toolExecNode{adapter: adapter},
			"guard_check":  guardCheckNode{adapter: adapter},
			"reflect":      reflectionNode{adapter: adapter},
			"evaluate":     evaluateNode{adapter: adapter},
			"repair":       repairNode{adapter: adapter},
			"compact":      compactNode{adapter: adapter},
			"done":         doneNode{adapter: adapter},
			"complete":     completeNode{adapter: adapter},
		},
		Edges: []graphruntime.Edge{
			{From: "prepare", To: "orchestrate", Condition: graphRouteOrchestrate, Priority: 1},
			{From: "orchestrate", To: "plan", Condition: graphRoutePlan, Priority: 1},
			{From: "orchestrate", To: "spawn", Condition: graphRouteSpawn, Priority: 1},
			{From: "orchestrate", To: "barrier_wait", Condition: graphRouteBarrierWait, Priority: 1},
			{From: "orchestrate", To: "merge", Condition: graphRouteMerge, Priority: 1},
			{From: "orchestrate", To: "generate", Condition: graphRouteOK, Priority: 1},
			{From: "spawn", To: "barrier_wait", Condition: graphRouteBarrierWait, Priority: 1},
			{From: "barrier_wait", To: "merge", Condition: graphRouteMerge, Priority: 1},
			{From: "barrier_wait", To: "generate", Condition: graphRouteGenerate, Priority: 1},
			{From: "merge", To: "evaluate", Condition: graphRouteEvaluate, Priority: 1},
			{From: "merge", To: "complete", Condition: graphRouteComplete, Priority: 1},
			{From: "plan", To: "prepare", Condition: graphRoutePlanRevise, Priority: 1},
			{From: "plan", To: "generate", Condition: graphRouteGenerate, Priority: 1},
			{From: "generate", To: "generate", Condition: graphRouteGenerate, Priority: 1},
			{From: "generate", To: "tool_exec", Condition: graphRouteToolExec, Priority: 1},
			{From: "generate", To: "reflect", Condition: graphRouteReflect, Priority: 1},
			{From: "generate", To: "compact", Condition: graphRouteCompact, Priority: 1},
			{From: "generate", To: "done", Condition: graphRouteDone, Priority: 1},
			{From: "tool_exec", To: "spawn", Condition: graphRouteSpawn, Priority: 1},
			{From: "tool_exec", To: "guard_check", Condition: graphRouteGuardCheck, Priority: 1},
			{From: "tool_exec", To: "done", Condition: graphRouteDone, Priority: 1},
			{From: "guard_check", To: "generate", Condition: graphRouteGenerate, Priority: 1},
			{From: "guard_check", To: "compact", Condition: graphRouteCompact, Priority: 1},
			{From: "reflect", To: "generate", Condition: graphRouteGenerate, Priority: 1},
			{From: "reflect", To: "done", Condition: graphRouteDone, Priority: 1},
			{From: "evaluate", To: "repair", Condition: graphRouteRepair, Priority: 1},
			{From: "evaluate", To: "complete", Condition: graphRouteComplete, Priority: 1},
			{From: "repair", To: "prepare", Condition: graphRoutePrepare, Priority: 1},
			{From: "compact", To: "generate", Condition: graphRouteGenerate, Priority: 1},
			{From: "done", To: "merge", Condition: graphRouteMerge, Priority: 1},
			{From: "done", To: "prepare", Condition: graphRoutePrepare, Priority: 1},
			{From: "done", To: "evaluate", Condition: graphRouteEvaluate, Priority: 1},
			{From: "done", To: "complete", Condition: graphRouteComplete, Priority: 1},
		},
	}
}

func NewAgentLoopRuntime(loop *AgentLoop, store graphruntime.SnapshotStore) (*graphruntime.Runtime, error) {
	return graphruntime.NewRuntime(NewAgentLoopGraph(loop), store)
}

func (a *graphLoopAdapter) executeNode(
	state *graphruntime.TurnState,
	exec *graphruntime.ExecutionState,
	fn func() (graphruntime.NodeResult, error),
) (graphruntime.NodeResult, error) {
	a.syncFromGraphState(state, exec)
	result, err := fn()
	// Keep TurnState snapshot-aligned even when the node returns an error.
	a.syncToGraphState(state, exec)
	if err != nil {
		return result, err
	}
	return result, nil
}

type prepareNode struct{ adapter *graphLoopAdapter }

func (n prepareNode) Name() string                { return "prepare" }
func (n prepareNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindPrepare }
func (n prepareNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		planning := n.adapter.loop.doPrepare(ctx)
		state.Intelligence.Planning = planning
		return graphruntime.NodeResult{
			RouteKey:      graphRouteOrchestrate,
			ObservedState: "prepare",
		}, nil
	})
}

type orchestrateNode struct{ adapter *graphLoopAdapter }

func (n orchestrateNode) Name() string                { return "orchestrate" }
func (n orchestrateNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindOrchestrate }
func (n orchestrateNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		if state.Orchestration.PendingMerge || (rt.Execution != nil && rt.Execution.PendingWake) {
			return graphruntime.NodeResult{RouteKey: graphRouteMerge, ObservedState: "orchestrate"}, nil
		}
		if state.Orchestration.ActiveBarrier != nil && state.Orchestration.ActiveBarrier.PendingCount > 0 {
			return graphruntime.NodeResult{RouteKey: graphRouteBarrierWait, ObservedState: "orchestrate"}, nil
		}
		if state.Intelligence.Planning.Required {
			return graphruntime.NodeResult{RouteKey: graphRoutePlan, ObservedState: "plan"}, nil
		}
		return graphruntime.NodeResult{RouteKey: graphRouteOK, ObservedState: "orchestrate"}, nil
	})
}

type spawnNode struct{ adapter *graphLoopAdapter }

func (n spawnNode) Name() string                { return "spawn" }
func (n spawnNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindSpawn }
func (n spawnNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return graphruntime.NodeResult{RouteKey: graphRouteBarrierWait, ObservedState: "spawn"}, nil
	})
}

type barrierWaitNode struct{ adapter *graphLoopAdapter }

func (n barrierWaitNode) Name() string                { return "barrier_wait" }
func (n barrierWaitNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindBarrierWait }
func (n barrierWaitNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		if state.Orchestration.ActiveBarrier != nil && state.Orchestration.ActiveBarrier.PendingCount > 0 {
			return graphruntime.NodeResult{
				Status:          graphruntime.NodeStatusWait,
				WaitReason:      graphruntime.WaitReasonBarrier,
				RouteKey:        graphRouteMerge,
				ObservedState:   "barrier_wait",
				NeedsCheckpoint: true,
			}, nil
		}
		return graphruntime.NodeResult{RouteKey: graphRouteGenerate, ObservedState: "barrier_wait"}, nil
	})
}

type mergeNode struct{ adapter *graphLoopAdapter }

func (n mergeNode) Name() string                { return "merge" }
func (n mergeNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindMerge }
func (n mergeNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		if n.adapter.loop.pendingWake.Load() || (state != nil && state.Orchestration.PendingMerge) {
			n.adapter.loop.pendingWake.Store(false)
			if n.adapter.loop.deps.Delta != nil {
				n.adapter.loop.deps.Delta.OnAutoWakeStart()
			}
			n.adapter.rs.setStepCount(0)
			n.adapter.rs.setRetryCount(0)
			func() {
				n.adapter.loop.mu.Lock()
				defer n.adapter.loop.mu.Unlock()
				n.adapter.loop.history = append(n.adapter.loop.history, n.adapter.loop.buildUserMessage(""))
			}()
			if state != nil {
				state.Orchestration.PendingMerge = false
				state.Orchestration.LastWakeSource = "barrier"
				if state.Orchestration.ActiveBarrier != nil && state.Orchestration.ActiveBarrier.Finalized {
					state.Orchestration.ActiveBarrier.PendingCount = 0
				}
				n.adapter.loop.setOrchestrationState(state.Orchestration)
			}
			return graphruntime.NodeResult{RouteKey: graphRoutePrepare, ObservedState: "merge"}, nil
		}
		if n.adapter.loop.Mode().EvoEnabled {
			return graphruntime.NodeResult{RouteKey: graphRouteEvaluate, ObservedState: "merge"}, nil
		}
		return graphruntime.NodeResult{RouteKey: graphRouteComplete, ObservedState: "merge"}, nil
	})
}

type planningNode struct{ adapter *graphLoopAdapter }

func (n planningNode) Name() string                { return "plan" }
func (n planningNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindPlan }
func (n planningNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		planning := state.Intelligence.Planning
		switch planning.ReviewDecision {
		case "approved":
			planning.ReviewRequired = false
			state.Intelligence.Planning = planning
			n.adapter.loop.setPlanningDecision(planning)
			return graphruntime.NodeResult{
				RouteKey:      graphRouteGenerate,
				ObservedState: "plan",
			}, nil
		case "revise":
			planning.ReviewRequired = false
			state.Intelligence.Planning = planning
			n.adapter.loop.setPlanningDecision(planning)
			message := "Plan review requested revision before execution."
			if feedback := strings.TrimSpace(planning.ReviewFeedback); feedback != "" {
				message = "Plan review requested revision: " + feedback
			}
			n.adapter.loop.InjectEphemeral(message)
			return graphruntime.NodeResult{
				RouteKey:      graphRoutePlanRevise,
				ObservedState: "plan",
			}, nil
		}
		if planning.Required && planning.ReviewRequired {
			message := "Planning review required before execution"
			if planning.Trigger != "" {
				message = "Planning trigger: " + planning.Trigger
			}
			if n.adapter.loop.deps.Delta != nil {
				n.adapter.loop.deps.Delta.OnPlanReview(message, append([]string(nil), planning.MissingArtifacts...))
			}
			return graphruntime.NodeResult{
				Status:          graphruntime.NodeStatusWait,
				WaitReason:      graphruntime.WaitReasonUserInput,
				RouteKey:        graphRoutePlan,
				ObservedState:   "plan",
				NeedsCheckpoint: true,
			}, nil
		}
		return graphruntime.NodeResult{
			RouteKey:      graphRouteGenerate,
			ObservedState: "plan",
		}, nil
	})
}

type generateNode struct{ adapter *graphLoopAdapter }

func (n generateNode) Name() string                { return "generate" }
func (n generateNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindGenerate }
func (n generateNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return n.adapter.loop.handleGenerate(ctx, n.adapter.rs)
	})
}

type toolExecNode struct{ adapter *graphLoopAdapter }

func (n toolExecNode) Name() string                { return "tool_exec" }
func (n toolExecNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindToolExec }
func (n toolExecNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return n.adapter.loop.handleToolExec(ctx, n.adapter.rs)
	})
}

type guardCheckNode struct{ adapter *graphLoopAdapter }

func (n guardCheckNode) Name() string                { return "guard_check" }
func (n guardCheckNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindGuard }
func (n guardCheckNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return n.adapter.loop.handleGuardCheck(n.adapter.rs)
	})
}

type compactNode struct{ adapter *graphLoopAdapter }

func (n compactNode) Name() string                { return "compact" }
func (n compactNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindCompact }
func (n compactNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return n.adapter.loop.handleCompact(ctx)
	})
}

type evaluateNode struct{ adapter *graphLoopAdapter }

func (n evaluateNode) Name() string                { return "evaluate" }
func (n evaluateNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindEvaluate }
func (n evaluateNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return n.adapter.loop.handleEvaluate(ctx, n.adapter.rs)
	})
}

type repairNode struct{ adapter *graphLoopAdapter }

func (n repairNode) Name() string                { return "repair" }
func (n repairNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindRepair }
func (n repairNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return n.adapter.loop.handleRepair(ctx, n.adapter.rs)
	})
}

type reflectionNode struct{ adapter *graphLoopAdapter }

func (n reflectionNode) Name() string                { return "reflect" }
func (n reflectionNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindReflect }
func (n reflectionNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		review := resolveReflectionReview(state)
		n.adapter.loop.setReviewDecision(graphruntime.ReviewDecisionState{
			SchemaName: graphReflectionSchema,
			Decision:   review.Decision,
			Reason:     review.Reason,
			RawJSON:    review.RawJSON,
			Valid:      review.RawJSON != "",
		})
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
		if review.Decision == "revise" {
			if review.Reason != "" {
				n.adapter.loop.InjectEphemeral("Self-review requested revision: " + review.Reason)
			}
			return graphruntime.NodeResult{
				RouteKey:         graphRouteGenerate,
				ObservedState:    "reflect",
				OutputSchemaName: graphReflectionSchema,
			}, nil
		}
		return graphruntime.NodeResult{
			RouteKey:         graphRouteDone,
			ObservedState:    "reflect",
			OutputSchemaName: graphReflectionSchema,
		}, nil
	})
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

type doneNode struct{ adapter *graphLoopAdapter }

func (n doneNode) Name() string                { return "done" }
func (n doneNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindDone }
func (n doneNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return n.adapter.loop.handleDone(ctx, n.adapter.rs)
	})
}

type completeNode struct{ adapter *graphLoopAdapter }

func (n completeNode) Name() string                { return "complete" }
func (n completeNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindComplete }
func (n completeNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return n.adapter.loop.handleComplete(ctx, n.adapter.rs)
	})
}
