//go:build !js

// `sprout health` reports a project's health on demand: size and complexity
// signals from the source tree, plus the project's failing build/test checks.
// Every concern is a finding with a small, separately approvable fix — the
// command reports them and never applies one.
//
// The heavy lifting lives in pkg/health (scanning, thresholds, the check
// runner adapter and the finding model); this file only wires the flags and
// renders the report as text or JSON.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/health"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// healthFlags holds the `sprout health` flag values.
type healthFlags struct {
	dir           string
	maxFileLines  int
	maxComplexity int
	noChecks      bool
	jsonOut       bool
}

var healthFlagsState healthFlags

var healthCmd = &cobra.Command{
	Use:   "health [dir]",
	Short: "Report size, complexity, and failing-check health signals",
	Long: `Report a project's health on demand.

The check scans the project's source tree (Go, TypeScript, JavaScript, and
Python) for size and complexity signals — files over a line threshold and
functions over a branching-complexity threshold — and runs the project's
configured build and test commands through the verification runner to report
failing checks.

Every concern is a finding that carries a small, separately approvable fix,
for example "split pkg/x/big.go: 1200 lines, exceeds 800". The command only
reports the proposed fixes; it never applies one.

The build and test commands come from the same trusted sources as the rest of
the system: the project's starter manifest (.sprout/starter.json) or its
explicit verification configuration — never from a model.

Examples:
  sprout health
  sprout health ./my-app
  sprout health --max-file-lines 500 --max-complexity 12
  sprout health --no-checks --json`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := healthFlagsState.dir
		if len(args) > 0 {
			dir = args[0]
		}
		return runHealth(cmd, dir, healthFlagsState, cmd.OutOrStdout())
	},
}

func init() {
	healthCmd.Flags().StringVar(&healthFlagsState.dir, "dir", "", "Project directory to scan (default: current directory)")
	healthCmd.Flags().IntVar(&healthFlagsState.maxFileLines, "max-file-lines", 0, "Report files longer than this many lines (default 800)")
	healthCmd.Flags().IntVar(&healthFlagsState.maxComplexity, "max-complexity", 0, "Report functions with a branching complexity above this (default 15)")
	healthCmd.Flags().BoolVar(&healthFlagsState.noChecks, "no-checks", false, "Skip the build/test checks; report size and complexity only")
	healthCmd.Flags().BoolVar(&healthFlagsState.jsonOut, "json", false, "Output machine-readable JSON")
	rootCmd.AddCommand(healthCmd)
}

// runHealth is the body of `sprout health`, cobra-free so tests drive it
// directly. It resolves the project root, builds a health report (running the
// project's build/test checks unless skipped), and renders it.
func runHealth(cmd *cobra.Command, dir string, flags healthFlags, out io.Writer) error {
	root, err := resolveHealthRoot(dir)
	if err != nil {
		return err
	}

	cfg := health.Config{
		MaxFileLines:  flags.maxFileLines,
		MaxComplexity: flags.maxComplexity,
	}
	if !flags.noChecks {
		runner, rerr := healthCheckRunner()
		if rerr != nil {
			return rerr
		}
		cfg.Checks = runner
	}

	report, err := health.Build(cmd.Context(), root, cfg)
	if err != nil {
		return err
	}

	if flags.jsonOut {
		return writeHealthJSON(out, report)
	}
	return writeHealthText(out, report)
}

// resolveHealthRoot resolves the directory to scan to an absolute path.
func resolveHealthRoot(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", dir, err)
	}
	return abs, nil
}

// healthCheckRunner builds the verification runner the health check uses for
// its build/test portion. Commands are resolved from the starter manifest and
// the project's explicit configuration only; when the project's configuration
// cannot be loaded the manifest is still honored, so the scan still runs.
func healthCheckRunner() (*verify.Runner, error) {
	runner := verify.New()
	mgr, err := configuration.NewManagerSilent()
	if err != nil {
		return runner, nil
	}
	if cfg := mgr.GetConfig(); cfg != nil {
		runner.ConfigCommands = verify.ConfigurationCommands(cfg)
	}
	return runner, nil
}

// writeHealthJSON renders the report as indented JSON.
func writeHealthJSON(out io.Writer, report *health.Report) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// writeHealthText renders the report for a terminal: a summary line, each
// finding with its proposed fix, and any run-level errors.
func writeHealthText(out io.Writer, report *health.Report) error {
	var b strings.Builder
	if len(report.Findings) == 0 {
		fmt.Fprintf(&b, "No health findings for %s (%d source files scanned).\n", report.Root, report.FilesScanned)
	} else {
		fix, warn := report.Counts()
		fmt.Fprintf(&b, "Health report for %s\n", report.Root)
		fmt.Fprintf(&b, "%d source files scanned; %d finding(s): %d to fix, %d to review\n",
			report.FilesScanned, len(report.Findings), fix, warn)

		for _, f := range report.Findings {
			marker := "!"
			if f.Severity == health.SeverityFix {
				marker = "x"
			}
			fmt.Fprintf(&b, "\n  [%s] %s\n", marker, f.Message)
			if f.Fix != nil {
				fmt.Fprintf(&b, "      proposed fix: %s\n", f.Fix.Summary)
				if f.Fix.Detail != "" {
					fmt.Fprintf(&b, "      %s\n", f.Fix.Detail)
				}
			}
		}
	}

	if report.Checks != "" {
		fmt.Fprintf(&b, "\nChecks: %s\n", report.Checks)
	}
	if report.FilesSkipped > 0 {
		fmt.Fprintf(&b, "Note: %d source file(s) were skipped (too large or unreadable)\n", report.FilesSkipped)
	}
	for _, e := range report.Errors {
		fmt.Fprintf(&b, "Note: %s\n", e)
	}

	_, err := io.WriteString(out, b.String())
	return err
}
