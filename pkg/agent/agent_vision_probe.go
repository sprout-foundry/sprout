package agent

import (
	"context"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// probeVisionResult looks up the probe-tested vision capability for the
// current model from the published registry. Returns nil if no probe data
// is available (never probed, model not in registry, or fetch failed).
//
// The result is cached keyed on model+provider identity so repeated calls
// during a conversation don't re-fetch. When the model or provider changes,
// the cache is invalidated and the next call re-fetches.
func (a *Agent) probeVisionResult() *bool {
	if a == nil {
		return nil
	}
	c := a.getClient()
	if c == nil {
		return nil
	}
	model := c.GetModel()
	if model == "" {
		return nil
	}
	provider := string(a.getClientType())

	a.visionProbeMu.RLock()
	if a.visionProbeModel == model && a.visionProbeProvider == provider && a.visionProbeResult != nil {
		result := a.visionProbeResult
		a.visionProbeMu.RUnlock()
		return result
	}
	a.visionProbeMu.RUnlock()

	var result *bool
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if models, err := api.GetModelsForProviderCtx(ctx, a.getClientType()); err == nil {
		for _, m := range models {
			if m.ID == model {
				result = m.VisionProbe
				break
			}
		}
	}
	cancel()

	a.visionProbeMu.Lock()
	// Double-check under write lock: another goroutine may have already
	// populated the cache for this key while we fetched. Prefer the
	// existing cached result over our fetch to avoid losing data.
	if a.visionProbeModel == model && a.visionProbeProvider == provider && a.visionProbeResult != nil {
		result = a.visionProbeResult
		a.visionProbeMu.Unlock()
		return result
	}
	a.visionProbeProvider = provider
	a.visionProbeModel = model
	a.visionProbeResult = result
	a.visionProbeMu.Unlock()

	return result
}

// effectiveVisionSupport reports whether the current model can process
// images, consulting probe ground truth when available and falling back to
// the client's config-based SupportsVision() when no probe data exists.
//
// Probe authority: a non-nil probe result overrides config flags. This is
// the fix for providers that blanket-enable supports_vision (deepinfra,
// openrouter) — the probe catches models that can't actually see images.
func (a *Agent) effectiveVisionSupport() bool {
	if a == nil {
		return false
	}
	c := a.getClient()
	if c == nil {
		return false
	}
	if probe := a.probeVisionResult(); probe != nil {
		return *probe
	}
	return c.SupportsVision()
}
