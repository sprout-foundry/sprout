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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
	"github.com/sprout-foundry/sprout/pkg/modelprobe"
)

func main() {
	registryDir := flag.String("registry-dir", "", "directory holding fresh canonical per-provider files (models/<provider>.json)")
	baseURL := flag.String("base-url", "https://sprout-foundry.github.io/sprout", "live registry base URL to diff against")
	maxProbeCost := flag.Float64("max-probe-cost", 0.10, "probe budget: skip models whose estimated per-probe cost exceeds this (USD); 0 disables")
	maxProbes := flag.Int("max-probes", 50, "max models to probe this run (overall cost cap)")
	maxRunCost := flag.Float64("max-run-cost", 0, "aggregate spend cap: stop probing once cumulative estimated cost reaches this (USD); 0 disables")
	maxPricePerMTok := flag.Float64("max-price-per-mtok", 0, "per-token price ceiling: skip models whose input OR output price exceeds this (USD/MTok); 0 disables")
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
	totalSpend := 0.0 // cumulative estimated probe cost (USD) across the run
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

		// Pre-flight: skip the entire provider if its API key is not set.
		// In CI, keys come from secrets mapped to env vars. A missing env var
		// means the key isn't configured — every model will fail identically.
		if envVar := providers.ProviderEnvVar(providerID); envVar != "" && os.Getenv(envVar) == "" {
			fmt.Printf("%s: skipped — API key (%s) not set\n", providerID, envVar)
			continue
		}

		// Check if there's any remaining budget for this provider.
		if probed >= *maxProbes {
			fmt.Printf("%s: skipped — global probe cap reached (%d/%d)\n", providerID, probed, *maxProbes)
			continue
		}
		if *maxRunCost > 0 && totalSpend >= *maxRunCost {
			fmt.Printf("%s: skipped — run cost cap reached ($%.4f / $%.2f)\n", providerID, totalSpend, *maxRunCost)
			continue
		}

		providerProbed := 0
		changed := false
		newCount := 0
		providerSkipReason := "" // set when a provider-level permanent error (e.g. 402) is discovered
		for i := range pf.Models {
			m := &pf.Models[i]

			// Carry forward a prior probe for an already-known model so we don't
			// lose accumulated results when the fresh file is regenerated. A model
			// that is in the baseline but has NO probe yet (newly added before,
			// or a prior run that errored / was budget-skipped) falls through to
			// be probed this run — that's how a transient failure gets retried.
			// A version mismatch also forces a re-probe so a ProbeVersion bump
			// invalidates all prior results.
			if prev, ok := baseline[m.ID]; ok && prev.Probe != nil && prev.Probe.ProbeVersion == modelprobe.ProbeVersion {
				m.Probe = prev.Probe
				m.RecommendedRoles = prev.RecommendedRoles
				changed = true // carry-forward populates the in-memory file; must persist
				continue       // already have a result; nothing to spend
			}
			newCount++

			// Skip remaining models if the provider has a known permanent issue
			// (e.g. 402 insufficient balance — every model will fail identically).
			if providerSkipReason != "" {
				continue
			}

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

			// Per-token price ceiling: reject models whose input or output
			// price exceeds the ceiling, even if the per-probe estimate is
			// under budget. A cheap probe on a $20/MTok model still isn't
			// worth surfacing as a recommended coding model.
			if *maxPricePerMTok > 0 && costKnown {
				if inCost > *maxPricePerMTok || outCost > *maxPricePerMTok {
					if *dryRun {
						fmt.Printf("  [skip] %s/%s — price $%.2f/$%.2f per MTok exceeds ceiling $%.2f\n",
							providerID, m.ID, inCost, outCost, *maxPricePerMTok)
					}
					continue
				}
			}

			// Aggregate spend cap: stop probing once cumulative estimated
			// cost reaches the run-level budget.
			estCost := modelprobe.EstimatedCostUSD(inCost, outCost)
			if *maxRunCost > 0 && totalSpend+estCost > *maxRunCost {
				fmt.Printf("  [skip] %s/%s — run cost cap reached ($%.4f + $%.4f > $%.2f)\n",
					providerID, m.ID, totalSpend, estCost, *maxRunCost)
				break
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
			totalSpend += estCost

			// Detect provider-level permanent errors (e.g. 402 insufficient
			// balance) and skip remaining models for this provider — every
			// model will fail identically.
			if isProviderLevelError(res.Reason) {
				providerSkipReason = res.Reason
			}

			// Transient (transport/5xx/timeout): do NOT persist a verdict.
			// Leaving the model un-probed means the next run retries it, rather
			// than carrying forward a wrong "failed/not-complex" result.
			if err != nil || res.Errored {
				fmt.Fprintf(os.Stderr, "  probe %s/%s inconclusive (will retry next run): %s\n", providerID, m.ID, res.Reason)
				continue
			}

			m.Probe = &modelcontract.ProbeResult{
				Passed:       res.Passed,
				Complex:      res.Complex,
				Vision:       res.Vision,
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
		if providerSkipReason != "" {
			fmt.Printf("  [%s] remaining models skipped — %s\n", providerID, providerSkipReason)
		}
		if changed && !*dryRun {
			if err := writeProviderFile(pfEntry.path, pf); err != nil {
				fmt.Fprintf(os.Stderr, "  write %s: %v\n", pfEntry.path, err)
			}
		}
	}

	probed, totalSpend = runEmbeddedFallback(probed, totalSpend, providerCap, *maxProbeCost, *maxProbes, *maxPricePerMTok, *maxRunCost, *dryRun, *registryDir, *baseURL, *fromEmbeddedConfigs)

	fmt.Printf("done: %d model(s) probed, est. cost $%.4f\n", probed, totalSpend)
}

// runEmbeddedFallback handles the --from-embedded-configs fallback: for each
// provider in the embedded-configs directory that has no canonical
// models/<id>.json file, build the canonical models and run the same probe
// loop as the main path. Returns the updated global probe count and
// cumulative estimated spend.
func runEmbeddedFallback(probed int, totalSpend float64, providerCap int, maxProbeCost float64, maxProbes int, maxPricePerMTok, maxRunCost float64, dryRun bool, registryDir, baseURL, fromEmbeddedConfigs string) (int, float64) {
	// Fix 3: Fall back to embedded configs for providers without a canonical
	// models/<id>.json file. This ensures providers without API keys in CI
	// still get probed once keys are available, instead of permanently unprobed.
	if fromEmbeddedConfigs != "" {
		embeddedFiles, _ := filepath.Glob(filepath.Join(fromEmbeddedConfigs, "*.json"))
		for _, ef := range embeddedFiles {
			providerID := strings.TrimSuffix(filepath.Base(ef), ".json")
			if providerID == "index" {
				continue
			}
			canonicalPath := filepath.Join(registryDir, "models", providerID+".json")
			if _, err := os.Stat(canonicalPath); err == nil {
				continue // already has canonical file from the main path
			}

			// Check if there's any remaining budget.
			if probed >= maxProbes {
				fmt.Printf("%s (embedded): skipped — global probe cap reached (%d/%d)\n", providerID, probed, maxProbes)
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
			baseline := fetchBaseline(baseURL, providerID)
			clientType, ctErr := api.ParseProviderName(providerID)

			// Pre-flight: skip if API key not set.
			if envVar := providers.ProviderEnvVar(providerID); envVar != "" && os.Getenv(envVar) == "" {
				fmt.Printf("%s (embedded): skipped — API key (%s) not set\n", providerID, envVar)
				continue
			}

			providerProbed := 0
			changed := false
			newCount := 0
			providerSkipReason := ""
			for i := range embeddedModels {
				m := &embeddedModels[i]

				if prev, ok := baseline[m.ID]; ok && prev.Probe != nil && prev.Probe.ProbeVersion == modelprobe.ProbeVersion {
					m.Probe = prev.Probe
					m.RecommendedRoles = prev.RecommendedRoles
					changed = true
					continue
				}
				newCount++

				if providerSkipReason != "" {
					continue
				}

				if providerProbed >= providerCap {
					fmt.Printf("  [%s (embedded)] reached per-provider cap of %d probes\n", providerID, providerCap)
					break
				}
				if probed >= maxProbes {
					break
				}

				inCost, outCost, costKnown := 0.0, 0.0, false
				if m.Pricing != nil {
					inCost, outCost, costKnown = m.Pricing.InputPerMTok, m.Pricing.OutputPerMTok, true
				}
				if ok, reason := modelprobe.WithinCostBudget(inCost, outCost, costKnown, maxProbeCost); !ok {
					if dryRun {
						fmt.Printf("  [skip] %s/%s (embedded) — %s\n", providerID, m.ID, reason)
					}
					continue
				}

				if maxPricePerMTok > 0 && costKnown {
					if inCost > maxPricePerMTok || outCost > maxPricePerMTok {
						if dryRun {
							fmt.Printf("  [skip] %s/%s (embedded) — price exceeds ceiling\n", providerID, m.ID)
						}
						continue
					}
				}

				estCost := modelprobe.EstimatedCostUSD(inCost, outCost)
				if maxRunCost > 0 && totalSpend+estCost > maxRunCost {
					fmt.Printf("  [skip] %s/%s (embedded) — run cost cap reached\n", providerID, m.ID)
					break
				}

				if dryRun {
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
				totalSpend += estCost

				if isProviderLevelError(res.Reason) {
					providerSkipReason = res.Reason
				}

				if err != nil || res.Errored {
					fmt.Fprintf(os.Stderr, "  probe %s/%s (embedded) inconclusive (will retry next run): %s\n", providerID, m.ID, res.Reason)
					continue
				}

				m.Probe = &modelcontract.ProbeResult{
					Passed:       res.Passed,
					Complex:      res.Complex,
					Vision:       res.Vision,
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
			if providerSkipReason != "" {
				fmt.Printf("  [%s (embedded)] remaining models skipped — %s\n", providerID, providerSkipReason)
			}
			if changed && !dryRun {
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
	return probed, totalSpend
}
