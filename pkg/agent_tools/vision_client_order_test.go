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
	// Load a config whose default model IS vision-tagged: with the fix the
	// vision tier is that model itself, not any pin.
	cfgJSON := `{
		"name": "vision-order-test",
		"endpoint": "https://example.invalid/v1/chat/completions",
		"auth": {"type": "api_key", "env_var": "VISION_ORDER_TEST_KEY"},
		"defaults": {"model": "flash-multimodal"},
		"models": {
			"default_context_limit": 128000,
			"supports_vision": true,
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

func TestGetVisionModelForProvider_PinServesNonVisionActiveModel(t *testing.T) {
	// Default model has no vision tag: the registry pin is the correct tier.
	cfgJSON := `{
		"name": "vision-order-test-2",
		"endpoint": "https://example.invalid/v1/chat/completions",
		"auth": {"type": "api_key", "env_var": "VISION_ORDER_TEST2_KEY"},
		"defaults": {"model": "text-only-model"},
		"models": {
			"default_context_limit": 128000,
			"vision_model": "pinned-vision-model",
			"model_info": [
				{"id": "text-only-model", "name": "Text", "tags": ["tools"]},
				{"id": "pinned-vision-model", "name": "Pinned", "tags": ["tools", "vision"]}
			]
		}
	}`
	if err := factory.GlobalFactory().LoadConfigFromBytes([]byte(cfgJSON)); err != nil {
		t.Fatalf("seed test provider config: %v", err)
	}

	got := GetVisionModelForProvider("vision-order-test-2")
	if got != "pinned-vision-model" {
		t.Fatalf("non-vision active model must fall to the pin: got %q, want %q", got, "pinned-vision-model")
	}
}

func TestGetVisionModelForProvider_ZaiCodingResolvesFlashNotTurbo(t *testing.T) {
	// The shipped zai-coding config pins glm-5.3-flash (was glm-5v-turbo,
	// excluded from the observed user's plan) and its default glm-5 carries
	// no vision tag — so the pin serves, and it must be the flash tier.
	visionTestProvider(t, "zai-coding")
	got := GetVisionModelForProvider("zai-coding")
	if strings.Contains(got, "5v") {
		t.Fatalf("zai-coding vision tier must not fork to glm-5v-turbo, got %q", got)
	}
	if got == "" {
		t.Fatal("zai-coding should resolve a vision model")
	}
}

func TestProviderModelSeesImages(t *testing.T) {
	visionTestProvider(t, "zai-coding")
	cfg, err := factory.GlobalFactory().GetProviderConfig("zai-coding")
	if err != nil || cfg == nil {
		t.Fatalf("zai-coding config unavailable: %v", err)
	}
	if !providerModelSeesImages(cfg, "glm-5.3-flash") {
		t.Error("glm-5.3-flash is vision-tagged and must read as seeing images")
	}
	if providerModelSeesImages(cfg, "glm-5") {
		t.Error("glm-5 carries no vision tag; it must not read as seeing images")
	}
	if providerModelSeesImages(nil, "any") {
		t.Error("nil config must read false")
	}
	if GetVisionModelForProvider(api.TestClientType) != "" {
		t.Error("test client must never resolve a vision model")
	}
}
