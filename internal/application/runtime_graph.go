package application

import (
	"sort"

	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func buildRuntimeGraph(sessionID string, runs []apitype.RuntimeRunInfo, caps []apitype.CapabilityInfo) apitype.OrchestrationGraphInfo {
	graph := apitype.OrchestrationGraphInfo{
		SessionID:                 sessionID,
		Nodes:                     runs,
		Capabilities:              caps,
		RootRunIDs:                rootRunIDs(runs),
		UserTurnRootRunIDs:        userTurnRootRunIDs(runs),
		PendingRunIDs:             pendingRunIDs(runs),
		PendingDecisionRunIDs:     pendingDecisionRunIDs(runs),
		PendingRuntimeControlRuns: pendingRuntimeControlRunIDs(runs),
		IngressNodes:              runtimeIngressNodes(runs),
		EventNodes:                runtimeEventNodes(runs),
		Edges:                     runtimeEdges(runs),
	}
	graph.Summary = apitype.OrchestrationGraphSummary{
		RootRunIDs:                append([]string(nil), graph.RootRunIDs...),
		UserTurnRootRunIDs:        append([]string(nil), graph.UserTurnRootRunIDs...),
		PendingRunIDs:             append([]string(nil), graph.PendingRunIDs...),
		PendingDecisionRunIDs:     append([]string(nil), graph.PendingDecisionRunIDs...),
		PendingRuntimeControlRuns: append([]string(nil), graph.PendingRuntimeControlRuns...),
		RunCount:                  len(graph.Nodes),
		IngressNodeCount:          len(graph.IngressNodes),
		EventNodeCount:            len(graph.EventNodes),
		EdgeCount:                 len(graph.Edges),
	}
	return graph
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

func userTurnRootRunIDs(runs []apitype.RuntimeRunInfo) []string {
	roots := make([]string, 0, len(runs))
	for _, run := range runs {
		if run.RunID == "" || run.ParentRunID != "" || run.Ingress == nil {
			continue
		}
		if run.Ingress.Category != "user_turn" {
			continue
		}
		roots = append(roots, run.RunID)
	}
	sort.Strings(roots)
	return roots
}

func pendingRunIDs(runs []apitype.RuntimeRunInfo) []string {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		if run.RunID == "" {
			continue
		}
		if run.Status == "wait" || run.WaitReason != "" {
			ids = append(ids, run.RunID)
		}
	}
	sort.Strings(ids)
	return ids
}

func pendingDecisionRunIDs(runs []apitype.RuntimeRunInfo) []string {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		if run.RunID == "" || run.PendingDecision == nil {
			continue
		}
		ids = append(ids, run.RunID)
	}
	sort.Strings(ids)
	return ids
}

func pendingRuntimeControlRunIDs(runs []apitype.RuntimeRunInfo) []string {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		if run.RunID == "" || run.Ingress == nil {
			continue
		}
		if run.Ingress.Category != "runtime_control" {
			continue
		}
		if run.Status != "wait" && run.WaitReason == "" {
			continue
		}
		ids = append(ids, run.RunID)
	}
	sort.Strings(ids)
	return ids
}

func runtimeEdges(runs []apitype.RuntimeRunInfo) []apitype.RuntimeEdgeInfo {
	edges := make([]apitype.RuntimeEdgeInfo, 0, len(runs)*3)
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
		if run.Ingress != nil && run.RunID != "" && run.Ingress.Kind != "" {
			addEdge(apitype.RuntimeEdgeInfo{
				Kind:        "ingress",
				SourceRunID: ingressNodeID(*run.Ingress),
				TargetRunID: run.RunID,
				Summary:     ingressSummary(*run.Ingress),
			})
		}
		for _, event := range run.Events {
			eventID := runtimeEventNodeID(run.RunID, event)
			if eventID == "" {
				continue
			}
			addEdge(apitype.RuntimeEdgeInfo{
				Kind:        "event",
				SourceRunID: eventID,
				TargetRunID: run.RunID,
				BarrierID:   event.BarrierID,
				Summary:     event.Summary,
			})
			if event.SourceRun != "" {
				addEdge(apitype.RuntimeEdgeInfo{
					Kind:        "event_source",
					SourceRunID: event.SourceRun,
					TargetRunID: eventID,
					BarrierID:   event.BarrierID,
					Summary:     event.Type,
				})
			}
		}
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

func runtimeIngressNodes(runs []apitype.RuntimeRunInfo) []apitype.RuntimeIngressNodeInfo {
	nodes := make([]apitype.RuntimeIngressNodeInfo, 0, len(runs))
	seen := make(map[string]struct{}, len(runs))
	for _, run := range runs {
		if run.Ingress == nil || run.Ingress.Kind == "" {
			continue
		}
		id := ingressNodeID(*run.Ingress)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		nodes = append(nodes, apitype.RuntimeIngressNodeInfo{
			ID:           id,
			Category:     ingressCategory(*run.Ingress),
			Phase:        ingressPhase(*run.Ingress),
			Kind:         run.Ingress.Kind,
			Source:       run.Ingress.Source,
			Trigger:      run.Ingress.Trigger,
			RunID:        run.Ingress.RunID,
			DecisionKind: run.Ingress.DecisionKind,
			Decision:     run.Ingress.Decision,
			At:           run.Ingress.At,
		})
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

func runtimeEventNodes(runs []apitype.RuntimeRunInfo) []apitype.RuntimeEventNodeInfo {
	nodes := make([]apitype.RuntimeEventNodeInfo, 0, len(runs))
	seen := make(map[string]struct{}, len(runs)*2)
	for _, run := range runs {
		for _, event := range run.Events {
			id := runtimeEventNodeID(run.RunID, event)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			nodes = append(nodes, apitype.RuntimeEventNodeInfo{
				ID:           id,
				Type:         event.Type,
				Kind:         event.Kind,
				Source:       event.Source,
				Trigger:      event.Trigger,
				DecisionKind: event.DecisionKind,
				Decision:     event.Decision,
				RunID:        event.RunID,
				SourceRun:    event.SourceRun,
				BarrierID:    event.BarrierID,
				At:           event.At,
				Summary:      event.Summary,
				PayloadJSON:  event.PayloadJSON,
			})
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

func runtimeEventNodeID(ownerRunID string, event apitype.RuntimeEventInfo) string {
	baseRunID := event.RunID
	if baseRunID == "" {
		baseRunID = ownerRunID
	}
	if baseRunID == "" || event.Type == "" {
		return ""
	}
	id := "event:" + baseRunID + ":" + event.Type
	if event.At != "" {
		id += ":" + event.At
		return id
	}
	if event.Summary != "" {
		id += ":" + event.Summary
		return id
	}
	if event.BarrierID != "" {
		id += ":" + event.BarrierID
	}
	return id
}

func ingressNodeID(info apitype.RuntimeIngressInfo) string {
	id := "ingress:" + info.Kind
	if info.Source != "" {
		id += ":" + info.Source
	}
	if info.Trigger != "" {
		id += ":" + info.Trigger
	}
	if info.DecisionKind != "" {
		id += ":" + info.DecisionKind
	}
	return id
}

func ingressSummary(info apitype.RuntimeIngressInfo) string {
	summary := info.Trigger
	if summary == "" {
		summary = info.Source
	}
	if info.DecisionKind != "" {
		if summary != "" {
			summary += ":"
		}
		summary += info.DecisionKind
	}
	if info.Decision != "" {
		if summary != "" {
			summary += "="
		}
		summary += info.Decision
	}
	return summary
}

func ingressCategory(info apitype.RuntimeIngressInfo) string {
	switch info.Kind {
	case "message":
		return "user_turn"
	case "decision":
		return "decision_control"
	case "resume", "reconnect":
		return "runtime_control"
	default:
		if info.Kind == "" {
			return ""
		}
		return "runtime_event"
	}
}

func ingressPhase(info apitype.RuntimeIngressInfo) string {
	switch info.Kind {
	case "message":
		return "entry"
	case "decision":
		return "review"
	case "resume":
		return "resume"
	case "reconnect":
		return "recovery"
	default:
		return "entry"
	}
}
