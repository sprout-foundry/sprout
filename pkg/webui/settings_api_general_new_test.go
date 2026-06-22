//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// applyPartialSettings — pure helper (config patching)
// ---------------------------------------------------------------------------

func TestApplyPartialSettings_ReasoningEffort(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"reasoning_effort": "high"}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q, want %q", cfg.ReasoningEffort, "high")
	}
	if len(unknown) != 0 {
		t.Errorf("expected no unknown keys, got %v", unknown)
	}
}

func TestApplyPartialSettings_InvalidReasoningEffort(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"reasoning_effort": "INVALID_VALUE"}
	_, err := applyPartialSettings(cfg, patch)
	if err == nil {
		t.Error("expected error for invalid reasoning_effort")
	}
}

func TestApplyPartialSettings_SystemPrompt(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"system_prompt_text": "custom prompt"}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SystemPromptText != "custom prompt" {
		t.Errorf("SystemPromptText = %q, want %q", cfg.SystemPromptText, "custom prompt")
	}
	if len(unknown) != 0 {
		t.Errorf("expected no unknown keys, got %v", unknown)
	}
}

func TestApplyPartialSettings_SkipPrompt(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"skip_prompt": true}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.SkipPrompt {
		t.Error("SkipPrompt should be true")
	}
}

func TestApplyPartialSettings_HistoryScope(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"history_scope": "global"}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HistoryScope != "global" {
		t.Errorf("HistoryScope = %q, want %q", cfg.HistoryScope, "global")
	}
}

func TestApplyPartialSettings_InvalidHistoryScope(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"history_scope": "bad_scope"}
	_, err := applyPartialSettings(cfg, patch)
	if err == nil {
		t.Error("expected error for invalid history_scope")
	}
}

func TestApplyPartialSettings_SelfReviewGateMode(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"self_review_gate_mode": "code"}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SelfReviewGateMode != "code" {
		t.Errorf("SelfReviewGateMode = %q, want %q", cfg.SelfReviewGateMode, "code")
	}
}

func TestApplyPartialSettings_InvalidSelfReviewGateMode(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"self_review_gate_mode": "invalid"}
	_, err := applyPartialSettings(cfg, patch)
	if err == nil {
		t.Error("expected error for invalid self_review_gate_mode")
	}
}

func TestApplyPartialSettings_SubagentFields(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"subagent_provider":      "anthropic",
		"subagent_model":         "claude-3",
		"subagent_max_parallel":  float64(5),
	}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SubagentProvider != "anthropic" {
		t.Errorf("SubagentProvider = %q, want %q", cfg.SubagentProvider, "anthropic")
	}
	if cfg.SubagentModel != "claude-3" {
		t.Errorf("SubagentModel = %q, want %q", cfg.SubagentModel, "claude-3")
	}
	if cfg.SubagentMaxParallel != 5 {
		t.Errorf("SubagentMaxParallel = %d, want 5", cfg.SubagentMaxParallel)
	}
	if len(unknown) != 0 {
		t.Errorf("expected no unknown keys, got %v", unknown)
	}
}

func TestApplyPartialSettings_ProviderModels(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"provider_models": map[string]interface{}{
			"openai":      "gpt-4",
			"anthropic":   "claude-3",
		},
	}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.ProviderModels) != 2 {
		t.Fatalf("expected 2 provider models, got %d", len(cfg.ProviderModels))
	}
	if cfg.ProviderModels["openai"] != "gpt-4" {
		t.Errorf("openai model = %q, want %q", cfg.ProviderModels["openai"], "gpt-4")
	}
}

func TestApplyPartialSettings_ProviderPriority(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"provider_priority": []interface{}{"openai", "anthropic", "google"},
	}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.ProviderPriority) != 3 {
		t.Fatalf("expected 3 priorities, got %d", len(cfg.ProviderPriority))
	}
	if cfg.ProviderPriority[0] != "openai" {
		t.Errorf("first priority = %q, want %q", cfg.ProviderPriority[0], "openai")
	}
}

func TestApplyPartialSettings_LastUsedProvider(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"last_used_provider": "openai"}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LastUsedProvider != "openai" {
		t.Errorf("LastUsedProvider = %q, want %q", cfg.LastUsedProvider, "openai")
	}
}

func TestApplyPartialSettings_Version(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"version": "2.0.0"}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", cfg.Version, "2.0.0")
	}
}

func TestApplyPartialSettings_ResourceDirectory(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"resource_directory": "/my/resources"}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ResourceDirectory != "/my/resources" {
		t.Errorf("ResourceDirectory = %q, want %q", cfg.ResourceDirectory, "/my/resources")
	}
}

func TestApplyPartialSettings_DisableThinking(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{"disable_thinking": true}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.DisableThinking {
		t.Error("DisableThinking should be true")
	}
}

func TestApplyPartialSettings_PDFOCRFields(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"pdf_ocr_enabled":  true,
		"pdf_ocr_provider": "openai",
		"pdf_ocr_model":    "gpt-4o",
	}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.PDFOCREnabled {
		t.Error("PDFOCREnabled should be true")
	}
	if cfg.PDFOCRProvider != "openai" {
		t.Errorf("PDFOCRProvider = %q, want %q", cfg.PDFOCRProvider, "openai")
	}
	if cfg.PDFOCRModel != "gpt-4o" {
		t.Errorf("PDFOCRModel = %q, want %q", cfg.PDFOCRModel, "gpt-4o")
	}
}

func TestApplyPartialSettings_CommitFields(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"commit_provider": "anthropic",
		"commit_model":    "claude-3",
	}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.CommitProvider != "anthropic" {
		t.Errorf("CommitProvider = %q", cfg.CommitProvider)
	}
	if cfg.CommitModel != "claude-3" {
		t.Errorf("CommitModel = %q", cfg.CommitModel)
	}
}

func TestApplyPartialSettings_ReviewFields(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"review_provider": "openai",
		"review_model":    "gpt-4o",
	}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReviewProvider != "openai" {
		t.Errorf("ReviewProvider = %q", cfg.ReviewProvider)
	}
}

func TestApplyPartialSettings_EnableZshDetection(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"enable_zsh_command_detection":     true,
		"auto_execute_detected_commands":   true,
	}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.EnableZshCommandDetection {
		t.Error("EnableZshCommandDetection should be true")
	}
	if !cfg.AutoExecuteDetectedCommands {
		t.Error("AutoExecuteDetectedCommands should be true")
	}
}

func TestApplyPartialSettings_APITimeouts(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": float64(30),
			"first_chunk_timeout_sec": float64(60),
			"chunk_timeout_sec":      float64(120),
			"overall_timeout_sec":    float64(600),
		},
	}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APITimeouts == nil {
		t.Fatal("APITimeouts should not be nil")
	}
	if cfg.APITimeouts.ConnectionTimeoutSec != 30 {
		t.Errorf("ConnectionTimeoutSec = %d, want 30", cfg.APITimeouts.ConnectionTimeoutSec)
	}
	if cfg.APITimeouts.FirstChunkTimeoutSec != 60 {
		t.Errorf("FirstChunkTimeoutSec = %d, want 60", cfg.APITimeouts.FirstChunkTimeoutSec)
	}
	if cfg.APITimeouts.ChunkTimeoutSec != 120 {
		t.Errorf("ChunkTimeoutSec = %d, want 120", cfg.APITimeouts.ChunkTimeoutSec)
	}
	if cfg.APITimeouts.OverallTimeoutSec != 600 {
		t.Errorf("OverallTimeoutSec = %d, want 600", cfg.APITimeouts.OverallTimeoutSec)
	}
}

func TestApplyPartialSettings_InvalidAPITimeout_Type(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": "not a number",
		},
	}
	_, err := applyPartialSettings(cfg, patch)
	if err == nil {
		t.Error("expected error for non-integer timeout")
	}
}

func TestApplyPartialSettings_InvalidAPITimeout_Zero(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": float64(0),
		},
	}
	_, err := applyPartialSettings(cfg, patch)
	if err == nil {
		t.Error("expected error for zero timeout")
	}
}

func TestApplyPartialSettings_APITimeoutsPartialKeys(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"api_timeouts": map[string]interface{}{
			"overall_timeout_sec": float64(300),
		},
	}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APITimeouts.OverallTimeoutSec != 300 {
		t.Errorf("OverallTimeoutSec = %d, want 300", cfg.APITimeouts.OverallTimeoutSec)
	}
}

func TestApplyPartialSettings_SubagentParallelEnabled(t *testing.T) {
	cfg := configuration.NewConfig()
	enabled := true
	patch := map[string]interface{}{
		"subagent_parallel_enabled": enabled,
	}
	_, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SubagentParallelEnabled == nil || !*cfg.SubagentParallelEnabled {
		t.Error("SubagentParallelEnabled should be true")
	}
}

func TestApplyPartialSettings_UnknownKeys(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"known_field": "value",
		"unknown_key": "value2",
	}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unknown) != 2 {
		t.Fatalf("expected 2 unknown keys, got %d: %v", len(unknown), unknown)
	}
}

func TestApplyPartialSettings_MixedKnownAndUnknown(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"reasoning_effort": "high",
		"bogus_key":       "value",
	}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q, want %q", cfg.ReasoningEffort, "high")
	}
	if len(unknown) != 1 || unknown[0] != "bogus_key" {
		t.Errorf("expected [\"bogus_key\"], got %v", unknown)
	}
}

func TestApplyPartialSettings_EmptyPatch(t *testing.T) {
	cfg := configuration.NewConfig()
	unknown, err := applyPartialSettings(cfg, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unknown) != 0 {
		t.Errorf("expected no unknown keys, got %v", unknown)
	}
}

func TestApplyPartialSettings_MultipleFields(t *testing.T) {
	cfg := configuration.NewConfig()
	patch := map[string]interface{}{
		"reasoning_effort":          "high",
		"system_prompt_text":        "my prompt",
		"skip_prompt":               false,
		"last_used_provider":        "openai",
		"enable_zsh_command_detection": true,
	}
	unknown, err := applyPartialSettings(cfg, patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(unknown) != 0 {
		t.Errorf("expected no unknown keys, got %v", unknown)
	}
	if cfg.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q", cfg.ReasoningEffort)
	}
	if cfg.SystemPromptText != "my prompt" {
		t.Errorf("SystemPromptText = %q", cfg.SystemPromptText)
	}
	if cfg.SkipPrompt {
		t.Error("SkipPrompt should be false")
	}
}

// ---------------------------------------------------------------------------
// expandNestedKeys — pure helper
// ---------------------------------------------------------------------------

func TestExpandNestedKeys_Nil(t *testing.T) {
	got := expandNestedKeys(nil)
	if got == nil {
		t.Fatal("nil map should return non-nil empty map")
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestExpandNestedKeys_Empty(t *testing.T) {
	got := expandNestedKeys(map[string]interface{}{})
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestExpandNestedKeys_Flat(t *testing.T) {
	m := map[string]interface{}{
		"a": "1",
		"b": "2",
	}
	got := expandNestedKeys(m)
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(got))
	}
	if got["a"] != "1" || got["b"] != "2" {
		t.Errorf("flat keys changed: %v", got)
	}
}

func TestExpandNestedKeys_OneLevelNested(t *testing.T) {
	m := map[string]interface{}{
		"top": "value",
		"nested": map[string]interface{}{
			"key": "val",
		},
	}
	got := expandNestedKeys(m)
	if got["top"] != "value" {
		t.Errorf("top = %v, want %q", got["top"], "value")
	}
	if got["nested.key"] != "val" {
		t.Errorf("nested.key = %v, want %q", got["nested.key"], "val")
	}
	// Original key should also be preserved
	if _, ok := got["nested"]; !ok {
		t.Error("original nested key should be preserved")
	}
}

func TestExpandNestedKeys_DeepNesting(t *testing.T) {
	m := map[string]interface{}{
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": float64(30),
		},
	}
	got := expandNestedKeys(m)
	if got["api_timeouts.connection_timeout_sec"] != float64(30) {
		t.Errorf("nested key = %v, want 30", got["api_timeouts.connection_timeout_sec"])
	}
}

func TestExpandNestedKeys_MultipleNested(t *testing.T) {
	m := map[string]interface{}{
		"api_timeouts": map[string]interface{}{
			"connection_timeout_sec": float64(30),
			"overall_timeout_sec":    float64(600),
		},
		"provider_models": map[string]interface{}{
			"openai": "gpt-4",
		},
	}
	got := expandNestedKeys(m)
	if got["api_timeouts.connection_timeout_sec"] != float64(30) {
		t.Errorf("got %v", got["api_timeouts.connection_timeout_sec"])
	}
	if got["api_timeouts.overall_timeout_sec"] != float64(600) {
		t.Errorf("got %v", got["api_timeouts.overall_timeout_sec"])
	}
	if got["provider_models.openai"] != "gpt-4" {
		t.Errorf("got %v", got["provider_models.openai"])
	}
}

// ---------------------------------------------------------------------------
// writeExternalPathConsentRequired — httptest
// ---------------------------------------------------------------------------

func TestWriteExternalPathConsentRequired_Basic(t *testing.T) {
	server := &ReactWebServer{}
	rec := httptest.NewRecorder()

	server.writeExternalPathConsentRequired(rec, "/outside/file.txt", "read")

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}
