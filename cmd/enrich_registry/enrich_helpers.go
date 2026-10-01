package main

// enrich_helpers.go — helper functions for the enrich_registry tool:
// provider-file collection, canonical-model construction from embedded
// configs, baseline fetching, provider-file I/O, provider-level error
// detection, and the budget allocation helpers. Split out of main.go.
import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// isProviderLevelError reports whether a probe result reason indicates a
// provider-level issue that will cause every model on that provider to fail
// identically. When detected, the caller skips remaining models for that
// provider rather than wasting probes.
//
// This is NOT the same as IsPermanentError. A per-model 402 (e.g. OpenRouter
// "requires more credits" for an expensive model) is permanent for that model
// but NOT provider-level — cheaper or free models on the same provider may
// still succeed. Only account-wide issues like MiniMax's "insufficient balance"
// qualify as provider-level.
func isProviderLevelError(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "insufficient balance")
}

// providerFile tracks a provider file to process along with its path.
type providerFile struct {
	providerID string
	path       string
}

// collectProviderFiles returns the list of canonical provider files sorted by
// provider ID so the probe loop processes them deterministically.
func collectProviderFiles(registryDir string) []providerFile {
	files, _ := filepath.Glob(filepath.Join(registryDir, "models", "*.json"))
	var out []providerFile
	for _, f := range files {
		providerID := strings.TrimSuffix(filepath.Base(f), ".json")
		if providerID == "index" {
			continue
		}
		out = append(out, providerFile{providerID: providerID, path: f})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].providerID < out[j].providerID })
	return out
}

// recommendedRoles returns the roles backed by the probe result alone.
// The probe IS the ground truth — if it passed, the model is eligible for
// subagent; if it cleared the complex tier, it qualifies for primary.
// This replaces the previous version that filtered by EligibleRoles,
// because the deterministic classifier can miss models the probe validates.
func recommendedRoles(passed, complex bool) []string {
	var rec []string
	if passed {
		rec = append(rec, modelcontract.RoleSubagent)
	}
	if complex {
		rec = append(rec, modelcontract.RolePrimary)
	}
	return rec
}

// buildCanonicalFromConfig constructs CanonicalModel entries from an embedded
// provider config. It uses model_info where available (rich metadata), and
// falls back to available_models for IDs listed there but not in model_info.
func buildCanonicalFromConfig(providerID string, cfg *providers.ProviderConfig) []modelcontract.CanonicalModel {
	// Build a lookup of model_info entries.
	infoMap := make(map[string]providers.ModelInfo)
	for _, mi := range cfg.Models.ModelInfo {
		infoMap[mi.ID] = mi
	}

	// Collect all model IDs (from model_info first, then available_models).
	seen := make(map[string]bool)
	var ids []string
	for _, mi := range cfg.Models.ModelInfo {
		if !seen[mi.ID] {
			seen[mi.ID] = true
			ids = append(ids, mi.ID)
		}
	}
	for _, id := range cfg.Models.AvailableModels {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	models := make([]modelcontract.CanonicalModel, 0, len(ids))
	for _, id := range ids {
		m := modelcontract.CanonicalModel{
			ID:       id,
			Provider: providerID,
		}
		if mi, ok := infoMap[id]; ok {
			m.DisplayName = mi.Name
			m.Description = mi.Description
			m.ContextWindow = mi.ContextLength
			if len(mi.Tags) > 0 {
				m.Capabilities = modelcontract.CapabilitiesFromTags(mi.Tags)
			}
			// Use pricing from model_info if available (some embedded configs
			// have input_cost/output_cost on model_info entries).
			if mi.InputCost > 0 || mi.OutputCost > 0 {
				m.Pricing = &modelcontract.Pricing{
					InputPerMTok:  mi.InputCost,
					OutputPerMTok: mi.OutputCost,
					Currency:      "USD",
					Estimated:     true,
					Source:        "embedded-config",
				}
			}
		} else {
			// Fallback: use provider defaults for models only in available_models.
			m.ContextWindow = cfg.Models.DefaultContextLimit
			if cfg.Models.ContextLimit > 0 && m.ContextWindow == 0 {
				m.ContextWindow = cfg.Models.ContextLimit
			}
		}

		// Compute eligible roles deterministically.
		m.EligibleRoles = modelcontract.ClassifyEligibleRoles(m)
		if w := modelcontract.ContextWarning(m.ContextWindow); w != "" {
			m.Warnings = append(m.Warnings, w)
		}

		models = append(models, m)
	}

	return models
}

func loadProviderFile(path string) (*modelcontract.ProviderFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pf modelcontract.ProviderFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, err
	}
	return &pf, nil
}

func writeProviderFile(path string, pf *modelcontract.ProviderFile) error {
	out, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// fetchBaseline returns the currently-published models for a provider keyed by
// ID, so the diff can find new models and carry prior probe results forward.
// A missing/unreachable baseline yields an empty map (every model is treated as
// new — bounded by --max-probes and the budget).
func fetchBaseline(baseURL, providerID string) map[string]modelcontract.CanonicalModel {
	out := map[string]modelcontract.CanonicalModel{}
	url := strings.TrimRight(baseURL, "/") + "/models/" + providerID + ".json"
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return out
	}
	// Parse as canonical; legacy (schema-1) files simply won't carry Probe.
	var pf modelcontract.ProviderFile
	if err := json.Unmarshal(body, &pf); err != nil {
		return out
	}
	for _, m := range pf.Models {
		out[m.ID] = m
	}
	return out
}

// --- Budget allocation helpers (testable) ---

// allocateProbeBudget computes per-provider probe caps for fair-share
// distribution across providers. Each provider gets at least minPerProvider
// probes (default 5), but never more than maxProbes total.
func allocateProbeBudget(numProviders, maxProbes int) (perProvider, total int) {
	if numProviders <= 0 {
		return 0, maxProbes
	}
	perProvider = maxInt(5, maxProbes/numProviders)
	return perProvider, maxProbes
}

// maxInt returns the larger of a and b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
