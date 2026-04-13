package service

import (
	"context"

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
	livePendingWake := a.loop.pendingWake.Load()
	a.syncFromGraphState(state, exec)
	sideEffects := newGraphNodeRuntime(a.loop, state, exec).captureLoopSideEffectBaseline()
	if livePendingWake && exec != nil && !exec.PendingWake {
		a.loop.pendingWake.Store(true)
	}
	result, err := fn()
	newGraphNodeRuntime(a.loop, state, exec).syncLoopSideEffectsSince(sideEffects)
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
		return prepareNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(ctx, state)
	})
}

type orchestrateNode struct{ adapter *graphLoopAdapter }

func (n orchestrateNode) Name() string                { return "orchestrate" }
func (n orchestrateNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindOrchestrate }
func (n orchestrateNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return orchestrateNodeService{}.Execute(rt, state)
	})
}

type spawnNode struct{ adapter *graphLoopAdapter }

func (n spawnNode) Name() string                { return "spawn" }
func (n spawnNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindSpawn }
func (n spawnNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return spawnNodeService{}.Execute()
	})
}

type barrierWaitNode struct{ adapter *graphLoopAdapter }

func (n barrierWaitNode) Name() string                { return "barrier_wait" }
func (n barrierWaitNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindBarrierWait }
func (n barrierWaitNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return barrierWaitNodeService{}.Execute(state)
	})
}

type mergeNode struct{ adapter *graphLoopAdapter }

func (n mergeNode) Name() string                { return "merge" }
func (n mergeNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindMerge }
func (n mergeNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return mergeNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(state, n.adapter.rs)
	})
}

type planningNode struct{ adapter *graphLoopAdapter }

func (n planningNode) Name() string                { return "plan" }
func (n planningNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindPlan }
func (n planningNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		runtime := newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)
		return planningNodeService{
			ephemeral: runtime,
			decision:  runtime,
			review:    n.adapter.loop.planReviewEmitter(),
		}.Execute(state), nil
	})
}

type generateNode struct{ adapter *graphLoopAdapter }

func (n generateNode) Name() string                { return "generate" }
func (n generateNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindGenerate }
func (n generateNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return generateNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(ctx, n.adapter.rs)
	})
}

type toolExecNode struct{ adapter *graphLoopAdapter }

func (n toolExecNode) Name() string                { return "tool_exec" }
func (n toolExecNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindToolExec }
func (n toolExecNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return toolExecNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(ctx, n.adapter.rs)
	})
}

type guardCheckNode struct{ adapter *graphLoopAdapter }

func (n guardCheckNode) Name() string                { return "guard_check" }
func (n guardCheckNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindGuard }
func (n guardCheckNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return guardCheckNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(n.adapter.rs)
	})
}

type compactNode struct{ adapter *graphLoopAdapter }

func (n compactNode) Name() string                { return "compact" }
func (n compactNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindCompact }
func (n compactNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return compactNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(ctx)
	})
}

type evaluateNode struct{ adapter *graphLoopAdapter }

func (n evaluateNode) Name() string                { return "evaluate" }
func (n evaluateNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindEvaluate }
func (n evaluateNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return evaluationNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute()
	})
}

type repairNode struct{ adapter *graphLoopAdapter }

func (n repairNode) Name() string                { return "repair" }
func (n repairNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindRepair }
func (n repairNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return repairNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(n.adapter.rs)
	})
}

type reflectionNode struct{ adapter *graphLoopAdapter }

func (n reflectionNode) Name() string                { return "reflect" }
func (n reflectionNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindReflect }
func (n reflectionNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return reflectionNodeService{ephemeral: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(state), nil
	})
}

type doneNode struct{ adapter *graphLoopAdapter }

func (n doneNode) Name() string                { return "done" }
func (n doneNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindDone }
func (n doneNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return doneNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(ctx, n.adapter.rs)
	})
}

type completeNode struct{ adapter *graphLoopAdapter }

func (n completeNode) Name() string                { return "complete" }
func (n completeNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindComplete }
func (n completeNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	return n.adapter.executeNode(state, rt.Execution, func() (graphruntime.NodeResult, error) {
		return completeNodeService{runtime: newGraphNodeRuntime(n.adapter.loop, state, rt.Execution)}.Execute(ctx, n.adapter.rs)
	})
}
