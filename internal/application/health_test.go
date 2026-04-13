package application

import (
	"context"
	"testing"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

func TestAdminQueriesHealthReady(t *testing.T) {
	registry := service.NewToolAdmin(&stubToolRegistry{tools: []service.ToolInfo{{Name: "read_file", Enabled: true}}})
	kernel := &ApplicationKernel{
		cfg:       config.NewManager(""),
		router:    llm.NewRouter(nil),
		toolAdmin: registry,
		discovery: stubDiscovery{capabilities: []service.ToolCapability{
			{Name: "read_file", Category: "builtin", Source: "filesystem", SourceKind: "builtin_provider"},
			{Name: "web_search", Category: "mcp", Source: "agent-search", SourceKind: "mcp_server"},
			{Name: "deploy", Category: "skill", Source: "deploy", SourceKind: "skill", SourcePath: "/skills/deploy"},
		}},
		loop:      &service.AgentLoop{},
		startedAt: time.Now().Add(-2 * time.Second),
	}

	resp := (&AdminQueries{ApplicationKernel: kernel}).Health()
	if resp.Status != "ok" {
		t.Fatalf("expected ok status, got %q", resp.Status)
	}
	if !resp.Ready {
		t.Fatal("expected ready health")
	}
	if resp.Tools != 1 {
		t.Fatalf("expected 1 tool, got %d", resp.Tools)
	}
	if resp.Checks["config"] != "ok" || resp.Checks["router"] != "ok" || resp.Checks["tools"] != "ok" || resp.Checks["runtime"] != "ok" {
		t.Fatalf("unexpected checks: %#v", resp.Checks)
	}
	if resp.CapabilityCategories["builtin"] != 1 || resp.CapabilityCategories["mcp"] != 1 || resp.CapabilityCategories["skill"] != 1 {
		t.Fatalf("unexpected capability category summary: %#v", resp.CapabilityCategories)
	}
	if resp.CapabilitySources["builtin_provider"] != 1 || resp.CapabilitySources["mcp_server"] != 1 || resp.CapabilitySources["skill"] != 1 {
		t.Fatalf("unexpected capability source summary: %#v", resp.CapabilitySources)
	}
	if resp.StartedAt == "" {
		t.Fatal("expected started_at")
	}
}

func TestAdminQueriesHealthDegraded(t *testing.T) {
	resp := (&AdminQueries{ApplicationKernel: &ApplicationKernel{}}).Health()
	if resp.Status != "degraded" {
		t.Fatalf("expected degraded status, got %q", resp.Status)
	}
	if resp.Ready {
		t.Fatal("expected not ready")
	}
	if resp.Checks["config"] != "missing" || resp.Checks["router"] != "missing" || resp.Checks["tools"] != "missing" || resp.Checks["runtime"] != "missing" {
		t.Fatalf("unexpected checks: %#v", resp.Checks)
	}
	if len(resp.CapabilityCategories) != 0 || len(resp.CapabilitySources) != 0 {
		t.Fatalf("expected empty capability summaries without discovery, got %#v %#v", resp.CapabilityCategories, resp.CapabilitySources)
	}
}

type stubDiscovery struct {
	capabilities []service.ToolCapability
}

func (s stubDiscovery) ListCapabilities(context.Context) []service.ToolCapability {
	return append([]service.ToolCapability(nil), s.capabilities...)
}

func (s stubDiscovery) FindByCategory(context.Context, string) []service.ToolCapability { return nil }
func (s stubDiscovery) FindByName(context.Context, string) *service.ToolCapability      { return nil }
func (s stubDiscovery) FindByTag(context.Context, ...string) []service.ToolCapability   { return nil }
func (s stubDiscovery) Advertise(context.Context) []string                              { return nil }
func (s stubDiscovery) Refresh(context.Context)                                         {}

type stubToolRegistry struct {
	tools []service.ToolInfo
}

func (s *stubToolRegistry) List() []service.ToolInfo {
	return s.tools
}

func (s *stubToolRegistry) Enable(string) error {
	return nil
}

func (s *stubToolRegistry) Disable(string) error {
	return nil
}
