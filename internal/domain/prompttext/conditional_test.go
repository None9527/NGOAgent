package prompttext

import (
	"strings"
	"testing"
)

func TestFormatSkillsWithBudget_Full(t *testing.T) {
	skills := []SkillEntry{
		{Name: "deploy", Description: "Deploy to production", Type: "guide", Path: "/skills/deploy"},
		{Name: "test", Description: "Run test suite", Type: "executable", Weight: "light", Path: "/skills/test"},
	}
	result := FormatSkillsWithBudget(skills, DefaultSkillCharBudget)
	if !strings.Contains(result, "deploy") || !strings.Contains(result, "test") {
		t.Fatalf("Expected both skills in output, got: %s", result)
	}
	if !strings.HasPrefix(result, "Available skills:") {
		t.Fatalf("Expected header, got: %s", result)
	}
	t.Log("✅ Full skill listing within budget")
}

func TestFormatSkillsWithBudget_Truncation(t *testing.T) {
	var skills []SkillEntry
	for i := 0; i < 100; i++ {
		skills = append(skills, SkillEntry{
			Name:        "skill-" + string(rune('A'+i%26)),
			Description: strings.Repeat("x", 200),
			Type:        "guide",
			Path:        "/skills/s",
		})
	}
	result := FormatSkillsWithBudget(skills, 3000) // Tight budget
	if len(result) > 3500 {                        // Allow small overflow from header
		t.Fatalf("Expected budget-controlled output ≤3500 chars, got %d", len(result))
	}
	t.Logf("✅ Truncated listing: %d chars (budget 3000)", len(result))
}

func TestFormatSkillsWithBudget_NamesOnly(t *testing.T) {
	var skills []SkillEntry
	for i := 0; i < 200; i++ {
		skills = append(skills, SkillEntry{
			Name:        "very-long-skill-name-" + string(rune('A'+i%26)),
			Description: strings.Repeat("y", 300),
			Type:        "guide",
			Path:        "/skills/s",
		})
	}
	result := FormatSkillsWithBudget(skills, 2000) // Very tight
	// Names-only mode: should not contain descriptions
	if strings.Contains(result, "[guide]") {
		t.Fatalf("Expected names-only mode, but found descriptions: %s", result[:200])
	}
	t.Logf("✅ Names-only fallback: %d chars", len(result))
}

func TestToolRunCommandDynamic(t *testing.T) {
	base := ToolRunCommandDynamic(ToolContext{})
	withGit := ToolRunCommandDynamic(ToolContext{HasGit: true})
	withSandbox := ToolRunCommandDynamic(ToolContext{HasSandbox: true})

	if strings.Contains(base, "git") {
		t.Fatal("Base should not contain git section")
	}
	if !strings.Contains(withGit, "Git operations") {
		t.Fatal("Git context should contain git section")
	}
	if !strings.Contains(withSandbox, "sandbox") {
		t.Fatal("Sandbox context should contain sandbox section")
	}
	t.Logf("✅ Conditional: base=%d, +git=%d, +sandbox=%d chars",
		len(base), len(withGit), len(withSandbox))
}

func TestToolSpawnAgentDynamic(t *testing.T) {
	basic := ToolSpawnAgentDynamic(ToolContext{})
	withBrain := ToolSpawnAgentDynamic(ToolContext{HasBrain: true})

	if !strings.Contains(basic, "Never delegate understanding") {
		t.Fatal("Should contain coordinator guidance")
	}
	if strings.Contains(basic, "Scratchpad") {
		t.Fatal("No-brain context should not have scratchpad section")
	}
	if !strings.Contains(withBrain, "Scratchpad") {
		t.Fatal("With-brain context should have scratchpad section")
	}
	t.Logf("✅ SpawnAgent dynamic: base=%d, +brain=%d chars",
		len(basic), len(withBrain))
}
