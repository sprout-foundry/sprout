//go:build !js

package appcheck

// run.go is the check itself: build, serve, probe, write the report. It
// reproduces the platform's app check step for step, so the report a project
// gets from `sprout check app` is the one its platform run produces.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultBudget bounds the time spent using the app, in seconds, when the
// caller does not set one (the platform check's default).
const DefaultBudget = 120 * time.Second

// Options configures one check.
type Options struct {
	// Root is the project root (the directory holding package.json).
	Root string
	// NoBuild skips the build when the build output already exists.
	NoBuild bool
	// Budget bounds the time spent using the app.
	Budget time.Duration
	// OutDir puts the report and screenshots outside the project ("" =
	// .sprout/app-check.json and .sprout/app-check/). The platform keeps
	// them outside the project so they never become part of the change.
	OutDir string
	// Browser opens the pages; nil means the shared Rod browser
	// (webcontent), which is also what browse_url drives.
	Browser RodBrowser
	// Log receives progress lines (nil = none).
	Log func(line string)
}

// browserFactories returns the browsers to try in order: the injected one, or
// the production chain. A test injects one; the production chain is a single
// browser so a missing browser reports "could not run" once, cleanly.
func (o Options) browser() RodBrowser {
	if o.Browser != nil {
		return o.Browser
	}
	return NewWebcontentBrowser()
}

// Run runs the check and returns its report. An error means the report could
// not be written; every expected failure (no project, no build, no browser,
// a crashed server) is represented in the report instead.
func Run(ctx context.Context, opts Options) (*Report, error) {
	start := time.Now()
	root := opts.Root
	if root == "" {
		root = "."
	}
	reportPath, shotsDir := reportPaths(root, opts.OutDir)

	finish := func(r *Report) (*Report, error) {
		r.CheckedAt = time.Now().UTC().Format(time.RFC3339)
		r.DurationMS = time.Since(start).Milliseconds()
		if r.Screenshot == "" && len(r.Pages) > 0 {
			r.Screenshot = r.Pages[0].Screenshot
		}
		if err := r.WriteJSON(reportPath); err != nil {
			return r, err
		}
		return r, nil
	}

	if _, err := os.Stat(filepath.Join(root, "package.json")); err != nil {
		return finish(NotRun("run it from the project root (no package.json here)."))
	}

	dist := filepath.Join(root, "dist")
	if !opts.NoBuild || !exists(dist) {
		if err := build(root); err != nil {
			return finish(Failed("npm run build failed:\n" + err.Error()))
		}
	}
	if !exists(dist) {
		return finish(NotRun("the build made no dist/ folder."))
	}

	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	app, stop, err := serveApp(serveCtx, serveOptions{Root: root, Dist: dist, Wrangler: wranglerBin(root)})
	if err != nil {
		return finish(Failed("The hosted app did not start:\n" + tail(err.Error(), 1500)))
	}
	defer stop()

	if err := os.RemoveAll(shotsDir); err != nil {
		return finish(NotRun("the check could not clear its screenshot folder (" + err.Error() + ")."))
	}
	budget := opts.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	run, probeErr := probe(ctx, probeConfig{Browser: opts.browser(), BaseURL: app.BaseURL, ShotsDir: shotsDir, Budget: budget})
	if probeErr != nil {
		if BrowserUnavailable(probeErr) {
			return finish(NotRun("browser not available (" + firstLine(probeErr) + ")."))
		}
		return finish(NotRun("the check failed to run (" + firstLine(probeErr) + ")."))
	}

	report := &Report{
		Status:         StatusRan,
		HasServer:      app.HasServer,
		Findings:       findingsFrom(run, app.HasServer),
		Pages:          run.pages,
		Forms:          run.forms,
		FailedRequests: run.failed,
		ConsoleErrors:  run.consoleErrors,
		ProbeKept:      run.probeKept,
		BudgetReached:  run.budgetReached,
	}
	if report.Forms == nil {
		report.Forms = []FormOutcome{}
	}
	if report.FailedRequests == nil {
		report.FailedRequests = []string{}
	}
	if report.ConsoleErrors == nil {
		report.ConsoleErrors = []string{}
	}
	addSettingFindings(report, root, dist, app.Log())
	// Page screenshots are stored relative to the report, so the report can
	// move with its folder.
	for i := range report.Pages {
		if report.Pages[i].Screenshot != "" {
			rel, err := filepath.Rel(filepath.Dir(reportPath), report.Pages[i].Screenshot)
			if err == nil {
				report.Pages[i].Screenshot = rel
			}
		}
	}
	return finish(report)
}

// addSettingFindings prepends findings about how the project is configured
// for hosting: a database whose migrations are not shipped, migrations that
// did not apply, and (last) a server that could not be run.
func addSettingFindings(report *Report, root, dist, serverLog string) {
	toml := readToml(root)
	var prepend []Finding
	if tomlD1Re.MatchString(toml) && !hasMigrations(dist) {
		prepend = append(prepend, Finding{Severity: SeverityProblem, Text: "The app has a database but its build output has no migrations (dist/_migrations/*.sql), so its tables are never created when it is hosted. The build must copy drizzle/migrations/*.sql there (the data module's postbuild script does)."})
	}
	if strings.Contains(serverLog, "migrations for ") && strings.Contains(serverLog, " failed") {
		prepend = append(prepend, Finding{Severity: SeverityProblem, Text: "The database migrations did not apply:\n" + tail(serverLog, 1200)})
	}
	report.Findings = append(prepend, report.Findings...)
	if report.HasServer && wranglerBin(root) == "" {
		report.Findings = append(report.Findings, Finding{Severity: SeverityNote, Text: "wrangler is not installed, so the server was not run."})
	}
}

// reportPaths returns the report file and the screenshot folder for an output
// choice.
func reportPaths(root, outDir string) (string, string) {
	if outDir == "" {
		return filepath.Join(root, ".sprout", "app-check.json"), filepath.Join(root, ".sprout", "app-check")
	}
	return filepath.Join(outDir, "app-check.json"), filepath.Join(outDir, "screenshots")
}

// exists reports whether path exists.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// firstLine returns the first line of err's message, for a one-line "could
// not run" reason.
func firstLine(err error) string {
	text := err.Error()
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return strings.TrimSuffix(text, ".")
}

// Summary is the short, model-readable form of a report: what the check did
// and what a person would run into.
func (r *Report) Summary() string {
	var b strings.Builder
	if r.Status != StatusRan {
		fmt.Fprintf(&b, "App check could not run: %s\n", r.Reason)
		return b.String()
	}
	problems, notes := r.Problems(), r.Notes()
	fmt.Fprintf(&b, "App check: %d page(s), %d form(s) or action(s) used; %d problem(s), %d note(s).\n",
		len(r.Pages), len(r.Forms), len(problems), len(notes))
	for _, f := range problems {
		fmt.Fprintf(&b, "problem: %s\n", f.Text)
	}
	limit := len(notes)
	if limit > 10 {
		limit = 10
	}
	for _, f := range notes[:limit] {
		fmt.Fprintf(&b, "note: %s\n", f.Text)
	}
	if len(notes) > limit {
		fmt.Fprintf(&b, "(%d more note(s) in the report)\n", len(notes)-limit)
	}
	return b.String()
}
