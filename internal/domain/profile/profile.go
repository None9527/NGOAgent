// Package profile defines the BehaviorOverlay interface and Omni base layer.
//
// Architecture: Omni (universal base) + composable Overlays
//
//	Prompt = OmniIdentity + Σ(active overlay IdentityTags)
//	       + OmniBehavior + Σ(active overlay Guidelines)
//	       + OmniTone     + Σ(active overlay ToneRules)
//
// Multiple overlays can be active simultaneously.
// Example: coding + research both active for "研究这个Go项目给我报告".
package profile

import "strings"

// BehaviorOverlay extends the Omni base with domain-specific behavior.
// Multiple overlays can be active at the same time (composable, not exclusive).
//
// Design constraints for new overlays:
//   1. Each overlay declares a governance axis (file interaction, information organization, etc.)
//   2. Guidelines() rules MUST use context qualifiers ("When doing X, ...") to avoid
//      conflicts when multiple overlays are active simultaneously.
//   3. Two overlays MUST NOT define contradictory absolute rules on the same axis.
//   4. IdentityTag() describes capabilities, not restrictions.
//   5. Overlays that only provide task knowledge (not behavioral changes) should be
//      implemented as Skills instead — loaded on-demand via skill(name="X").
type BehaviorOverlay interface {
	// Name returns the overlay identifier (e.g. "coding", "research", "ops").
	Name() string

	// Signal returns true if this overlay should activate based on context.
	Signal(userMessage string, workspaceFiles []string) bool

	// IdentityTag returns text appended to OmniIdentity.
	// Keep short — multiple tags will be joined with ", ".
	IdentityTag() string

	// Guidelines returns domain-specific task execution rules.
	// Multiple overlays' guidelines are concatenated (additive).
	Guidelines() string

	// ToneRules returns domain-specific formatting/style rules.
	// Appended to OmniTone.
	ToneRules() string
}

// ═══════════════════════════════════════════
// Overlay activation: detect which overlays should be active
// ═══════════════════════════════════════════

// ActiveOverlays returns all overlays whose Signal returns true.
// Multiple overlays can activate simultaneously.
func ActiveOverlays(overlays []BehaviorOverlay, userMessage string, workspaceFiles []string) []BehaviorOverlay {
	var active []BehaviorOverlay
	for _, o := range overlays {
		if o.Signal(userMessage, workspaceFiles) {
			active = append(active, o)
		}
	}
	// No default fallback — Omni base alone is sufficient for non-domain tasks.
	// CodingOverlay.Signal fires via workspace file detection for all real coding scenarios.
	return active
}

// ═══════════════════════════════════════════
// Composition: combine Omni + active overlays into final sections
// ═══════════════════════════════════════════

// ComposeIdentity builds the full identity from Omni + active overlay tags.
func ComposeIdentity(active []BehaviorOverlay) string {
	if len(active) == 0 {
		return OmniIdentity
	}
	var tags []string
	for _, o := range active {
		if tag := o.IdentityTag(); tag != "" {
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		return OmniIdentity
	}
	return OmniIdentity + " Specialized in " + strings.Join(tags, " and ") + "."
}

// ComposeGuidelines builds task guidelines from all active overlays.
func ComposeGuidelines(active []BehaviorOverlay) string {
	var parts []string
	for _, o := range active {
		if g := o.Guidelines(); g != "" {
			parts = append(parts, g)
		}
	}
	return strings.Join(parts, "\n\n")
}

// ComposeTone builds tone rules from Omni + all active overlay tone rules.
func ComposeTone(active []BehaviorOverlay) string {
	base := OmniTone
	for _, o := range active {
		if t := o.ToneRules(); t != "" {
			base += "\n" + t
		}
	}
	return base + "\n\n" + OmniOutputEfficiency
}

// ActiveNames returns the names of all active overlays (for logging).
func ActiveNames(active []BehaviorOverlay) string {
	if len(active) == 0 {
		return "none"
	}
	names := make([]string, len(active))
	for i, o := range active {
		names[i] = o.Name()
	}
	return strings.Join(names, "+")
}

// ── Legacy compatibility ──

// DomainProfile is kept as an alias for backward compatibility.
// New code should use BehaviorOverlay.
type DomainProfile = BehaviorOverlay

// FindProfile looks up an overlay by name.
func FindProfile(overlays []BehaviorOverlay, name string) BehaviorOverlay {
	for _, o := range overlays {
		if o.Name() == name {
			return o
		}
	}
	return nil
}

// DetectProfile returns the primary profile name (legacy compat).
func DetectProfile(overlays []BehaviorOverlay, msg string, files []string) string {
	active := ActiveOverlays(overlays, msg, files)
	if len(active) > 0 {
		return active[0].Name()
	}
	return "coding"
}
