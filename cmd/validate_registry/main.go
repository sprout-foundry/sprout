// Command validate_registry checks every providers/*.json against the
// runtime schema before the publish workflow uploads to GitHub Pages.
//
// Without this step a malformed config (e.g. forgotten field after a
// schema change, accidental file rename, a non-HTTPS endpoint that
// slipped past code review) would ship to clients and get silently
// rejected six hours later when refreshFromRemote runs. Failing the
// CI job here keeps the bad file off the registry.
//
// Usage:
//
//	go run ./cmd/validate_registry providers/
//
// Exits non-zero on the first invalid file, with a diagnostic naming
// the offending file and the schema rule it violated.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/providerregistry"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: validate_registry <providers-dir>\n")
		os.Exit(2)
	}
	dir := os.Args[1]

	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate_registry: read %s: %v\n", dir, err)
		os.Exit(1)
	}

	var failures int
	var checked int

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		// index.json is structurally different (it lists ids, not a
		// provider config) — skip it here; the index has its own
		// schema enforced by scripts/generate-provider-index.sh.
		if name == "index.json" {
			continue
		}

		path := filepath.Join(dir, name)
		id := strings.TrimSuffix(name, ".json")

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "  FAIL %s: read: %v\n", path, readErr)
			failures++
			continue
		}

		var cfg providerregistry.RemoteProviderConfig
		if decodeErr := json.Unmarshal(data, &cfg); decodeErr != nil {
			fmt.Fprintf(os.Stderr, "  FAIL %s: decode: %v\n", path, decodeErr)
			failures++
			continue
		}

		if valErr := providerregistry.ValidateForPublish(id, &cfg); valErr != nil {
			fmt.Fprintf(os.Stderr, "  FAIL %s: %v\n", path, valErr)
			failures++
			continue
		}

		checked++
	}

	fmt.Printf("validate_registry: %d ok, %d failed (in %s)\n", checked, failures, dir)
	if failures > 0 {
		os.Exit(1)
	}
}
