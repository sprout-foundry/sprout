package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func newTestManageSettingsHandler() *manageSettingsHandler {
	return &manageSettingsHandler{}
}

func newTestConfigManager(cfg *configuration.Config) *configuration.Manager {
	return configuration.NewManagerWithConfig(cfg, nil)
}

func newTestToolEnv(mgr *configuration.Manager) ToolEnv {
	return ToolEnv{
		EventBus:      events.NewEventBus(),
		ConfigManager: mgr,
	}
}

// ---------------------------------------------------------------------------
// Validate tests
// ---------------------------------------------------------------------------

func TestManageSettingsValidate_OperationRequired(t *testing.T) {
	h := newTestManageSettingsHandler()
	err := h.Validate(map[string]any{})
	if err == nil {
		t.Fatal("expected error when operation is missing")
	}
	if !strings.Contains(err.Error(), "operation") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestManageSettingsValidate_GetRequiresKey(t *testing.T) {
	h := newTestManageSettingsHandler()
	err := h.Validate(map[string]any{"operation": "get"})
	if err == nil {
		t.Fatal("expected error when key is missing for get")
	}
}

func TestManageSettingsValidate_SetRequiresKeyValue(t *testing.T) {
	h := newTestManageSettingsHandler()
	err := h.Validate(map[string]any{"operation": "set", "key": "provider"})
	if err == nil {
		t.Fatal("expected error when value is missing for set")
	}
}

func TestManageSettingsValidate_ListProvidersNoParams(t *testing.T) {
	h := newTestManageSettingsHandler()
	err := h.Validate(map[string]any{"operation": "list_providers"})
	if err != nil {
		t.Errorf("list_providers should not require params, got: %v", err)
	}
}

func TestManageSettingsValidate_DescribeAllNoParams(t *testing.T) {
	h := newTestManageSettingsHandler()
	err := h.Validate(map[string]any{"operation": "describe_all"})
	if err != nil {
		t.Errorf("describe_all should not require params, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// handleListProviders tests
// ---------------------------------------------------------------------------

func TestManageSettingsListProviders_NoFilter(t *testing.T) {
	cfg := &configuration.Config{
		ProviderModels: make(map[string]string),
	}
	mgr := newTestConfigManager(cfg)
	env := newTestToolEnv(mgr)
	h := newTestManageSettingsHandler()

	result, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "list_providers",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain the provider list header
	if !strings.Contains(result.Output, "Available providers") {
		t.Errorf("expected provider list header, got: %s", result.Output)
	}
	// Should NOT contain model listings (no filter provided)
	if strings.Contains(result.Output, "Models for") {
		t.Errorf("should not show models without provider filter, got: %s", result.Output)
	}
}

func TestManageSettingsListProviders_WithFilter_ShowsModels(t *testing.T) {
	cfg := &configuration.Config{
		ProviderModels: make(map[string]string),
	}
	mgr := newTestConfigManager(cfg)
	env := newTestToolEnv(mgr)
	h := newTestManageSettingsHandler()

	result, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "list_providers",
		"provider":  "openai",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain the provider list header
	if !strings.Contains(result.Output, "Available providers") {
		t.Errorf("expected provider list header, got: %s", result.Output)
	}

	// When a specific provider filter is given, should show models from the factory
	// The GlobalFactory loads embedded configs, so openai should have models
	if strings.Contains(result.Output, "openai") && !strings.Contains(result.Output, "No providers available") {
		// If openai is in the available providers, it should also show models
		if !strings.Contains(result.Output, "Models for") {
			t.Errorf("expected model listing for filtered provider, got: %s", result.Output)
		}
	}
}

func TestManageSettingsListProviders_FilterNoMatch(t *testing.T) {
	cfg := &configuration.Config{
		ProviderModels: make(map[string]string),
	}
	mgr := newTestConfigManager(cfg)
	env := newTestToolEnv(mgr)
	h := newTestManageSettingsHandler()

	result, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "list_providers",
		"provider":  "nonexistent_provider_xyz",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should show 0 providers or no matching provider
	if strings.Contains(result.Output, "Models for") {
		t.Errorf("should not show models for non-matching filter, got: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// handleGet tests
// ---------------------------------------------------------------------------

func TestManageSettingsGet_Provider(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "openai",
		ProviderModels:   map[string]string{"openai": "gpt-4"},
	}
	mgr := newTestConfigManager(cfg)
	env := newTestToolEnv(mgr)
	h := newTestManageSettingsHandler()

	result, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "get",
		"key":       "provider",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "openai" {
		t.Errorf("expected 'openai', got %q", result.Output)
	}
}

func TestManageSettingsGet_NotSet(t *testing.T) {
	cfg := &configuration.Config{
		ProviderModels: make(map[string]string),
	}
	mgr := newTestConfigManager(cfg)
	env := newTestToolEnv(mgr)
	h := newTestManageSettingsHandler()

	result, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "get",
		"key":       "provider",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Output, "not set") {
		t.Errorf("expected 'not set' message, got %q", result.Output)
	}
}

// ---------------------------------------------------------------------------
// handleSet tests
// ---------------------------------------------------------------------------

func TestManageSettingsSet_Provider(t *testing.T) {
	cfg := &configuration.Config{
		ProviderModels: make(map[string]string),
	}
	mgr := newTestConfigManager(cfg)
	env := newTestToolEnv(mgr)
	h := newTestManageSettingsHandler()

	result, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "set",
		"key":       "provider",
		"value":     "openai",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Output, "updated") {
		t.Errorf("expected 'updated' message, got %q", result.Output)
	}
	// Verify the config was actually updated
	updatedCfg := mgr.GetConfig()
	if updatedCfg.LastUsedProvider != "openai" {
		t.Errorf("expected provider to be 'openai', got %q", updatedCfg.LastUsedProvider)
	}
}

func TestManageSettingsSet_InvalidKey(t *testing.T) {
	cfg := &configuration.Config{
		ProviderModels: make(map[string]string),
	}
	mgr := newTestConfigManager(cfg)
	env := newTestToolEnv(mgr)
	h := newTestManageSettingsHandler()

	result, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "set",
		"key":       "invalid_key_xyz",
		"value":     "some_value",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected error for invalid key, got output: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// handleDescribeAll tests
// ---------------------------------------------------------------------------

func TestManageSettingsDescribeAll(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "openai",
		ProviderModels:   map[string]string{"openai": "gpt-4"},
	}
	mgr := newTestConfigManager(cfg)
	env := newTestToolEnv(mgr)
	h := newTestManageSettingsHandler()

	result, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "describe_all",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Output, "Settings Overview") {
		t.Errorf("expected 'Settings Overview' header, got: %s", result.Output)
	}
	// Should list at least some known keys
	if !strings.Contains(result.Output, "provider:") {
		t.Errorf("expected 'provider:' in output, got: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// handleTestCredential tests
// ---------------------------------------------------------------------------

func TestManageSettingsTestCredential_EmptyProvider(t *testing.T) {
	h := newTestManageSettingsHandler()
	result, err := h.Execute(context.Background(), ToolEnv{}, map[string]any{
		"operation": "test_credential",
		"provider":  "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected error for empty provider, got: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// handlePreview tests
// ---------------------------------------------------------------------------

func TestManageSettingsPreview_Provider(t *testing.T) {
	cfg := &configuration.Config{
		LastUsedProvider: "openai",
		ProviderModels:   map[string]string{"openai": "gpt-4"},
	}
	mgr := newTestConfigManager(cfg)
	env := newTestToolEnv(mgr)
	h := newTestManageSettingsHandler()

	result, err := h.Execute(context.Background(), env, map[string]any{
		"operation": "preview",
		"key":       "provider",
		"value":     "anthropic",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Output, "Preview") {
		t.Errorf("expected 'Preview' in output, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Current:") {
		t.Errorf("expected 'Current:' in output, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "Proposed:") {
		t.Errorf("expected 'Proposed:' in output, got: %s", result.Output)
	}
}

// ---------------------------------------------------------------------------
// ToolDefinition tests
// ---------------------------------------------------------------------------

func TestManageSettingsDefinition(t *testing.T) {
	h := newTestManageSettingsHandler()
	def := h.Definition()

	if def.Name != "manage_settings" {
		t.Errorf("expected name 'manage_settings', got %q", def.Name)
	}

	// Check that description mentions model listing
	if !strings.Contains(def.Description, "model") {
		t.Errorf("description should mention models, got: %s", def.Description)
	}

	// Check provider parameter description mentions models
	providerParamDesc := ""
	for _, p := range def.Parameters {
		if p.Name == "provider" {
			providerParamDesc = p.Description
			break
		}
	}
	if !strings.Contains(providerParamDesc, "model") {
		t.Errorf("provider parameter description should mention models, got: %s", providerParamDesc)
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestValidateSettingKey(t *testing.T) {
	validKeys := []string{"provider", "model", "reasoning_effort", "disable_thinking", "subagent_provider"}
	for _, key := range validKeys {
		if err := validateSettingKey(key); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", key, err)
		}
	}

	if err := validateSettingKey("invalid_key"); err == nil {
		t.Error("expected error for invalid key")
	}
}

func TestSetConfigField_ReasoningEffort(t *testing.T) {
	cfg := &configuration.Config{}

	// Valid values
	for _, v := range []string{"low", "medium", "high"} {
		if err := setConfigField(cfg, "reasoning_effort", v); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", v, err)
		}
	}

	// Invalid value
	if err := setConfigField(cfg, "reasoning_effort", "extreme"); err == nil {
		t.Error("expected error for invalid reasoning_effort value")
	}
}

func TestSetConfigField_DisableThinking(t *testing.T) {
	cfg := &configuration.Config{}

	if err := setConfigField(cfg, "disable_thinking", "true"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !cfg.DisableThinking {
		t.Error("expected DisableThinking to be true")
	}

	if err := setConfigField(cfg, "disable_thinking", "false"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cfg.DisableThinking {
		t.Error("expected DisableThinking to be false")
	}

	if err := setConfigField(cfg, "disable_thinking", "maybe"); err == nil {
		t.Error("expected error for invalid disable_thinking value")
	}
}

func TestSetConfigField_HistoryScope(t *testing.T) {
	cfg := &configuration.Config{}

	for _, v := range []string{"project", "global"} {
		if err := setConfigField(cfg, "history_scope", v); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", v, err)
		}
	}

	if err := setConfigField(cfg, "history_scope", "invalid"); err == nil {
		t.Error("expected error for invalid history_scope value")
	}
}

func TestSetConfigField_OutputVerbosity(t *testing.T) {
	cfg := &configuration.Config{}

	for _, v := range []string{"compact", "default", "verbose"} {
		if err := setConfigField(cfg, "output_verbosity", v); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", v, err)
		}
	}

	if err := setConfigField(cfg, "output_verbosity", "silent"); err == nil {
		t.Error("expected error for invalid output_verbosity value")
	}
}

func TestSetConfigField_DisabledPersonas(t *testing.T) {
	cfg := &configuration.Config{}

	if err := setConfigField(cfg, "disabled_personas", "coder, reviewer"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.DisabledPersonas) != 2 {
		t.Errorf("expected 2 personas, got %d", len(cfg.DisabledPersonas))
	}
	if cfg.DisabledPersonas[0] != "coder" {
		t.Errorf("expected first persona 'coder', got %q", cfg.DisabledPersonas[0])
	}
}

func TestSetConfigField_ModelRequiresProvider(t *testing.T) {
	cfg := &configuration.Config{
		ProviderModels: make(map[string]string),
	}

	// Should fail when no provider is selected
	if err := setConfigField(cfg, "model", "gpt-4"); err == nil {
		t.Error("expected error when setting model without provider")
	}

	// Should succeed when provider is selected
	cfg.LastUsedProvider = "openai"
	if err := setConfigField(cfg, "model", "gpt-4"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cfg.ProviderModels["openai"] != "gpt-4" {
		t.Errorf("expected model 'gpt-4' for openai, got %q", cfg.ProviderModels["openai"])
	}
}
