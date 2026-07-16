package providers

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// =============================================================================
// GenericProvider.VisionCapabilities — SP-103-D3 / AUDIT-GAP-2
//
// Pins the OpenAI-compatible tier documented on
// (p *GenericProvider).VisionCapabilities(). Without these tests the
// fully-constructed branch would be regressable to the zero struct,
// silently breaking resize / batch-splitting defaults downstream
// (SP-103-B2 / SP-103-D2). Mirrors TestOllamaLocal_VisionCapabilities_
// ReflectsProvider in pkg/agent_api/vision_capabilities_test.go for the
// local-Ollama tier.
// =============================================================================

// TestGenericProvider_VisionCapabilities_ReturnsOpenAITier_Configured
// verifies that a fully-constructed *GenericProvider (config != nil)
// reports the documented OpenAI-compatible tier:
//
//	20MB per image, 500 images per request, 2048px longest side,
//	detail tiers {low, high, auto}.
//
// `NewGenericProvider` runs `config.Validate()` so the bare literal
// passes the same checks the factory applies at runtime — no separate
// minimal-valid-config fixture needed.
func TestGenericProvider_VisionCapabilities_ReturnsOpenAITier_Configured(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test-vision-caps",
		Endpoint: "https://api.test-vision-caps.example.com/v1/chat/completions",
		Auth: AuthConfig{
			Type: "none",
		},
		Defaults: RequestDefaults{
			Model: "vision-test-model",
		},
		Models: ModelConfig{
			DefaultContextLimit: 128000,
		},
	}
	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create GenericProvider: %v", err)
	}
	if provider.config == nil {
		t.Fatal("NewGenericProvider returned a provider with nil config — test fixture is invalid")
	}

	caps := provider.VisionCapabilities()

	// 20MB single-image cap — OpenAI's gpt-4o ceiling.
	if caps.MaxImageBytes != 20_000_000 {
		t.Errorf("MaxImageBytes: got %d, want 20000000", caps.MaxImageBytes)
	}
	// 500 images per request — generous OpenAI tier, well above every
	// concrete backend's typical limit.
	if caps.MaxImageCount != 500 {
		t.Errorf("MaxImageCount: got %d, want 500", caps.MaxImageCount)
	}
	// 2048px longest side — fine-tunes well for vision-capable models.
	if caps.MaxImageDimension != 2048 {
		t.Errorf("MaxImageDimension: got %d, want 2048", caps.MaxImageDimension)
	}
	// Detail tiers must match the canonical {low, high, auto} exactly —
	// downstream detail-selection code reads these by name.
	wantTiers := []string{"low", "high", "auto"}
	if len(caps.DetailTiers) != len(wantTiers) {
		t.Fatalf("DetailTiers length: got %d, want %d (%v)", len(caps.DetailTiers), len(wantTiers), caps.DetailTiers)
	}
	for i, want := range wantTiers {
		if caps.DetailTiers[i] != want {
			t.Errorf("DetailTiers[%d]: got %q, want %q (full slice: %v)", i, caps.DetailTiers[i], want, caps.DetailTiers)
		}
	}

	// Sanity: the fully-populated caps must round-trip through
	// VisionCapabilitiesOrDefault untouched (non-zero scalar preserved).
	filled := api.VisionCapabilitiesOrDefault(caps)
	if filled.MaxImageBytes != 20_000_000 ||
		filled.MaxImageCount != 500 ||
		filled.MaxImageDimension != 2048 {
		t.Errorf("OrDefault mutated non-zero scalars: got %+v", filled)
	}
}

// TestGenericProvider_VisionCapabilities_PerProviderDifferentiation
// verifies SP-103-D3: the switch on p.config.Name returns different
// caps for "anthropic", "openai", and unknown providers.
func TestGenericProvider_VisionCapabilities_PerProviderDifferentiation(t *testing.T) {
	newConfig := func(name string) *ProviderConfig {
		return &ProviderConfig{
			Name:     name,
			Endpoint: "https://api." + name + ".example.com/v1/chat/completions",
			Auth: AuthConfig{
				Type: "none",
			},
			Defaults: RequestDefaults{
				Model: "test-model",
			},
			Models: ModelConfig{
				DefaultContextLimit: 128000,
			},
		}
	}

	t.Run("anthropic returns tighter caps", func(t *testing.T) {
		provider, err := NewGenericProvider(newConfig("anthropic"))
		if err != nil {
			t.Fatalf("failed to create GenericProvider: %v", err)
		}

		caps := provider.VisionCapabilities()

		// Anthropic's documented limits: 5MB per image, 1568px.
		if caps.MaxImageBytes != 5_000_000 {
			t.Errorf("MaxImageBytes: got %d, want 5000000", caps.MaxImageBytes)
		}
		if caps.MaxImageCount != 20 {
			t.Errorf("MaxImageCount: got %d, want 20", caps.MaxImageCount)
		}
		if caps.MaxImageDimension != 1568 {
			t.Errorf("MaxImageDimension: got %d, want 1568", caps.MaxImageDimension)
		}
		// Anthropic doesn't use named detail tiers.
		if len(caps.DetailTiers) != 0 {
			t.Errorf("DetailTiers: got %v, want empty", caps.DetailTiers)
		}
	})

	t.Run("openai returns OpenAI caps", func(t *testing.T) {
		provider, err := NewGenericProvider(newConfig("openai"))
		if err != nil {
			t.Fatalf("failed to create GenericProvider: %v", err)
		}

		caps := provider.VisionCapabilities()

		if caps.MaxImageBytes != 20_000_000 {
			t.Errorf("MaxImageBytes: got %d, want 20000000", caps.MaxImageBytes)
		}
		if caps.MaxImageCount != 10 {
			t.Errorf("MaxImageCount: got %d, want 10", caps.MaxImageCount)
		}
		if caps.MaxImageDimension != 2048 {
			t.Errorf("MaxImageDimension: got %d, want 2048", caps.MaxImageDimension)
		}
		wantTiers := []string{"low", "high", "auto"}
		if len(caps.DetailTiers) != len(wantTiers) {
			t.Fatalf("DetailTiers length: got %d, want %d", len(caps.DetailTiers), len(wantTiers))
		}
		for i, want := range wantTiers {
			if caps.DetailTiers[i] != want {
				t.Errorf("DetailTiers[%d]: got %q, want %q", i, caps.DetailTiers[i], want)
			}
		}
	})

	t.Run("unknown provider returns generous defaults", func(t *testing.T) {
		provider, err := NewGenericProvider(newConfig("openrouter"))
		if err != nil {
			t.Fatalf("failed to create GenericProvider: %v", err)
		}

		caps := provider.VisionCapabilities()

		// Unknown providers get the generous default tier.
		if caps.MaxImageBytes != 20_000_000 {
			t.Errorf("MaxImageBytes: got %d, want 20000000", caps.MaxImageBytes)
		}
		if caps.MaxImageCount != 500 {
			t.Errorf("MaxImageCount: got %d, want 500", caps.MaxImageCount)
		}
		if caps.MaxImageDimension != 2048 {
			t.Errorf("MaxImageDimension: got %d, want 2048", caps.MaxImageDimension)
		}
	})

	t.Run("anthropic and openai differ", func(t *testing.T) {
		// Regression: ensure the two named providers don't accidentally
		// return the same values.
		pAnthropic, err := NewGenericProvider(newConfig("anthropic"))
		if err != nil {
			t.Fatalf("failed to create anthropic provider: %v", err)
		}
		pOpenAI, err := NewGenericProvider(newConfig("openai"))
		if err != nil {
			t.Fatalf("failed to create openai provider: %v", err)
		}

		aCaps := pAnthropic.VisionCapabilities()
		oCaps := pOpenAI.VisionCapabilities()

		if aCaps.MaxImageBytes == oCaps.MaxImageBytes {
			t.Errorf("anthropic and openai should differ on MaxImageBytes (both %d)", aCaps.MaxImageBytes)
		}
		if aCaps.MaxImageCount == oCaps.MaxImageCount {
			t.Errorf("anthropic and openai should differ on MaxImageCount (both %d)", aCaps.MaxImageCount)
		}
		if aCaps.MaxImageDimension == oCaps.MaxImageDimension {
			t.Errorf("anthropic and openai should differ on MaxImageDimension (both %d)", aCaps.MaxImageDimension)
		}
	})
}

// TestGenericProvider_VisionCapabilities_FallsBackToDefaults_WhenConfigNil
// verifies the defensive guard documented on the method comment: when
// p.config is nil (a unit-test fixture constructed directly with a bare
// struct, bypassing NewGenericProvider), the method returns
// VisionCapabilitiesDefault() rather than the zero struct.
func TestGenericProvider_VisionCapabilities_FallsBackToDefaults_WhenConfigNil(t *testing.T) {
	p := &GenericProvider{}

	caps := p.VisionCapabilities()
	def := api.VisionCapabilitiesDefault()

	if caps.MaxImageBytes != def.MaxImageBytes {
		t.Errorf("MaxImageBytes: got %d, want default %d", caps.MaxImageBytes, def.MaxImageBytes)
	}
	if caps.MaxImageCount != def.MaxImageCount {
		t.Errorf("MaxImageCount: got %d, want default %d", caps.MaxImageCount, def.MaxImageCount)
	}
	if caps.MaxImageDimension != def.MaxImageDimension {
		t.Errorf("MaxImageDimension: got %d, want default %d", caps.MaxImageDimension, def.MaxImageDimension)
	}
	if len(caps.DetailTiers) != 0 {
		t.Errorf("DetailTiers: default should be empty (auto-select), got %v", caps.DetailTiers)
	}
	if caps.MaxImageBytes != 5_000_000 {
		t.Errorf("MaxImageBytes: nil-config fallback got %d, want 5000000", caps.MaxImageBytes)
	}
	if caps.MaxImageCount != 20 {
		t.Errorf("MaxImageCount: nil-config fallback got %d, want 20", caps.MaxImageCount)
	}
	if caps.MaxImageDimension != 1536 {
		t.Errorf("MaxImageDimension: nil-config fallback got %d, want 1536", caps.MaxImageDimension)
	}
}
