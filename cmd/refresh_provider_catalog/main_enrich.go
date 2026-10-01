package main

// main_enrich.go — the model enrichment and normalization layer for the
// refresh_provider_catalog tool: normalizeModels, enrichFromConfig,
// mergeConfigOnlyModels, fetchOpenRouterModels, and
// enrichFromOpenRouter. Split out of main.go.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

func normalizeModels(models []api.ModelInfo) []providercatalog.Model {
	out := make([]providercatalog.Model, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		// Filter out OpenRouter meta-models (routing aliases, not real models).
		if openRouterMetaSet[id] {
			continue
		}
		// Filter out models with negative costs (sentinel values from
		// OpenRouter variable-pricing meta-models or malformed data).
		if model.InputCost < 0 || model.OutputCost < 0 {
			continue
		}
		out = append(out, providercatalog.Model{
			ID:            id,
			Name:          strings.TrimSpace(model.Name),
			Description:   strings.TrimSpace(model.Description),
			ContextLength: model.ContextLength,
			Tags:          append([]string(nil), model.Tags...),
			InputCost:     model.InputCost,
			OutputCost:    model.OutputCost,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID)
	})
	return out
}

// enrichFromConfig merges pricing, context window, capabilities, and display
// metadata from the embedded provider config's model_info entries into the
// canonical models returned by the provider's API. API-provided data takes
// precedence — config values only fill gaps.
func enrichFromConfig(providerID string, models []modelcontract.CanonicalModel) []modelcontract.CanonicalModel {
	configPath := filepath.Join("pkg", "agent_providers", "configs", providerID+".json")
	cfg, err := providers.LoadProviderConfig(configPath)
	if err != nil || len(cfg.Models.ModelInfo) == 0 {
		return models
	}

	lookup := make(map[string]providers.ModelInfo, len(cfg.Models.ModelInfo))
	for _, mi := range cfg.Models.ModelInfo {
		lookup[mi.ID] = mi
	}

	for i := range models {
		mi, ok := lookupModel(lookup, models[i].ID)
		if !ok {
			continue
		}

		// Fill pricing when it is nil OR present but zero-valued
		// (common for OpenAI-compatible endpoints that return empty Pricing structs).
		pricingIsZero := models[i].Pricing == nil ||
			(models[i].Pricing.InputPerMTok == 0 && models[i].Pricing.OutputPerMTok == 0)
		if pricingIsZero && (mi.InputCost > 0 || mi.OutputCost > 0) {
			models[i].Pricing = &modelcontract.Pricing{
				InputPerMTok:  mi.InputCost,
				OutputPerMTok: mi.OutputCost,
				CachedPerMTok: mi.CachedCost,
				Currency:      "USD",
				Source:        "embedded-config",
			}
		}
		if models[i].ContextWindow == 0 && mi.ContextLength > 0 {
			models[i].ContextWindow = mi.ContextLength
		}
		if models[i].DisplayName == "" && mi.Name != "" {
			models[i].DisplayName = mi.Name
		}
		if models[i].Description == "" && mi.Description != "" {
			models[i].Description = mi.Description
		}
		// Merge tags into capabilities without overwriting API-provided caps
		if len(mi.Tags) > 0 {
			caps := modelcontract.CapabilitiesFromTags(mi.Tags)
			if models[i].Capabilities.Tools == nil {
				models[i].Capabilities.Tools = caps.Tools
			}
			if models[i].Capabilities.Vision == nil {
				models[i].Capabilities.Vision = caps.Vision
			}
			if models[i].Capabilities.Reasoning == nil {
				models[i].Capabilities.Reasoning = caps.Reasoning
			}
			if models[i].Capabilities.StructuredOutput == nil {
				models[i].Capabilities.StructuredOutput = caps.StructuredOutput
			}
		}
	}

	return models
}

// mergeConfigOnlyModels adds models from the embedded provider config that
// the provider API didn't return. This ensures models like deepseek-chat or
// deepseek-reasoner — which exist in config but not in /v1/models — appear
// in the catalog with their config-provided metadata.
func mergeConfigOnlyModels(providerID string, models []modelcontract.CanonicalModel) []modelcontract.CanonicalModel {
	configPath := filepath.Join("pkg", "agent_providers", "configs", providerID+".json")
	cfg, err := providers.LoadProviderConfig(configPath)
	if err != nil || len(cfg.Models.ModelInfo) == 0 {
		return models
	}

	existing := make(map[string]bool, len(models))
	for _, m := range models {
		existing[m.ID] = true
	}

	for _, mi := range cfg.Models.ModelInfo {
		if existing[mi.ID] {
			continue
		}
		models = append(models, modelcontract.CanonicalModel{
			ID:            mi.ID,
			DisplayName:   mi.Name,
			Description:   mi.Description,
			ContextWindow: mi.ContextLength,
			Status:        modelcontract.StatusActive,
			Capabilities:  modelcontract.CapabilitiesFromTags(mi.Tags),
			Source:        "embedded-config",
		})
	}
	return models
}

// fetchOpenRouterModels fetches OpenRouter's public model list and builds a
// lookup map keyed by the model ID (with provider/ prefix stripped). The map
// is cached in openRouterModelsCache so it's only fetched once per run.
func fetchOpenRouterModels(ctx context.Context) map[string]openRouterModel {
	if openRouterModelsCache != nil {
		return openRouterModelsCache
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openRouterModelsURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: build OpenRouter request: %v\n", err)
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: fetch OpenRouter models: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "warn: OpenRouter returned HTTP %d\n", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: read OpenRouter response: %v\n", err)
		return nil
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(body, &orResp); err != nil {
		fmt.Fprintf(os.Stderr, "warn: parse OpenRouter response: %v\n", err)
		return nil
	}

	cache := make(map[string]openRouterModel, len(orResp.Data))
	for _, m := range orResp.Data {
		// Strip the provider/ prefix (e.g. "deepseek/deepseek-v4-flash" → "deepseek-v4-flash")
		_, id, _ := strings.Cut(m.ID, "/")
		cache[id] = m
	}

	openRouterModelsCache = cache
	return cache
}

// enrichFromOpenRouter fills pricing gaps by cross-referencing OpenRouter's
// public model list. OpenRouter aggregates pricing for 300+ models across
// providers. Prices include OpenRouter's markup over native provider pricing,
// so the source is stamped as "openrouter-cross-ref". Runs after
// enrichFromConfig; only fills models where Pricing is still nil or zero-valued.
func enrichFromOpenRouter(ctx context.Context, models []modelcontract.CanonicalModel) []modelcontract.CanonicalModel {
	cache := fetchOpenRouterModels(ctx)
	if cache == nil {
		return models
	}

	for i := range models {
		// Skip models that already have meaningful pricing (non-nil AND non-zero).
		pricingHasValues := models[i].Pricing != nil &&
			(models[i].Pricing.InputPerMTok > 0 || models[i].Pricing.OutputPerMTok > 0)
		if pricingHasValues {
			continue
		}

		orModel, ok := cache[models[i].ID]
		if !ok {
			// Try fuzzy match: strip date suffix (e.g. "gpt-5-2025-08-07" → "gpt-5").
			orModel, ok = cache[stripDateSuffix(models[i].ID)]
		}
		if !ok {
			continue
		}

		pricing := &modelcontract.Pricing{
			Currency: "USD",
			Source:   "openrouter-cross-ref",
		}

		if orModel.Pricing.Prompt != "" {
			if v, err := strconv.ParseFloat(orModel.Pricing.Prompt, 64); err == nil && v > 0 {
				pricing.InputPerMTok = v * 1e6
			}
		}
		if orModel.Pricing.Completion != "" {
			if v, err := strconv.ParseFloat(orModel.Pricing.Completion, 64); err == nil && v > 0 {
				pricing.OutputPerMTok = v * 1e6
			}
		}
		if orModel.Pricing.InputCacheRead != "" {
			if v, err := strconv.ParseFloat(orModel.Pricing.InputCacheRead, 64); err == nil && v > 0 {
				pricing.CachedPerMTok = v * 1e6
			}
		}

		// Only assign pricing if at least one field was populated; leaving
		// Pricing as nil/zero-valued avoids downstream code misinterpreting
		// a zero-valued struct as "priced at $0" rather than "pricing unknown".
		if pricing.InputPerMTok > 0 || pricing.OutputPerMTok > 0 || pricing.CachedPerMTok > 0 {
			models[i].Pricing = pricing
		}
	}

	return models
}
