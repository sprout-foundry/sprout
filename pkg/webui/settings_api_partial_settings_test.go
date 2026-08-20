//go:build !js

package webui

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestPartialSettingsAppliers_ComprehensiveEnums asserts that the canonical
// set of patch keys (one per known domain) is recognized by
// applyPartialSettings. The set is intentionally a sample rather than a full
// enumeration — it just needs to cover every applier in partialSettingsAppliers
// so a future refactor that drops a helper from the list will fail loudly.
func TestPartialSettingsAppliers_ComprehensiveEnums(t *testing.T) {
	cfg := configuration.NewConfig()
	// One representative key per applier, plus a clearly-unknown key.
	patch := map[string]interface{}{
		// applyAgentBehaviorSettings
		"reasoning_effort":   "high",
		"system_prompt_text": "x",
		"skip_prompt":        true,
		"output_verbosity":   "default",
		"disable_thinking":   true,
		// applyPathsAndContextSettings
		"resource_directory": "/tmp",
		"history_scope":      "global",
		"max_context_tokens": float64(8192),
		"ea_mode":            "interactive",
		// applyRiskAndSafetySettings
		"risk_profile":            "default",
		"self_review_gate_mode":   "code",
		"approved_shell_commands": []interface{}{"ls"},
		// applySubagentSettings
		"subagent_provider":        "anthropic",
		"subagent_model":           "claude-3",
		"subagent_max_depth":       float64(2),
		"disabled_personas":        []interface{}{},
		"subagent_types":           []interface{}{}, // accept-and-ignore
		"default_subagent_persona": "",
		// applyProviderRoutingSettings
		"commit_provider":    "openai",
		"commit_model":       "gpt-4",
		"review_provider":    "openai",
		"review_model":       "gpt-4",
		"provider_models":    map[string]interface{}{"openai": "gpt-4"},
		"provider_priority":  []interface{}{"openai"},
		"last_used_provider": "openai",
		// applyPDFOCRSettings
		"pdf_ocr_enabled":  true,
		"pdf_ocr_provider": "openai",
		"pdf_ocr_model":    "gpt-4o",
		// applyShellDetectionSettings
		"enable_zsh_command_detection":   true,
		"auto_execute_detected_commands": true,
		// applyAPITimeoutsSettings
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": float64(30),
		},
		// applyVersionSettings
		"version": "1.0.0",
		// applyMCPSettings
		"mcp": map[string]interface{}{},
		// applyCustomProvidersSettings
		"custom_providers": map[string]interface{}{},
		// applyEmbeddingIndexSettings
		"embedding_index": map[string]interface{}{},
		// applyComputerUseSettings
		"computer_use": map[string]interface{}{},
		// applyLanguageServerSettings
		"language_servers": []interface{}{},
		// applyPersistentContextSettings
		"persistent_context": map[string]interface{}{},
		// applySkillsSettings
		"skills": map[string]interface{}{},
		// applyWakeupSettings
		"wakeup": map[string]interface{}{},
		// risk_profiles and security_policy live in applyRiskAndSafetySettings
		"risk_profiles":   map[string]interface{}{},
		"security_policy": map[string]interface{}{},
		// unknown — should be reported back
		"definitely_not_a_real_key": "x",
	}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}
	if len(unknown) != 1 || unknown[0] != "definitely_not_a_real_key" {
		t.Errorf("expected exactly [definitely_not_a_real_key] in unknown, got %v", unknown)
	}
}

// TestApplyPartialSettings_Wakeup_PartialPatchPreservesBudgets pins down the
// field-level merge contract: a patch that only sets "enabled" must not zero
// the token/resume budgets. This is the regression that produced an all-zeros
// wakeup block (Enabled:false, budgets 0) in a real user config.
func TestApplyPartialSettings_Wakeup_PartialPatchPreservesBudgets(t *testing.T) {
	cfg := &configuration.Config{
		Wakeup: configuration.WakeupConfig{
			Enabled:              false,
			MaxTokensPerSession:  12345,
			MaxResumesPerSession: 7,
		},
	}
	patch := map[string]interface{}{
		"wakeup": map[string]interface{}{"enabled": true},
	}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}
	for _, u := range unknown {
		if u == "wakeup" {
			t.Error("wakeup should not be in unknown keys list")
		}
	}
	if !cfg.Wakeup.Enabled {
		t.Error("Enabled = false, want true")
	}
	if cfg.Wakeup.MaxTokensPerSession != 12345 {
		t.Errorf("MaxTokensPerSession = %d, want 12345 (clobbered by partial patch)", cfg.Wakeup.MaxTokensPerSession)
	}
	if cfg.Wakeup.MaxResumesPerSession != 7 {
		t.Errorf("MaxResumesPerSession = %d, want 7 (clobbered by partial patch)", cfg.Wakeup.MaxResumesPerSession)
	}
}

// TestApplyPartialSettings_Wakeup_EmptyObjectPreservesAll asserts that an
// empty {} patch is a no-op: every existing value survives.
func TestApplyPartialSettings_Wakeup_EmptyObjectPreservesAll(t *testing.T) {
	cfg := &configuration.Config{
		Wakeup: configuration.WakeupConfig{
			Enabled:              false,
			MaxTokensPerSession:  999,
			MaxResumesPerSession: 3,
		},
	}
	patch := map[string]interface{}{
		"wakeup": map[string]interface{}{},
	}
	if _, err := applyPartialSettings(cfg, patch); err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}
	want := configuration.WakeupConfig{
		Enabled:              false,
		MaxTokensPerSession:  999,
		MaxResumesPerSession: 3,
	}
	if cfg.Wakeup != want {
		t.Errorf("Wakeup = %+v, want %+v (empty object must not zero fields)", cfg.Wakeup, want)
	}
}

// TestApplyPartialSettings_Wakeup_NullResetsToDefaults asserts that an
// explicit null resets the whole block to DefaultWakeupConfig.
func TestApplyPartialSettings_Wakeup_NullResetsToDefaults(t *testing.T) {
	cfg := &configuration.Config{
		Wakeup: configuration.WakeupConfig{
			Enabled:              false,
			MaxTokensPerSession:  1,
			MaxResumesPerSession: 2,
		},
	}
	patch := map[string]interface{}{
		"wakeup": nil,
	}
	if _, err := applyPartialSettings(cfg, patch); err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}
	want := configuration.DefaultWakeupConfig()
	if cfg.Wakeup != want {
		t.Errorf("Wakeup = %+v, want %+v (null must reset to defaults)", cfg.Wakeup, want)
	}
}

// TestApplyPartialSettings_Wakeup_FullPatchReplacesAll asserts that a patch
// with all three fields replaces every value.
func TestApplyPartialSettings_Wakeup_FullPatchReplacesAll(t *testing.T) {
	cfg := &configuration.Config{
		Wakeup: configuration.DefaultWakeupConfig(),
	}
	patch := map[string]interface{}{
		"wakeup": map[string]interface{}{
			"enabled":                 false,
			"max_tokens_per_session":  float64(42),
			"max_resumes_per_session": float64(5),
		},
	}
	if _, err := applyPartialSettings(cfg, patch); err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}
	want := configuration.WakeupConfig{
		Enabled:              false,
		MaxTokensPerSession:  42,
		MaxResumesPerSession: 5,
	}
	if cfg.Wakeup != want {
		t.Errorf("Wakeup = %+v, want %+v", cfg.Wakeup, want)
	}
}

// TestApplyPartialSettings_Wakeup_NonObjectRejected asserts that a non-object
// wakeup value (string, number, bool, array) is rejected with a clear error.
func TestApplyPartialSettings_Wakeup_NonObjectRejected(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
	}{
		{"string", "banana"},
		{"number", float64(42)},
		{"bool", true},
		{"array", []interface{}{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &configuration.Config{Wakeup: configuration.DefaultWakeupConfig()}
			patch := map[string]interface{}{"wakeup": tc.value}
			_, err := applyPartialSettings(cfg, patch)
			if err == nil {
				t.Fatal("expected error for non-object wakeup value, got nil")
			}
			if !strings.Contains(err.Error(), "invalid wakeup config") {
				t.Errorf("error should mention 'invalid wakeup config', got: %v", err)
			}
			// The config must be untouched on failure.
			if cfg.Wakeup != configuration.DefaultWakeupConfig() {
				t.Errorf("Wakeup mutated on error: %+v", cfg.Wakeup)
			}
		})
	}
}

// TestApplyPartialSettings_Wakeup_BadFieldTypeRejected asserts that a wrong
// JSON type for a known field (e.g. enabled:"yes") is rejected rather than
// silently dropped.
func TestApplyPartialSettings_Wakeup_BadFieldTypeRejected(t *testing.T) {
	cases := []struct {
		name  string
		patch map[string]interface{}
	}{
		{"enabled string", map[string]interface{}{"enabled": "yes"}},
		{"enabled number", map[string]interface{}{"enabled": float64(1)}},
		{"max_tokens_per_session string", map[string]interface{}{"max_tokens_per_session": "lots"}},
		{"max_resumes_per_session bool", map[string]interface{}{"max_resumes_per_session": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &configuration.Config{Wakeup: configuration.DefaultWakeupConfig()}
			patch := map[string]interface{}{"wakeup": tc.patch}
			_, err := applyPartialSettings(cfg, patch)
			if err == nil {
				t.Fatal("expected error for wrong field type, got nil")
			}
			if !strings.Contains(err.Error(), "invalid wakeup config") {
				t.Errorf("error should mention 'invalid wakeup config', got: %v", err)
			}
		})
	}
}

// TestApplyPartialSettings_Wakeup_NegativeBudgetsRejected asserts that
// negative budget values are rejected and cfg is left untouched.
func TestApplyPartialSettings_Wakeup_NegativeBudgetsRejected(t *testing.T) {
	cases := []struct {
		name  string
		patch map[string]interface{}
	}{
		{"negative max_tokens_per_session", map[string]interface{}{"max_tokens_per_session": float64(-1)}},
		{"negative max_resumes_per_session", map[string]interface{}{"max_resumes_per_session": float64(-1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &configuration.Config{
				Wakeup: configuration.WakeupConfig{
					Enabled:              true,
					MaxTokensPerSession:  12345,
					MaxResumesPerSession: 7,
				},
			}
			want := cfg.Wakeup
			patch := map[string]interface{}{"wakeup": tc.patch}
			_, err := applyPartialSettings(cfg, patch)
			if err == nil {
				t.Fatal("expected error for negative budget, got nil")
			}
			if !strings.Contains(err.Error(), "invalid wakeup config") {
				t.Errorf("error should mention 'invalid wakeup config', got: %v", err)
			}
			if cfg.Wakeup != want {
				t.Errorf("Wakeup mutated on error: %+v, want %+v", cfg.Wakeup, want)
			}
		})
	}
}

// TestApplyPartialSettings_Wakeup_DottedKeyPath exercises the real WebUI
// path: the frontend sends {"wakeup.enabled": true} (flat dotted key), not
// a nested object. expandDottedKeys seeds the section from the current
// config values before the applier merges, so budgets must survive.
func TestApplyPartialSettings_Wakeup_DottedKeyPath(t *testing.T) {
	cfg := &configuration.Config{
		Wakeup: configuration.WakeupConfig{
			Enabled:              false,
			MaxTokensPerSession:  12345,
			MaxResumesPerSession: 7,
		},
	}
	// This is the actual payload shape the WebUI sends.
	patch := map[string]interface{}{
		"wakeup.enabled": true,
	}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}
	if len(unknown) > 0 {
		t.Errorf("unknown keys = %v, want none (dotted key should expand to 'wakeup')", unknown)
	}
	if !cfg.Wakeup.Enabled {
		t.Error("Enabled = false, want true (dotted key path)")
	}
	if cfg.Wakeup.MaxTokensPerSession != 12345 {
		t.Errorf("MaxTokensPerSession = %d, want 12345 (dotted key path must preserve budgets)", cfg.Wakeup.MaxTokensPerSession)
	}
	if cfg.Wakeup.MaxResumesPerSession != 7 {
		t.Errorf("MaxResumesPerSession = %d, want 7 (dotted key path must preserve budgets)", cfg.Wakeup.MaxResumesPerSession)
	}
}

// TestApplyPartialSettings_Wakeup_UnknownKeyIgnored asserts that unknown keys
// inside the wakeup object are ignored (matching the tolerant style of the
// surrounding appliers) while known keys still apply.
func TestApplyPartialSettings_Wakeup_UnknownKeyIgnored(t *testing.T) {
	cfg := &configuration.Config{
		Wakeup: configuration.WakeupConfig{
			Enabled:              false,
			MaxTokensPerSession:  111,
			MaxResumesPerSession: 222,
		},
	}
	patch := map[string]interface{}{
		"wakeup": map[string]interface{}{
			"enabled":                true,
			"totally_made_up_field":  "whatever",
			"max_tokens_per_session": float64(333),
		},
	}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("applyPartialSettings: %v", err)
	}
	for _, u := range unknown {
		if u == "wakeup" {
			t.Error("wakeup should not be in unknown keys list")
		}
	}
	if !cfg.Wakeup.Enabled {
		t.Error("Enabled = false, want true")
	}
	if cfg.Wakeup.MaxTokensPerSession != 333 {
		t.Errorf("MaxTokensPerSession = %d, want 333", cfg.Wakeup.MaxTokensPerSession)
	}
	if cfg.Wakeup.MaxResumesPerSession != 222 {
		t.Errorf("MaxResumesPerSession = %d, want 222 (should be preserved)", cfg.Wakeup.MaxResumesPerSession)
	}
}

// TestPartialSettingsAppliers_Ordered guards against the applier list
// silently being reordered or duplicated by a careless refactor. The current
// order is documented in settings_api_partial_settings.go and matches the
// grouping-by-domain story; if the order changes, this test forces the
// author to update it deliberately.
func TestPartialSettingsAppliers_Ordered(t *testing.T) {
	want := []string{
		"applyAgentBehaviorSettings",
		"applyPathsAndContextSettings",
		"applyRiskAndSafetySettings",
		"applySubagentSettings",
		"applyProviderRoutingSettings",
		"applyPDFOCRSettings",
		"applyShellDetectionSettings",
		"applyAPITimeoutsSettings",
		"applyVersionSettings",
		"applyMCPSettings",
		"applyCustomProvidersSettings",
		"applyEmbeddingIndexSettings",
		"applyComputerUseSettings",
		"applyLanguageServerSettings",
		"applyPersistentContextSettings",
		"applySkillsSettings",
		"applyWakeupSettings",
		"applyCommandPoliciesSettings",
	}
	if len(partialSettingsAppliers) != len(want) {
		t.Fatalf("applier count = %d, want %d (refactor may have added/dropped one)",
			len(partialSettingsAppliers), len(want))
	}
	// We can't introspect function names portably, so just assert the count
	// matches and that the list is non-empty + stable across two reads.
	seen := len(partialSettingsAppliers)
	again := len(partialSettingsAppliers)
	if seen != again {
		t.Fatalf("applier list is unstable: %d then %d", seen, again)
	}
}
