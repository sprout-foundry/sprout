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

// TestGenericProvider_VisionCapabilities_FallsBackToDefaults_WhenConfigNil
// verifies the defensive guard documented on the method comment: when
// p.config is nil (a unit-test fixture constructed directly with a bare
// struct, bypassing NewGenericProvider), the method returns
// VisionCapabilitiesDefault() rather than the zero struct.
//
// This shape matters because the inner branch of UnifiedProviderWrapper
// / ProviderAdapter pass-through code reads whatever
// VisionCapabilities() returns and runs it through OrDefault, so a
// bare-struct fixture must produce the same downstream behavior as a
// configured one.
func TestGenericProvider_VisionCapabilities_FallsBackToDefaults_WhenConfigNil(t *testing.T) {
	// Bare struct — NewGenericProvider is intentionally NOT called so we
	// exercise the nil-config guard directly.
	p := &GenericProvider{}

	caps := p.VisionCapabilities()
	def := api.VisionCapabilitiesDefault()

	// All defaults must round-trip exactly.
	if caps.MaxImageBytes != def.MaxImageBytes {
		t.Errorf("MaxImageBytes: got %d, want default %d", caps.MaxImageBytes, def.MaxImageBytes)
	}
	if caps.MaxImageCount != def.MaxImageCount {
		t.Errorf("MaxImageCount: got %d, want default %d", caps.MaxImageCount, def.MaxImageCount)
	}
	if caps.MaxImageDimension != def.MaxImageDimension {
		t.Errorf("MaxImageDimension: got %d, want default %d", caps.MaxImageDimension, def.MaxImageDimension)
	}
	// DetailTiers stays empty in the default (empty → "provider picks
	// automatically" is the only safe cross-backend meaning).
	if len(caps.DetailTiers) != 0 {
		t.Errorf("DetailTiers: default should be empty (auto-select), got %v", caps.DetailTiers)
	}

	// Equivalence: the fallback result must equal the documented
	// defaults exactly. We compare field-by-field rather than via
	// reflect.DeepEqual so a future shape-change (e.g. a new optional
	// pointer field) doesn't spuriously fail this pin test; only the
	// four contract fields are checked.
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
