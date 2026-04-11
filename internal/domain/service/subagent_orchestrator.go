package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ngoclaw/ngoagent/internal/domain/model"
	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	"github.com/ngoclaw/ngoagent/pkg/ctxutil"
)

type AgentDefinitionResolver interface {
	Resolve(agentType string) (*model.AgentDefinition, error)
}

type LoopResolver func(sessionID string) *AgentLoop

type TranscriptSaverFunc func(sessionID, taskName, runID, status, output string) error

type SubagentOrchestrator struct {
	factory         *LoopFactory
	defaultLoop     *AgentLoop
	resolveLoop     LoopResolver
	resolveAgent    AgentDefinitionResolver
	eventPusher     func(sessionID, eventType string, data any)
	transcriptSaver TranscriptSaverFunc
	maxSubagents    int

	mu       sync.Mutex
	barriers map[string]*SubagentBarrier
}

func subagentBarrierKey(sessionID, parentRunID string) string {
	if parentRunID != "" {
		return sessionID + ":" + parentRunID
	}
	return sessionID
}

func NewSubagentOrchestrator(factory *LoopFactory, defaultLoop *AgentLoop, resolveLoop LoopResolver, resolveAgent AgentDefinitionResolver) *SubagentOrchestrator {
	return &SubagentOrchestrator{
		factory:      factory,
		defaultLoop:  defaultLoop,
		resolveLoop:  resolveLoop,
		resolveAgent: resolveAgent,
		barriers:     make(map[string]*SubagentBarrier),
	}
}

func (o *SubagentOrchestrator) SetEventPusher(fn func(sessionID, eventType string, data any)) {
	o.eventPusher = fn
}

func (o *SubagentOrchestrator) SetTranscriptSaver(fn TranscriptSaverFunc) {
	o.transcriptSaver = fn
}

func (o *SubagentOrchestrator) SetMaxSubagents(n int) {
	o.maxSubagents = n
}

func (o *SubagentOrchestrator) Spawn(ctx context.Context, fallbackSessionID, task, taskName, agentType string) (string, error) {
	if o.factory == nil {
		return "", fmt.Errorf("subagent orchestrator missing factory")
	}
	if o.resolveAgent == nil {
		return "", fmt.Errorf("subagent orchestrator missing agent resolver")
	}

	agentDef, err := o.resolveAgent.Resolve(agentType)
	if err != nil {
		return "", fmt.Errorf("agent type %q not found: %w", agentType, err)
	}

	runtimeSID := ctxutil.SessionIDFromContext(ctx)
	if runtimeSID == "" {
		runtimeSID = fallbackSessionID
	}
	parentRunID := ctxutil.RunIDFromContext(ctx)
	parentLoop := o.resolveParentLoop(runtimeSID)
	if parentLoop != nil {
		task = BuildSubagentContext(task, parentLoop.History(), agentDef)
	}

	if taskName == "" {
		taskName = "sub-agent"
	}

	barrier := o.getOrCreateBarrier(runtimeSID, parentRunID, parentLoop, agentDef)
	ch := NewSubagentChannel(func(runID, result string, err error) {
		barrier.OnComplete(runID, result, err)
	})
	run := o.factory.Create(runtimeSID, ch, agentDef)
	run.Loop.BindParentRun(parentRunID)
	run.Loop.InjectEphemeral(prompttext.EphSubAgentContext)
	if agentDef != nil && agentDef.Model != "" {
		run.Loop.SetModel(agentDef.Model)
	}
	if parentLoop != nil {
		parentLoop.InjectEphemeral(prompttext.EphCoordinatorMode)
	}

	if err := barrier.Add(run.ID, taskName); err != nil {
		return "", fmt.Errorf("cannot spawn sub-agent: %v", err)
	}

	runCtx := ctxutil.WithSessionID(context.Background(), runtimeSID)
	runID := o.factory.RunAsync(runCtx, run, task)
	if parentLoop != nil {
		parentLoop.RegisterSpawnedChild(parentRunID, runID, taskName, agentType)
	}
	if o.eventPusher != nil {
		capturedSID := runtimeSID
		capturedRunID := runID
		capturedName := taskName
		ch.Collector().StepPush = func(toolName string) {
			o.eventPusher(capturedSID, "subagent_progress", map[string]any{
				"type":         "subagent_progress",
				"run_id":       capturedRunID,
				"task_name":    capturedName,
				"status":       "running",
				"done":         0,
				"total":        0,
				"current_step": toolName,
			})
		}
	}

	return runID, nil
}

func (o *SubagentOrchestrator) resolveParentLoop(sessionID string) *AgentLoop {
	if sessionID != "" && o.resolveLoop != nil {
		if loop := o.resolveLoop(sessionID); loop != nil {
			return loop
		}
	}
	return o.defaultLoop
}

func (o *SubagentOrchestrator) getOrCreateBarrier(sessionID, parentRunID string, parentLoop *AgentLoop, agentDef *model.AgentDefinition) *SubagentBarrier {
	o.mu.Lock()
	defer o.mu.Unlock()

	key := subagentBarrierKey(sessionID, parentRunID)
	if existing, ok := o.barriers[key]; ok && existing.Pending() > 0 {
		return existing
	}

	capturedLoop := parentLoop
	capturedSID := sessionID
	capturedKey := key
	barrier := NewSubagentBarrier(capturedLoop, func() {
		go func() {
			slog.Info(fmt.Sprintf("[barrier] Auto-waking parent loop for session %s", capturedSID))
			wakeLoop := o.resolveParentLoop(capturedSID)
			if wakeLoop == nil {
				return
			}
			if err := wakeLoop.Run(context.Background(), ""); err != nil {
				slog.Info(fmt.Sprintf("[barrier] Auto-wake failed for session %s: %v (pendingWake likely handled it)", capturedSID, err))
			} else if o.eventPusher != nil {
				o.eventPusher(capturedSID, "auto_wake_done", map[string]string{"type": "auto_wake_done"})
			}
		}()
		if capturedLoop != nil {
			capturedLoop.ClearActiveBarrier()
		}
		o.mu.Lock()
		delete(o.barriers, capturedKey)
		o.mu.Unlock()
	})
	if capturedLoop != nil {
		capturedLoop.SetActiveBarrier(barrier)
	}
	if o.eventPusher != nil {
		pusher := o.eventPusher
		barrier.SetProgressPush(func(runID, taskName, status string, done, total int, errMsg, output string) {
			pusher(capturedSID, "subagent_progress", map[string]any{
				"type":      "subagent_progress",
				"run_id":    runID,
				"task_name": taskName,
				"status":    status,
				"done":      done,
				"total":     total,
				"error":     errMsg,
				"output":    output,
			})
		})
	}
	if o.maxSubagents > 0 {
		barrier.SetMaxConcurrent(o.maxSubagents)
	}
	if agentDef != nil && agentDef.MaxTimeout > 0 {
		barrier.SetTimeout(agentDef.MaxTimeout)
	}
	if o.transcriptSaver != nil {
		barrier.SetTranscriptSaver(capturedSID, func(sid, name, rid, status, output string) {
			if err := o.transcriptSaver(sid, name, rid, status, output); err != nil {
				slog.Info(fmt.Sprintf("[barrier] Transcript save failed for %s: %v", rid, err))
			}
		})
	}
	o.barriers[key] = barrier
	return barrier
}
