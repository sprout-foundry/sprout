package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
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
		// A recommendation that no longer exists in the provider's live
		// model list is stale by definition — recompute it instead of
		// serving a retired model to fresh installs. A recommendation that
		// still exists is left alone: auto-picking "newest" would churn
		// users onto unproven checkpoints (or regressions) unattended.
		if !modelListed(provider.RecommendedModel, provider.Models) {
			if provider.DefaultModel != "" && modelListed(provider.DefaultModel, provider.Models) {
				provider.RecommendedModel = provider.DefaultModel
			} else if len(provider.Models) > 0 {
				provider.RecommendedModel = provider.Models[0].ID
			}
			fmt.Fprintf(os.Stdout, "recomputed stale recommendation for %s: %s -> %s\n",
				providerID, provider.DefaultModel, provider.RecommendedModel)
		}
		if provider.RecommendedModel == "" {
			if provider.DefaultModel != "" {
				provider.RecommendedModel = provider.DefaultModel
			} else if len(provider.Models) > 0 {
				provider.RecommendedModel = provider.Models[0].ID
			}
		}
		if !modelListed(provider.DefaultModel, provider.Models) && len(provider.Models) > 0 {
			previous := provider.DefaultModel
			provider.DefaultModel = provider.RecommendedModel
			fmt.Fprintf(os.Stdout, "recomputed stale default for %s: %s -> %s\n",
				providerID, previous, provider.DefaultModel)
		}
		// Zombie-pricing warning: a recommended model that is still listed
		// but has lost its pricing while sibling models report it is
		// usually a deprecation in progress (e.g. DeepSeek-V3.1-Terminus
		// kept serving long after its pricing stopped updating). The
		// recommendation still works, so this stays a warning for a human
		// rather than an auto-recompute — but it should be loud.
		if rec := provider.RecommendedModel; modelListed(rec, provider.Models) {
			if hasPricing, hasAnyPricing := modelPricingState(rec, provider.Models); !hasPricing && hasAnyPricing {
				fmt.Fprintf(os.Stdout, "WARNING: recommended model %s for %s has no pricing while sibling models do — check for a newer checkpoint\n",
					rec, providerID)
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
		UpdatedAt:       now,
		Source:          "refresh_provider_catalog",
		OnboardingOrder: baseCatalog.OnboardingOrder,
		Providers:       make([]providercatalog.Provider, 0, len(orderedIDs)),
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

// modelPricingState reports whether the named model has pricing and whether
// any sibling model in the list does. Used to detect zombie recommendations:
// still listed but silently losing maintenance (pricing first to go).
func modelPricingState(modelID string, models []providercatalog.Model) (hasPricing, hasAnyPricing bool) {
	for _, m := range models {
		has := m.InputCost > 0 || m.OutputCost > 0
		if has {
			hasAnyPricing = true
		}
		if strings.EqualFold(m.ID, modelID) {
			hasPricing = has
		}
	}
	return hasPricing, hasAnyPricing
}

// modelListed reports whether modelID appears in the provider's live model
// list. Empty modelID is treated as unlisted (nothing to recommend).
func modelListed(modelID string, models []providercatalog.Model) bool {
	if strings.TrimSpace(modelID) == "" {
		return false
	}
	for _, m := range models {
		if strings.EqualFold(m.ID, modelID) {
			return true
		}
	}
	return false
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
