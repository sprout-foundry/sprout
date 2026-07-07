// Package api — vision capabilities table (AUDIT-GAP-2 / SP-103-D3).
//
// Provides a structured per-provider view of the vision limits that
// historically lived in a single binary SupportsVision() flag plus ad-hoc
// constants scattered across resize / batching code paths. Downstream
// consumers (image resize at SP-103-B2, batch splitting at SP-103-D2) read
// this table instead of using a one-size 1536px cap.
package api

// VisionCapabilities describes per-provider vision limits. Populated
// from provider/model metadata and read once at construction. Fields
// with zero values mean "unknown — use default".
//
// Why per provider? Real provider limits diverge sharply:
//
//   - Anthropic caps single-image base64 payloads at ~5MB and auto-resizes
//     to 1568px on the longest side, accepting up to 100 images per turn.
//   - OpenAI's gpt-4o accepts ~20MB images and supports low/high/auto
//     detail tiers, with up to 500 images in some endpoints.
//   - Local Ollama (llama3.2-vision) caps out around 3.5MB and works best
//     at 1024px on the longest side with only a handful of images.
//
// A single 1536px + 5MB + N-image cap is a poor fit for any of them.
//
// The defaults used when a field is zero are intentionally safe (not
// permissive): they keep the call working while image-resize /
// batch-splitting code paths evolve. See VisionCapabilitiesDefault.
type VisionCapabilities struct {
	// MaxImageBytes is the largest single image the provider accepts
	// inline (oversized images must be resized before embedding).
	MaxImageBytes int
	// MaxImageCount is the max number of inline images per request.
	MaxImageCount int
	// MaxImageDimension is the longest-side cap (px). Providers differ
	// widely: Anthropic auto-resizes to 1568px, OpenAI keeps native
	// at low/auto, Gemini up to 3072 in some configs. Oversized
	// images should be resized to MaxImageDimension before embedding.
	MaxImageDimension int
	// DetailTiers lists supported detail levels (e.g. "low","high" or
	// "low","high","auto"). Empty means the provider picks automatically.
	DetailTiers []string
}

// VisionCapabilitiesDefault returns the package-wide safe fallback when a
// provider-specific capability table is empty or missing. The defaults are
// chosen to be conservative (works on every supported provider) rather
// than best-of-breed:
//
//   - 5_000_000 bytes ≈ Anthropic's official single-image cap.
//   - 20 images per request — well under every provider limit.
//   - 1536 px on the longest side — matches the historical hard-coded
//     resize cap so existing behavior is preserved when a provider
//     returns zero fields.
//
// Callers that need provider-tuned behavior should read the actual
// VisionCapabilities() from the provider, not the defaults.
func VisionCapabilitiesDefault() VisionCapabilities {
	return VisionCapabilities{
		MaxImageBytes:     5_000_000,
		MaxImageCount:     20,
		MaxImageDimension: 1536,
	}
}

// VisionCapabilitiesOrDefault returns caps with zero-valued fields filled
// in from VisionCapabilitiesDefault(). Non-zero fields are preserved
// untouched. Use this at the call site (resize / batch-split) so
// partially-populated tables still produce safe behavior.
//
// Example:
//
//	caps := client.VisionCapabilities()         // may be all zeros
//	caps  = api.VisionCapabilitiesOrDefault(caps) // now safe to read
//	if len(images) > caps.MaxImageCount { ... }
func VisionCapabilitiesOrDefault(caps VisionCapabilities) VisionCapabilities {
	def := VisionCapabilitiesDefault()
	if caps.MaxImageBytes <= 0 {
		caps.MaxImageBytes = def.MaxImageBytes
	}
	if caps.MaxImageCount <= 0 {
		caps.MaxImageCount = def.MaxImageCount
	}
	if caps.MaxImageDimension <= 0 {
		caps.MaxImageDimension = def.MaxImageDimension
	}
	// DetailTiers empty is meaningful ("provider picks automatically"),
	// so we don't fill it from a default.
	return caps
}
