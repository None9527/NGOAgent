package application

import (
	"fmt"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

type filesystemToolProvider struct{}

func (filesystemToolProvider) Name() string { return "filesystem" }

func (filesystemToolProvider) Register(in *toolAssemblyInput) assembledToolHandles {
	in.Register(&tool.ReadFileTool{})
	in.Register(&tool.WriteFileTool{FileHistory: in.fileHistory})
	in.Register(&tool.EditFileTool{FileHistory: in.fileHistory})
	in.Register(&tool.GlobTool{})
	in.Register(&tool.GrepSearchTool{})
	in.Register(tool.NewRunCommandTool(in.sbMgr))
	in.Register(tool.NewCommandStatusTool(in.sbMgr))
	return assembledToolHandles{}
}

type researchToolProvider struct{}

func (researchToolProvider) Name() string { return "research" }

func (researchToolProvider) Register(in *toolAssemblyInput) assembledToolHandles {
	in.Register(tool.NewWebSearchTool(in.cfg.Search.Endpoint))
	in.Register(tool.NewWebFetchTool(in.cfg.Search.Endpoint))
	in.Register(tool.NewDeepResearchTool(in.cfg.Search.Endpoint))
	return assembledToolHandles{}
}

type planningToolProvider struct{}

func (planningToolProvider) Name() string { return "planning" }

func (planningToolProvider) Register(in *toolAssemblyInput) assembledToolHandles {
	in.Register(tool.NewTaskPlanTool(in.brainDir))
	in.Register(tool.NewTaskBoundaryTool())
	in.Register(tool.NewNotifyUserTool())
	in.Register(&tool.UpdateProjectContextTool{})
	return assembledToolHandles{}
}

type knowledgeToolProvider struct{}

func (knowledgeToolProvider) Name() string { return "knowledge" }

func (knowledgeToolProvider) Register(in *toolAssemblyInput) assembledToolHandles {
	in.Register(tool.NewSaveKnowledgeTool(in.kiStore, in.kiRetriever, in.cfg.Embedding.SimilarityThreshold))
	in.Register(tool.NewRecallTool(in.kiRetriever, in.memStore, in.diaryStore))
	in.Register(tool.NewSendMessageTool(in.brainDir))
	in.Register(tool.NewTaskListTool(in.brainDir))
	return assembledToolHandles{}
}

type runtimeToolProvider struct{}

func (runtimeToolProvider) Name() string { return "runtime" }

func (runtimeToolProvider) Register(in *toolAssemblyInput) assembledToolHandles {
	spawnTool := tool.NewSpawnAgentTool(nil)
	in.Register(spawnTool)
	skillTool := tool.NewSkillTool(in.skillMgr)
	in.Register(skillTool)
	in.Register(tool.NewEvoTool("/tmp/ngoagent-evo", in.sbMgr))
	in.Register(tool.NewBrainArtifactTool(nil))
	in.Register(tool.NewUndoEditTool(in.fileHistory))
	return assembledToolHandles{spawn: spawnTool, skill: skillTool}
}

type gitToolProvider struct{}

func (gitToolProvider) Name() string { return "git" }

func (gitToolProvider) Register(in *toolAssemblyInput) assembledToolHandles {
	in.Register(&tool.GitStatusTool{})
	in.Register(&tool.GitDiffTool{})
	in.Register(&tool.GitLogTool{})
	in.Register(&tool.GitCommitTool{})
	in.Register(&tool.GitBranchTool{})
	return assembledToolHandles{}
}

type mediaToolProvider struct{}

func (mediaToolProvider) Name() string { return "media" }

func (mediaToolProvider) Register(in *toolAssemblyInput) assembledToolHandles {
	viewMediaAddr := fmt.Sprintf("http://localhost:%d", in.cfg.Server.HTTPPort)
	if in.cfg.Server.HTTPPort == 0 {
		viewMediaAddr = "http://localhost:19996"
	}
	in.Register(tool.NewViewMediaTool(viewMediaAddr))
	return assembledToolHandles{}
}

type workspaceUtilityToolProvider struct{}

func (workspaceUtilityToolProvider) Name() string { return "workspace_utility" }

func (workspaceUtilityToolProvider) Register(in *toolAssemblyInput) assembledToolHandles {
	in.Register(&tool.TreeTool{})
	in.Register(&tool.FindFilesTool{})
	in.Register(&tool.CountLinesTool{})
	in.Register(&tool.DiffFilesTool{})
	in.Register(tool.NewHTTPFetchTool())
	in.Register(&tool.ClipboardTool{})
	return assembledToolHandles{}
}
