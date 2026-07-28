package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Command audit_pricing validates provider pricing in embedded configs and
// the provider catalog against a verified pricing manifest. It reports
// mismatches, missing pricing, and stale entries. Use --update to write
// corrected pricing back to the config files.

func main() {
	configsDir := flagString("configs-dir", "pkg/agent_providers/configs",
		"directory containing provider config JSON files")
	doUpdate := flagBool("update", false,
		"write corrected pricing back to config files")
	failOnDrift := flagBool("fail-on-drift", false,
		"exit code 1 if any drift is found")

	// Collect results for each provider in the manifest.
	var results []ProviderAuditResult

	// Sort provider IDs for deterministic output.
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

		if *doUpdate {
			if err := updateConfig(configPath, manifest); err != nil {
				fmt.Fprintf(os.Stderr, "update %s: %v\n", providerID, err)
			} else {
				fmt.Fprintf(os.Stderr, "updated %s pricing from manifest\n", providerID)
			}
		}
	}

	fmt.Println(formatReport(results, *configsDir))

	// Count drift for exit code decision.
	totalDrift := 0
	for _, r := range results {
		for _, m := range r.Models {
			if m.Status == "drift" {
				totalDrift++
			}
		}
	}

	if *failOnDrift && totalDrift > 0 {
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
