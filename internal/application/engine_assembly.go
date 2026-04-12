package application

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/memory"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
)

type engineAssemblyInput struct {
	cfg             *config.Config
	sessionID       string
	brainDir        string
	router          *llm.Router
	promptEngine    *prompt.Engine
	registry        *tool.Registry
	secHook         *security.Hook
	sbMgr           *sandbox.Manager
	brainStore      *brain.ArtifactStore
	kiStore         *knowledge.Store
	kiRetriever     *knowledge.Retriever
	wsStore         *workspace.Store
	skillMgr        *skill.Manager
	historyStore    *persistence.HistoryStore
	fileHistory     *workspace.FileHistory
	memStore        *memory.Store
	diaryStore      *memory.DiaryStore
	snapshotStore   *persistence.RunSnapshotStore
	evoStore        *persistence.EvoStore
	transcriptStore *persistence.TranscriptStore
	repo            *persistence.Repository
	agentRegistry   *service.AgentRegistry
	spawnTool       *tool.SpawnAgentTool
	skillTool       *tool.SkillTool
}

type engineAssembly struct {
	baseDeps   service.Deps
	factory    *service.LoopFactory
	loop       *service.AgentLoop
	loopPool   *service.LoopPool
	sessionMgr *service.SessionManager
	chatEngine *service.ChatEngine
	modelMgr   *service.ModelManager
	toolAdmin  *service.ToolAdmin
}

func assembleEngine(in engineAssemblyInput) engineAssembly {
	kiDistiller := llm.NewKnowledgeDistiller(in.router)
	var dedupChecker service.KIDuplicateChecker
	if in.kiRetriever != nil {
		dedupChecker = in.kiRetriever
	}
	hookChain := service.NewHookManager()
	hookChain.Add(service.NewKIDistillHook(func() service.KIStore { return in.kiStore }, kiDistiller, in.cfg.Embedding.SimilarityThreshold, dedupChecker))
	hookChain.AddToolHook(service.NewAuditHook())
	hookChain.AddCompactHook(service.NewCompactAuditHook())
	if in.memStore != nil {
		hookChain.AddCompactHook(service.NewMemoryCompactHook(in.memStore, in.sessionID, dedupChecker))
		hookChain.Add(service.NewMemoryPostRunHook(in.memStore, dedupChecker))
	}
	hookChain.Add(service.NewDiaryHook(&diaryAdapter{store: in.diaryStore}))

	var evoEvaluator *service.EvoEvaluator
	var evoRepairRouter *service.RepairRouter
	if in.cfg.Evo.AutoEval {
		evalModel := in.cfg.Evo.EvalModel
		if evalModel == "" {
			evalModel = in.cfg.Agent.DefaultModel
		}
		if evalProvider, err := in.router.Resolve(evalModel); err == nil {
			evoEvaluator = service.NewEvoEvaluator(evalProvider, in.cfg.Evo, in.evoStore)
			evoRepairRouter = service.NewRepairRouter(in.cfg.Evo, in.evoStore)
			slog.Info(fmt.Sprintf("[evo] Evaluator active: model=%s threshold=%.1f maxRetries=%d",
				evalModel, in.cfg.Evo.ScoreThreshold, in.cfg.Evo.MaxRetries))
		} else {
			slog.Info(fmt.Sprintf("[evo] Warning: eval model %q not found, evo disabled: %v", evalModel, err))
		}
	}

	baseDeps := service.Deps{
		Config:          in.cfg,
		LLMRouter:       in.router,
		PromptEngine:    in.promptEngine,
		ToolExec:        in.registry,
		Security:        &securityAdapter{hook: in.secHook},
		Delta:           &service.Delta{},
		Brain:           in.brainStore,
		KIStore:         in.kiStore,
		KIRetriever:     in.kiRetriever,
		Workspace:       in.wsStore,
		SkillMgr:        in.skillMgr,
		HistoryStore:    &historyAdapter{store: in.historyStore},
		FileHistory:     in.fileHistory,
		Hooks:           hookChain,
		MemoryStore:     in.memStore,
		SnapshotStore:   in.snapshotStore,
		EvoEvaluator:    evoEvaluator,
		EvoRepairRouter: evoRepairRouter,
		EvoStore:        in.evoStore,
		WebhookHook:     buildWebhookHook(in.cfg, in.sessionID),
	}
	factory := service.NewLoopFactory(baseDeps, 8)

	loop := service.NewAgentLoop(baseDeps)
	loop.SetDelta(&service.Delta{})

	var loopPool *service.LoopPool
	orchestrator := service.NewSubagentOrchestrator(factory, loop, func(runtimeSID string) *service.AgentLoop {
		if loopPool != nil {
			if parentLoop := loopPool.GetIfExists(runtimeSID); parentLoop != nil {
				return parentLoop
			}
		}
		return loop
	}, in.agentRegistry)
	orchestrator.SetEventPusher(in.spawnTool.EventPusher)
	orchestrator.SetMaxSubagents(in.cfg.Agent.MaxSubagents)
	if in.transcriptStore != nil {
		orchestrator.SetTranscriptSaver(func(sessionID, taskName, runID, status, output string) error {
			return in.transcriptStore.SaveSimple(sessionID, taskName, runID, status, output)
		})
	}
	in.spawnTool.SetSpawnFunc(func(ctx context.Context, task, taskName, agentType string) (string, error) {
		orchestrator.SetEventPusher(in.spawnTool.EventPusher)
		return orchestrator.Spawn(ctx, in.sessionID, task, taskName, agentType)
	})
	in.skillTool.SetSpawnFunc(in.spawnTool.GetSpawnFunc())

	hasGit := false
	if cwd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(cwd, ".git")); err == nil {
			hasGit = true
		}
	}
	in.spawnTool.ToolCtx = prompttext.ToolContext{
		HasGit:     hasGit,
		HasSandbox: in.sbMgr != nil,
		SkillCount: len(in.skillMgr.List()),
		HasBrain:   in.brainDir != "",
	}
	if in.brainDir != "" {
		in.spawnTool.ScratchDir = filepath.Join(in.brainDir, in.sessionID, "scratchpad")
	}
	in.spawnTool.SetAgentTypes(in.agentRegistry.TypeNames())

	loopPool = service.NewLoopPool(func(sid string) *service.AgentLoop {
		return service.NewAgentLoop(baseDeps)
	}, in.brainDir)

	sessMgr := service.NewSessionManager(&sessionRepoAdapter{repo: in.repo, loc: in.cfg.LoadLocation()})
	chatEngine := service.NewChatEngine(loopPool, sessMgr, &historyAdapter{store: in.historyStore})
	modelMgr := service.NewModelManager(in.router)
	toolAdmin := service.NewToolAdmin(&toolRegistryAdapter{reg: in.registry})

	hookChain.Add(service.NewTitleDistillHook(
		llm.NewTitleDistiller(in.router),
		sessMgr,
	))
	if !config.IsBootstrapped() {
		hookChain.Add(&bootstrapReadyHook{})
	}

	return engineAssembly{
		baseDeps:   baseDeps,
		factory:    factory,
		loop:       loop,
		loopPool:   loopPool,
		sessionMgr: sessMgr,
		chatEngine: chatEngine,
		modelMgr:   modelMgr,
		toolAdmin:  toolAdmin,
	}
}
