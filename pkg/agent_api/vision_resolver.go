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
	// Limits are the per-model vision limits (GenericProvider layers the
	// current model's model_info.vision_limits over the provider table;
	// SP-140 Phase 1). Zero-valued fields mean "unknown" and must pass
	// through VisionCapabilitiesOrDefault().
	Limits VisionCapabilities
	// Source records how the answer was reached: "runtime" (a verified
	// on-the-wire observation), "structural" (the transport cannot carry
	// image parts), or — when the answer is the optimistic default — the
	// strongest supporting signal, "probe" or "declared", else "optimistic".
	Source string
}

// StructuralVisionCapability is implemented by clients whose request builder
// cannot carry image parts at all (a text-only inference engine). It is a
// structural fact about the transport, unlike SupportsVision — a *declared*
// capability that can lag reality. It is the only static way to turn images
// off; a provider that merely rejects them is handled by the runtime
// capability cache (verify-and-remember), not by an opt-out.
type StructuralVisionCapability interface {
	CannotAcceptImages() bool
}

// VisionSelfHealing is implemented by clients that recover from a vision
// capability rejection at request time: record the rejection, strip the image
// parts with a note, and retry once. The resolver's optimistic default applies
// only to such clients — an image sent to a model that cannot take it would
// otherwise hard-fail the turn, so a client with no recovery path keeps its
// declared answer as authoritative.
type VisionSelfHealing interface {
	RecoversFromVisionRejection() bool
}

// ResolveVisionCapability resolves vision capability for a client.
//
// Precedence:
//  1. Structural veto: a client that cannot carry image parts never gets
//     them.
//  2. Runtime-learned observation for the provider/model pair (see
//     vision_learned.go) — recorded when the provider accepted or
//     capability-rejected images on the wire. A verified rejection is the
//     only negative answer that suppresses images.
//  3. Optimistic default, for self-healing clients only: attach and try. A
//     negative published probe or declaration is a config guess, not a
//     verified rejection — provider configs and model catalogs lag model
//     releases, and most current models accept images. Pre-emptively
//     rerouting an image to a separate "vision model" because a config said
//     no is the failure mode this removes; the send path verifies the real
//     answer on first contact and remembers it. A client that cannot recover
//     from a rejection on the wire keeps its declared answer, so an image
//     cannot hard-fail the turn.
//
// No conversational refinement: OCR-only models flow inline like any other
// vision model. The prior per-client "conversational vision" distinction
// (SP-103-C2) was removed with SP-140 — for an OCR-only primary, an inline
// turn and the OCR path both yield extracted text, so the distinction only
// rerouted delivery.
func ResolveVisionCapability(c ClientInterface) VisionCapability {
	if c == nil {
		return VisionCapability{}
	}
	if structural, ok := c.(StructuralVisionCapability); ok && structural.CannotAcceptImages() {
		return VisionCapability{
			Limits: c.VisionCapabilities(),
			Source: "structural",
		}
	}

	accepts := c.SupportsVision()
	source := "declared"
	if learned := LearnedVisionAcceptance(c.GetProvider(), c.GetModel()); learned != nil {
		accepts = *learned
		source = "runtime"
	} else if probe := visionProbeFor(c); probe != nil && *probe {
		accepts = true
		source = "probe"
	} else if !accepts && recoversFromVisionRejection(c) {
		accepts = true
		source = "optimistic"
	}

	return VisionCapability{
		AcceptsImages: accepts,
		Limits:        c.VisionCapabilities(),
		Source:        source,
	}
}

// recoversFromVisionRejection reports whether the client can strip a rejected
// image and retry the turn, which is what makes an optimistic attach safe.
func recoversFromVisionRejection(c ClientInterface) bool {
	selfHealing, ok := c.(VisionSelfHealing)
	return ok && selfHealing.RecoversFromVisionRejection()
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
