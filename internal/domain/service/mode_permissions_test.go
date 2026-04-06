package service

import "testing"

func TestModeFromString(t *testing.T) {
	tests := []struct {
		mode string
		evo  bool
		want ModePermissions
	}{
		{"auto", false, ModePermissions{Name: "auto"}},
		{"plan", false, ModePermissions{Name: "plan", ForcePlan: true}},
		{"agentic", false, ModePermissions{Name: "agentic", ForcePlan: true, AutoApprove: true, SelfReview: true, PhaseDetect: true}},
		// "evo" backward compat → auto+evo
		{"evo", false, ModePermissions{Name: "auto", EvoEnabled: true}},
		// explicit evo layer
		{"auto", true, ModePermissions{Name: "auto", EvoEnabled: true}},
		{"agentic", true, ModePermissions{Name: "agentic", ForcePlan: true, AutoApprove: true, SelfReview: true, PhaseDetect: true, EvoEnabled: true}},
		{"plan", true, ModePermissions{Name: "plan", ForcePlan: true, EvoEnabled: true}},
		// unknown → auto
		{"invalid", false, ModePermissions{Name: "auto"}},
		{"", false, ModePermissions{Name: "auto"}},
	}

	for _, tt := range tests {
		got := ModeFromString(tt.mode, tt.evo)
		if got != tt.want {
			t.Errorf("ModeFromString(%q, %v)\n  got  %+v\n  want %+v", tt.mode, tt.evo, got, tt.want)
		}
	}
}

func TestModeString(t *testing.T) {
	tests := []struct {
		mode ModePermissions
		want string
	}{
		{ModeFromString("auto", false), "auto"},
		{ModeFromString("plan", false), "plan"},
		{ModeFromString("agentic", false), "agentic"},
		{ModeFromString("evo", false), "auto+evo"},
		{ModeFromString("agentic", true), "agentic+evo"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestModePermissionMatrix(t *testing.T) {
	// Verify the exact permission matrix from the design doc
	auto := ModeFromString("auto", false)
	plan := ModeFromString("plan", false)
	agentic := ModeFromString("agentic", false)
	autoEvo := ModeFromString("auto", true)
	agenticEvo := ModeFromString("agentic", true)

	// auto: nothing forced
	if auto.ForcePlan || auto.AutoApprove || auto.SelfReview || auto.EvoEnabled {
		t.Error("auto should have all permissions false")
	}

	// plan: only ForcePlan
	if !plan.ForcePlan || plan.AutoApprove || plan.SelfReview || plan.EvoEnabled {
		t.Error("plan should only have ForcePlan=true")
	}

	// agentic: ForcePlan + AutoApprove + SelfReview + PhaseDetect, no evo
	if !agentic.ForcePlan || !agentic.AutoApprove || !agentic.SelfReview || !agentic.PhaseDetect || agentic.EvoEnabled {
		t.Error("agentic permissions incorrect")
	}

	// auto+evo: only evo
	if autoEvo.ForcePlan || autoEvo.AutoApprove || !autoEvo.EvoEnabled {
		t.Error("auto+evo should only have EvoEnabled=true")
	}

	// agentic+evo: all permissions
	if !agenticEvo.ForcePlan || !agenticEvo.AutoApprove || !agenticEvo.SelfReview || !agenticEvo.EvoEnabled {
		t.Error("agentic+evo should have all permissions true")
	}
}
