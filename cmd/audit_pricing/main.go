package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Command audit_pricing validates provider pricing in embedded configs and
// the provider catalog against a verified pricing manifest. It reports
// mismatches, missing pricing, and stale entries.
//
// Modes:
//   audit_pricing --configs-dir=DIR            audit manifest vs configs
//   audit_pricing --discover --configs-dir=DIR discover new/stale models via live APIs
//   audit_pricing --discover --update           also auto-add new models to configs + manifest
//   audit_pricing --update                      write corrected pricing from manifest to configs
//   audit_pricing --fail-on-drift               exit 1 if drift detected

func main() {
	configsDir := flagString("configs-dir", "pkg/agent_providers/configs",
		"directory containing provider config JSON files")
	manifestPath := flagString("manifest-path", "cmd/audit_pricing/manifest.json",
		"path to the manifest JSON file (for --discover --update writes)")
	doUpdate := flagBool("update", false,
		"write changes back to config files and manifest")
	doDiscover := flagBool("discover", false,
		"call live provider APIs to discover new/stale models")
	failOnDrift := flagBool("fail-on-drift", false,
		"exit code 1 if any drift is found")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// --- Discovery phase: find new/stale models via live APIs ---
	var discoverResults []DiscoverResult
	if *doDiscover {
		dr, _ := discoverAndAuditAll(ctx, *configsDir, *manifestPath, *doUpdate)
		discoverResults = dr
		fmt.Println(formatDiscoverReport(discoverResults))
	}

	// --- Audit phase: compare manifest prices vs config prices ---
	var results []ProviderAuditResult

	providerIDs := make([]string, 0, len(manifests))
	for id := range manifests {
		providerIDs = append(providerIDs, id)
	}
	sort.Strings(providerIDs)

	for _, providerID := range providerIDs {
		manifest := manifests[providerID]
		configPath := filepath.Join(*configsDir, providerID+".json")
		result := auditProvider(providerID, manifest, configPath)
		results = append(results, result)

		if *doUpdate && !*doDiscover {
			if err := updateConfig(configPath, manifest); err != nil {
				fmt.Fprintf(os.Stderr, "update %s: %v\n", providerID, err)
			} else {
				fmt.Fprintf(os.Stderr, "updated %s pricing from manifest\n", providerID)
			}
		}
	}

	fmt.Println(formatReport(results, *configsDir))

	// Count drift + new models for exit code decision.
	totalDrift := 0
	for _, r := range results {
		for _, m := range r.Models {
			if m.Status == "drift" {
				totalDrift++
			}
		}
	}
	totalNew := 0
	for _, dr := range discoverResults {
		totalNew += len(dr.NewModels)
	}

	if *failOnDrift && (totalDrift > 0 || totalNew > 0) {
		os.Exit(1)
	}
}

// Minimal flag helpers to avoid importing flag for simple cases.
func flagString(name, def, _ string) *string {
	val := def
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--"+name {
			if i+1 < len(os.Args) {
				val = os.Args[i+1]
			}
			break
		}
		if strings.HasPrefix(os.Args[i], "--"+name+"=") {
			val = os.Args[i][len("--"+name)+1:]
			break
		}
	}
	return &val
}

func flagBool(name string, def bool, _ string) *bool {
	val := def
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--"+name {
			val = true
			break
		}
	}
	return &val
}
