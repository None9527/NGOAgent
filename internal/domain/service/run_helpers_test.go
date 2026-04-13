package service

import (
	"testing"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

func TestActiveToolDefsLoadsMemoryToolsForNaturalChineseRememberRequest(t *testing.T) {
	loop := NewAgentLoop(Deps{})
	loop.history = []llm.Message{{Role: "user", Content: "以后请记住：我偏好真实多轮 Agent E2E 测试。"}}

	active := loop.activeToolDefs([]llm.ToolDef{
		toolDef("read_file"),
		toolDef("write_file"),
		toolDef("edit_file"),
		toolDef("run_command"),
		toolDef("command_status"),
		toolDef("glob"),
		toolDef("grep_search"),
		toolDef("task_boundary"),
		toolDef("notify_user"),
		toolDef("task_plan"),
		toolDef("spawn_agent"),
		toolDef("brain_artifact"),
		toolDef("tree"),
		toolDef("find_files"),
		toolDef("count_lines"),
		toolDef("diff_files"),
		toolDef("save_memory"),
		toolDef("recall"),
		toolDef("save_knowledge"),
		toolDef("view_media"),
		toolDef("git_status"),
		toolDef("git_diff"),
		toolDef("git_log"),
		toolDef("git_commit"),
		toolDef("git_branch"),
		toolDef("web_search"),
		toolDef("web_fetch"),
		toolDef("deep_research"),
		toolDef("http_fetch"),
		toolDef("clipboard"),
		toolDef("extra_a"),
		toolDef("extra_b"),
	})

	if !hasToolDef(active, "save_memory") {
		t.Fatalf("expected save_memory to be loaded for natural Chinese remember request; got %#v", toolDefNames(active))
	}
	if !hasToolDef(active, "recall") {
		t.Fatalf("expected recall to be loaded with memory tier; got %#v", toolDefNames(active))
	}
}

func toolDef(name string) llm.ToolDef {
	return llm.ToolDef{Function: llm.ToolFuncDef{Name: name}}
}

func hasToolDef(defs []llm.ToolDef, name string) bool {
	for _, def := range defs {
		if def.Function.Name == name {
			return true
		}
	}
	return false
}

func toolDefNames(defs []llm.ToolDef) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Function.Name)
	}
	return names
}
