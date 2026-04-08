package profile

import (
	"strings"
	"testing"
)

func TestCodingOverlayIdentity(t *testing.T) {
	o := &CodingOverlay{}
	tag := o.IdentityTag()
	if !strings.Contains(tag, "software development") {
		t.Error("coding identity tag should mention software development")
	}
}

func TestComposeIdentitySingle(t *testing.T) {
	active := []BehaviorOverlay{&CodingOverlay{}}
	full := ComposeIdentity(active)
	if !strings.Contains(full, "NGOAgent") {
		t.Error("composed identity should contain NGOAgent")
	}
	if !strings.Contains(full, "software development") {
		t.Error("composed identity should contain coding specialization")
	}
}

func TestComposeIdentityMultiple(t *testing.T) {
	active := []BehaviorOverlay{&CodingOverlay{}, &ResearchOverlay{}}
	full := ComposeIdentity(active)
	if !strings.Contains(full, "software development") {
		t.Error("composed identity should contain coding")
	}
	if !strings.Contains(full, "research") {
		t.Error("composed identity should contain research")
	}
	if !strings.Contains(full, " and ") {
		t.Error("multiple overlays should be joined with 'and'")
	}
}

func TestOmniIdentityNoCodingWords(t *testing.T) {
	codingWords := []string{"coding", "code", "software", "debug", "refactor", "compile"}
	for _, w := range codingWords {
		if strings.Contains(strings.ToLower(OmniIdentity), w) {
			t.Errorf("OmniIdentity should not contain coding-specific word %q", w)
		}
	}
}

// TestOmniBehaviorNoCodingWords verifies Omni base is truly domain-agnostic.
// After the slimdown, OmniBehavior must NOT reference any tool name, file concept,
// or domain-specific term. Only meta-cognitive and epistemological rules allowed.
func TestOmniBehaviorNoCodingWords(t *testing.T) {
	codingOnlyWords := []string{
		"codebase", "architecture", "edit_file",
		"run the test", "execute the script", "refactor",
		"error handling", "update_project_context",
		"Search broadly", "narrow down",
	}
	for _, w := range codingOnlyWords {
		if strings.Contains(OmniBehavior, w) {
			t.Errorf("OmniBehavior should not contain coding-specific content %q — belongs in CodingOverlay", w)
		}
	}
}

// TestCodingGuidelinesComplete verifies CodingOverlay received all migrated rules.
func TestCodingGuidelinesComplete(t *testing.T) {
	o := &CodingOverlay{}
	g := o.Guidelines()
	requiredContent := []string{
		"Coding tasks",
		"Comment policy",
		"premature abstraction",
		// Migrated from Omni:
		"Do not modify files",
		"Search broadly",
		"update_project_context",
		"run the test",
		"Don't add features",
		"error handling",
		// Axis declaration:
		"Axis:",
	}
	for _, rc := range requiredContent {
		if !strings.Contains(g, rc) {
			t.Errorf("CodingOverlay.Guidelines() missing %q — coding capability loss!", rc)
		}
	}
}

func TestResearchGuidelinesComplete(t *testing.T) {
	o := &ResearchOverlay{}
	g := o.Guidelines()
	requiredContent := []string{
		"Research tasks",
		"Cross-verify",
		"Survey",
		"Deep-dive",
		"Synthesis",
		"Axis:",
	}
	for _, rc := range requiredContent {
		if !strings.Contains(g, rc) {
			t.Errorf("ResearchOverlay.Guidelines() missing %q", rc)
		}
	}
}

func TestComposeGuidelinesMultiple(t *testing.T) {
	active := []BehaviorOverlay{&CodingOverlay{}, &ResearchOverlay{}}
	combined := ComposeGuidelines(active)
	if !strings.Contains(combined, "Coding tasks") {
		t.Error("combined guidelines should contain coding rules")
	}
	if !strings.Contains(combined, "Research tasks") {
		t.Error("combined guidelines should contain research rules")
	}
}

func TestSignalCoding(t *testing.T) {
	o := &CodingOverlay{}
	if !o.Signal("帮我 debug 这个函数", nil) {
		t.Error("'debug' should activate coding overlay")
	}
	if !o.Signal("", []string{"go.mod"}) {
		t.Error("go.mod should activate coding overlay")
	}
}

func TestSignalResearch(t *testing.T) {
	o := &ResearchOverlay{}
	if !o.Signal("研究这个项目给我报告", nil) {
		t.Error("'研究' + '报告' should activate research overlay")
	}
	if o.Signal("帮我修个bug", nil) {
		t.Error("'修bug' should NOT activate research overlay")
	}
}

func TestActiveOverlaysBoth(t *testing.T) {
	overlays := []BehaviorOverlay{&CodingOverlay{}, &ResearchOverlay{}}
	// "研究这个Go项目" + go.mod → both should activate
	active := ActiveOverlays(overlays, "研究这个Go项目给我报告", []string{"go.mod"})
	if len(active) != 2 {
		t.Errorf("expected 2 active overlays, got %d", len(active))
	}
}

func TestActiveOverlaysCodingOnly(t *testing.T) {
	overlays := []BehaviorOverlay{&CodingOverlay{}, &ResearchOverlay{}}
	active := ActiveOverlays(overlays, "帮我修个bug", []string{"go.mod"})
	if len(active) != 1 {
		t.Errorf("expected 1 active overlay (coding), got %d", len(active))
	}
	if active[0].Name() != "coding" {
		t.Errorf("expected coding, got %s", active[0].Name())
	}
}

// TestActiveOverlaysDefault verifies that when no signal fires,
// no overlay is activated (Omni base alone handles the request).
func TestActiveOverlaysDefault(t *testing.T) {
	overlays := []BehaviorOverlay{&CodingOverlay{}, &ResearchOverlay{}}
	// "你好" — no signals, no workspace files → should return empty
	active := ActiveOverlays(overlays, "你好", nil)
	if len(active) != 0 {
		t.Errorf("expected 0 overlays (Omni-only), got %d: %s", len(active), ActiveNames(active))
	}
}

func TestActiveNames(t *testing.T) {
	active := []BehaviorOverlay{&CodingOverlay{}, &ResearchOverlay{}}
	name := ActiveNames(active)
	if name != "coding+research" {
		t.Errorf("expected 'coding+research', got %q", name)
	}
}

func TestOverlayInterface(t *testing.T) {
	// Verify both implement BehaviorOverlay
	var _ BehaviorOverlay = &CodingOverlay{}
	var _ BehaviorOverlay = &ResearchOverlay{}
}

// TestOmniAloneSufficient verifies that Omni base alone produces
// a complete, usable prompt without any overlays.
func TestOmniAloneSufficient(t *testing.T) {
	identity := ComposeIdentity(nil)
	if !strings.Contains(identity, "NGOAgent") {
		t.Error("Omni-only identity should contain NGOAgent")
	}

	guidelines := ComposeGuidelines(nil)
	if guidelines != "" {
		t.Error("Omni-only guidelines should be empty (no overlays)")
	}

	tone := ComposeTone(nil)
	if !strings.Contains(tone, "Tone and style") {
		t.Error("Omni-only tone should contain OmniTone")
	}
	if !strings.Contains(tone, "Output efficiency") {
		t.Error("Omni-only tone should contain OmniOutputEfficiency")
	}
}

// TestOverlayAxisDeclared verifies every overlay declares its governance axis.
func TestOverlayAxisDeclared(t *testing.T) {
	overlays := []BehaviorOverlay{&CodingOverlay{}, &ResearchOverlay{}}
	for _, o := range overlays {
		g := o.Guidelines()
		if !strings.Contains(g, "Axis:") {
			t.Errorf("%s overlay Guidelines() must declare governance axis with 'Axis:' line", o.Name())
		}
	}
}
