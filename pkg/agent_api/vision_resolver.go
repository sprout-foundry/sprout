package api

import (
	"context"
	"strings"
	"time"
)

// VisionCapability is the resolved, per-model vision answer for a client.
// It is the single source of truth for vision decisions (SP-140): callers
// never read SupportsVision() flags, probe registries, or conversational
// heuristics directly.
type VisionCapability struct {
	// AcceptsImages reports whether the model can take image input at all
	// (inline multimodal turns, OCR extraction).
	AcceptsImages bool
	// Limits are the per-client vision limits; zero-valued fields mean
	// "unknown" and must pass through VisionCapabilitiesOrDefault().
	Limits VisionCapabilities
	// Source records which layer decided: "probe" (published probe data)
	// or "declared" (client's own SupportsVision).
	Source string
}

// ResolveVisionCapability resolves vision capability for a client.
//
// Precedence:
//  1. Runtime-learned observation for the provider/model pair (see
//     vision_learned.go) — recorded when the provider accepted or
//     capability-rejected images on the wire.
//  2. Published probe data for the model (registry ground truth).
//  3. The client's own declaration (config tags / provider flag).
//
// No conversational refinement: OCR-only models are declared vision-capable
// by their clients and flow inline like any other vision model. The prior
// per-client "conversational vision" distinction (SP-103-C2) was removed
// with SP-140 — for an OCR-only primary, an inline turn and the OCR path
// both yield extracted text, so the distinction only rerouted delivery.
func ResolveVisionCapability(c ClientInterface) VisionCapability {
	if c == nil {
		return VisionCapability{}
	}

	accepts := c.SupportsVision()
	source := "declared"
	if learned := LearnedVisionAcceptance(c.GetProvider(), c.GetModel()); learned != nil {
		accepts = *learned
		source = "runtime"
	} else if probe := visionProbeFor(c); probe != nil {
		accepts = *probe
		source = "probe"
	}

	return VisionCapability{
		AcceptsImages: accepts,
		Limits:        c.VisionCapabilities(),
		Source:        source,
	}
}

// visionProbeFor looks up the published probe result for the client's
// current model. Returns nil when unknown (never probed, model not in the
// registry, or listing failed). Lookup goes through the model registry,
// which owns caching; repeated calls are cheap.
func visionProbeFor(c ClientInterface) *bool {
	model := strings.TrimSpace(c.GetModel())
	if model == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	models, err := GetModelsForProviderCtx(ctx, ClientType(c.GetProvider()))
	if err != nil {
		return nil
	}
	for i := range models {
		if models[i].ID == model {
			return models[i].VisionProbe
		}
	}
	return nil
}
