// Command enrich_registry annotates freshly-generated canonical model files
// with capability-probe results. It diffs the fresh files against the
// currently-published registry, carries forward prior probe results for
// already-known models, and probes only NEW + eligible + within-budget models
// (capped), writing each model's probe outcome and recommended_roles back.
//
// Run it after refresh_provider_catalog, before publishing:
//
//	enrich_registry --registry-dir=models --max-input-cost=10 --max-probes=50
//	enrich_registry --registry-dir=models --dry-run   # report only, no spend
//
// Probing requires provider API keys (configured as CI secrets). Models without
// an available key, over budget, or not eligible are left un-probed.
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
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
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
	flag.Parse()

	if *registryDir == "" {
		fmt.Fprintln(os.Stderr, "usage: enrich_registry --registry-dir <dir> [--max-probe-cost N --max-probes N --dry-run]")
		os.Exit(2)
	}

	modelprobe.LimitRequestTokens()

	files, _ := filepath.Glob(filepath.Join(*registryDir, "models", "*.json"))
	probed := 0
	for _, f := range files {
		providerID := strings.TrimSuffix(filepath.Base(f), ".json")
		if providerID == "index" {
			continue
		}
		pf, err := loadProviderFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", providerID, err)
			continue
		}

		baseline := fetchBaseline(*baseURL, providerID)
		clientType, ctErr := api.ParseProviderName(providerID)

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

			// Only spend on models worth recommending: eligible, affordable, keyed.
			if len(m.EligibleRoles) == 0 {
				continue
			}
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
			if probed >= *maxProbes {
				fmt.Printf("  [cap] reached --max-probes=%d; stopping\n", *maxProbes)
				break
			}
			if *dryRun {
				fmt.Printf("  [would probe] %s/%s (est. probe cost $%.4f)\n",
					providerID, m.ID, modelprobe.EstimatedCostUSD(inCost, outCost))
				probed++
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
			m.RecommendedRoles = recommendedRoles(m.EligibleRoles, res.Passed, res.Complex)
			changed = true
			fmt.Printf("  probed %s/%s → passed=%v complex=%v score=%.2f\n",
				providerID, m.ID, res.Passed, res.Complex, res.Score)
		}

		fmt.Printf("%s: %d new model(s), %d total\n", providerID, newCount, len(pf.Models))
		if changed && !*dryRun {
			if err := writeProviderFile(f, pf); err != nil {
				fmt.Fprintf(os.Stderr, "  write %s: %v\n", f, err)
			}
		}
	}
	fmt.Printf("done: %d model(s) probed\n", probed)
}

// recommendedRoles narrows the deterministic eligible roles to those the probe
// actually backs: the subagent role needs the gate (Passed); the primary role
// needs the complex discovery/scoping tier (Complex). Recommended ⊆ eligible.
func recommendedRoles(eligible []string, passed, complex bool) []string {
	var rec []string
	for _, role := range eligible {
		switch role {
		case modelcontract.RoleSubagent:
			if passed {
				rec = append(rec, role)
			}
		case modelcontract.RolePrimary:
			if complex {
				rec = append(rec, role)
			}
		}
	}
	return rec
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
