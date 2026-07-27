package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// openRouterModelsURL is the public OpenRouter endpoint for fetching model
// pricing data. Exposed as a package-level var so tests can override it.
var openRouterModelsURL = "https://openrouter.ai/api/v1/models"

// openRouterModelsCache holds the per-run OpenRouter model lookup. Populated
// lazily on the first call to enrichFromOpenRouter and reused for every
// provider in that run.
var openRouterModelsCache map[string]openRouterModel

// openRouterResponse mirrors the JSON shape from /api/v1/models.
type openRouterResponse struct {
	Data []openRouterModel `json:"data"`
}

// openRouterModel holds the subset of fields we need from OpenRouter.
type openRouterModel struct {
	ID      string            `json:"id"`
	Pricing openRouterPricing `json:"pricing"`
}

type openRouterPricing struct {
	Prompt         string `json:"prompt"`
	Completion     string `json:"completion"`
	InputCacheRead string `json:"input_cache_read"`
}

func main() {
	registryDir := flag.String("registry-dir", "", "output directory for per-provider JSON files (for model registry server)")
	flag.Parse()

	repoRoot, err := os.Getwd()
	if err != nil {
		failf("resolve working directory: %v", err)
	}

	catalogPath := filepath.Join(repoRoot, "pkg", "providercatalog", "providers.json")
	baseCatalog := providercatalog.Current()

	providerIndex := make(map[string]providercatalog.Provider, len(baseCatalog.Providers))
	for _, provider := range baseCatalog.Providers {
		providerIndex[provider.ID] = provider
	}

	orderedIDs := make([]string, 0, len(baseCatalog.Providers))
	for _, provider := range baseCatalog.Providers {
		orderedIDs = append(orderedIDs, provider.ID)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	for _, providerID := range orderedIDs {
		clientType, err := api.ParseProviderName(providerID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", providerID, err)
			continue
		}

		canon, err := api.GetCanonicalModelsForProvider(context.Background(), clientType)
		if err != nil {
			fmt.Fprintf(os.Stderr, "keep existing catalog models for %s: %v\n", providerID, err)
			continue
		}
		if len(canon) == 0 {
			continue
		}

		canon = enrichFromConfig(providerID, canon)
		canon = mergeConfigOnlyModels(providerID, canon)

		// Final fallback: fill pricing gaps from OpenRouter's public model list.
		// Uses a shared cache so OpenRouter is only queried once per run.
		orCtx, orCancel := context.WithTimeout(context.Background(), 15*time.Second)
		canon = enrichFromOpenRouter(orCtx, canon)
		orCancel()

		// Project to ModelInfo for the baked providers.json catalog; the full
		// canonical models are published to the per-provider registry file.
		models := make([]api.ModelInfo, len(canon))
		for i := range canon {
			models[i] = api.CanonicalToModelInfo(canon[i])
		}

		provider := providerIndex[providerID]
		provider.Models = normalizeModels(models)
		if provider.RecommendedModel == "" {
			if provider.DefaultModel != "" {
				provider.RecommendedModel = provider.DefaultModel
			} else if len(provider.Models) > 0 {
				provider.RecommendedModel = provider.Models[0].ID
			}
		}
		providerIndex[providerID] = provider
		fmt.Fprintf(os.Stdout, "updated %s with %d models\n", providerID, len(provider.Models))

		// Write per-provider canonical JSON for the registry server.
		if *registryDir != "" {
			// Carry forward probe data from any prior per-provider file so that
			// refresh_provider_catalog alone (without enrich_registry) doesn't
			// silently drop Probe + RecommendedRoles.
			canon = carryForwardProbeData(*registryDir, providerID, canon)
			writeProviderJSON(*registryDir, providerID, now, canon)
		}
	}

	nextCatalog := providercatalog.Catalog{
		UpdatedAt: now,
		Source:    "refresh_provider_catalog",
		Providers: make([]providercatalog.Provider, 0, len(orderedIDs)),
	}

	for _, providerID := range orderedIDs {
		nextCatalog.Providers = append(nextCatalog.Providers, providerIndex[providerID])
	}

	encoded, err := json.MarshalIndent(nextCatalog, "", "  ")
	if err != nil {
		failf("marshal catalog: %v", err)
	}
	encoded = append(encoded, '\n')

	if *registryDir == "" {
		if err := os.WriteFile(catalogPath, encoded, 0o644); err != nil {
			failf("write catalog: %v", err)
		}
		fmt.Printf("wrote %s\n", catalogPath)
	} else {
		fmt.Printf("wrote per-provider JSON files to %s/models/\n", *registryDir)
	}
}

// carryForwardProbeData reads any prior per-provider JSON file and stamps the
// Probe + RecommendedRoles from the prior file onto the freshly-built canonical
// models. Models that are new (not in the prior file) are left untouched;
// models removed by the provider (in fresh but not in prior) also keep no probe
// data — we don't carry stale verdicts for models that no longer exist. The
// models slice is mutated in-place and also returned for caller convenience.
// This ensures refresh_provider_catalog alone doesn't silently drop probe data.
func carryForwardProbeData(registryDir, providerID string, models []modelcontract.CanonicalModel) []modelcontract.CanonicalModel {
	priorPath := filepath.Join(registryDir, "models", providerID+".json")
	data, err := os.ReadFile(priorPath)
	if err != nil {
		// No prior file — nothing to carry forward.
		return models
	}

	var prior modelcontract.ProviderFile
	if err := json.Unmarshal(data, &prior); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not parse prior %s: %v\n", priorPath, err)
		return models
	}

	// Build a lookup of prior probe data keyed by model ID.
	priorProbe := make(map[string]*modelcontract.ProbeResult, len(prior.Models))
	priorRoles := make(map[string][]string, len(prior.Models))
	for _, m := range prior.Models {
		if m.Probe != nil {
			priorProbe[m.ID] = m.Probe
		}
		if len(m.RecommendedRoles) > 0 {
			priorRoles[m.ID] = append([]string(nil), m.RecommendedRoles...)
		}
	}

	// Stamp probe data onto the fresh models.
	for i := range models {
		if probe, ok := priorProbe[models[i].ID]; ok {
			models[i].Probe = probe
		}
		if roles, ok := priorRoles[models[i].ID]; ok {
			models[i].RecommendedRoles = roles
		}
	}

	return models
}

// writeProviderJSON writes a per-provider canonical model file (schema 2) for
// the model registry server. Older deployed clients that don't understand
// schema 2 reject it and gracefully fall back to the live provider API.
func writeProviderJSON(registryDir, providerID, updatedAt string, models []modelcontract.CanonicalModel) {
	modelsDir := filepath.Join(registryDir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create registry dir: %v\n", err)
		return
	}

	payload := modelcontract.ProviderFile{
		SchemaVersion: modelcontract.SchemaVersion,
		Provider:      providerID,
		GeneratedAt:   updatedAt,
		Models:        models,
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal %s registry: %v\n", providerID, err)
		return
	}
	encoded = append(encoded, '\n')

	filePath := filepath.Join(modelsDir, providerID+".json")
	if err := os.WriteFile(filePath, encoded, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s registry: %v\n", providerID, err)
		return
	}

	fmt.Fprintf(os.Stdout, "  → wrote %s (%d models)\n", filePath, len(models))
}

// openRouterMetaSet is the denylist used inside normalizeModels to filter
// out OpenRouter routing aliases that are not real models.
var openRouterMetaSet = map[string]bool{
	"openrouter/auto":        true,
	"openrouter/auto-beta":   true,
	"openrouter/bodybuilder": true,
	"openrouter/fusion":      true,
	"openrouter/pareto-code": true,
}

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

func failf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// dateSuffixRE matches a YYYY-MM-DD date suffix (e.g. "-2025-08-07").
var dateSuffixRE = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

// stripDateSuffix removes a YYYY-MM-DD date suffix from a model ID.
// "gpt-5-2025-08-07" → "gpt-5". Returns the original if no suffix found.
func stripDateSuffix(id string) string {
	if dateSuffixRE.MatchString(id) {
		return id[:len(id)-11] // strip "-YYYY-MM-DD" (11 chars)
	}
	return id
}

// lookupModel looks up a model ID in the config lookup map, with a fuzzy
// fallback that strips date suffixes (e.g. "gpt-5-2025-08-07" → "gpt-5").
func lookupModel(lookup map[string]providers.ModelInfo, id string) (providers.ModelInfo, bool) {
	if mi, ok := lookup[id]; ok {
		return mi, true
	}
	if stripped := stripDateSuffix(id); stripped != id {
		if mi, ok := lookup[stripped]; ok {
			return mi, true
		}
	}
	return providers.ModelInfo{}, false
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
