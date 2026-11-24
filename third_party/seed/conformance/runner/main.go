// Conformance test runner for the seed project.
//
// Usage:
//
//	runner --cli ./seed-cli --specs ./conformance/specs/
//	runner --cli ./seed-cli --specs ./conformance/specs/ --filter "basic"
//	runner --cli ./seed-cli --specs ./conformance/specs/ --verbose
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	cliPath := flag.String("cli", "./seed-cli", "Path to CLI binary")
	specsDir := flag.String("specs", "./conformance/specs", "Directory containing spec JSON files")
	verbose := flag.Bool("verbose", false, "Show all events and responses")
	filter := flag.String("filter", "", "Run only specs matching this substring")
	flag.Parse()

	// Load all spec files from the directory.
	specs, err := loadSpecs(*specsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load specs: %v\n", err)
		os.Exit(1)
	}

	// Filter specs if requested.
	if *filter != "" {
		var filtered []Spec
		for _, s := range specs {
			if strings.Contains(s.Name, *filter) || strings.Contains(s.Description, *filter) {
				filtered = append(filtered, s)
			}
		}
		specs = filtered
	}

	if len(specs) == 0 {
		fmt.Fprintln(os.Stderr, "No specs found")
		os.Exit(1)
	}

	// Sort specs by name for deterministic order.
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].Name < specs[j].Name
	})

	// Print TAP header.
	fmt.Printf("1..%d\n", len(specs))

	passed := 0
	failed := 0
	var failures []string

	startTime := time.Now()

	for i, spec := range specs {
		result := RunSpec(spec, *cliPath, *verbose)

		if result.Failed {
			failed++
			fmt.Printf("not ok %d - %s\n", i+1, spec.Name)
			for _, ar := range result.Assertions {
				if !ar.Passed {
					fmt.Printf("#   assertion failed: %s\n", ar.Message)
					failures = append(failures, fmt.Sprintf("%s: %s", spec.Name, ar.Message))
				}
			}
		} else {
			passed++
			fmt.Printf("ok %d - %s\n", i+1, spec.Name)
		}

		if *verbose {
			fmt.Printf("#   responses: %d, events: %d\n", len(result.Response), len(result.Events))
		}
	}

	elapsed := time.Since(startTime)

	// Summary.
	fmt.Fprintf(os.Stderr, "\n--- Summary ---\n")
	fmt.Fprintf(os.Stderr, "Total: %d, Passed: %d, Failed: %d, Duration: %s\n",
		len(specs), passed, failed, elapsed.Round(100*time.Millisecond))

	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "\nFailures:\n")
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "  - %s\n", f)
		}
		os.Exit(1)
	}
}

// loadSpecs reads all .json files from the given directory (recursively) and returns parsed specs.
func loadSpecs(dir string) ([]Spec, error) {
	var specs []Spec
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading spec file %s: %w", path, err)
		}
		var spec Spec
		if err := json.Unmarshal(data, &spec); err != nil {
			return fmt.Errorf("parsing spec file %s: %w", path, err)
		}
		specs = append(specs, spec)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking specs directory %s: %w", dir, err)
	}
	return specs, nil
}
