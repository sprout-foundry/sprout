// Package verify implements the SP-149 verification check runner for the
// "build", "test", and "page" check kinds (SP-149 §149a): after a turn that
// changed application code, the runtime (not the model) runs the project's
// build and test commands, and — for page checks — starts the app from the
// manifest's dev command and port, opens each listed route headless, and
// fails on console errors. It reports a structured result (checks, pass/fail,
// output excerpts, screenshot references) that the turn-end hook (SP-149
// §149c / 149.5) and the final-reply contract (149.6) consume.
//
// Where commands come from (SP-149 §149b): a check command comes only
// from (1) the project's starter manifest (.sprout/starter.json, SP-153)
// or (2) the project's explicit configuration
// (configuration.VerificationConfig.BuildCommand / .TestCommand, set by a
// human in the project layer .sprout/workspace.json or in global config) —
// never from model output. The Runner's API has no parameter or field that
// accepts a command: an active plan's acceptance items are read only for
// their ids and kinds, and their Check field (which may carry
// model-proposed commands, because plans are written by the model) is
// ignored. Without an active plan, the build and test commands still run
// as a baseline (§149a).
//
// The package is stdlib plus the pure-data packages (plancontract,
// startermanifest) and their file-system halves (planstore, starterstore),
// so it can be imported by the CLI and by WASM builds.
package verify

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
	"github.com/sprout-foundry/sprout/pkg/startermanifest"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// ManifestLoader loads a project's starter manifest (SP-153). A missing
// manifest must surface starterstore.ErrNoManifest so the runner can fall
// back to the explicit project configuration.
type ManifestLoader func(root string) (*startermanifest.StarterManifest, error)

// PlanLoader loads a project's active SP-148 plan. A missing plan must
// surface planstore.ErrNoPlan so the runner can run the baseline.
type PlanLoader func(root string) (*plancontract.Plan, error)

// ConfigCommandsProvider resolves a project's explicit build and test
// commands (SP-149 §149b) — the trusted source a human sets in the
// project's configuration. Model output never reaches this provider.
type ConfigCommandsProvider func(root string) (Commands, error)

// Runner runs the build, test, and page checks of a SP-149 verification run.
//
// Every field is injectable so the run is testable without a shell; New()
// wires the production defaults. Fields that are nil are treated as
// "absent" (no manifest source, no plan source, no explicit config,
// no default executor) rather than panicking — but a Runner without an
// executor cannot run anything.
type Runner struct {
	// Manifest loads the project's starter manifest. Default:
	// starterstore.LoadStarterManifest (.sprout/starter.json).
	Manifest ManifestLoader
	// Plans loads the active SP-148 plan. Default: planstore
	// (.sprout/plan.json), not-found as planstore.ErrNoPlan.
	Plans PlanLoader
	// ConfigCommands resolves the project's explicit build/test commands
	// (SP-149 §149b). Nil means "no explicit configuration source".
	ConfigCommands ConfigCommandsProvider
	// Exec executes each resolved check command. Default: &ShellExecutor{}.
	Exec Executor
	// Browser opens routes for page checks (SP-149 §149.3). Nil → page
	// checks are skipped with reason "no browser configured". Default:
	// NewWebcontentPageBrowser().
	Browser PageBrowser
	// Timeout bounds a single check. Default: DefaultTimeout; a
	// non-positive value uses DefaultTimeout.
	Timeout time.Duration
	// DevServerTimeout bounds how long a page check waits for the manifest's
	// dev server to come up. Default: DefaultDevServerTimeout; a
	// non-positive value uses DefaultDevServerTimeout.
	DevServerTimeout time.Duration
	// ScreenshotDir is where page-check screenshots are written. "" →
	// <root>/.sprout/verify/screenshots (the .sprout/ directory is
	// gitignored project state).
	ScreenshotDir string
	// MaxExcerptBytes bounds Check.Excerpt (see boundedExcerpt).
	// Default: DefaultMaxExcerptBytes.
	MaxExcerptBytes int
}

// New returns a Runner with the production defaults: the starter manifest
// and the plan from the project's .sprout/ directory, the shell executor,
// the webcontent-backed page browser, and the default per-check, dev-server,
// and excerpt bounds. The caller wires the explicit project configuration
// source (ConfigurationCommands from a merged configuration.Config) when one
// exists.
func New() *Runner {
	return &Runner{
		Manifest:         starterstore.LoadStarterManifest,
		Plans:            planstore.New().Load,
		Exec:             &ShellExecutor{},
		Browser:          NewWebcontentPageBrowser(),
		Timeout:          DefaultTimeout,
		DevServerTimeout: DefaultDevServerTimeout,
		MaxExcerptBytes:  DefaultMaxExcerptBytes,
	}
}

// Run executes the verification checks for the project rooted at root and
// returns the structured result (SP-149 §149a/§149c).
//
//   - With an active plan, the runner runs one check per acceptance kind it
//     implements (build and test in 149.2; page in 149.3; interaction and
//     manual come in 149.4), one check per kind, each covering every
//     acceptance item of that kind.
//   - Without a plan, the build and test commands run as a baseline. A
//     baseline run never produces a page check, even when the manifest
//     declares a dev command, port, and routes (§149a).
//
// Command resolution is per check kind: the starter manifest's command
// wins where it is set, the explicit project configuration fills the gap,
// and a check with no trusted command is skipped (never guessed). The
// plan's acceptance Check fields are never read for a command or a route
// (SP-149 §149b): a page check's routes come only from the manifest.
//
// Run only returns a non-nil error for setup problems (no executor,
// empty root, nil runner). Run-level findings (a corrupt manifest or
// plan, an executor failure) are recorded on the result, never
// swallowed.
func (r *Runner) Run(ctx context.Context, root string) (*Result, error) {
	if r == nil {
		return nil, errors.New("verify: nil runner")
	}
	if r.Exec == nil {
		return nil, errors.New("verify: no executor configured (use New() or set Runner.Exec)")
	}
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("verify: project root is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	result := &Result{Checks: []Check{}}

	manifest := r.loadManifest(root, result)
	plan := r.loadPlan(root, result)
	cmds := r.resolveCommands(manifest, root, result)

	if plan == nil {
		result.Baseline = true
		result.Checks = append(result.Checks,
			Check{Kind: plancontract.KindBuild, Command: cmds.Build},
			Check{Kind: plancontract.KindTest, Command: cmds.Test})
	} else {
		result.PlanRevision = plan.Revision
		result.Checks = append(result.Checks, r.planChecks(plan, manifest, cmds.Build, cmds.Test)...)
	}

	for i := range result.Checks {
		if ctx.Err() != nil {
			result.Checks[i].Skipped = true
			result.Checks[i].Reason = "verification run cancelled before this check"
			continue
		}
		switch result.Checks[i].Kind {
		case plancontract.KindPage:
			r.runPageCheck(ctx, root, &result.Checks[i], manifest)
		default:
			r.runCheck(ctx, root, &result.Checks[i])
		}
	}
	return result, nil
}

// loadManifest returns the project's starter manifest, or nil when the
// project has no usable one (missing → no manifest; corrupt → nil plus a
// recorded run-level error, so the run proceeds with the configuration
// source and, for a page check, a skip reason).
func (r *Runner) loadManifest(root string, result *Result) *startermanifest.StarterManifest {
	if r.Manifest == nil {
		return nil
	}
	manifest, err := r.Manifest(root)
	switch {
	case err == nil:
		return manifest
	case errors.Is(err, starterstore.ErrNoManifest):
		return nil
	default:
		result.Errors = append(result.Errors, "starter manifest: "+err.Error())
		return nil
	}
}

// loadPlan returns the active plan, or nil when the project has no usable
// plan (missing → baseline, corrupt → baseline plus a recorded error).
func (r *Runner) loadPlan(root string, result *Result) *plancontract.Plan {
	if r.Plans == nil {
		return nil
	}
	plan, err := r.Plans(root)
	switch {
	case err == nil:
		return plan
	case errors.Is(err, planstore.ErrNoPlan):
		return nil
	default:
		result.Errors = append(result.Errors, "plan: "+err.Error())
		return nil
	}
}

// resolveCommands returns the trusted commands for this project (SP-149
// §149b): per kind, the starter manifest's command wins where it is set,
// the explicit project configuration fills the gap, and nothing else is
// ever consulted. A manifest that exists but is invalid was already
// recorded as a run-level error by loadManifest, so its (discarded)
// commands stand for no configuration source here.
func (r *Runner) resolveCommands(manifest *startermanifest.StarterManifest, root string, result *Result) Commands {
	var configCommands Commands
	if r.ConfigCommands != nil {
		c, err := r.ConfigCommands(root)
		if err != nil {
			result.Errors = append(result.Errors, "project configuration: "+err.Error())
		} else {
			configCommands = c
		}
	}

	return Commands{
		Build: firstNonBlank(manifestBuild(manifest), configCommands.Build),
		Test:  firstNonBlank(manifestTest(manifest), configCommands.Test),
	}
}

// planChecks builds the checks an active plan gates in 149.3: one check
// per implemented kind that the plan declares (build and test in 149.2,
// page in 149.3; interaction and manual come in 149.4), each covering
// every acceptance item of that kind (plan order). A kind the plan
// declares but no trusted command exists for still gets its check —
// skipped, with a reason — so the result says plainly what could not be
// verified. The single page check carries the manifest's dev command and
// its routes come only from the manifest (SP-149 §149b); the plan's
// acceptance Check fields are never read.
func (r *Runner) planChecks(plan *plancontract.Plan, manifest *startermanifest.StarterManifest, buildCmd, testCmd string) []Check {
	var buildItems, testItems, pageItems []string
	for _, a := range plan.Acceptance {
		switch a.Kind {
		case plancontract.KindBuild:
			buildItems = append(buildItems, a.ID)
		case plancontract.KindTest:
			testItems = append(testItems, a.ID)
		case plancontract.KindPage:
			pageItems = append(pageItems, a.ID)
		}
	}
	var checks []Check
	if len(buildItems) > 0 {
		checks = append(checks, Check{
			Kind:    plancontract.KindBuild,
			Items:   buildItems,
			Command: buildCmd,
		})
	}
	if len(testItems) > 0 {
		checks = append(checks, Check{
			Kind:    plancontract.KindTest,
			Items:   testItems,
			Command: testCmd,
		})
	}
	if len(pageItems) > 0 {
		checks = append(checks, Check{
			Kind:    plancontract.KindPage,
			Items:   pageItems,
			Command: manifestDev(manifest),
		})
	}
	return checks
}

// runCheck executes one check and fills in its outcome. A check with no
// trusted command is skipped, not failed: there is nothing to verify,
// and the runner never invents a command (SP-149 §149b).
func (r *Runner) runCheck(ctx context.Context, root string, c *Check) {
	if c.Command == "" {
		c.Skipped = true
		c.Reason = "no " + string(c.Kind) + " command available: set the starter manifest or the project's explicit verification configuration"
		return
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	start := time.Now()
	outcome, err := r.Exec.Run(runCtx, root, c.Command)
	c.Duration = time.Since(start)
	c.Excerpt = boundedExcerpt(outcome.Output, r.MaxExcerptBytes)
	if err != nil {
		c.Passed = false
		c.Reason = err.Error()
		return
	}
	c.Passed = outcome.Passed
	if !outcome.Passed && strings.TrimSpace(outcome.Reason) != "" {
		c.Reason = outcome.Reason
	}
}

func manifestBuild(m *startermanifest.StarterManifest) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Build)
}

func manifestTest(m *startermanifest.StarterManifest) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Test)
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
