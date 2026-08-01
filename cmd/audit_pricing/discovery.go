package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// DiscoverResult holds the comparison between live API models and the
// provider's config file. It drives both the report and the auto-update logic.
type DiscoverResult struct {
	ProviderID  string
	APIError    string                         // non-empty if the live API call failed
	NewModels   []modelcontract.CanonicalModel // in live API but NOT in config
	StaleModels []string                       // in config but NOT in live API
}

// discoverProvider calls the live provider API to list models and compares
// the result against the provider's config file. Returns the set of models
// that exist in the live API but not in the embedded config (new) and vice
// versa (stale). If the API call fails, APIError is set and no comparison
// is performed.
func discoverProvider(ctx context.Context, providerID, configPath string) DiscoverResult {
	dr := DiscoverResult{ProviderID: providerID}

	clientType, err := api.ParseProviderName(providerID)
	if err != nil {
		dr.APIError = err.Error()
		return dr
	}

	canon, err := api.GetCanonicalModelsForProvider(ctx, clientType)
	if err != nil {
		dr.APIError = err.Error()
		return dr
	}

	// Build a set of config model IDs for comparison.
	cfg, err := providers.LoadProviderConfig(configPath)
	if err != nil {
		dr.APIError = fmt.Sprintf("load config: %v", err)
		return dr
	}
	configIDs := make(map[string]bool, len(cfg.Models.ModelInfo))
	for _, mi := range cfg.Models.ModelInfo {
		configIDs[mi.ID] = true
	}
	apiIDs := make(map[string]bool, len(canon))
	for _, m := range canon {
		apiIDs[m.ID] = true
		if !configIDs[m.ID] {
			dr.NewModels = append(dr.NewModels, m)
		}
	}
	for _, mi := range cfg.Models.ModelInfo {
		if !apiIDs[mi.ID] {
			dr.StaleModels = append(dr.StaleModels, mi.ID)
		}
	}
	sort.Slice(dr.NewModels, func(i, j int) bool {
		return dr.NewModels[i].ID < dr.NewModels[j].ID
	})
	sort.Strings(dr.StaleModels)
	return dr
}

// canonicalToConfigEntry converts a discovered canonical model into a JSON
// map suitable for appending to a provider config's model_info array.
func canonicalToConfigEntry(m modelcontract.CanonicalModel) map[string]interface{} {
	entry := map[string]interface{}{
		"id":             m.ID,
		"context_length": m.ContextWindow,
	}
	if m.DisplayName != "" {
		entry["name"] = m.DisplayName
	} else {
		entry["name"] = m.ID
	}
	if m.Description != "" {
		entry["description"] = m.Description
	}
	tags := modelcontract.CapabilityTags(m.Capabilities)
	if len(tags) > 0 {
		entry["tags"] = tags
	}
	if m.Pricing != nil {
		entry["input_cost"] = m.Pricing.InputPerMTok
		entry["output_cost"] = m.Pricing.OutputPerMTok
		if m.Pricing.CachedPerMTok > 0 {
			entry["cached_input_cost"] = m.Pricing.CachedPerMTok
		}
	}
	return entry
}

// addModelsToConfig appends new model_info entries (and optionally
// available_models entries) to the provider config file. Preserves all
// existing fields. Only adds models not already present in the config.
func addModelsToConfig(configPath string, newModels []modelcontract.CanonicalModel) (int, error) {
	if len(newModels) == 0 {
		return 0, nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return 0, fmt.Errorf("read config: %w", err)
	}
	var rawCfg map[string]interface{}
	if err := json.Unmarshal(data, &rawCfg); err != nil {
		return 0, fmt.Errorf("parse config: %w", err)
	}
	modelsRaw, ok := rawCfg["models"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("config has no models section")
	}
	modelInfoRaw, ok := modelsRaw["model_info"].([]interface{})
	if !ok {
		modelInfoRaw = []interface{}{}
	}

	// Build a set of existing IDs to avoid duplicates.
	existing := make(map[string]bool, len(modelInfoRaw))
	for _, entry := range modelInfoRaw {
		if mi, ok := entry.(map[string]interface{}); ok {
			if id, ok := safeID(mi); ok {
				existing[id] = true
			}
		}
	}

	added := 0
	for _, m := range newModels {
		if existing[m.ID] {
			continue
		}
		modelInfoRaw = append(modelInfoRaw, canonicalToConfigEntry(m))
		existing[m.ID] = true
		added++
	}

	if added > 0 {
		modelsRaw["model_info"] = modelInfoRaw

		// Also extend available_models if it's non-empty (live-discovery
		// providers with empty available_models stay empty).
		if availRaw, ok := modelsRaw["available_models"].([]interface{}); ok && len(availRaw) > 0 {
			availSet := make(map[string]bool, len(availRaw))
			for _, v := range availRaw {
				if s, ok := v.(string); ok {
					availSet[s] = true
				}
			}
			for _, m := range newModels {
				if !availSet[m.ID] {
					availRaw = append(availRaw, m.ID)
					availSet[m.ID] = true
				}
			}
			modelsRaw["available_models"] = availRaw
		}

		out, err := json.MarshalIndent(rawCfg, "", "  ")
		if err != nil {
			return added, fmt.Errorf("marshal config: %w", err)
		}
		if err := os.WriteFile(configPath, append(out, '\n'), 0o644); err != nil {
			return added, fmt.Errorf("write config: %w", err)
		}
	}
	return added, nil
}

// addModelsToManifest appends pricing entries for newly discovered models
// to the manifest, then writes it back to disk. Skips models already in
// the manifest. Sets last_verified to today.
func addModelsToManifest(manifestPath, providerID string, newModels []modelcontract.CanonicalModel) (int, error) {
	if len(newModels) == 0 {
		return 0, nil
	}
	// Load the on-disk manifest (not the embedded copy — we need to write).
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return 0, fmt.Errorf("read manifest: %w", err)
	}
	var diskManifests map[string]ProviderManifest
	if err := json.Unmarshal(data, &diskManifests); err != nil {
		return 0, fmt.Errorf("parse manifest: %w", err)
	}

	pm, ok := diskManifests[providerID]
	if !ok {
		// New provider entry — initialize.
		pm = ProviderManifest{
			Source:       fmt.Sprintf("https://api.%s.com/v1/models", providerID),
			LastVerified: time.Now().UTC().Format("2006-01-02"),
		}
	}

	existing := make(map[string]bool, len(pm.Models))
	for _, m := range pm.Models {
		existing[m.ID] = true
	}

	added := 0
	for _, m := range newModels {
		if existing[m.ID] {
			continue
		}
		entry := PricingEntry{
			ID:            m.ID,
			InputPerMTok:  0,
			OutputPerMTok: 0,
		}
		if m.Pricing != nil {
			entry.InputPerMTok = m.Pricing.InputPerMTok
			entry.OutputPerMTok = m.Pricing.OutputPerMTok
			entry.CachedPerMTok = m.Pricing.CachedPerMTok
		}
		pm.Models = append(pm.Models, entry)
		existing[m.ID] = true
		added++
	}

	if added > 0 {
		pm.LastVerified = time.Now().UTC().Format("2006-01-02")
		diskManifests[providerID] = pm
		return added, saveManifest(manifestPath, diskManifests)
	}
	return 0, nil
}

// formatDiscoverReport produces a human-readable report of discovery results.
func formatDiscoverReport(results []DiscoverResult) string {
	var sb strings.Builder
	totalNew, totalStale, totalErr := 0, 0, 0
	for _, dr := range results {
		if dr.APIError != "" {
			sb.WriteString(fmt.Sprintf("\n%s: API ERROR (%s)\n", dr.ProviderID, dr.APIError))
			totalErr++
			continue
		}
		if len(dr.NewModels) == 0 && len(dr.StaleModels) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("\n%s\n", dr.ProviderID))
		for _, m := range dr.NewModels {
			in, out := 0.0, 0.0
			if m.Pricing != nil {
				in, out = m.Pricing.InputPerMTok, m.Pricing.OutputPerMTok
			}
			estimated := ""
			if m.Pricing != nil && m.Pricing.Estimated {
				estimated = " [estimated]"
			}
			sb.WriteString(fmt.Sprintf("  + %s    ctx=%d  in=$%.4f  out=$%.4f%s\n",
				m.ID, m.ContextWindow, in, out, estimated))
			totalNew++
		}
		for _, id := range dr.StaleModels {
			sb.WriteString(fmt.Sprintf("  - %s    (in config, not in live API)\n", id))
			totalStale++
		}
	}
	sb.WriteString(fmt.Sprintf("\nDiscovery: %d new, %d stale, %d errors\n",
		totalNew, totalStale, totalErr))
	return sb.String()
}

// discoverAndAuditAll runs discovery for all provider config files, optionally
// auto-applying new models to configs and the manifest. Returns the discovery
// results for reporting and the list of files that were modified.
func discoverAndAuditAll(ctx context.Context, configsDir, manifestPath string, doUpdate bool) ([]DiscoverResult, []string) {
	// Discover providers that have config files but may not be in the manifest.
	configFiles, err := filepath.Glob(filepath.Join(configsDir, "*.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "glob configs: %v\n", err)
		return nil, nil
	}
	sort.Strings(configFiles)

	var allResults []DiscoverResult
	var modifiedFiles []string

	for _, cfgPath := range configFiles {
		providerID := strings.TrimSuffix(filepath.Base(cfgPath), ".json")

		// Skip local-only providers — no remote API to discover from.
		cfg, err := providers.LoadProviderConfig(cfgPath)
		if err != nil || cfg == nil {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(cfg.Endpoint), "https://") {
			continue
		}

		dr := discoverProvider(ctx, providerID, cfgPath)
		allResults = append(allResults, dr)

		if doUpdate && dr.APIError == "" && len(dr.NewModels) > 0 {
			n, err := addModelsToConfig(cfgPath, dr.NewModels)
			if err != nil {
				fmt.Fprintf(os.Stderr, "update config %s: %v\n", providerID, err)
			} else if n > 0 {
				fmt.Fprintf(os.Stderr, "added %d model(s) to %s config\n", n, providerID)
				modifiedFiles = append(modifiedFiles, cfgPath)
			}
			n2, err := addModelsToManifest(manifestPath, providerID, dr.NewModels)
			if err != nil {
				fmt.Fprintf(os.Stderr, "update manifest %s: %v\n", providerID, err)
			} else if n2 > 0 {
				fmt.Fprintf(os.Stderr, "added %d model(s) to %s manifest\n", n2, providerID)
			}
		}
	}
	return allResults, modifiedFiles
}
