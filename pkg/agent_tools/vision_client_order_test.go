package tools

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// GetVisionModelForProvider resolution order (SP-137: runtime > declared):
// the ACTIVE model wins when it is itself vision-capable; the registry
// vision_model pin only serves a non-vision active model. Pinned against
// the observed failure: an agent on a natively-multimodal flash model had
// every vision call forked to a registry-pinned model its plan excluded.

// visionTestProvider returns a provider name whose embedded registry config
// exists and carries the fields under test, or skips the test when the
// embedded config shape changes.
func visionTestProvider(t *testing.T, name string) {
	t.Helper()
	if _, err := factory.GlobalFactory().GetProviderConfig(name); err != nil {
		t.Skipf("embedded config for %s unavailable: %v", name, err)
	}
}

func TestGetVisionModelForProvider_ActiveModelWinsWhenVisionCapable(t *testing.T) {
	// Load a config whose default model IS vision-tagged: the vision tier is
	// that model itself, not any pin.
	cfgJSON := `{
		"name": "vision-order-test",
		"endpoint": "https://example.invalid/v1/chat/completions",
		"auth": {"type": "api_key", "env_var": "VISION_ORDER_TEST_KEY"},
		"defaults": {"model": "flash-multimodal"},
		"models": {
			"default_context_limit": 128000,
			"vision_model": "pinned-vision-model",
			"model_info": [
				{"id": "flash-multimodal", "name": "Flash", "tags": ["tools", "vision"]},
				{"id": "pinned-vision-model", "name": "Pinned", "tags": ["tools", "vision"]}
			]
		}
	}`
	if err := factory.GlobalFactory().LoadConfigFromBytes([]byte(cfgJSON)); err != nil {
		t.Fatalf("seed test provider config: %v", err)
	}

	got := GetVisionModelForProvider("vision-order-test")
	if got != "flash-multimodal" {
		t.Fatalf("active vision-capable model must win: got %q, want %q", got, "flash-multimodal")
	}
}

func TestGetVisionModelForProvider_UntaggedActiveModelWins(t *testing.T) {
	// The active model names no modality: vision is assumed, so the active
	// model still wins and no pin is needed to serve the vision tier.
	cfgJSON := `{
		"name": "vision-order-test-2",
		"endpoint": "https://example.invalid/v1/chat/completions",
		"auth": {"type": "api_key", "env_var": "VISION_ORDER_TEST2_KEY"},
		"defaults": {"model": "untagged-model"},
		"models": {
			"default_context_limit": 128000,
			"vision_model": "pinned-vision-model",
			"model_info": [
				{"id": "untagged-model", "name": "Untagged", "tags": ["tools"]},
				{"id": "pinned-vision-model", "name": "Pinned", "tags": ["tools", "vision"]}
			]
		}
	}`
	if err := factory.GlobalFactory().LoadConfigFromBytes([]byte(cfgJSON)); err != nil {
		t.Fatalf("seed test provider config: %v", err)
	}

	got := GetVisionModelForProvider("vision-order-test-2")
	if got != "untagged-model" {
		t.Fatalf("untagged active model is assumed vision-capable and must win: got %q, want %q", got, "untagged-model")
	}
}

func TestGetVisionModelForProvider_PinServesOptedOutActiveModel(t *testing.T) {
	// Default model is explicitly text-only: the pin is the correct tier,
	// because the active model genuinely cannot take images.
	cfgJSON := `{
		"name": "vision-order-test-3",
		"endpoint": "https://example.invalid/v1/chat/completions",
		"auth": {"type": "api_key", "env_var": "VISION_ORDER_TEST3_KEY"},
		"defaults": {"model": "text-only-model"},
		"models": {
			"default_context_limit": 128000,
			"vision_model": "pinned-vision-model",
			"model_info": [
				{"id": "text-only-model", "name": "Text", "tags": ["tools", "text-only"]},
				{"id": "pinned-vision-model", "name": "Pinned", "tags": ["tools", "vision"]}
			]
		}
	}`
	if err := factory.GlobalFactory().LoadConfigFromBytes([]byte(cfgJSON)); err != nil {
		t.Fatalf("seed test provider config: %v", err)
	}

	got := GetVisionModelForProvider("vision-order-test-3")
	if got != "pinned-vision-model" {
		t.Fatalf("text-only active model must fall to the pin: got %q, want %q", got, "pinned-vision-model")
	}
}

func TestGetVisionModelForProvider_ZaiCodingResolvesActiveModel(t *testing.T) {
	// The shipped zai-coding config no longer pins a vision model: with
	// vision assumed, its active model serves the vision tier directly and
	// vision is never forked to a separate (possibly plan-excluded) model.
	visionTestProvider(t, "zai-coding")
	got := GetVisionModelForProvider("zai-coding")
	if strings.Contains(got, "5v") {
		t.Fatalf("zai-coding vision tier must not fork to glm-5v-turbo, got %q", got)
	}
	if got == "" {
		t.Fatal("zai-coding should resolve a vision model")
	}
	if got != "glm-5" {
		t.Fatalf("zai-coding vision tier should be its active model, got %q", got)
	}
}

func TestProviderModelSeesImages(t *testing.T) {
	visionTestProvider(t, "zai-coding")
	cfg, err := factory.GlobalFactory().GetProviderConfig("zai-coding")
	if err != nil || cfg == nil {
		t.Fatalf("zai-coding config unavailable: %v", err)
	}
	if !providerModelSeesImages(cfg, "glm-4.6v") {
		t.Error("glm-4.6v is vision-tagged and must read as seeing images")
	}
	if !providerModelSeesImages(cfg, "glm-5") {
		t.Error("glm-5 names no modality; vision is assumed and it must read as seeing images")
	}
	if providerModelSeesImages(cfg, "glm-5-turbo") {
		t.Error("glm-5-turbo carries a text-only tag and must not read as seeing images")
	}
	if providerModelSeesImages(nil, "any") {
		t.Error("nil config must read false")
	}
	if GetVisionModelForProvider(api.TestClientType) != "" {
		t.Error("test client must never resolve a vision model")
	}
}
