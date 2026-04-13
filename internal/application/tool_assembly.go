package application

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

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
	manifest []toolProviderManifest
}

type toolAssemblyInput struct {
	cfg         *config.Config
	registry    *tool.Registry
	registered  []string
	overwritten []string
	brainDir    string
	fileHistory *workspace.FileHistory
	sbMgr       *sandbox.Manager
	kiStore     *knowledge.Store
	kiRetriever *knowledge.Retriever
	memStore    *memory.Store
	diaryStore  *memory.DiaryStore
	skillMgr    *skill.Manager
}

type builtinToolProvider interface {
	Name() string
	Register(*toolAssemblyInput) assembledToolHandles
}

type toolProviderSet []builtinToolProvider

type assembledToolHandles struct {
	spawn *tool.SpawnAgentTool
	skill *tool.SkillTool
}

type toolProviderManifest struct {
	Name       string
	Tools      []string
	Overwrites []string
}

type toolProviderSetResult struct {
	handles  assembledToolHandles
	manifest []toolProviderManifest
}

func (in *toolAssemblyInput) Register(registered tool.Tool) {
	name := registered.Name()
	if _, exists := in.registry.Get(name); exists {
		in.overwritten = append(in.overwritten, name)
	}
	in.registry.Register(registered)
	in.registered = append(in.registered, name)
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

	input := toolAssemblyInput{
		cfg:         cfg,
		registry:    registry,
		brainDir:    brainDir,
		fileHistory: fileHistory,
		sbMgr:       sbMgr,
		kiStore:     kiStore,
		kiRetriever: kiRetriever,
		memStore:    memStore,
		diaryStore:  diaryStore,
		skillMgr:    skillMgr,
	}

	result := defaultToolProviderSet().Register(input)
	logToolProviderManifest(result.manifest)

	return assembledTools{
		registry: registry,
		spawn:    result.handles.spawn,
		skill:    result.handles.skill,
		manifest: result.manifest,
	}
}

func defaultToolProviderSet() toolProviderSet {
	return toolProviderSet{
		filesystemToolProvider{},
		researchToolProvider{},
		planningToolProvider{},
		knowledgeToolProvider{},
		runtimeToolProvider{},
		gitToolProvider{},
		mediaToolProvider{},
		workspaceUtilityToolProvider{},
	}
}

func (providers toolProviderSet) Register(input toolAssemblyInput) toolProviderSetResult {
	var handles assembledToolHandles
	manifest := make([]toolProviderManifest, 0, len(providers))
	for _, provider := range providers {
		providerInput := input
		handles = mergeToolHandles(handles, provider.Register(&providerInput))
		manifest = append(manifest, toolProviderManifest{
			Name:       provider.Name(),
			Tools:      sortedToolNames(providerInput.registered),
			Overwrites: sortedToolNames(providerInput.overwritten),
		})
	}
	return toolProviderSetResult{
		handles:  handles,
		manifest: manifest,
	}
}

func mergeToolHandles(base assembledToolHandles, next assembledToolHandles) assembledToolHandles {
	if next.spawn != nil {
		base.spawn = next.spawn
	}
	if next.skill != nil {
		base.skill = next.skill
	}
	return base
}

func sortedToolNames(names []string) []string {
	names = append([]string(nil), names...)
	sort.Strings(names)
	return names
}

func logToolProviderManifest(manifest []toolProviderManifest) {
	total, overwrites, groups := summarizeToolProviderManifest(manifest)
	if total == 0 {
		return
	}
	slog.Info("[tools] builtin providers registered",
		"providers", len(manifest),
		"tools", total,
		"overwrites", overwrites,
		"groups", groups,
	)
}

func summarizeToolProviderManifest(manifest []toolProviderManifest) (int, int, string) {
	total := 0
	overwrites := 0
	groups := make([]string, 0, len(manifest))
	for _, entry := range manifest {
		total += len(entry.Tools)
		overwrites += len(entry.Overwrites)
		groups = append(groups, fmt.Sprintf("%s:%d", entry.Name, len(entry.Tools)))
	}
	return total, overwrites, strings.Join(groups, ",")
}
