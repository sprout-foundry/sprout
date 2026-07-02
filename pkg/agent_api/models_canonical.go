package api

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
	"github.com/sprout-foundry/sprout/pkg/modelregistry"
)

// openRouterReference lazily builds (once per process) the OpenRouter-derived
// reference catalog used to enrich providers with sparse native metadata (e.g.
// OpenAI). Best-effort: a fetch failure yields nil and enrichment is skipped.
var (
	openRouterRefOnce sync.Once
	openRouterRefCat  *modelcontract.ReferenceCatalog
)

func openRouterReference(ctx context.Context) *modelcontract.ReferenceCatalog {
	openRouterRefOnce.Do(func() {
		canon, err := (modelcontract.OpenRouterAdapter{}).ListModels(ctx)
		if err == nil && len(canon) > 0 {
			openRouterRefCat = modelcontract.NewReferenceCatalog(canon)
		}
	})
	return openRouterRefCat
}

// canonicalAdapterModels returns canonical models from a provider's adapter,
// injecting any dependencies it needs (OpenAI requires an API key and the
// OpenRouter reference catalog). The bool reports whether an adapter handled
// the provider; providers without one fall back to the legacy ListModels path.
func canonicalAdapterModels(ctx context.Context, providerID string) ([]modelcontract.CanonicalModel, bool, error) {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "deepinfra":
		m, err := modelcontract.DeepInfraAdapter{}.ListModels(ctx)
		return m, true, err
	case "openrouter":
		m, err := modelcontract.OpenRouterAdapter{}.ListModels(ctx)
		return m, true, err
	case "openai":
		apiKey, _ := credentials.ResolveProviderAPIKey("openai", "OpenAI")
		m, err := modelcontract.OpenAIAdapter{APIKey: apiKey, Reference: openRouterReference(ctx)}.ListModels(ctx)
		return m, true, err
	case "cerebras":
		m, err := modelcontract.NewOpenAICompatAdapter("cerebras", "https://api.cerebras.ai/v1/models", "CEREBRAS_API_KEY").ListModels(ctx)
		return m, true, err
	case "chutes":
		m, err := modelcontract.NewOpenAICompatAdapter("chutes", "https://llm.chutes.ai/v1/models", "CHUTES_API_KEY").ListModels(ctx)
		return m, true, err
	case "ollama-cloud":
		m, err := modelcontract.NewOpenAICompatAdapter("ollama-cloud", "https://ollama.com/v1/models", "OLLAMA_API_KEY").ListModels(ctx)
		return m, true, err
	case "deepseek":
		m, err := modelcontract.NewOpenAICompatAdapter("deepseek", "https://api.deepseek.com/v1/models", "DEEPSEEK_API_KEY").ListModels(ctx)
		return m, true, err
	case "mistral":
		m, err := modelcontract.NewOpenAICompatAdapter("mistral", "https://api.mistral.ai/v1/models", "MISTRAL_API_KEY").ListModels(ctx)
		return m, true, err
	case "minimax":
		// MiniMax exposes a single OpenAI-compatible /v1/models endpoint that
		// works identically for both pay-as-you-go API keys and Token Plan
		// (subscription) keys. The same key hits the same endpoint regardless
		// of plan, so one adapter covers both.
		m, err := modelcontract.NewOpenAICompatAdapter("minimax", "https://api.minimax.io/v1/models", "MINIMAX_API_KEY").ListModels(ctx)
		return m, true, err
	case "zai":
		// Z.AI general API — full GLM catalog.
		m, err := modelcontract.NewOpenAICompatAdapter("zai", "https://api.z.ai/api/paas/v4/models", "ZAI_API_KEY").ListModels(ctx)
		return m, true, err
	case "zai-coding":
		// GLM Coding Plan — dedicated coding-plan endpoint with a separate
		// key (ZAI_CODING_API_KEY). Returns the coding-eligible model subset.
		m, err := modelcontract.NewOpenAICompatAdapter("zai-coding", "https://api.z.ai/api/coding/paas/v4/models", "ZAI_CODING_API_KEY").ListModels(ctx)
		return m, true, err
	default:
		return nil, false, nil
	}
}

// CanonicalToModelInfo projects a canonical model down to the ModelInfo shape
// existing consumers expect. Known-true capabilities are surfaced as Tags so
// callers that inspect tags (e.g. the CLI "Supports tools" line) work unchanged.
// Exported for the registry publisher (cmd/refresh_provider_catalog).
func CanonicalToModelInfo(m modelcontract.CanonicalModel) ModelInfo {
	mi := ModelInfo{
		ID:            m.ID,
		Name:          m.DisplayName,
		Description:   m.Description,
		Provider:      m.Provider,
		ContextLength: m.ContextWindow,
		EligibleRoles: m.EligibleRoles,
		Tags:          modelcontract.CapabilityTags(m.Capabilities),
	}
	if mi.Name == "" {
		mi.Name = m.ID
	}
	if m.Pricing != nil {
		mi.InputCost = m.Pricing.InputPerMTok
		mi.OutputCost = m.Pricing.OutputPerMTok
		mi.CachedInputCost = m.Pricing.CachedPerMTok
		if mi.InputCost > 0 || mi.OutputCost > 0 {
			mi.Cost = (mi.InputCost + mi.OutputCost) / 2.0
		}
	}
	return mi
}

// modelInfoToCanonical projects a legacy ModelInfo up to the canonical shape.
// Used for providers that don't yet have a canonical adapter, so the publisher
// can emit a uniform canonical file. Capabilities are recovered from Tags
// (known-true on presence; unknown otherwise).
func modelInfoToCanonical(m ModelInfo) modelcontract.CanonicalModel {
	cm := modelcontract.CanonicalModel{
		ID:            m.ID,
		Provider:      m.Provider,
		DisplayName:   m.Name,
		Description:   m.Description,
		ContextWindow: m.ContextLength,
		Status:        modelcontract.StatusActive,
		Capabilities:  modelcontract.CapabilitiesFromTags(m.Tags),
		EligibleRoles: m.EligibleRoles,
	}
	if m.InputCost > 0 || m.OutputCost > 0 {
		cm.Pricing = &modelcontract.Pricing{
			InputPerMTok:  m.InputCost,
			OutputPerMTok: m.OutputCost,
			CachedPerMTok: m.CachedInputCost,
			Currency:      "USD",
		}
	}
	return cm
}

// GetCanonicalModelsForProvider returns canonical models for a provider — from
// its adapter where one exists, otherwise by projecting the legacy ModelInfo
// path up to canonical. Used by the registry publisher to emit the canonical
// per-provider file.
func GetCanonicalModelsForProvider(ctx context.Context, clientType ClientType) ([]modelcontract.CanonicalModel, error) {
	if canon, handled, err := canonicalAdapterModels(ctx, string(clientType)); handled {
		if err != nil {
			return nil, agenterrors.Wrap(err, fmt.Sprintf("failed to list models for %s", clientType))
		}
		modelcontract.FillEligibleRoles(canon)
		return canon, nil
	}
	models, err := GetModelsForProviderCtx(ctx, clientType)
	if err != nil {
		return nil, err
	}
	canon := make([]modelcontract.CanonicalModel, len(models))
	for i := range models {
		canon[i] = modelInfoToCanonical(models[i])
	}
	modelcontract.FillEligibleRoles(canon)
	return canon, nil
}

// convertRegistryModels converts modelregistry.RawModel slices to ModelInfo.
func convertRegistryModels(raw []modelregistry.RawModel) []ModelInfo {
	out := make([]ModelInfo, len(raw))
	for i, m := range raw {
		out[i] = ModelInfo{
			ID:               m.ID,
			Name:             m.Name,
			Description:      m.Description,
			Provider:         m.Provider,
			Size:             m.Size,
			Cost:             m.Cost,
			InputCost:        m.InputCost,
			OutputCost:       m.OutputCost,
			CachedInputCost:  m.CachedInputCost,
			ContextLength:    m.ContextLength,
			Tags:             append([]string(nil), m.Tags...),
			EligibleRoles:    append([]string(nil), m.EligibleRoles...),
			RecommendedRoles: append([]string(nil), m.RecommendedRoles...),
			Warnings:         append([]string(nil), m.Warnings...),
		}
	}
	return out
}
