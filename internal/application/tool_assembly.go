package application

import (
	"fmt"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/memory"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
)

type assembledTools struct {
	registry *tool.Registry
	spawn    *tool.SpawnAgentTool
	skill    *tool.SkillTool
}

func assembleBuiltinTools(
	cfg *config.Config,
	workspaceDir string,
	brainDir string,
	fileHistory *workspace.FileHistory,
	sbMgr *sandbox.Manager,
	kiStore *knowledge.Store,
	kiRetriever *knowledge.Retriever,
	memStore *memory.Store,
	diaryStore *memory.DiaryStore,
	skillMgr *skill.Manager,
) assembledTools {
	registry := tool.NewRegistry()
	registry.SetWorkspaceDir(workspaceDir)

	registry.Register(&tool.ReadFileTool{})
	registry.Register(&tool.WriteFileTool{FileHistory: fileHistory})
	registry.Register(&tool.EditFileTool{FileHistory: fileHistory})
	registry.Register(&tool.GlobTool{})
	registry.Register(&tool.GrepSearchTool{})
	registry.Register(tool.NewRunCommandTool(sbMgr))
	registry.Register(tool.NewCommandStatusTool(sbMgr))

	registry.Register(tool.NewWebSearchTool(cfg.Search.Endpoint))
	registry.Register(tool.NewWebFetchTool(cfg.Search.Endpoint))
	registry.Register(tool.NewDeepResearchTool(cfg.Search.Endpoint))
	registry.Register(tool.NewTaskPlanTool(brainDir))
	registry.Register(tool.NewTaskBoundaryTool())
	registry.Register(tool.NewNotifyUserTool())
	registry.Register(&tool.UpdateProjectContextTool{})
	registry.Register(tool.NewSaveKnowledgeTool(kiStore, kiRetriever, cfg.Embedding.SimilarityThreshold))
	registry.Register(tool.NewRecallTool(kiRetriever, memStore, diaryStore))
	registry.Register(tool.NewSendMessageTool(brainDir))
	registry.Register(tool.NewTaskListTool(brainDir))

	spawnTool := tool.NewSpawnAgentTool(nil)
	registry.Register(spawnTool)
	skillTool := tool.NewSkillTool(skillMgr)
	registry.Register(skillTool)

	registry.Register(tool.NewEvoTool("/tmp/ngoagent-evo", sbMgr))
	registry.Register(tool.NewBrainArtifactTool(nil))
	registry.Register(tool.NewUndoEditTool(fileHistory))

	registry.Register(&tool.GitStatusTool{})
	registry.Register(&tool.GitDiffTool{})
	registry.Register(&tool.GitLogTool{})
	registry.Register(&tool.GitCommitTool{})
	registry.Register(&tool.GitBranchTool{})

	viewMediaAddr := fmt.Sprintf("http://localhost:%d", cfg.Server.HTTPPort)
	if cfg.Server.HTTPPort == 0 {
		viewMediaAddr = "http://localhost:19996"
	}
	registry.Register(tool.NewViewMediaTool(viewMediaAddr))

	registry.Register(&tool.TreeTool{})
	registry.Register(&tool.FindFilesTool{})
	registry.Register(&tool.CountLinesTool{})
	registry.Register(&tool.DiffFilesTool{})
	registry.Register(tool.NewHTTPFetchTool())
	registry.Register(&tool.ClipboardTool{})

	return assembledTools{
		registry: registry,
		spawn:    spawnTool,
		skill:    skillTool,
	}
}
