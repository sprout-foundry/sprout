//go:build !js

package cmd

// api.go — the `sprout api` command group: host-facing tooling for the
// versioned API contract in docs/api. The conformance subcommand runs the
// read-only probe suite (pkg/apiconformance) against any implementation of the
// contract and exits non-zero on failure, so a host can run it in CI against
// the endpoint it serves.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/apiconformance"
)

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Tooling for the versioned API contract",
	Long: `Tooling for the versioned API contract (docs/api).

The contract is a generated OpenAPI document plus a conformance suite that
any implementation (the local daemon, the in-browser agent, or a host that
serves part of the API) can run against itself.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// Flag variables for the conformance subcommand. Package-level so tests can
// set them directly without driving cobra flag parsing.
var (
	apiConcBaseURL  string
	apiConcFamilies []string
	apiConcSpec     string
	apiConcJSON     bool
	apiConcTimeout  time.Duration
)

var apiConformanceCmd = &cobra.Command{
	Use:   "conformance",
	Short: "Run the API conformance suite against a live implementation",
	Long: `Run the read-only conformance suite against a live implementation of the
API contract.

The suite loads the OpenAPI contract document, sends its safe, read-only
probes to --base-url, checks each response status and body shape, and prints
a per-family report. It exits 0 when every probe passes, 1 when any probe
fails (the report is printed to stdout, the failure line to stderr), and 2
on a usage error.

Mutating endpoints are never probed: the suite is read-only by design, so it
is safe to run against a live workspace.`,
	RunE: runAPIConformance,
}

// runAPIConformance loads the contract, filters the probe set, runs the safe
// suite against the base URL, and prints the report. It is the RunE of the
// conformance subcommand and is exercised directly by tests.
func runAPIConformance(cmd *cobra.Command, args []string) error {
	if apiConcBaseURL == "" {
		return usageErrorf(cmd, "a --base-url is required")
	}

	specPath := apiConcSpec
	if specPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve working directory: %w", err)
		}
		specPath, err = locateOpenAPIContract(cwd)
		if err != nil {
			return fmt.Errorf("locate the OpenAPI contract: %w", err)
		}
	}

	spec, err := apiconformance.LoadSpec(specPath)
	if err != nil {
		return fmt.Errorf("load OpenAPI contract %s: %w", specPath, err)
	}

	probes := apiconformance.DefaultProbes()
	if len(apiConcFamilies) > 0 {
		// filterProbesByFamily's only failure mode is an unknown family name,
		// which is a bad --families value: a usage error (exit 2), not a
		// runtime error.
		var filterErr error
		probes, filterErr = filterProbesByFamily(probes, spec.Families(), apiConcFamilies)
		if filterErr != nil {
			return usageErrorf(cmd, "%v", filterErr)
		}
		if len(probes) == 0 {
			return usageErrorf(cmd, "the selected families have no safe read-only probes")
		}
	}

	opts := []apiconformance.Option{apiconformance.WithProbes(probes)}
	if apiConcTimeout > 0 {
		opts = append(opts, apiconformance.WithClient(&http.Client{Timeout: apiConcTimeout}))
	}

	// cmd.Context() is nil when RunE is invoked directly (as the tests do,
	// without going through cobra's Execute); fall back to a background
	// context so the probes can still run.
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	suite := apiconformance.New(apiConcBaseURL, spec, opts...)
	report, err := suite.RunSafe(ctx)
	if err != nil {
		return fmt.Errorf("run conformance suite: %w", err)
	}

	if apiConcJSON {
		out, err := report.JSON()
		if err != nil {
			return fmt.Errorf("render report: %w", err)
		}
		_, _ = fmt.Fprintln(os.Stdout, string(out))
	} else {
		_, _ = fmt.Fprintln(os.Stdout, report.Render())
	}

	if !report.AllPassed() {
		return fmt.Errorf("%d of %d probes failed (report printed to stdout)", len(report.Failed()), len(report.Results))
	}
	return nil
}

// locateOpenAPIContract walks up from start (at most a bounded number of
// levels) looking for docs/api/openapi.yaml and returns its absolute path.
// A host that built the binary elsewhere passes --spec explicitly instead.
func locateOpenAPIContract(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	dir := abs
	for depth := 0; depth < 12; depth++ {
		candidate := filepath.Join(dir, "docs", "api", "openapi.yaml")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() { // #nosec G304 -- walked-up repo path
			return filepath.Abs(candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("docs/api/openapi.yaml not found walking up from %s (pass --spec to point at it)", start)
}

// filterProbesByFamily keeps the probes whose family is in wanted, validating
// every wanted name against the contract's known families. An unknown family
// is a usage error (it names a tag the contract does not define, which is
// almost always a typo or a version skew).
func filterProbesByFamily(probes []apiconformance.Probe, known, wanted []string) ([]apiconformance.Probe, error) {
	valid := make(map[string]struct{}, len(known))
	for _, f := range known {
		valid[f] = struct{}{}
	}
	selected := make(map[string]struct{}, len(wanted))
	for _, f := range wanted {
		if _, ok := valid[f]; !ok {
			return nil, fmt.Errorf("unknown family %q (the contract defines: %s)", f, strings.Join(known, ", "))
		}
		selected[f] = struct{}{}
	}
	filtered := make([]apiconformance.Probe, 0, len(probes))
	for _, p := range probes {
		if _, ok := selected[p.Family]; ok {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

func init() {
	apiConformanceCmd.Flags().StringVar(&apiConcBaseURL, "base-url", "", "Base URL of the implementation to check (required)")
	apiConformanceCmd.Flags().StringSliceVar(&apiConcFamilies, "families", nil, "Restrict the safe probes to these contract families (comma-separated or repeated; default: all)")
	apiConformanceCmd.Flags().StringVar(&apiConcSpec, "spec", "", "Path to the OpenAPI contract document (default: locate docs/api/openapi.yaml from the working directory)")
	apiConformanceCmd.Flags().BoolVar(&apiConcJSON, "json", false, "Print the per-family report as JSON on stdout (for CI parsing)")
	apiConformanceCmd.Flags().DurationVar(&apiConcTimeout, "timeout", 0, "Per-probe request timeout (default 10s)")
	_ = apiConformanceCmd.MarkFlagRequired("base-url")

	apiCmd.AddCommand(apiConformanceCmd)
	rootCmd.AddCommand(apiCmd)
}
