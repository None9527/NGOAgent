package service

import (
	"context"
	"testing"
)

type mockMCPSource struct {
	tools []MCPToolDescriptor
}

func (m *mockMCPSource) ListMCPTools() []MCPToolDescriptor { return m.tools }

type mockSkillSource struct {
	skills []SkillDescriptor
}

func (m *mockSkillSource) ListSkills() []SkillDescriptor { return m.skills }

func TestAggregatedToolDiscovery_ListCapabilities(t *testing.T) {
	registry := &mockToolRegistryForDiscovery{tools: []ToolInfo{
		{Name: "read_file", Enabled: true},
		{Name: "write_file", Enabled: true},
		{Name: "disabled_tool", Enabled: false},
	}}
	mcp := &mockMCPSource{tools: []MCPToolDescriptor{
		{Name: "web_search", Description: "Search the web", Server: "brave"},
	}}
	skills := &mockSkillSource{skills: []SkillDescriptor{
		{Name: "deploy", Description: "Deploy to cloud", Path: "/skills/deploy", Enabled: true},
		{Name: "inactive_skill", Description: "Inactive", Path: "/skills/x", Enabled: false},
	}}

	d := NewAggregatedToolDiscovery(registry, mcp, skills)
	caps := d.ListCapabilities(context.Background())

	// Should have: read_file, write_file (builtin) + web_search (mcp) + deploy (skill) = 4
	if len(caps) != 4 {
		t.Errorf("expected 4 capabilities, got %d", len(caps))
		for _, c := range caps {
			t.Logf("  %s (%s)", c.Name, c.Category)
		}
	}
}

func TestAggregatedToolDiscovery_FindByCategory(t *testing.T) {
	registry := &mockToolRegistryForDiscovery{tools: []ToolInfo{
		{Name: "read_file", Enabled: true},
	}}
	mcp := &mockMCPSource{tools: []MCPToolDescriptor{
		{Name: "search", Server: "brave"},
	}}

	d := NewAggregatedToolDiscovery(registry, mcp, nil)
	ctx := context.Background()

	builtins := d.FindByCategory(ctx, "builtin")
	if len(builtins) != 1 || builtins[0].Name != "read_file" {
		t.Errorf("expected 1 builtin, got %v", builtins)
	}

	mcpTools := d.FindByCategory(ctx, "mcp")
	if len(mcpTools) != 1 || mcpTools[0].Name != "search" {
		t.Errorf("expected 1 mcp tool, got %v", mcpTools)
	}
}

func TestAggregatedToolDiscovery_BuiltinSources(t *testing.T) {
	registry := &mockToolRegistryForDiscovery{tools: []ToolInfo{
		{Name: "read_file", Enabled: true},
		{Name: "web_search", Enabled: true},
	}}
	d := NewAggregatedToolDiscovery(registry, nil, nil)
	d.SetBuiltinSources(map[string]string{
		"read_file":  "filesystem",
		"web_search": "research",
	})

	readFile := d.FindByName(context.Background(), "read_file")
	if readFile == nil {
		t.Fatal("expected read_file capability")
	}
	if readFile.Source != "filesystem" {
		t.Fatalf("expected filesystem source, got %q", readFile.Source)
	}
	if readFile.SourceKind != "builtin_provider" {
		t.Fatalf("expected builtin_provider source kind, got %q", readFile.SourceKind)
	}
	if !hasString(readFile.Tags, "filesystem") {
		t.Fatalf("expected filesystem tag, got %#v", readFile.Tags)
	}
}

func TestAggregatedToolDiscovery_SkillUsesStableIdentityAndPathMetadata(t *testing.T) {
	skills := &mockSkillSource{skills: []SkillDescriptor{
		{Name: "deploy", Description: "Deploy to cloud", Path: "/skills/deploy", Enabled: true},
	}}
	d := NewAggregatedToolDiscovery(nil, nil, skills)

	deploy := d.FindByName(context.Background(), "deploy")
	if deploy == nil {
		t.Fatal("expected deploy capability")
	}
	if deploy.Source != "deploy" {
		t.Fatalf("expected stable skill identity source, got %q", deploy.Source)
	}
	if deploy.SourceKind != "skill" {
		t.Fatalf("expected skill source kind, got %q", deploy.SourceKind)
	}
	if deploy.SourcePath != "/skills/deploy" {
		t.Fatalf("expected skill source path metadata, got %q", deploy.SourcePath)
	}
}

func TestAggregatedToolDiscovery_FindByName(t *testing.T) {
	registry := &mockToolRegistryForDiscovery{tools: []ToolInfo{
		{Name: "read_file", Enabled: true},
	}}
	d := NewAggregatedToolDiscovery(registry, nil, nil)
	ctx := context.Background()

	found := d.FindByName(ctx, "read_file")
	if found == nil || found.Name != "read_file" {
		t.Error("expected to find read_file")
	}
	if d.FindByName(ctx, "nonexistent") != nil {
		t.Error("expected nil for nonexistent tool")
	}
}

func TestAggregatedToolDiscovery_FindByTag(t *testing.T) {
	mcp := &mockMCPSource{tools: []MCPToolDescriptor{
		{Name: "search", Server: "brave"},
		{Name: "index", Server: "elastic"},
	}}
	d := NewAggregatedToolDiscovery(nil, mcp, nil)
	ctx := context.Background()

	results := d.FindByTag(ctx, "brave")
	if len(results) != 1 || results[0].Name != "search" {
		t.Errorf("expected 1 result for tag 'brave', got %d", len(results))
	}

	// "mcp" tag should match all.
	all := d.FindByTag(ctx, "mcp")
	if len(all) != 2 {
		t.Errorf("expected 2 results for tag 'mcp', got %d", len(all))
	}
}

func TestAggregatedToolDiscovery_Deduplication(t *testing.T) {
	registry := &mockToolRegistryForDiscovery{tools: []ToolInfo{
		{Name: "search", Enabled: true},
	}}
	mcp := &mockMCPSource{tools: []MCPToolDescriptor{
		{Name: "search", Server: "external"},
	}}

	d := NewAggregatedToolDiscovery(registry, mcp, nil)
	caps := d.ListCapabilities(context.Background())
	if len(caps) != 1 {
		t.Errorf("expected dedup to 1, got %d", len(caps))
	}
	// Builtin should win (first in aggregation order).
	if caps[0].Category != "builtin" {
		t.Errorf("expected builtin to win dedup, got %s", caps[0].Category)
	}
}

func TestAggregatedToolDiscovery_Advertise(t *testing.T) {
	registry := &mockToolRegistryForDiscovery{tools: []ToolInfo{
		{Name: "z_tool", Enabled: true},
		{Name: "a_tool", Enabled: true},
	}}
	d := NewAggregatedToolDiscovery(registry, nil, nil)
	names := d.Advertise(context.Background())
	if len(names) != 2 || names[0] != "a_tool" || names[1] != "z_tool" {
		t.Errorf("expected sorted [a_tool, z_tool], got %v", names)
	}
}

func TestAggregatedToolDiscovery_Refresh(t *testing.T) {
	registry := &mockToolRegistryForDiscovery{tools: []ToolInfo{
		{Name: "tool1", Enabled: true},
	}}
	d := NewAggregatedToolDiscovery(registry, nil, nil)
	ctx := context.Background()

	caps1 := d.ListCapabilities(ctx)
	if len(caps1) != 1 {
		t.Fatalf("expected 1, got %d", len(caps1))
	}

	// Mutate the source.
	registry.tools = append(registry.tools, ToolInfo{Name: "tool2", Enabled: true})

	// Still cached.
	caps2 := d.ListCapabilities(ctx)
	if len(caps2) != 1 {
		t.Fatalf("cache should prevent seeing new tool, got %d", len(caps2))
	}

	// Force refresh.
	d.Refresh(ctx)
	caps3 := d.ListCapabilities(ctx)
	if len(caps3) != 2 {
		t.Errorf("expected 2 after refresh, got %d", len(caps3))
	}
}

func TestAggregatedToolDiscovery_NilSources(t *testing.T) {
	d := NewAggregatedToolDiscovery(nil, nil, nil)
	caps := d.ListCapabilities(context.Background())
	if len(caps) != 0 {
		t.Errorf("expected 0 with nil sources, got %d", len(caps))
	}
}

// mockToolRegistryForDiscovery implements ToolRegistry for test.
type mockToolRegistryForDiscovery struct {
	tools []ToolInfo
}

func (m *mockToolRegistryForDiscovery) List() []ToolInfo          { return m.tools }
func (m *mockToolRegistryForDiscovery) Enable(name string) error  { return nil }
func (m *mockToolRegistryForDiscovery) Disable(name string) error { return nil }

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
