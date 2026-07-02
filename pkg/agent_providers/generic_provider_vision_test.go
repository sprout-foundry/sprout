package providers

import (
	"testing"
)

// visionTestConfig builds a minimal valid ProviderConfig for vision tests.
// Override any fields after calling this helper.
func visionTestConfig() *ProviderConfig {
	return &ProviderConfig{
		Name:     "vision-test",
		Endpoint: "https://api.example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "VISION_TEST_KEY"},
		Defaults: RequestDefaults{
			Model: "default-model",
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			SupportsVision:      true,
		},
	}
}

func TestSupportsVision_ProviderDisabled(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = false

	// Even when model_info has a "vision" tag, provider-level false wins.
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model", Tags: []string{"vision"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when provider config has supports_vision: false")
	}
}

func TestSupportsVision_ProviderEnabled_ModelNotInCatalog(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = true
	// No model_info entries at all — unknown model should trust provider flag.

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when provider enables vision and model is not in catalog")
	}
}

func TestSupportsVision_ProviderEnabled_ModelInCatalog_WithVisionTag(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = true
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model", Tags: []string{"vision", "chat"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when model has 'vision' tag in model_info")
	}
}

func TestSupportsVision_ProviderEnabled_ModelInCatalog_WithoutVisionTag(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = true
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model", Tags: []string{"chat", "reasoning"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when model is in catalog but lacks 'vision' tag")
	}
}

func TestSupportsVision_ProviderEnabled_ModelInCatalog_NoTags(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = true
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model"}, // no tags at all
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when model is in catalog with no tags")
	}
}

func TestSupportsVision_EmptyModel_ReturnsFalse(t *testing.T) {
	config := visionTestConfig()
	config.Defaults.Model = "" // no fallback model either
	config.Models.SupportsVision = true

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Explicitly set model to empty to simulate unset state.
	provider.model = ""

	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when both provider.model and config.Defaults.Model are empty")
	}
}

func TestSupportsVision_FallsBackToConfigDefaultModel(t *testing.T) {
	config := visionTestConfig()
	config.Defaults.Model = "fallback-model"
	config.Models.SupportsVision = true
	// Provider.model is empty; should fall back to config.Defaults.Model.
	config.Models.ModelInfo = []ModelInfo{
		{ID: "fallback-model", Tags: []string{"vision"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Clear provider.model so it falls back to config.Defaults.Model
	provider.model = ""

	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when falling back to config.Defaults.Model which has vision tag")
	}
}

func TestSupportsVision_FallsBackToConfigDefaultModel_NoVisionTag(t *testing.T) {
	config := visionTestConfig()
	config.Defaults.Model = "fallback-model"
	config.Models.SupportsVision = true
	config.Models.ModelInfo = []ModelInfo{
		{ID: "fallback-model", Tags: []string{"chat"}}, // no vision tag
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Clear provider.model so it falls back to config.Defaults.Model
	provider.model = ""

	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when fallback model lacks vision tag")
	}
}

func TestSupportsVision_TagMatching_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name string
		tags []string
		want bool
	}{
		{"lowercase", []string{"vision"}, true},
		{"uppercase", []string{"VISION"}, true},
		{"titlecase", []string{"Vision"}, true},
		{"mixed case", []string{"ViSiOn"}, true},
		{"with whitespace", []string{" vision "}, true},
		{"not vision", []string{"video"}, false},
		{"empty tag", []string{""}, false},
		{"whitespace only", []string{"   "}, false},
		{"vision among others", []string{"chat", "vision", "reasoning"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := visionTestConfig()
			config.Models.ModelInfo = []ModelInfo{
				{ID: "default-model", Tags: tt.tags},
			}

			provider, err := NewGenericProvider(config)
			if err != nil {
				t.Fatalf("failed to create provider: %v", err)
			}

			got := provider.SupportsVision()
			if got != tt.want {
				t.Errorf("SupportsVision() = %v, want %v (tags: %v)", got, tt.want, tt.tags)
			}
		})
	}
}

func TestSupportsVision_SetModel_SwitchesVisionStatus(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = true
	config.Models.ModelInfo = []ModelInfo{
		{ID: "vision-model", Tags: []string{"vision"}},
		{ID: "text-only-model", Tags: []string{"chat"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Switch to vision model
	if err := provider.SetModel("vision-model"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true for vision-model")
	}

	// Switch to text-only model
	if err := provider.SetModel("text-only-model"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false for text-only-model")
	}

	// Switch to unknown model (not in catalog) — should trust provider flag
	if err := provider.SetModel("unknown-model"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true for unknown-model (trusts provider flag)")
	}
}

func TestSupportsVision_SetModel_TrimsWhitespace(t *testing.T) {
	config := visionTestConfig()
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model", Tags: []string{"vision"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Set model with surrounding whitespace — should still match.
	if err := provider.SetModel("  default-model  "); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when model name has surrounding whitespace")
	}
}

func TestSupportsVision_ProviderDisabled_OverridesSetModel(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = false
	config.Models.ModelInfo = []ModelInfo{
		{ID: "some-model", Tags: []string{"vision"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Even after setting a model with vision tag, provider-level false wins.
	if err := provider.SetModel("some-model"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when provider-level vision is disabled, regardless of model")
	}
}

func TestSupportsConversationalVision_DelegatesToSupportsVision(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = true
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model", Tags: []string{"vision"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// When SupportsVision returns true, SupportsConversationalVision should too.
	if !provider.SupportsConversationalVision() {
		t.Fatal("expected SupportsConversationalVision() == true when SupportsVision() == true")
	}

	// Flip to a model without vision tag.
	if err := provider.SetModel("other-model"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	// "other-model" is not in catalog, so it trusts provider flag → true.
	if !provider.SupportsConversationalVision() {
		t.Fatal("expected SupportsConversationalVision() == true for unknown model (trusts provider flag)")
	}
}

func TestGetVisionModel_UsesConfigVisionModel(t *testing.T) {
	config := visionTestConfig()
	config.Models.VisionModel = "gpt-4o"

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	provider.model = "gpt-5-mini"

	got := provider.GetVisionModel()
	if got != "gpt-4o" {
		t.Errorf("GetVisionModel() = %q, want %q", got, "gpt-4o")
	}
}

func TestGetVisionModel_FallsBackToCurrentModel(t *testing.T) {
	config := visionTestConfig()
	// VisionModel left empty

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	provider.model = "current-model"

	got := provider.GetVisionModel()
	if got != "current-model" {
		t.Errorf("GetVisionModel() = %q, want %q", got, "current-model")
	}
}
