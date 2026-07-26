package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

const floatEpsilon = 1e-9

// ModelAudit holds the audit result for a single model.
type ModelAudit struct {
	ModelID     string
	Status      string // "verified", "drift", "missing_in_config", "missing_in_manifest"
	InputCost   float64
	OutputCost  float64
	CachedCost  float64
	Drifts      []PriceDrift
}

// PriceDrift describes a single field where config and manifest diverge.
type PriceDrift struct {
	Field    string  // "input_cost", "output_cost", "cached_input_cost"
	Config   float64
	Manifest float64
}

// ProviderAuditResult holds the audit result for one provider.
type ProviderAuditResult struct {
	ProviderID   string
	Source       string
	LastVerified string
	Models       []ModelAudit
}

// safeID safely extracts the "id" string from a JSON node, returning false
// if the field is missing or not a string.
func safeID(entry map[string]interface{}) (string, bool) {
	v, ok := entry["id"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// auditProvider compares the manifest against the provider's config file and
// returns the audit result with per-model status and drift details.
func auditProvider(providerID string, manifest ProviderManifest, configPath string) ProviderAuditResult {
	result := ProviderAuditResult{
		ProviderID:   providerID,
		Source:       manifest.Source,
		LastVerified: manifest.LastVerified,
	}
	cfg, err := providers.LoadProviderConfig(configPath)
	if err != nil {
		for _, m := range manifest.Models {
			result.Models = append(result.Models, ModelAudit{
				ModelID:    m.ID,
				Status:     "missing_in_config",
				InputCost:  m.InputPerMTok,
				OutputCost: m.OutputPerMTok,
				CachedCost: m.CachedPerMTok,
			})
		}
		return result
	}
	configLookup := make(map[string]providers.ModelInfo, len(cfg.Models.ModelInfo))
	for _, mi := range cfg.Models.ModelInfo {
		configLookup[mi.ID] = mi
	}

	manifestChecked := make(map[string]bool)
	for _, mp := range manifest.Models {
		manifestChecked[mp.ID] = true
		cm, ok := configLookup[mp.ID]
		if !ok {
			result.Models = append(result.Models, ModelAudit{
				ModelID:    mp.ID,
				Status:     "missing_in_config",
				InputCost:  mp.InputPerMTok,
				OutputCost: mp.OutputPerMTok,
				CachedCost: mp.CachedPerMTok,
			})
			continue
		}
		drifts := comparePricing(mp, cm)
		status := "verified"
		if len(drifts) > 0 {
			status = "drift"
		}
		result.Models = append(result.Models, ModelAudit{
			ModelID:    mp.ID,
			Status:     status,
			InputCost:  cm.InputCost,
			OutputCost: cm.OutputCost,
			CachedCost: cm.CachedCost,
			Drifts:     drifts,
		})
	}
	for _, mi := range cfg.Models.ModelInfo {
		if !manifestChecked[mi.ID] {
			result.Models = append(result.Models, ModelAudit{
				ModelID:    mi.ID,
				Status:     "missing_in_manifest",
				InputCost:  mi.InputCost,
				OutputCost: mi.OutputCost,
				CachedCost: mi.CachedCost,
			})
		}
	}

	sort.Slice(result.Models, func(i, j int) bool {
		return strings.ToLower(result.Models[i].ModelID) < strings.ToLower(result.Models[j].ModelID)
	})
	return result
}

// comparePricing returns field-level differences between manifest and config pricing.
// Cached pricing is only compared when the manifest specifies a non-zero value.
func comparePricing(manifest PricingEntry, configPricing providers.ModelInfo) []PriceDrift {
	var drifts []PriceDrift
	if !floatsEqual(configPricing.InputCost, manifest.InputPerMTok) {
		drifts = append(drifts, PriceDrift{"input_cost", configPricing.InputCost, manifest.InputPerMTok})
	}
	if !floatsEqual(configPricing.OutputCost, manifest.OutputPerMTok) {
		drifts = append(drifts, PriceDrift{"output_cost", configPricing.OutputCost, manifest.OutputPerMTok})
	}
	if manifest.CachedPerMTok > 0 && !floatsEqual(configPricing.CachedCost, manifest.CachedPerMTok) {
		drifts = append(drifts, PriceDrift{"cached_input_cost", configPricing.CachedCost, manifest.CachedPerMTok})
	}
	return drifts
}

// formatReport produces a human-readable audit report suitable for CI logs.
func formatReport(results []ProviderAuditResult, _ string) string {
	var sb strings.Builder
	sb.WriteString("Pricing Audit Report\n")
	sb.WriteString("════════════════════════════════════════\n")
	tv, td, tm, te := 0, 0, 0, 0
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("\n%s (source: %s)\n", r.ProviderID, r.Source))
		for _, m := range r.Models {
			switch m.Status {
			case "verified":
				sb.WriteString(fmt.Sprintf("  ✓ %s    in=$%.4f  out=$%.4f  cached=$%.4f\n",
					m.ModelID, m.InputCost, m.OutputCost, m.CachedCost))
				tv++
			case "drift":
				var parts []string
				for _, d := range m.Drifts {
					parts = append(parts, fmt.Sprintf("%s: config=$%.4f manifest=$%.4f", d.Field, d.Config, d.Manifest))
				}
				sb.WriteString(fmt.Sprintf("  ✗ %s    DRIFT: %s\n", m.ModelID, strings.Join(parts, " ")))
				td++
			case "missing_in_config":
				cs := ""
				if m.CachedCost > 0 {
					cs = fmt.Sprintf(" cached=$%.4f", m.CachedCost)
				}
				sb.WriteString(fmt.Sprintf("  ⚠ %s    MISSING in config (manifest has in=$%.4f out=$%.4f%s)\n",
					m.ModelID, m.InputCost, m.OutputCost, cs))
				tm++
			case "missing_in_manifest":
				sb.WriteString(fmt.Sprintf("  ? %s    in config but NOT in manifest\n", m.ModelID))
				te++
			}
		}
	}
	sb.WriteString(fmt.Sprintf("\nSummary: %d verified, %d drift, %d missing, %d extra\n", tv, td, tm, te))
	return sb.String()
}

// updateConfig writes corrected pricing from the manifest into the provider
// config file, preserving all existing fields like context_length, name, tags.
// Only EXISTING config entries are updated — new models require human review.
func updateConfig(configPath string, manifest ProviderManifest) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var rawCfg map[string]interface{}
	if err := json.Unmarshal(data, &rawCfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	modelsRaw, ok := rawCfg["models"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("config has no models section")
	}
	modelInfoRaw, ok := modelsRaw["model_info"].([]interface{})
	if !ok {
		modelInfoRaw = []interface{}{}
		modelsRaw["model_info"] = modelInfoRaw
	}
	manifestLookup := make(map[string]PricingEntry, len(manifest.Models))
	for _, m := range manifest.Models {
		manifestLookup[m.ID] = m
	}
	// Update existing entries only.
	for i, entry := range modelInfoRaw {
		mi, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id, ok := safeID(mi)
		if !ok {
			continue
		}
		mp, found := manifestLookup[id]
		if !found {
			continue
		}
		mi["input_cost"] = mp.InputPerMTok
		mi["output_cost"] = mp.OutputPerMTok
		if mp.CachedPerMTok > 0 {
			mi["cached_input_cost"] = mp.CachedPerMTok
		} else {
			delete(mi, "cached_input_cost")
		}
		modelInfoRaw[i] = mi
	}
	modelsRaw["model_info"] = modelInfoRaw
	out, err := json.MarshalIndent(rawCfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func floatsEqual(a, b float64) bool {
	return math.Abs(a-b) < floatEpsilon
}

