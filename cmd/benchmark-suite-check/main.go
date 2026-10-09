// Command benchmark-suite-check validates a benchmark task suite directory
// against the on-disk task fixture format, so a suite can be checked
// WITHOUT running the (priced) benchmark.
//
// It loads every task file individually with the SAME loader the harness
// uses (pkg/benchmark.LoadTask: task-field rules + plancontract.Validate on
// the embedded plan) and additionally enforces the suite layout invariant
// that the harness's LoadSuite applies: a task's starter field must equal
// the starter directory it sits in (<suite>/<starter-id>/<task-id>.json).
// Checking file-by-file (instead of LoadSuite's fail-fast sweep) means a
// single bad fixture reports its own problems without aborting the sweep,
// which is what you want while authoring a large held-back suite.
//
// Usage:
//
//	benchmark-suite-check <suite-dir>
//
// Output: one PASS/FAIL line per task file (a FAIL lists every problem),
// then a per-starter summary and a final result. Exit 0 = every task file
// valid; exit 1 = at least one invalid; exit 2 = usage / unreadable dir.
//
// This tool reads the suite and the embedded starter manifests only; it
// never starts a dev server, runs a task, or calls a provider.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/benchmark"
)

func main() {
	suiteDir := flag.String("suite", "", "Suite directory holding <starter-id>/<task-id>.json fixtures")
	flag.Parse()
	if *suiteDir == "" {
		if len(os.Args) > 1 {
			*suiteDir = os.Args[1]
		}
	}
	if *suiteDir == "" {
		fmt.Fprintln(os.Stderr, "usage: benchmark-suite-check <suite-dir> (or -suite <suite-dir>)")
		os.Exit(2)
	}

	failed, perStarter, err := checkSuite(*suiteDir)
	if err != nil {
		// A missing/unreadable ROOT suite dir is a usage problem (exit 2),
		// distinct from fixtures that fail to load (exit 1).
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}
	starterNames := make([]string, 0, len(perStarter))
	for s := range perStarter {
		starterNames = append(starterNames, s)
	}
	sort.Strings(starterNames)
	fmt.Println("---- summary ----")
	for _, s := range starterNames {
		fmt.Printf("  %s: %d task(s)\n", s, perStarter[s])
	}
	if failed > 0 {
		fmt.Printf("RESULT: %d file(s) FAILED\n", failed)
		os.Exit(1)
	}
	total := 0
	for _, n := range starterNames {
		total += perStarter[n]
	}
	fmt.Printf("RESULT: all valid, %d task(s) loaded\n", total)
}

// checkSuite loads every task file under dir with the harness's loader and
// enforces the suite layout invariant. It returns the number of failed
// files and the count of valid tasks per starter directory. It prints one
// line per task file as it goes. If the root suite directory cannot be
// read, it returns a non-nil error (a usage problem, not a fixture problem)
// and no per-file results.
func checkSuite(dir string) (failed int, perStarter map[string]int, err error) {
	perStarter = map[string]int{}

	starterDirs, err := os.ReadDir(dir)
	if err != nil {
		return 0, perStarter, fmt.Errorf("read suite dir %s: %w", dir, err)
	}

	for _, sd := range starterDirs {
		if !sd.IsDir() {
			continue
		}
		starter := sd.Name()
		files, err := os.ReadDir(filepath.Join(dir, starter))
		if err != nil {
			fmt.Printf("FAIL %s/ : read dir: %v\n", starter, err)
			failed++
			continue
		}
		for _, f := range files {
			if f.IsDir() || !isTaskFileName(f.Name()) {
				continue
			}
			path := filepath.Join(dir, starter, f.Name())
			task, err := benchmark.LoadTask(path)
			if err != nil {
				fmt.Printf("FAIL %s/%s: %s\n", starter, f.Name(), oneLine(err))
				failed++
				continue
			}
			if task.Starter != starter {
				fmt.Printf("FAIL %s/%s: starter field %q does not match its directory %q\n",
					starter, f.Name(), task.Starter, starter)
				failed++
				continue
			}
			perStarter[starter]++
			fmt.Printf("PASS %s/%s\n", starter, f.Name())
		}
	}
	return failed, perStarter, nil
}

// isTaskFileName mirrors the suite layout rule: a task file is a .json
// file with a non-empty stem (a file named exactly ".json" is not a task).
func isTaskFileName(name string) bool {
	stem := strings.TrimSuffix(name, ".json")
	return stem != "" && stem != name
}

// oneLine collapses a (multi-problem) error to a single line for the
// per-file report.
func oneLine(err error) string {
	return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " ")
}
