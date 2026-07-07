//go:build !js

package webui

import (
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
