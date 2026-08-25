package api

import (
	"testing"
)

// =============================================================================
// VisionCapabilities — SP-103-D3 / AUDIT-GAP-2
//
// Unit tests for the per-provider vision capability struct, default helper,
// and the delegation methods added to Provider, BaseProvider,
// ProviderAdapter, UnifiedProviderWrapper, and OllamaLocalClient.
// =============================================================================

// TestVisionCapabilities_DefaultReturnsSafeDefaults verifies the canonical
// safe-default configuration documented in the SP-103-D3 / AUDIT-GAP-2
// spec. These are the values call sites fall back to when a provider
// returns an empty VisionCapabilities{}; preserving them here is a
// contract — downstream code (SP-103-B2 resize, SP-103-D2 batch splitting)
// depends on the exact numbers.
func TestVisionCapabilities_DefaultReturnsSafeDefaults(t *testing.T) {
	caps := VisionCapabilitiesDefault()

	// 5_000_000 bytes ≈ Anthropic's documented single-image limit.
	if caps.MaxImageBytes != 5_000_000 {
		t.Errorf("MaxImageBytes: default = %d, want 5000000", caps.MaxImageBytes)
	}
	// 20 images per request — strictly under every supported provider.
	if caps.MaxImageCount != 20 {
		t.Errorf("MaxImageCount: default = %d, want 20", caps.MaxImageCount)
	}
	// 1536 px on the longest side — matches the historical hard-coded
	// resize cap so existing behavior is preserved when a provider
	// returns zero fields.
	if caps.MaxImageDimension != 1536 {
		t.Errorf("MaxImageDimension: default = %d, want 1536", caps.MaxImageDimension)
	}
	// DetailTiers is intentionally left empty by default: it means
	// "provider picks automatically" which is the only behavior that's
	// safe across every backend.
	if len(caps.DetailTiers) != 0 {
		t.Errorf("DetailTiers: default should be empty, got %v", caps.DetailTiers)
	}
}

// TestVisionCapabilities_OrDefaultFillsZeros exercises the standard
// "merge with defaults" pattern: a partially-populated table should have
// zero-valued scalars replaced, while zero-valued slices are preserved
// (because empty DetailTiers carries meaning: "provider auto-selects").
func TestVisionCapabilities_OrDefaultFillsZeros(t *testing.T) {
	t.Run("all-zero input becomes defaults", func(t *testing.T) {
		// Empty struct + OrDefault must produce the same shape as
		// VisionCapabilitiesDefault() (modulo the documented empty-slice
		// carve-out).
		caps := VisionCapabilitiesOrDefault(VisionCapabilities{})
		def := VisionCapabilitiesDefault()

		if caps.MaxImageBytes != def.MaxImageBytes {
			t.Errorf("MaxImageBytes: got %d, want default %d", caps.MaxImageBytes, def.MaxImageBytes)
		}
		if caps.MaxImageCount != def.MaxImageCount {
			t.Errorf("MaxImageCount: got %d, want default %d", caps.MaxImageCount, def.MaxImageCount)
		}
		if caps.MaxImageDimension != def.MaxImageDimension {
			t.Errorf("MaxImageDimension: got %d, want default %d", caps.MaxImageDimension, def.MaxImageDimension)
		}
	})

	t.Run("partial input preserves non-zero fields", func(t *testing.T) {
		// A provider saying "I cap at 1000px and 50 images per request"
		// should keep those values and only borrow the byte cap and
		// dimension defaults it didn't specify.
		in := VisionCapabilities{
			MaxImageCount:     50,
			MaxImageDimension: 1000,
		}
		caps := VisionCapabilitiesOrDefault(in)

		if caps.MaxImageBytes != 5_000_000 {
			t.Errorf("MaxImageBytes: zero-filled to %d, want default 5000000", caps.MaxImageBytes)
		}
		if caps.MaxImageCount != 50 {
			t.Errorf("MaxImageCount: provider value dropped, got %d, want 50", caps.MaxImageCount)
		}
		if caps.MaxImageDimension != 1000 {
			t.Errorf("MaxImageDimension: provider value dropped, got %d, want 1000", caps.MaxImageDimension)
		}
	})

	t.Run("empty DetailTiers is preserved (provider auto-select is meaningful)", func(t *testing.T) {
		// A provider returning no detail tiers is saying "I pick for you".
		// Filling in ["low","high"] would silently change semantics, so
		// the helper must leave it empty.
		caps := VisionCapabilitiesOrDefault(VisionCapabilities{})

		if caps.DetailTiers != nil {
			t.Errorf("DetailTiers: empty/nil must be preserved, got %v", caps.DetailTiers)
		}
	})

	t.Run("populated DetailTiers is preserved", func(t *testing.T) {
		// When the provider sends tiers, those exact values must round-trip.
		in := VisionCapabilities{
			DetailTiers: []string{"low", "high", "auto"},
		}
		caps := VisionCapabilitiesOrDefault(in)

		if len(caps.DetailTiers) != 3 ||
			caps.DetailTiers[0] != "low" ||
			caps.DetailTiers[1] != "high" ||
			caps.DetailTiers[2] != "auto" {
			t.Errorf("DetailTiers: round-trip failed, got %v", caps.DetailTiers)
		}
	})
}

// TestBaseProvider_VisionCapabilities_DefaultsToZero verifies that a
// freshly-constructed BaseProvider — without anyone setting visionCaps —
// reports the zero struct. That zero is the "unknown, caller should fall
// back via VisionCapabilitiesOrDefault" sentinel documented on the type.
func TestBaseProvider_VisionCapabilities_DefaultsToZero(t *testing.T) {
	p := &BaseProvider{}
	caps := p.VisionCapabilities()

	if caps.MaxImageBytes != 0 ||
		caps.MaxImageCount != 0 ||
		caps.MaxImageDimension != 0 ||
		caps.DetailTiers != nil {
		t.Errorf("BaseProvider{}.VisionCapabilities() = %+v, want zero struct", caps)
	}

	// Sanity: passing that zero through OrDefault must produce a usable
	// table — the whole point of the helper.
	filled := VisionCapabilitiesOrDefault(caps)
	if filled.MaxImageBytes != 5_000_000 {
		t.Errorf("zero -> OrDefault should yield safe bytes, got %d", filled.MaxImageBytes)
	}

	// Direct field mutation: concrete providers (Anthropic/OpenAI/etc.)
	// embed BaseProvider and write to p.visionCaps at construction.
	// Verify the field round-trips through the method.
	p.visionCaps = VisionCapabilities{
		MaxImageBytes:     20_000_000,
		MaxImageCount:     500,
		MaxImageDimension: 2048,
		DetailTiers:       []string{"low", "high", "auto"},
	}
	got := p.VisionCapabilities()
	if got.MaxImageBytes != 20_000_000 {
		t.Errorf("set MaxImageBytes: got %d, want 20000000", got.MaxImageBytes)
	}
	if got.MaxImageCount != 500 {
		t.Errorf("set MaxImageCount: got %d, want 500", got.MaxImageCount)
	}
	if got.MaxImageDimension != 2048 {
		t.Errorf("set MaxImageDimension: got %d, want 2048", got.MaxImageDimension)
	}
	if len(got.DetailTiers) != 3 {
		t.Errorf("set DetailTiers: got %d tiers, want 3", len(got.DetailTiers))
	}
}

// TestOllamaLocal_VisionCapabilities_ReflectsProvider pins the documented
// local-Ollama values. Anything different would change resize behavior
// across all llama3.2-vision / glm-ocr deployments.
func TestOllamaLocal_VisionCapabilities_ReflectsProvider(t *testing.T) {
	// VisionCapabilities on OllamaLocalClient is static (doesn't depend
	// on the configured model), so we can verify across a range of
	// models.
	models := []string{
		"llama3.2-vision",
		"llama3.2",
		"glm-ocr",
		"qwen2.5-coder:7b",
		"", // even with no model configured
	}
	for _, m := range models {
		t.Run(m, func(t *testing.T) {
			c := &OllamaLocalClient{model: m}
			caps := c.VisionCapabilities()

			// 5MB is the conservative Ollama cap. Older Ollama versions
			// were ~3.5MB; 5MB matches Anthropic and keeps everything
			// uniformly safe.
			if caps.MaxImageBytes != 5_000_000 {
				t.Errorf("MaxImageBytes: got %d, want 5000000", caps.MaxImageBytes)
			}
			// 5 images per request — local context windows are tight.
			if caps.MaxImageCount != 5 {
				t.Errorf("MaxImageCount: got %d, want 5", caps.MaxImageCount)
			}
			// 1024 px longest side — llama3.2-vision sweet spot.
			if caps.MaxImageDimension != 1024 {
				t.Errorf("MaxImageDimension: got %d, want 1024", caps.MaxImageDimension)
			}
			// DetailTiers is intentionally nil — Ollama picks
			// automatically.
			if caps.DetailTiers != nil {
				t.Errorf("DetailTiers: got %v, want nil", caps.DetailTiers)
			}
		})
	}
}

// TestProviderAdapter_VisionCapabilities_Delegates verifies the adapter
// pattern documented at provider_adapter.go:119. When the wrapped client
// implements the method, the adapter must forward verbatim; otherwise it
// must return the zero struct (callers run through OrDefault before reading).
func TestProviderAdapter_VisionCapabilities_Delegates(t *testing.T) {
	// enhancedMockClient (in provider_adapter_coverage_test.go) sets
	// visionCaps on construction and exposes it via VisionCapabilities —
	// same field pattern the real GenericProvider uses.
	caps := VisionCapabilities{
		MaxImageBytes:     11_111_111,
		MaxImageCount:     7,
		MaxImageDimension: 888,
		DetailTiers:       []string{"x", "y"},
	}
	mock := &enhancedMockClient{visionCaps: caps}
	adapter := NewProviderAdapter(OpenAIClientType, mock)

	got := adapter.VisionCapabilities()
	if got.MaxImageBytes != caps.MaxImageBytes ||
		got.MaxImageCount != caps.MaxImageCount ||
		got.MaxImageDimension != caps.MaxImageDimension {
		t.Errorf("adapter dropped field values: got %+v, want %+v", got, caps)
	}
	if len(got.DetailTiers) != 2 || got.DetailTiers[0] != "x" || got.DetailTiers[1] != "y" {
		t.Errorf("adapter dropped DetailTiers: got %v, want [x y]", got.DetailTiers)
	}

	t.Run("zero caps on wrapped client → zero on adapter → OrDefault rescues", func(t *testing.T) {
		// The bare mockClient returns an empty VisionCapabilities{}.
		// Adapter should propagate; OrDefault should fill in defaults.
		mc := &mockClient{}
		adapter2 := NewProviderAdapter(OpenAIClientType, mc)

		got := adapter2.VisionCapabilities()
		if got.MaxImageBytes != 0 || got.MaxImageCount != 0 || got.MaxImageDimension != 0 {
			t.Errorf("adapter returned non-zero for zero client: %+v", got)
		}
		filled := VisionCapabilitiesOrDefault(adapter2.VisionCapabilities())
		if filled.MaxImageBytes != 5_000_000 {
			t.Errorf("OrDefault rescue path broken, got bytes=%d", filled.MaxImageBytes)
		}
	})
}

// =============================================================================
// TestUnifiedWrapper_VisionCapabilities_PropagatesInnerCaps
//
// Verifies the documented UnifiedProviderWrapper.VisionCapabilities()
// behaviour: it propagates the inner provider's capability table
// unchanged. The type-assertion (`_, ok := w.provider.(interface{...
// VisionCapabilities() VisionCapabilities})`) short-circuits to the
// zero struct when the inner lacks the method. From within package
// api this branch is unreachable (every concrete Provider in the
// tree implements the method). The wrapper contract is therefore
// verified by the observable shape: zero in → zero out, populated
// in → populated out. Callers run through VisionCapabilitiesOrDefault
// before reading any field, so the zero-out behaviour is safe.
// =============================================================================
func TestUnifiedWrapper_VisionCapabilities_PropagatesInnerCaps(t *testing.T) {
	// Sanity check on the populated path: when enhancedMockClient
	// reports real values, the wrapper delivers them unchanged.
	// enhancedMockClient embeds mockClient, so it also satisfies
	// ProviderInterface.
	caps := VisionCapabilities{
		MaxImageBytes:     20_000_000,
		MaxImageCount:     500,
		MaxImageDimension: 2048,
		DetailTiers:       []string{"low", "high", "auto"},
	}
	mock := &enhancedMockClient{visionCaps: caps}
	wrapper := NewUnifiedProviderWrapper(mock)

	got := wrapper.VisionCapabilities()
	if got.MaxImageBytes != 20_000_000 ||
		got.MaxImageCount != 500 ||
		got.MaxImageDimension != 2048 {
		t.Errorf("populated caps didn't round-trip through wrapper: got %+v", got)
	}
	if len(got.DetailTiers) != 3 {
		t.Errorf("DetailTiers not preserved: got %v", got.DetailTiers)
	}
}

// =============================================================================
// TestUnifiedWrapper_VisionCapabilities_PropagatesZerosToCaller
//
// Companion to TestUnifiedWrapper_VisionCapabilities_PropagatesInnerCaps:
// the populated-cap round-trip has a zero-cap counterpart, and both
// branches of the type-assertion in unified.go (method exists vs.
// method absent) collapse to the same observable behaviour
// (zero in → zero out). From within package api we can only observe
// one branch directly — every Provider here implements the method —
// but exercising the zero-in shape proves the wrapper's short-circuit
// fallback would yield the same result for a hypothetical
// method-less legacy provider.
// =============================================================================
func TestUnifiedWrapper_VisionCapabilities_PropagatesZerosToCaller(t *testing.T) {
	// mockClient satisfies ProviderInterface directly (its
	// CheckConnection() has no ctx, matching the legacy
	// ProviderInterface sig). It returns zero VisionCapabilities{};
	// the wrapper then propagates that zero. OrDefault fills in
	// defaults. This exercises the populated branch end-to-end —
	// the type-assertion succeeds and the inner method returns
	// the zero struct, which is observably identical to the
	// short-circuit branch (the only behaviour SP-103-D3 contracts
	// on).
	mc := &mockClient{}
	wrapper := NewUnifiedProviderWrapper(mc)
	if wrapper == nil {
		t.Fatal("NewUnifiedProviderWrapper returned nil")
	}

	got := wrapper.VisionCapabilities()
	if got.MaxImageBytes != 0 || got.MaxImageCount != 0 || got.MaxImageDimension != 0 {
		t.Errorf("wrapper should return zero for empty inner, got %+v", got)
	}

	// Verify the rescue path: feeding the wrapper's zero into
	// OrDefault yields a usable configuration. This is what
	// SP-103-B2 / SP-103-D2 code paths will do.
	filled := VisionCapabilitiesOrDefault(got)
	if filled.MaxImageBytes != 5_000_000 ||
		filled.MaxImageCount != 20 ||
		filled.MaxImageDimension != 1536 {
		t.Errorf("OrDefault-after-wrapper rescue: got %+v, want defaults", filled)
	}
}
