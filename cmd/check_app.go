//go:build !js

// `sprout check app` runs the app check: it builds the project, serves the
// build output the way the platform hosts it, uses the app in a headless
// browser the way a visitor would, and reports what a person would run into.
// The report is .sprout/app-check.json plus screenshots in
// .sprout/app-check/ (or --output); the platform parses the JSON, so its shape is
// a contract (see pkg/appcheck.Report).
//
// The browser is the same Chromium browse_url drives (pkg/webcontent's Rod
// renderer), so there is one browser stack and no driver pinned to a browser
// version. The check never stands in the way of work: it exits 1 only when it
// ran and found problems, and 0 when it found none or could not run.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/appcheck"
)

// appCheckFlags holds the `sprout check app` flag values.
type appCheckFlags struct {
	noBuild bool
	budget  int
	out     string
}

var appCheckFlagState appCheckFlags

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Run project checks that look at the running app",
	Long: `Run project checks that look at the running app.

Check commands build a project, run what it produces, and report what a person
would run into. They are also available to the agent as tools.`,
}

var checkAppCmd = &cobra.Command{
	Use:   "app",
	Short: "Use the built app in a browser and report what a person would run into",
	Long: `Use the built app in a browser and report what a person would run into.

Builds the project (unless --no-build is given and dist/ exists), serves dist/
the way the platform hosts it — the bundled server (dist/worker.js, wrangler
dev) when the project has one, a static server otherwise — and uses the app in
a headless browser: visiting routes, filling and sending forms with sensible
values, and recording pages that do not load or are blank, forms that refuse
values or send nothing, failed requests, console errors, and entries gone after
a fresh load.

Writes .sprout/app-check.json and screenshots in .sprout/app-check/ (or in the
--output folder) and prints the findings. Exits 1 when it found problems, 0 when
it found none or could not run (no browser): the check never stands in the way
of work.

Examples:
  sprout check app
  sprout check app --no-build --budget 60
  sprout check app --output /tmp/app-check`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runAppCheck(cmd, appCheckFlagState, cmd.OutOrStdout())
	},
}

func init() {
	checkAppCmd.Flags().BoolVar(&appCheckFlagState.noBuild, "no-build", false, "Skip the build when dist/ already exists")
	checkAppCmd.Flags().IntVar(&appCheckFlagState.budget, "budget", 0, "Seconds to spend using the app (default 120)")
	checkAppCmd.Flags().StringVar(&appCheckFlagState.out, "output", "", "Write the report and screenshots here instead of .sprout/")
	// --out is the platform scaffold's spelling (app-check.mjs); kept as a
	// hidden alias.
	checkAppCmd.Flags().StringVar(&appCheckFlagState.out, "out", "", "Alias for --output")
	_ = checkAppCmd.Flags().MarkHidden("out")
	checkCmd.AddCommand(checkAppCmd)
	rootCmd.AddCommand(checkCmd)
}

// errAppCheckProblems marks a check that ran and found problems: the findings
// were already printed, so the error exists only to make the process exit 1
// (the check's contract). The central error renderer skips it.
var errAppCheckProblems = errors.New("the app check found problems")

// RunAppCheck runs the app check and returns its exit code (1 when it ran and
// found problems, 0 otherwise). It is the entry point tests drive.
func RunAppCheck(ctx context.Context, root string, flags appCheckFlags, out io.Writer) (int, error) {
	if flags.budget < 0 {
		return 0, errors.New("--budget must be zero or more seconds")
	}
	report, err := appcheck.Run(ctx, appcheck.Options{
		Root:    root,
		NoBuild: flags.noBuild,
		Budget:  time.Duration(flags.budget) * time.Second,
		OutDir:  flags.out,
	})
	if err != nil {
		return 0, err
	}
	renderAppCheck(report, root, flags.out, out)
	return report.ExitCode(), nil
}

// runAppCheck is the cobra body: it runs the check in the current directory,
// prints its findings, and returns errAppCheckProblems when there are any so
// the process exits 1.
func runAppCheck(cmd *cobra.Command, flags appCheckFlags, out io.Writer) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve the project root: %w", err)
	}
	code, err := RunAppCheck(cmd.Context(), root, flags, out)
	if err != nil {
		return err
	}
	if code != 0 {
		return errAppCheckProblems
	}
	return nil
}

// renderAppCheck prints the report the way the platform check printed it: a
// summary line, each finding, and where the details are.
func renderAppCheck(report *appcheck.Report, root, outDir string, out io.Writer) {
	if report.Status != appcheck.StatusRan {
		_, _ = fmt.Fprintf(out, "App check could not run: %s\n", report.Reason)
		return
	}
	problems := report.Problems()
	notes := report.Notes()
	_, _ = fmt.Fprintf(out, "App check: %d page(s), %d form(s) or action(s) used; %d problem(s), %d note(s).\n",
		len(report.Pages), len(report.Forms), len(problems), len(notes))
	for _, f := range problems {
		_, _ = fmt.Fprintf(out, " problem: %s\n", f.Text)
	}
	for _, f := range notes {
		_, _ = fmt.Fprintf(out, " note: %s\n", f.Text)
	}
	reportPath := filepath.Join(root, ".sprout", "app-check.json")
	if outDir != "" {
		reportPath = filepath.Join(outDir, "app-check.json")
	} else if rel, err := filepath.Rel(root, reportPath); err == nil {
		reportPath = rel
	}
	_, _ = fmt.Fprintf(out, "Details: %s\n", reportPath)
}
