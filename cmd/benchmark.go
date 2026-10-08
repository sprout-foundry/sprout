//go:build !js

// benchmark.go — the on-demand `sprout benchmark` entry point for the
// agent task benchmark (pkg/benchmark): load a task suite, run it against
// real providers (network + cost — never part of `go test`), and write the
// markdown + JSON report. The suite-level failures (a bad suite dir, no
// tasks, a cancelled run) exit non-zero; a run's own failure rides on the
// report, exactly as the harness's record-and-continue contract promises.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/benchmark"
	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

const (
	// benchmarkDefaultSuiteDir is where the committed task fixtures live,
	// relative to the sprout checkout root (the usual working directory
	// for a benchmark run).
	benchmarkDefaultSuiteDir = "benchmarks/tasks"
	// benchmarkDefaultOutDir receives report.md and report.json.
	benchmarkDefaultOutDir = "benchmark-results"
)

// benchmarkRunnerFor is the seam tests use to run the CLI end to end
// without a provider: production leaves it nil and the command builds the
// harness's default runner (real provider clients); a test swaps in a
// runner whose AgentFactory is the scripted model.
var benchmarkRunnerFor func(suiteDir string, models []benchmark.ModelSpec, runs int, timeout time.Duration, keepRuns benchmark.KeepRuns, evidenceDir string) *benchmark.Runner

var (
	benchmarkSuiteDir string
	benchmarkModels   string
	benchmarkOutDir   string
	benchmarkRuns     int
	benchmarkTimeout  time.Duration
	benchmarkKeepRuns string
)

var benchmarkCmd = &cobra.Command{
	Use:   "benchmark [suite-dir]",
	Short: "Run the agent task benchmark (real providers: network + cost)",
	Long: `Run the agent task benchmark and write a report.

Loads every task fixture under the suite directory (layout:
<suite>/<starter-id>/<task-id>.json), runs each task's plain-language
request headlessly — a fresh copy of the task's starter per run,
3 runs per model by default — and writes report.md + report.json
comparing models and starters.

This benchmark calls real providers: every run costs network and money.
Pass/fail comes only from the turn-end verification (the plan's checks
run against the fresh copy), never from the model's own reply.

Each run carries a wall-clock timeout; a hung turn is stopped through the
agent's interrupt mechanism and recorded as a failed run.

Run evidence (the run's working-copy diff against its baseline, the agent
transcript and the verification output) is kept under <output>/runs/ for
failed runs by default; --keep-runs=all keeps it for every run and
--keep-runs=none keeps none.

Examples:
  # Committed fixture suite, default model list (the provider catalog's
  # recommended models)
  sprout benchmark

  # Two models, one run each, into a chosen directory
  sprout benchmark --models anthropic/claude-sonnet-4-5,openai/gpt-5-mini --runs 1 --output out/bench

  # Stop a run that hangs for more than five minutes
  sprout benchmark --timeout 5m

  # Keep evidence for every run, passed or failed
  sprout benchmark --keep-runs all`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBenchmarkCmd,
}

func init() {
	benchmarkCmd.Flags().StringVar(&benchmarkSuiteDir, "suite", "",
		"Suite directory holding <starter-id>/<task-id>.json fixtures (default benchmarks/tasks)")
	benchmarkCmd.Flags().StringVar(&benchmarkModels, "models", "",
		"Comma-separated provider/model list (default: the provider catalog's recommended models)")
	benchmarkCmd.Flags().StringVar(&benchmarkOutDir, "output", "",
		"Directory for report.md and report.json (default benchmark-results)")
	benchmarkCmd.Flags().IntVar(&benchmarkRuns, "runs", 0,
		"Runs per task per model (default 3)")
	benchmarkCmd.Flags().DurationVar(&benchmarkTimeout, "timeout", 0,
		"Per-run wall-clock limit; a run exceeding it is interrupted and recorded as failed (default 10m)")
	benchmarkCmd.Flags().StringVar(&benchmarkKeepRuns, "keep-runs", "failed",
		"Keep run evidence (working-copy diff, transcript, verification output) for failed runs, all runs, or none (failed|all|none, default failed)")
	rootCmd.AddCommand(benchmarkCmd)
}

// runBenchmarkCmd validates the invocation, runs the suite, and writes the
// report. Invocation mistakes (bad --models, a missing suite dir, a suite
// with no tasks) are usage errors; the run itself is a long, priced
// operation whose per-run failures never abort it.
func runBenchmarkCmd(cmd *cobra.Command, args []string) error {
	suiteDir := strings.TrimSpace(benchmarkSuiteDir)
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		suiteDir = strings.TrimSpace(args[0])
	}
	if suiteDir == "" {
		suiteDir = benchmarkDefaultSuiteDir
	}

	models, err := parseBenchmarkModels(benchmarkModels)
	if err != nil {
		return usageErrorf(cmd, "%v", err)
	}

	keepRuns, err := benchmark.ParseKeepRuns(benchmarkKeepRuns)
	if err != nil {
		return usageErrorf(cmd, "%v", err)
	}

	tasks, err := benchmark.LoadSuite(suiteDir)
	if err != nil {
		return newUsageError(cmd, fmt.Errorf("load benchmark suite: %w", err))
	}
	if len(tasks) == 0 {
		return usageErrorf(cmd,
			"no benchmark tasks found in %s (tasks live at <suite>/<starter-id>/<task-id>.json)",
			suiteDir)
	}

	outDir := strings.TrimSpace(benchmarkOutDir)
	if outDir == "" {
		outDir = benchmarkDefaultOutDir
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create benchmark output dir %s: %w", outDir, err)
	}

	// The suite runs against real providers, so the run needs the user's
	// live configuration (credentials, models, verification commands).
	// Without --models the suite runs the default model list, so detect
	// the missing-config case before the run starts — a benchmark is the
	// wrong place to discover it, after minutes of priced calls. With
	// --models every entry names its provider and the runner switches to
	// it per run, so no configured default provider is needed.
	var runner *benchmark.Runner
	if benchmarkRunnerFor != nil {
		runner = benchmarkRunnerFor(suiteDir, models, benchmarkRuns, benchmarkTimeout, keepRuns, outDir)
	} else {
		mgr, err := configuration.NewManagerSilent()
		if err != nil {
			return withHint(fmt.Errorf("initializing configuration: %w", err),
				"Run 'sprout keys set <provider>' to configure an API key.")
		}
		if len(models) == 0 && strings.TrimSpace(mgr.GetConfig().LastUsedProvider) == "" {
			return withHint(errors.New("no provider configured for the benchmark to run against"),
				"Run 'sprout keys set <provider>' to configure an API key, or 'sprout benchmark --help' for what a run costs.")
		}
		runner = &benchmark.Runner{
			ConfigManager: mgr,
			Models:        models,
			RunsPerTask:   benchmarkRuns,
			Timeout:       benchmarkTimeout,
			KeepRuns:      keepRuns,
			EvidenceDir:   outDir,
		}
	}

	suiteModels := runner.SuiteModels()
	console.GlyphAction.Printf("Running %d task(s) × %d model(s), %d run(s) per pair — real provider calls (network + cost)",
		len(tasks), len(suiteModels), effectiveBenchmarkRuns(runner))

	// Run on the command's context so an interrupt (Ctrl+C, or a caller
	// that cancels an embedding command context) stops the suite between
	// pairs. The partial report is still written from the runs completed
	// before the stop — an interrupted benchmark is exactly when its
	// partial numbers matter. Cobra's ExecuteContext stamps the context on
	// the command it was called on (the root here), not on every child, so
	// the root's context is the fallback.
	// The root's context, not the found command's: cobra stamps the context
	// on whichever command ExecuteContext was called on (the root here) and
	// only fills a child's from it when the child's is nil, so a child's
	// context can go stale across Execute calls in one process.
	runCtx := cmd.Root().Context()
	runs, runErr := runner.RunSuite(runCtx, tasks)

	// The report is written from every completed run, even when the suite
	// stopped early (a cancelled run returns the runs accumulated so far):
	// an interrupted benchmark is exactly when its partial numbers matter.
	meta := benchmark.Meta{
		RunDate: time.Now().Format("2006-01-02"),
		Version: buildinfo.Version,
	}
	report := benchmark.BuildReport(runs, suiteModels, meta)
	mdPath, jsonPath, err := writeBenchmarkReports(outDir, report)
	if err != nil {
		return err
	}

	printBenchmarkSummary(report, mdPath, jsonPath)

	if runErr != nil {
		console.GlyphWarning.Printf("Benchmark suite stopped early: %v (partial report written)", runErr)
		return fmt.Errorf("benchmark suite stopped: %w", runErr)
	}
	return nil
}

// writeBenchmarkReports writes report.md and report.json into outDir and
// returns their paths.
func writeBenchmarkReports(outDir string, report benchmark.Report) (string, string, error) {
	mdPath := filepath.Join(outDir, "report.md")
	jsonPath := filepath.Join(outDir, "report.json")

	if err := os.WriteFile(mdPath, []byte(report.Markdown()), 0o644); err != nil {
		return "", "", fmt.Errorf("write benchmark report %s: %w", mdPath, err)
	}
	data, err := report.JSON()
	if err != nil {
		return "", "", fmt.Errorf("render benchmark report %s: %w", jsonPath, err)
	}
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		return "", "", fmt.Errorf("write benchmark report %s: %w", jsonPath, err)
	}
	return mdPath, jsonPath, nil
}

// printBenchmarkSummary prints the report headline: the pooled pass rate
// per starter per model (the number a readiness bar reads) and the written
// paths. The full detail lives in the report files.
func printBenchmarkSummary(report benchmark.Report, mdPath, jsonPath string) {
	fmt.Println()
	console.GlyphSuccess.Print("Benchmark report written:")
	fmt.Printf("  %s\n  %s\n", mdPath, jsonPath)

	if len(report.Starters) > 0 {
		fmt.Println()
		fmt.Println("Pass rate per starter per model:")
		for _, sr := range report.Starters {
			fmt.Printf("  %s %s/%s: %d/%d runs passed (%.0f%%)\n",
				sr.Starter, sr.Provider, sr.Model, sr.Passed, sr.RunsTotal, 100*sr.PassRate)
		}
	}
	if len(report.FailureCategories) > 0 {
		fmt.Println()
		fmt.Println("Failure categories (see the report for the per-run detail):")
		for _, cat := range sortedBenchmarkFailureCategories(report.FailureCategories) {
			fmt.Printf("  %s: %d\n", cat, report.FailureCategories[cat])
		}
	}
}

// sortedBenchmarkFailureCategories returns the categories in sorted order —
// the map itself is only read, never ranged directly (map iteration order
// would make the CLI output non-deterministic).
func sortedBenchmarkFailureCategories(cats map[string]int) []string {
	out := make([]string, 0, len(cats))
	for k := range cats {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// parseBenchmarkModels parses the --models value: a comma-separated list of
// "provider/model" (or bare "model", which leaves the provider to the run's
// configuration). Empty input returns nil — the harness's default model
// list applies. A malformed entry is an invocation error.
func parseBenchmarkModels(spec string) ([]benchmark.ModelSpec, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	var out []benchmark.ModelSpec
	for _, part := range strings.Split(spec, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		provider, model := "", entry
		if i := strings.Index(entry, "/"); i >= 0 {
			provider, model = strings.TrimSpace(entry[:i]), strings.TrimSpace(entry[i+1:])
		}
		if model == "" {
			return nil, fmt.Errorf("bad --models entry %q (want provider/model, e.g. openai/gpt-5-mini)", entry)
		}
		out = append(out, benchmark.ModelSpec{Model: model, Provider: provider})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("bad --models %q: no models found (want a comma-separated provider/model list)", spec)
	}
	return out, nil
}

// effectiveBenchmarkRuns is the runs-per-pair count the start line reports:
// the runner's own resolution (the --runs flag where positive, 3 otherwise).
func effectiveBenchmarkRuns(runner *benchmark.Runner) int {
	if runner != nil && runner.RunsPerTask > 0 {
		return runner.RunsPerTask
	}
	return 3
}
