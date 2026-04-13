package application

import (
	"testing"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

type stubToolProvider struct {
	name    string
	tools   []tool.Tool
	handles assembledToolHandles
}

func (p stubToolProvider) Name() string {
	return p.name
}

func (p stubToolProvider) Register(in *toolAssemblyInput) assembledToolHandles {
	for _, registered := range p.tools {
		in.Register(registered)
	}
	return p.handles
}

func TestAssembleBuiltinToolsRegistersExpectedProviders(t *testing.T) {
	cfg := &config.Config{
		Server:    config.ServerConfig{HTTPPort: 0},
		Search:    config.SearchConfig{Endpoint: "http://search.test"},
		Embedding: config.EmbeddingConfig{SimilarityThreshold: 0.75},
	}

	assembled := assembleBuiltinTools(
		cfg,
		t.TempDir(),
		t.TempDir(),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	expected := []string{
		"read_file",
		"write_file",
		"edit_file",
		"glob",
		"grep_search",
		"run_command",
		"command_status",
		"web_search",
		"web_fetch",
		"deep_research",
		"task_plan",
		"task_boundary",
		"notify_user",
		"update_project_context",
		"save_knowledge",
		"recall",
		"send_message",
		"task_list",
		"spawn_agent",
		"skill",
		"evo",
		"brain_artifact",
		"undo_edit",
		"git_status",
		"git_diff",
		"git_log",
		"git_commit",
		"git_branch",
		"view_media",
		"tree",
		"find_files",
		"count_lines",
		"diff_files",
		"http_fetch",
		"clipboard",
	}

	for _, name := range expected {
		if _, ok := assembled.registry.Get(name); !ok {
			t.Fatalf("expected registered tool %q", name)
		}
	}
	if got := len(assembled.registry.List()); got != len(expected) {
		t.Fatalf("expected %d registered tools, got %d", len(expected), got)
	}

	spawnTool, ok := assembled.registry.Get("spawn_agent")
	if !ok {
		t.Fatal("expected spawn_agent in registry")
	}
	if assembled.spawn != spawnTool.(*tool.SpawnAgentTool) {
		t.Fatal("expected assembled spawn handle to match registry tool")
	}

	skillTool, ok := assembled.registry.Get("skill")
	if !ok {
		t.Fatal("expected skill in registry")
	}
	if assembled.skill != skillTool.(*tool.SkillTool) {
		t.Fatal("expected assembled skill handle to match registry tool")
	}
}

func TestToolProviderSetMergesRuntimeHandles(t *testing.T) {
	spawnTool := tool.NewSpawnAgentTool(nil)
	skillTool := tool.NewSkillTool(nil)
	registry := tool.NewRegistry()

	result := toolProviderSet{
		stubToolProvider{name: "spawn", tools: []tool.Tool{&tool.ReadFileTool{}}, handles: assembledToolHandles{spawn: spawnTool}},
		stubToolProvider{name: "skill", tools: []tool.Tool{&tool.GlobTool{}}, handles: assembledToolHandles{skill: skillTool}},
	}.Register(toolAssemblyInput{registry: registry})

	if result.handles.spawn != spawnTool {
		t.Fatal("expected spawn handle from provider set")
	}
	if result.handles.skill != skillTool {
		t.Fatal("expected skill handle from provider set")
	}
	if !sameStrings(result.manifest[0].Tools, []string{"read_file"}) {
		t.Fatalf("expected spawn provider manifest to record read_file, got %#v", result.manifest[0].Tools)
	}
	if !sameStrings(result.manifest[1].Tools, []string{"glob"}) {
		t.Fatalf("expected skill provider manifest to record glob, got %#v", result.manifest[1].Tools)
	}
}

func TestToolProviderSetRecordsOverwrites(t *testing.T) {
	registry := tool.NewRegistry()
	result := toolProviderSet{
		stubToolProvider{name: "first", tools: []tool.Tool{&tool.ReadFileTool{}}},
		stubToolProvider{name: "second", tools: []tool.Tool{&tool.ReadFileTool{}}},
	}.Register(toolAssemblyInput{registry: registry})

	if len(result.manifest) != 2 {
		t.Fatalf("expected two manifest entries, got %d", len(result.manifest))
	}
	if len(result.manifest[0].Overwrites) != 0 {
		t.Fatalf("expected no overwrite in first provider, got %#v", result.manifest[0].Overwrites)
	}
	if !sameStrings(result.manifest[1].Overwrites, []string{"read_file"}) {
		t.Fatalf("expected second provider overwrite, got %#v", result.manifest[1].Overwrites)
	}
}

func TestDefaultToolProviderSetNames(t *testing.T) {
	providers := defaultToolProviderSet()
	expected := []string{
		"filesystem",
		"research",
		"planning",
		"knowledge",
		"runtime",
		"git",
		"media",
		"workspace_utility",
	}
	if len(providers) != len(expected) {
		t.Fatalf("expected %d providers, got %d", len(expected), len(providers))
	}
	for i, provider := range providers {
		if provider.Name() != expected[i] {
			t.Fatalf("provider %d: expected %q, got %q", i, expected[i], provider.Name())
		}
	}
}

func TestAssembleBuiltinToolsRecordsProviderManifest(t *testing.T) {
	cfg := &config.Config{
		Server:    config.ServerConfig{HTTPPort: 0},
		Search:    config.SearchConfig{Endpoint: "http://search.test"},
		Embedding: config.EmbeddingConfig{SimilarityThreshold: 0.75},
	}

	assembled := assembleBuiltinTools(
		cfg,
		t.TempDir(),
		t.TempDir(),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	expected := map[string][]string{
		"filesystem":        {"command_status", "edit_file", "glob", "grep_search", "read_file", "run_command", "write_file"},
		"runtime":           {"brain_artifact", "evo", "skill", "spawn_agent", "undo_edit"},
		"media":             {"view_media"},
		"workspace_utility": {"clipboard", "count_lines", "diff_files", "find_files", "http_fetch", "tree"},
	}
	if len(assembled.manifest) != len(defaultToolProviderSet()) {
		t.Fatalf("expected manifest for every provider, got %d", len(assembled.manifest))
	}
	for _, entry := range assembled.manifest {
		tools, ok := expected[entry.Name]
		if !ok {
			continue
		}
		if !sameStrings(entry.Tools, tools) {
			t.Fatalf("provider %q: expected tools %#v, got %#v", entry.Name, tools, entry.Tools)
		}
		delete(expected, entry.Name)
	}
	if len(expected) != 0 {
		t.Fatalf("missing manifest entries: %#v", expected)
	}
}

func TestSummarizeToolProviderManifest(t *testing.T) {
	total, overwrites, groups := summarizeToolProviderManifest([]toolProviderManifest{
		{Name: "filesystem", Tools: []string{"read_file", "write_file"}},
		{Name: "runtime", Tools: []string{"spawn_agent"}, Overwrites: []string{"read_file"}},
	})
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if overwrites != 1 {
		t.Fatalf("expected 1 overwrite, got %d", overwrites)
	}
	if groups != "filesystem:2,runtime:1" {
		t.Fatalf("unexpected groups summary: %q", groups)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
