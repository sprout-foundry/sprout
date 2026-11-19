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
			SupportsVision:      boolPtr(true),
		},
	}
}

func TestSupportsVision_ProviderOptOut(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = boolPtr(false)

	// Even when model_info has a "vision" tag, the provider-level opt-out wins.
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model", Tags: []string{"vision"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when provider config opts out with supports_vision: false")
	}
}

func TestSupportsVision_UnsetDefaultsToTrue(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = nil // absent from config

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when supports_vision is absent (vision is the default)")
	}
}

func TestSupportsVision_ModelNotInCatalog_AssumesVision(t *testing.T) {
	config := visionTestConfig()
	// No model_info entries at all — an uncatalogued model is assumed to see images.

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true for a model absent from the catalog")
	}
}

func TestSupportsVision_ModelInCatalog_WithVisionTag(t *testing.T) {
	config := visionTestConfig()
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

func TestSupportsVision_ModelInCatalog_UntaggedAssumesVision(t *testing.T) {
	config := visionTestConfig()
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model", Tags: []string{"chat", "reasoning"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// A tag list that names no modality is not a veto: the model is assumed
	// to see images and the send path verifies on first contact.
	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when model_info names no modality")
	}
}

func TestSupportsVision_ModelInCatalog_NoTags(t *testing.T) {
	config := visionTestConfig()
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model"}, // no tags at all
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when model is in catalog with no tags")
	}
}

func TestSupportsVision_ModelInCatalog_TextOnlyTag(t *testing.T) {
	config := visionTestConfig()
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model", Tags: []string{"chat", "text-only"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when model carries a text-only tag")
	}
}

func TestSupportsVision_TextOnlyTagBeatsVisionTag(t *testing.T) {
	config := visionTestConfig()
	config.Models.ModelInfo = []ModelInfo{
		{ID: "default-model", Tags: []string{"vision", "text-only"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// An explicit modality restriction is the more specific statement.
	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when a text-only tag accompanies a vision tag")
	}
}

func TestSupportsVision_EmptyModel_AssumesVision(t *testing.T) {
	config := visionTestConfig()
	config.Defaults.Model = "" // no fallback model either

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Explicitly set model to empty to simulate unset state.
	provider.mu.Lock()
	provider.model = ""
	provider.mu.Unlock()

	// No model to disqualify: vision stays assumed, and the send path
	// verifies on first contact.
	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when no model is resolved at all")
	}
}

func TestSupportsVision_FallsBackToConfigDefaultModel(t *testing.T) {
	config := visionTestConfig()
	config.Defaults.Model = "fallback-model"
	config.Models.SupportsVision = boolPtr(true)
	// Provider.model is empty; should fall back to config.Defaults.Model.
	config.Models.ModelInfo = []ModelInfo{
		{ID: "fallback-model", Tags: []string{"vision"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Clear provider.model so it falls back to config.Defaults.Model
	provider.mu.Lock()
	provider.model = ""
	provider.mu.Unlock()

	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when falling back to config.Defaults.Model which has vision tag")
	}
}

func TestSupportsVision_FallsBackToConfigDefaultModel_UntaggedAssumesVision(t *testing.T) {
	config := visionTestConfig()
	config.Defaults.Model = "fallback-model"
	config.Models.ModelInfo = []ModelInfo{
		{ID: "fallback-model", Tags: []string{"chat"}}, // names no modality
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Clear provider.model so it falls back to config.Defaults.Model
	provider.mu.Lock()
	provider.model = ""
	provider.mu.Unlock()

	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true when the fallback model names no modality")
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
		{"not vision", []string{"video"}, true},
		{"empty tag", []string{""}, true},
		{"whitespace only", []string{"   "}, true},
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
	config.Models.SupportsVision = boolPtr(true)
	config.Models.ModelInfo = []ModelInfo{
		{ID: "vision-model", Tags: []string{"vision"}},
		{ID: "text-only-model", Tags: []string{"chat", "text-only"}},
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

	// Switch to unknown model (not in catalog) — vision is assumed
	if err := provider.SetModel("unknown-model"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if !provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == true for unknown-model (assumed)")
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

func TestSupportsVision_ProviderOptOut_OverridesSetModel(t *testing.T) {
	config := visionTestConfig()
	config.Models.SupportsVision = boolPtr(false)
	config.Models.ModelInfo = []ModelInfo{
		{ID: "some-model", Tags: []string{"vision"}},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Even after setting a model with vision tag, the provider opt-out wins.
	if err := provider.SetModel("some-model"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if provider.SupportsVision() {
		t.Fatal("expected SupportsVision() == false when the provider opts out, regardless of model")
	}
}

func TestGetVisionModel_UsesConfigVisionModel(t *testing.T) {
	config := visionTestConfig()
	config.Models.VisionModel = "gpt-4o"

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if err := provider.SetModel("gpt-5-mini"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}

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

	if err := provider.SetModel("current-model"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}

	got := provider.GetVisionModel()
	if got != "current-model" {
		t.Errorf("GetVisionModel() = %q, want %q", got, "current-model")
	}
}
