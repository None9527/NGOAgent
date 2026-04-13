package application

import (
	"context"
	"testing"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/mcp"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/skill"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

func TestRuntimeCapabilitySourceDescriptors(t *testing.T) {
	sources := []capabilitySource{
		skillWatcherSource{},
		mcpCapabilitySource{},
		skillPromotionSource{},
	}
	expected := []capabilitySourceDescriptor{
		{Name: "skill_watcher", Kind: "skill_runtime", DiscoverySource: "skill"},
		{Name: "mcp", Kind: "mcp_server", DiscoverySource: "mcp"},
		{Name: "skill_promotion", Kind: "skill_runtime", DiscoverySource: "skill"},
	}
	for i, source := range sources {
		if got := source.Descriptor(); got != expected[i] {
			t.Fatalf("source %d: expected %#v, got %#v", i, expected[i], got)
		}
	}
}

func TestCapabilitySourcesReturnStableStatuses(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	registry := tool.NewRegistry()
	skillMgr := skill.NewManager(t.TempDir())
	results := []capabilitySourceStartResult{
		skillWatcherSource{mgr: skillMgr, stopCh: stopCh}.Start(context.Background(), registry),
		mcpCapabilitySource{mgr: mcp.NewManager()}.Start(context.Background(), registry),
		skillPromotionSource{mgr: skillMgr}.Start(context.Background(), registry),
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 source results, got %d", len(results))
	}
	expected := []struct {
		name   string
		status capabilitySourceStartStatus
	}{
		{name: "skill_watcher", status: capabilitySourceStarted},
		{name: "mcp", status: capabilitySourceSkipped},
		{name: "skill_promotion", status: capabilitySourceSkipped},
	}
	for i, want := range expected {
		if results[i].Source.Name != want.name {
			t.Fatalf("result %d: expected name %q, got %q", i, want.name, results[i].Source.Name)
		}
		if results[i].Status != want.status {
			t.Fatalf("result %d: expected status %q, got %q", i, want.status, results[i].Status)
		}
	}
}
