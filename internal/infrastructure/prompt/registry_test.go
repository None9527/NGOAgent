package prompt

import (
	"strings"
	"testing"
)

// TestRegistryRegisterAndBuild verifies basic section registration and assembly.
func TestRegistryRegisterAndBuild(t *testing.T) {
	r := NewRegistry()
	r.Register(SectionMeta{Name: "Alpha", Order: 2, Priority: 0,
		Factory: func(d Deps) Section { return Section{Content: "alpha-content"} },
	})
	r.Register(SectionMeta{Name: "Beta", Order: 1, Priority: 1,
		Factory: func(d Deps) Section { return Section{Content: "beta-content"} },
	})

	sections := r.Build(Deps{})

	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	// Beta (Order=1) must come before Alpha (Order=2)
	if sections[0].Name != "Beta" {
		t.Errorf("expected first section Beta, got %s", sections[0].Name)
	}
	if sections[1].Name != "Alpha" {
		t.Errorf("expected second section Alpha, got %s", sections[1].Name)
	}
	// Metadata stamps applied by registry
	if sections[0].Order != 1 {
		t.Errorf("Beta Order should be 1, got %d", sections[0].Order)
	}
	if sections[1].Priority != 0 {
		t.Errorf("Alpha Priority should be 0, got %d", sections[1].Priority)
	}
}

// TestRegistryOverride verifies that re-registering a name replaces the factory.
func TestRegistryOverride(t *testing.T) {
	r := NewRegistry()
	r.Register(SectionMeta{Name: "Sec", Order: 1, Priority: 0,
		Factory: func(d Deps) Section { return Section{Content: "original"} },
	})
	r.Register(SectionMeta{Name: "Sec", Order: 1, Priority: 0,
		Factory: func(d Deps) Section { return Section{Content: "overridden"} },
	})

	sections := r.Build(Deps{})
	if sections[0].Content != "overridden" {
		t.Errorf("expected overridden content, got %q", sections[0].Content)
	}
}

// TestRegistryUnregister verifies removal by name.
func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register(SectionMeta{Name: "Keep", Order: 1, Priority: 0,
		Factory: func(d Deps) Section { return Section{Content: "keep"} },
	})
	r.Register(SectionMeta{Name: "Drop", Order: 2, Priority: 0,
		Factory: func(d Deps) Section { return Section{Content: "drop"} },
	})
	r.Unregister("Drop")

	sections := r.Build(Deps{})
	if len(sections) != 1 {
		t.Fatalf("expected 1 section after unregister, got %d", len(sections))
	}
	if sections[0].Name != "Keep" {
		t.Errorf("expected Keep, got %s", sections[0].Name)
	}
}

// TestRegistryDepsInjection verifies Deps are correctly passed to factories.
func TestRegistryDepsInjection(t *testing.T) {
	r := NewRegistry()
	r.Register(SectionMeta{Name: "Ctx", Order: 1, Priority: 0,
		Factory: func(d Deps) Section {
			return Section{Content: "mode=" + d.Mode}
		},
	})
	sections := r.Build(Deps{Mode: "chat"})
	if sections[0].Content != "mode=chat" {
		t.Errorf("expected mode=chat, got %q", sections[0].Content)
	}
}

// TestRegistrySize and Names helpers.
func TestRegistrySizeAndNames(t *testing.T) {
	r := NewRegistry()
	if r.Size() != 0 {
		t.Error("empty registry should have size 0")
	}
	r.Register(SectionMeta{Name: "Z", Order: 1, Priority: 0, Factory: func(d Deps) Section { return Section{} }})
	r.Register(SectionMeta{Name: "A", Order: 2, Priority: 0, Factory: func(d Deps) Section { return Section{} }})
	if r.Size() != 2 {
		t.Errorf("expected size 2, got %d", r.Size())
	}
	names := r.Names()
	if names[0] != "A" || names[1] != "Z" {
		t.Errorf("Names() should be sorted: got %v", names)
	}
}

// TestEngineDefaultRegistry verifies the Engine initializes with all 19 defaults.
func TestEngineDefaultRegistry(t *testing.T) {
	e := NewEngine()
	r := e.Registry()

	expectedNames := []string{
		"CoreBehavior", "DoingTasks", "Ephemeral", "Focus", "Identity",
		"KnowledgeIndex", "OutputCapabilities", "ProjectContext",
		"ResponseFormat", "Runtime", "Safety", "SemanticMemory",
		"Skills", "ToneAndStyle", "ToolCalling", "ToolProtocol", "Tooling",
		"UserRules", "Variants",
	}

	names := r.Names()
	if len(names) != len(expectedNames) {
		t.Errorf("expected %d sections, got %d: %v", len(expectedNames), len(names), names)
	}
	for _, want := range expectedNames {
		found := false
		for _, got := range names {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("section %q not registered", want)
		}
	}
}

// TestEngineAssembleOrder verifies sections emerge in Order order.
func TestEngineAssembleOrder(t *testing.T) {
	e := NewEngine()
	result, _ := e.Assemble(Deps{Mode: "chat"})

	// Identity (Order=1) content should precede ResponseFormat (Order=14)
	idxIdentity := strings.Index(result, "NGOAgent")
	idxResponse := strings.Index(result, "Response rules")
	if idxIdentity < 0 {
		t.Error("Identity section (NGOAgent) not found in assembled prompt")
	}
	if idxResponse < 0 {
		t.Error("ResponseFormat section not found in assembled prompt")
	}
	if idxIdentity > idxResponse {
		t.Error("Identity should appear before ResponseFormat")
	}
}

// TestEngineRegistryExtension verifies a custom section can be injected after init.
func TestEngineRegistryExtension(t *testing.T) {
	e := NewEngine()
	e.Registry().Register(SectionMeta{
		Name: "CustomPlugin", Order: 6, Priority: 1,
		Factory: func(d Deps) Section {
			return Section{Content: "CUSTOM_PLUGIN_CONTENT"}
		},
	})

	result, _ := e.Assemble(Deps{Mode: "chat"})
	if !strings.Contains(result, "CUSTOM_PLUGIN_CONTENT") {
		t.Error("custom section not found in assembled prompt")
	}
}

// TestCacheableFlag verifies SectionMeta.Cacheable is propagated to Section.
func TestCacheableFlag(t *testing.T) {
	r := NewRegistry()
	r.Register(SectionMeta{Name: "Static", Order: 1, Priority: 0, Cacheable: true,
		Factory: func(d Deps) Section { return Section{Content: "static-content"} },
	})
	r.Register(SectionMeta{Name: "Dynamic", Order: 2, Priority: 0, Cacheable: false,
		Factory: func(d Deps) Section { return Section{Content: "dynamic-content"} },
	})

	sections := r.Build(Deps{})
	if !sections[0].Cacheable {
		t.Errorf("Static section should have Cacheable=true, got false")
	}
	if sections[1].Cacheable {
		t.Errorf("Dynamic section should have Cacheable=false, got true")
	}
}

// TestAssembleSplitSeparation verifies static/dynamic split correctness.
func TestAssembleSplitSeparation(t *testing.T) {
	e := NewEngine()
	deps := Deps{
		Mode:          "chat",
		ConvSummary:   "KI-SUMMARY-UNIQUE",
		Ephemeral:     []string{"EPHEMERAL-MSG-UNIQUE"},
		MemoryContent: "",
	}

	result := e.AssembleSplit(deps)

	// Identity belongs to static (Order=1, Cacheable=true)
	if !strings.Contains(result.Static, "NGOAgent") {
		t.Error("Identity (NGOAgent) should be in Static portion")
	}
	// Ephemeral belongs to dynamic (Order=19, Cacheable=false)
	if !strings.Contains(result.Dynamic, "EPHEMERAL-MSG-UNIQUE") {
		t.Error("Ephemeral message should be in Dynamic portion")
	}
	// KnowledgeIndex belongs to dynamic (Order=15, Cacheable=false)
	if !strings.Contains(result.Dynamic, "KI-SUMMARY-UNIQUE") {
		t.Error("KnowledgeIndex should be in Dynamic portion")
	}
	// Static should NOT contain ephemeral
	if strings.Contains(result.Static, "EPHEMERAL-MSG-UNIQUE") {
		t.Error("Ephemeral message should NOT be in Static portion")
	}
}

// TestAssembleSplitTokenAccounting verifies token counts are sane.
func TestAssembleSplitTokenAccounting(t *testing.T) {
	e := NewEngine()
	result := e.AssembleSplit(Deps{Mode: "chat", TokenBudget: 32000})

	if result.TokensTotal <= 0 {
		t.Errorf("TokensTotal should be positive, got %d", result.TokensTotal)
	}
	if result.TokenStatic <= 0 {
		t.Errorf("TokenStatic should be positive, got %d", result.TokenStatic)
	}
	if result.TokenStatic > result.TokensTotal {
		t.Errorf("TokenStatic (%d) should be <= TokensTotal (%d)", result.TokenStatic, result.TokensTotal)
	}
	// Static should dominate (>50% for a session with no dynamic content)
	staticRatio := float64(result.TokenStatic) / float64(result.TokensTotal)
	if staticRatio < 0.5 {
		t.Errorf("Static ratio should be >50%%, got %.1f%%", staticRatio*100)
	}
}
