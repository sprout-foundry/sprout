package api

import (
	"context"
	"fmt"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
	"github.com/sprout-foundry/sprout/pkg/modelregistry"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// ModelInfo represents information about an available model
type ModelInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name,omitempty"`
	Description     string   `json:"description,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	Size            string   `json:"size,omitempty"`
	Cost            float64  `json:"cost,omitempty"`
	InputCost       float64  `json:"input_cost,omitempty"`
	OutputCost      float64  `json:"output_cost,omitempty"`
	CachedInputCost float64  `json:"cached_input_cost,omitempty"`
	ContextLength   int      `json:"context_length,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	// EligibleRoles lists the agentic roles a model meets the minimum
	// deterministic bar for ("primary", "subagent"). This is an
	// eligibility pre-filter (currently context-window based), NOT a
	// quality recommendation — the capability probe provides the
	// authoritative agentic-capable signal. Empty means below the bar or
	// unknown. Additive/omitempty so older clients ignore it.
	EligibleRoles []string `json:"eligible_roles,omitempty"`
	// RecommendedRoles ⊆ EligibleRoles, gated on passing the capability probe
	// (subagent ← gates passed, primary ← complex stage passed). Empty when
	// un-probed or not recommended. Populated from the published registry.
	RecommendedRoles []string `json:"recommended_roles,omitempty"`
	// Warnings are non-blocking caveats to surface in the picker (e.g. a small
	// context window in the 64K–128K band). Populated from the published registry.
	Warnings []string `json:"warnings,omitempty"`
}

// Agentic-coding eligibility thresholds. These set the *minimum* context
// window for a role and are a deterministic placeholder for the capability
// probe (which will provide the authoritative agentic-capable signal).
// Eligibility ≠ recommendation.
const (
	subagentMinContext = 64_000
	primaryMinContext  = 128_000
)

// ClassifyEligibleRoles returns the agentic roles a model meets the minimum
// deterministic bar for, based on its context window. Returns nil when the
// model is below the subagent threshold or its context length is unknown.
func ClassifyEligibleRoles(m ModelInfo) []string {
	switch {
	case m.ContextLength >= primaryMinContext:
		return []string{"primary", "subagent"}
	case m.ContextLength >= subagentMinContext:
		return []string{"subagent"}
	default:
		return nil
	}
}

// fillEligibleRoles populates EligibleRoles for any model that doesn't already
// carry it, so live-API models get the heuristic while registry- or
// probe-provided roles are preserved.
func fillEligibleRoles(models []ModelInfo) []ModelInfo {
	for i := range models {
		if len(models[i].EligibleRoles) == 0 {
			models[i].EligibleRoles = ClassifyEligibleRoles(models[i])
		}
	}
	return models
}

// ModelsListInterface defines methods for listing available models
type ModelsListInterface interface {
	ListAvailableModels() ([]ModelInfo, error)
	GetDefaultModel() string
	IsModelAvailable(modelID string) bool
}

// GetAvailableModels returns available models for the current provider
func GetAvailableModels() ([]ModelInfo, error) {
	// Use unified provider detection
	clientType, err := DetermineProvider("", "")
	if err != nil {
		// Fallback to a reasonable default
		clientType = OllamaLocalClientType
	}
	return GetModelsForProvider(clientType)
}

// GetModelsForProvider returns available models for a specific provider
func GetModelsForProvider(clientType ClientType) ([]ModelInfo, error) {
	return GetModelsForProviderCtx(context.Background(), clientType)
}

// GetModelsForProviderCtx returns available models for a specific provider with context support.
// It checks the model registry first (if enabled), falling back to direct per-provider API calls.
func GetModelsForProviderCtx(ctx context.Context, clientType ClientType) ([]ModelInfo, error) {
	// Try the model registry first — fast, cached, no API key required.
	// Only attempt registry fetch for known providers in the catalog to avoid unnecessary network requests.
	providerID := string(clientType)
	if _, exists := providercatalog.FindProvider(providerID); exists {
		if registryModels, err := modelregistry.FetchModels(ctx, providerID); err == nil && registryModels != nil {
			return fillEligibleRoles(convertRegistryModels(registryModels)), nil
		}
	}

	// Canonical adapters are the source of truth for live listing where one
	// exists — they normalize the provider's native API into the canonical
	// contract (capabilities, pricing, context, lifecycle) before projecting
	// down to ModelInfo for existing consumers.
	if canon, handled, listErr := canonicalAdapterModels(ctx, providerID); handled {
		if listErr != nil {
			return nil, agenterrors.Wrap(listErr, fmt.Sprintf("failed to list models for %s", clientType))
		}
		modelcontract.FillEligibleRoles(canon)
		out := make([]ModelInfo, len(canon))
		for i := range canon {
			out[i] = CanonicalToModelInfo(canon[i])
		}
		return out, nil
	}

	// Fall back to the provider's direct ListModels method.
	provider, err := createProviderForType(clientType)
	if err != nil {
		return nil, agenterrors.Wrap(err, fmt.Sprintf("failed to create provider for %s", clientType))
	}

	if provider == nil {
		return nil, agenterrors.NewValidation(fmt.Sprintf("provider %s does not support model listing", clientType), nil)
	}

	models, listErr := provider.ListModels(ctx)
	if listErr != nil {
		return nil, agenterrors.Wrap(listErr, fmt.Sprintf("failed to list models for %s", clientType))
	}

	return fillEligibleRoles(models), nil
}
