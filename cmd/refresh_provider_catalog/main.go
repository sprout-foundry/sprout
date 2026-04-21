package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
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

		models, err := api.GetModelsForProvider(clientType)
		if err != nil {
			fmt.Fprintf(os.Stderr, "keep existing catalog models for %s: %v\n", providerID, err)
			continue
		}
		if len(models) == 0 {
			continue
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

		// Write per-provider JSON for the registry server.
		if *registryDir != "" {
			writeProviderJSON(*registryDir, providerID, now, models)
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

	if err := os.WriteFile(catalogPath, encoded, 0o644); err != nil {
		failf("write catalog: %v", err)
	}

	fmt.Printf("wrote %s\n", catalogPath)
	if *registryDir != "" {
		fmt.Printf("wrote per-provider JSON files to %s/models/\n", *registryDir)
	}
}

// providerRegistryFile is the JSON schema for a per-provider model file served
// by the registry. Uses api.ModelInfo rather than modelregistry.ModelInfo to
// avoid a dependency on the modelregistry package (which would create an import
// cycle through agent_api). Both types have identical JSON representations.
type providerRegistryFile struct {
	UpdatedAt string                `json:"updated_at"`
	Models    []api.ModelInfo       `json:"models"`
}

// writeProviderJSON writes a per-provider model JSON file for the model registry server.
func writeProviderJSON(registryDir, providerID, updatedAt string, models []api.ModelInfo) {
	modelsDir := filepath.Join(registryDir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create registry dir: %v\n", err)
		return
	}

	payload := providerRegistryFile{
		UpdatedAt: updatedAt,
		Models:    models,
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
