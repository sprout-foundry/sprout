package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

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

func normalizeModels(models []api.ModelInfo) []providercatalog.Model {
	out := make([]providercatalog.Model, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
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
