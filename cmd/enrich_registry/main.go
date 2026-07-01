// Command enrich_registry annotates freshly-generated canonical model files
// with capability-probe results. It diffs the fresh files against the
// currently-published registry, carries forward prior probe results for
// already-known models, and probes only NEW + within-budget models (capped),
// writing each model's probe outcome and recommended_roles back.
//
// Run it after refresh_provider_catalog, before publishing:
//
//	enrich_registry --registry-dir=models --max-probe-cost=0.10 --max-probes=50
//	enrich_registry --registry-dir=models --dry-run   # report only, no spend
//
// Probing requires provider API keys (configured as CI secrets). Models without
// an available key, over budget, or not eligible are left un-probed.
//
// Fair-share budget: the global --max-probes cap is split across providers so
// no single provider (e.g., deepinfra with 89 models) consumes the entire
// budget. Each provider gets min(5, maxProbes/numProviders) probes minimum.
//
// Probe is the ground truth: models with empty EligibleRoles are still probed
// because the deterministic classifier can miss capable models.
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
	"sort"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
	"github.com/sprout-foundry/sprout/pkg/modelprobe"
)

func main() {
	registryDir := flag.String("registry-dir", "", "directory holding fresh canonical per-provider files (models/<provider>.json)")
	baseURL := flag.String("base-url", "https://sprout-foundry.github.io/sprout", "live registry base URL to diff against")
	maxProbeCost := flag.Float64("max-probe-cost", 0.10, "probe budget: skip models whose estimated per-probe cost exceeds this (USD); 0 disables")
	maxProbes := flag.Int("max-probes", 50, "max models to probe this run (overall cost cap)")
	dryRun := flag.Bool("dry-run", false, "don't probe; just report what would be probed/skipped")
	fromEmbeddedConfigs := flag.String("from-embedded-configs", "", "dir of embedded provider configs to fall back to for providers without a canonical models/<id>.json file")
	flag.Parse()

	if *registryDir == "" {
		fmt.Fprintln(os.Stderr, "usage: enrich_registry --registry-dir <dir> [--max-probe-cost N --max-probes N --dry-run]")
		os.Exit(2)
	}

	modelprobe.LimitRequestTokens()

	// Collect all provider files to process (canonical models/*.json first).
	providerFiles := collectProviderFiles(*registryDir)

	// Compute per-provider cap for fair-share budget allocation.
	numProviders := len(providerFiles)
	providerCap, _ := allocateProbeBudget(numProviders, *maxProbes)

	probed := 0
	for _, pfEntry := range providerFiles {
		providerID := pfEntry.providerID
		if providerID == "index" {
			continue
		}

		pf, err := loadProviderFile(pfEntry.path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", providerID, err)
			continue
		}

		baseline := fetchBaseline(*baseURL, providerID)
		clientType, ctErr := api.ParseProviderName(providerID)

		// Check if there's any remaining budget for this provider.
		if probed >= *maxProbes {
			fmt.Printf("%s: skipped — global probe cap reached (%d/%d)\n", providerID, probed, *maxProbes)
			continue
		}

		providerProbed := 0
		changed := false
		newCount := 0
		for i := range pf.Models {
			m := &pf.Models[i]

			// Carry forward a prior probe for an already-known model so we don't
			// lose accumulated results when the fresh file is regenerated. A model
			// that is in the baseline but has NO probe yet (newly added before,
			// or a prior run that errored / was budget-skipped) falls through to
			// be probed this run — that's how a transient failure gets retried.
			if prev, ok := baseline[m.ID]; ok && prev.Probe != nil {
				m.Probe = prev.Probe
				m.RecommendedRoles = prev.RecommendedRoles
				continue // already have a result; nothing to spend
			}
			newCount++

			// Per-provider cap: stop probing this provider once its share is used.
			if providerProbed >= providerCap {
				fmt.Printf("  [%s] reached per-provider cap of %d probes\n", providerID, providerCap)
				break
			}

			// Overall cap: skip probing once global budget is exhausted.
			if probed >= *maxProbes {
				break
			}

			// Only probe models that are affordable and have a key available.
			// NOTE: we no longer skip models with empty EligibleRoles — the probe
			// is the ground truth for capability; the classifier can miss models.
			inCost, outCost, costKnown := 0.0, 0.0, false
			if m.Pricing != nil {
				inCost, outCost, costKnown = m.Pricing.InputPerMTok, m.Pricing.OutputPerMTok, true
			}
			if ok, reason := modelprobe.WithinCostBudget(inCost, outCost, costKnown, *maxProbeCost); !ok {
				if *dryRun {
					fmt.Printf("  [skip] %s/%s — %s\n", providerID, m.ID, reason)
				}
				continue
			}

			if *dryRun {
				fmt.Printf("  [would probe] %s/%s (est. probe cost $%.4f)\n",
					providerID, m.ID, modelprobe.EstimatedCostUSD(inCost, outCost))
				probed++
				providerProbed++
				continue
			}
			if ctErr != nil {
				fmt.Fprintf(os.Stderr, "  cannot probe %s/%s: %v\n", providerID, m.ID, ctErr)
				continue
			}

			client, err := factory.CreateProviderClient(clientType, m.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  client %s/%s: %v\n", providerID, m.ID, err)
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
			res, err := modelprobe.Run(ctx, client, providerID, m.ID)
			cancel()
			probed++
			providerProbed++

			// Inconclusive (transport/5xx/timeout): do NOT persist a verdict.
			// Leaving the model un-probed means the next run retries it, rather
			// than carrying forward a wrong "failed/not-complex" result.
			if err != nil || res.Errored {
				fmt.Fprintf(os.Stderr, "  probe %s/%s inconclusive (will retry next run): %s\n", providerID, m.ID, res.Reason)
				continue
			}

			m.Probe = &modelcontract.ProbeResult{
				Passed:       res.Passed,
				Complex:      res.Complex,
				Score:        res.Score,
				LastProbedAt: res.ProbedAt,
				ProbeVersion: res.ProbeVersion,
			}
			m.RecommendedRoles = recommendedRoles(res.Passed, res.Complex)
			changed = true
			fmt.Printf("  probed %s/%s → passed=%v complex=%v score=%.2f\n",
				providerID, m.ID, res.Passed, res.Complex, res.Score)
		}

		fmt.Printf("%s: %d new model(s), %d total, probed %d (cap %d)\n",
			providerID, newCount, len(pf.Models), providerProbed, providerCap)
		if changed && !*dryRun {
			if err := writeProviderFile(pfEntry.path, pf); err != nil {
				fmt.Fprintf(os.Stderr, "  write %s: %v\n", pfEntry.path, err)
			}
		}
	}

	// Fix 3: Fall back to embedded configs for providers without a canonical
	// models/<id>.json file. This ensures providers without API keys in CI
	// still get probed once keys are available, instead of permanently unprobed.
	if *fromEmbeddedConfigs != "" {
		embeddedFiles, _ := filepath.Glob(filepath.Join(*fromEmbeddedConfigs, "*.json"))
		for _, ef := range embeddedFiles {
			providerID := strings.TrimSuffix(filepath.Base(ef), ".json")
			if providerID == "index" {
				continue
			}
			canonicalPath := filepath.Join(*registryDir, "models", providerID+".json")
			if _, err := os.Stat(canonicalPath); err == nil {
				continue // already has canonical file from the main path
			}

			// Check if there's any remaining budget.
			if probed >= *maxProbes {
				fmt.Printf("%s (embedded): skipped — global probe cap reached (%d/%d)\n", providerID, probed, *maxProbes)
				continue
			}

			// Parse the embedded config and build canonical models.
			cfg, err := providers.LoadProviderConfig(ef)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skip embedded %s: %v\n", providerID, err)
				continue
			}
			if len(cfg.Models.AvailableModels) == 0 && len(cfg.Models.ModelInfo) == 0 {
				fmt.Printf("%s (embedded): no models defined in config\n", providerID)
				continue
			}

			embeddedModels := buildCanonicalFromConfig(providerID, cfg)
			if len(embeddedModels) == 0 {
				continue
			}

			// Run the same probe loop as the main path.
			baseline := fetchBaseline(*baseURL, providerID)
			clientType, ctErr := api.ParseProviderName(providerID)

			providerProbed := 0
			changed := false
			newCount := 0
			for i := range embeddedModels {
				m := &embeddedModels[i]

				if prev, ok := baseline[m.ID]; ok && prev.Probe != nil {
					m.Probe = prev.Probe
					m.RecommendedRoles = prev.RecommendedRoles
					continue
				}
				newCount++

				if providerProbed >= providerCap {
					fmt.Printf("  [%s (embedded)] reached per-provider cap of %d probes\n", providerID, providerCap)
					break
				}
				if probed >= *maxProbes {
					break
				}

				inCost, outCost, costKnown := 0.0, 0.0, false
				if m.Pricing != nil {
					inCost, outCost, costKnown = m.Pricing.InputPerMTok, m.Pricing.OutputPerMTok, true
				}
				if ok, reason := modelprobe.WithinCostBudget(inCost, outCost, costKnown, *maxProbeCost); !ok {
					if *dryRun {
						fmt.Printf("  [skip] %s/%s (embedded) — %s\n", providerID, m.ID, reason)
					}
					continue
				}

				if *dryRun {
					fmt.Printf("  [would probe] %s/%s (embedded, est. probe cost $%.4f)\n",
						providerID, m.ID, modelprobe.EstimatedCostUSD(inCost, outCost))
					probed++
					providerProbed++
					continue
				}
				if ctErr != nil {
					fmt.Fprintf(os.Stderr, "  cannot probe %s/%s (embedded): %v\n", providerID, m.ID, ctErr)
					continue
				}

				client, err := factory.CreateProviderClient(clientType, m.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  client %s/%s (embedded): %v\n", providerID, m.ID, err)
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
				res, err := modelprobe.Run(ctx, client, providerID, m.ID)
				cancel()
				probed++
				providerProbed++

				if err != nil || res.Errored {
					fmt.Fprintf(os.Stderr, "  probe %s/%s (embedded) inconclusive (will retry next run): %s\n", providerID, m.ID, res.Reason)
					continue
				}

				m.Probe = &modelcontract.ProbeResult{
					Passed:       res.Passed,
					Complex:      res.Complex,
					Score:        res.Score,
					LastProbedAt: res.ProbedAt,
					ProbeVersion: res.ProbeVersion,
				}
				m.RecommendedRoles = recommendedRoles(res.Passed, res.Complex)
				changed = true
				fmt.Printf("  probed %s/%s (embedded) → passed=%v complex=%v score=%.2f\n",
					providerID, m.ID, res.Passed, res.Complex, res.Score)
			}

			fmt.Printf("%s (embedded): %d new model(s), %d total, probed %d (cap %d)\n",
				providerID, newCount, len(embeddedModels), providerProbed, providerCap)
			if changed && !*dryRun {
				pf := &modelcontract.ProviderFile{
					SchemaVersion: modelcontract.SchemaVersion,
					Provider:      providerID,
					GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
					Models:        embeddedModels,
				}
				if err := writeProviderFile(canonicalPath, pf); err != nil {
					fmt.Fprintf(os.Stderr, "  write %s: %v\n", canonicalPath, err)
				}
			}
		}
	}

	fmt.Printf("done: %d model(s) probed\n", probed)
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
