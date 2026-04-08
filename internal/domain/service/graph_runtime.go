package service

import (
	"context"

	"github.com/ngoclaw/ngoagent/internal/domain/graphruntime"
)

const (
	graphRouteOK         = "ok"
	graphRoutePrepare    = "prepare"
	graphRouteGenerate   = "generate"
	graphRouteToolExec   = "tool_exec"
	graphRouteGuardCheck = "guard_check"
	graphRouteCompact    = "compact"
	graphRouteDone       = "done"
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
			"prepare":     prepareNode{adapter: adapter},
			"generate":    generateNode{adapter: adapter},
			"tool_exec":   toolExecNode{adapter: adapter},
			"guard_check": guardCheckNode{adapter: adapter},
			"compact":     compactNode{adapter: adapter},
			"done":        doneNode{adapter: adapter},
		},
		Edges: []graphruntime.Edge{
			{From: "prepare", To: "generate", Condition: graphRouteOK, Priority: 1},
			{From: "generate", To: "generate", Condition: graphRouteGenerate, Priority: 1},
			{From: "generate", To: "tool_exec", Condition: graphRouteToolExec, Priority: 1},
			{From: "generate", To: "compact", Condition: graphRouteCompact, Priority: 1},
			{From: "generate", To: "done", Condition: graphRouteDone, Priority: 1},
			{From: "tool_exec", To: "guard_check", Condition: graphRouteGuardCheck, Priority: 1},
			{From: "tool_exec", To: "done", Condition: graphRouteDone, Priority: 1},
			{From: "guard_check", To: "generate", Condition: graphRouteGenerate, Priority: 1},
			{From: "guard_check", To: "compact", Condition: graphRouteCompact, Priority: 1},
			{From: "compact", To: "generate", Condition: graphRouteGenerate, Priority: 1},
			{From: "done", To: "prepare", Condition: graphRoutePrepare, Priority: 1},
		},
	}
}

func NewAgentLoopRuntime(loop *AgentLoop, store graphruntime.SnapshotStore) (*graphruntime.Runtime, error) {
	return graphruntime.NewRuntime(NewAgentLoopGraph(loop), store)
}

type prepareNode struct{ adapter *graphLoopAdapter }

func (n prepareNode) Name() string                { return "prepare" }
func (n prepareNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindPrepare }
func (n prepareNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	n.adapter.syncFromGraphState(state, rt.Execution)
	n.adapter.loop.doPrepare(ctx)
	n.adapter.loop.setPhase(StateGenerate)
	n.adapter.syncToGraphState(state, rt.Execution)
	return graphruntime.NodeResult{RouteKey: graphRouteOK}, nil
}

type generateNode struct{ adapter *graphLoopAdapter }

func (n generateNode) Name() string                { return "generate" }
func (n generateNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindGenerate }
func (n generateNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	n.adapter.syncFromGraphState(state, rt.Execution)
	result, err := n.adapter.loop.handleGenerate(ctx, n.adapter.rs)
	if err != nil {
		return result, err
	}
	n.adapter.syncToGraphState(state, rt.Execution)
	return result, nil
}

type toolExecNode struct{ adapter *graphLoopAdapter }

func (n toolExecNode) Name() string                { return "tool_exec" }
func (n toolExecNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindToolExec }
func (n toolExecNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	n.adapter.syncFromGraphState(state, rt.Execution)
	result, err := n.adapter.loop.handleToolExec(ctx, n.adapter.rs)
	if err != nil {
		return result, err
	}
	n.adapter.syncToGraphState(state, rt.Execution)
	return result, nil
}

type guardCheckNode struct{ adapter *graphLoopAdapter }

func (n guardCheckNode) Name() string                { return "guard_check" }
func (n guardCheckNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindGuard }
func (n guardCheckNode) Execute(_ context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	n.adapter.syncFromGraphState(state, rt.Execution)
	result, err := n.adapter.loop.handleGuardCheck(n.adapter.rs)
	if err != nil {
		return result, err
	}
	n.adapter.syncToGraphState(state, rt.Execution)
	return result, nil
}

type compactNode struct{ adapter *graphLoopAdapter }

func (n compactNode) Name() string                { return "compact" }
func (n compactNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindCompact }
func (n compactNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	n.adapter.syncFromGraphState(state, rt.Execution)
	result, err := n.adapter.loop.handleCompact(ctx)
	if err != nil {
		return result, err
	}
	n.adapter.syncToGraphState(state, rt.Execution)
	return result, nil
}

type doneNode struct{ adapter *graphLoopAdapter }

func (n doneNode) Name() string                { return "done" }
func (n doneNode) Kind() graphruntime.NodeKind { return graphruntime.NodeKindDone }
func (n doneNode) Execute(ctx context.Context, rt *graphruntime.RuntimeContext, state *graphruntime.TurnState) (graphruntime.NodeResult, error) {
	n.adapter.syncFromGraphState(state, rt.Execution)
	result, err := n.adapter.loop.handleDone(ctx, n.adapter.rs)
	if err != nil {
		return result, err
	}
	n.adapter.syncToGraphState(state, rt.Execution)
	return result, nil
}
