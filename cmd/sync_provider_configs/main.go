// Command sync_provider_configs updates the embedded provider config
// models.available_models field to match the canonical registry.
//
// After refresh_provider_catalog regenerates models/<id>.json canonical files,
// this command walks the embedded pkg/agent_providers/configs/ directory and
// syncs each provider's available_models list to match — but ONLY for
// providers whose embedded config currently has a non-empty available_models
// list. This preserves the live-discovery design (openai/deepinfra/lmstudio
// stay empty).
//
// Usage:
//
//	sync_provider_configs --registry-dir=models
//	sync_provider_configs --dry-run --registry-dir=models --configs-dir=pkg/agent_providers/configs
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

func main() {
	registryDir := flag.String("registry-dir", "./models", "directory holding canonical per-provider files (models/<provider>.json)")
	configsDir := flag.String("configs-dir", "./pkg/agent_providers/configs", "directory holding embedded provider config JSON files")
	dryRun := flag.Bool("dry-run", false, "report what would change without writing")
	flag.Parse()

	err := Run(*configsDir, *registryDir, *dryRun)
	if err != nil {
		failf("%v", err)
	}
}

// Run executes the sync logic. It is exported for testing.
func Run(configsDir, registryDir string, dryRun bool) error {
	files, err := filepath.Glob(filepath.Join(configsDir, "*.json"))
	if err != nil {
		return fmt.Errorf("glob configs: %w", err)
	}

	for _, cfgPath := range files {
		providerID := strings.TrimSuffix(filepath.Base(cfgPath), ".json")

		// Load embedded config (generic map so we preserve all fields).
		cfgData, err := os.ReadFile(cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", providerID, err)
			continue
		}

		var cfg map[string]interface{}
		if err := json.Unmarshal(cfgData, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", providerID, err)
			continue
		}

		// Skip if no endpoint field (malformed).
		endpoint, ok := cfg["endpoint"].(string)
		if !ok || endpoint == "" {
			continue
		}

		// Skip local-only providers (non-HTTPS endpoint).
		if !strings.HasPrefix(strings.ToLower(endpoint), "https://") {
			continue
		}

		// Skip if available_models is empty or missing (live-discovery pattern).
		modelsMap, ok := cfg["models"].(map[string]interface{})
		if !ok {
			continue
		}
		availRaw, ok := modelsMap["available_models"].([]interface{})
		if !ok || len(availRaw) == 0 {
			continue
		}

		// Build current list from the embedded config.
		currentList := make([]string, len(availRaw))
		for i, v := range availRaw {
			if s, ok := v.(string); ok {
				currentList[i] = s
			}
		}

		// Read canonical registry file.
		regPath := filepath.Join(registryDir, "models", providerID+".json")
		regData, err := os.ReadFile(regPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s — no registry file at %s (keeping embedded list)\n", providerID, regPath)
			continue
		}

		var reg modelcontract.ProviderFile
		if err := json.Unmarshal(regData, &reg); err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", providerID, err)
			continue
		}

		// Build new list from canonical models.
		newList := make([]string, len(reg.Models))
		for i, m := range reg.Models {
			newList[i] = m.ID
		}

		// Compare lists.
		if equalLists(currentList, newList) {
			continue // no-op
		}

		if dryRun {
			fmt.Printf("[dry-run] would sync %s: %d → %d models\n", providerID, len(currentList), len(newList))
			continue
		}

		// Update the available_models field.
		modelsMap["available_models"] = newList

		// Marshal back to JSON with indentation, preserving the original format.
		newData, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", providerID, err)
			continue
		}
		newData = append(newData, '\n')

		// Atomic write: write to temp file, then rename.
		tmpPath := cfgPath + ".tmp"
		if err := os.WriteFile(tmpPath, newData, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", providerID, err)
			continue
		}
		if err := os.Rename(tmpPath, cfgPath); err != nil {
			// Clean up temp file on failure.
			os.Remove(tmpPath)
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", providerID, err)
			continue
		}

		fmt.Printf("synced %s: %d → %d models\n", providerID, len(currentList), len(newList))
	}

	return nil
}

// equalLists reports whether two string slices are identical (same length, same elements in order).
func equalLists(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func failf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
