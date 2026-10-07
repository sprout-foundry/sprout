// Package verify implements the verification check runner for the
// "build", "test", "page", and "interaction" check kinds:
// after a turn that changed application code, the runtime (not the model)
// runs the project's build and test commands, and — for page and
// interaction checks — starts the app from the manifest's dev command and
// port and drives a headless browser: page checks open each listed route and
// fail on console errors, while interaction checks run the plan's scripted
// browser steps and confirm the expected outcome. Manual items are listed in
// the result and never gated. It reports a structured result (checks,
// pass/fail, output excerpts, screenshot references) that the turn-end hook
// and the final-reply contract consume.
//
// Where commands come from: a check command comes only
// from (1) the project's starter manifest (.sprout/starter.json)
// or (2) the project's explicit configuration
// (configuration.VerificationConfig.BuildCommand / .TestCommand, set by a
// human in the project layer .sprout/workspace.json or in global config) —
// never from model output. The Runner's API has no parameter or field that
// accepts a command: an active plan's acceptance items are read only for
// their ids and kinds, and their Check field (which may carry
// model-proposed commands, because plans are written by the model) is
// ignored. Without an active plan, the build and test commands still run
// as a baseline.
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

// ManifestLoader loads a project's starter manifest. A missing
// manifest must surface starterstore.ErrNoManifest so the runner can fall
// back to the explicit project configuration.
type ManifestLoader func(root string) (*startermanifest.StarterManifest, error)

// PlanLoader loads a project's active plan. A missing plan must
// surface planstore.ErrNoPlan so the runner can run the baseline.
type PlanLoader func(root string) (*plancontract.Plan, error)

// ConfigCommandsProvider resolves a project's explicit build and test
// commands — the trusted source a human sets in the
// project's configuration. Model output never reaches this provider.
type ConfigCommandsProvider func(root string) (Commands, error)

// Runner runs the build, test, page, and interaction checks of a
// verification run.
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
	// Plans loads the active plan. Default: planstore
	// (.sprout/plan.json), not-found as planstore.ErrNoPlan.
	Plans PlanLoader
	// ConfigCommands resolves the project's explicit build/test commands.
	// Nil means "no explicit configuration source".
	ConfigCommands ConfigCommandsProvider
	// Exec executes each resolved check command. Default: &ShellExecutor{}.
	Exec Executor
	// Browser opens routes for page checks. Nil → page
	// checks are skipped with reason "no browser configured". Default:
	// NewWebcontentPageBrowser().
	Browser PageBrowser
	// StepBrowser runs the plan's scripted browser steps for interaction
	// checks. Nil → interaction checks are skipped with
	// reason "no browser configured". Default: NewWebcontentStepBrowser().
	StepBrowser StepBrowser
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
	// RequireTest is the mechanical input to the require-a-test-for-new-
	// behavior check: when Enabled, a turn that added new behavior must
	// either carry an active-plan test acceptance item or add/change a test
	// file, and the run reports a failing check otherwise. The default (zero
	// value) is disabled — the run's checks are then unchanged.
	RequireTest RequireTestInput
}

// New returns a Runner with the production defaults: the starter manifest
// and the plan from the project's .sprout/ directory, the shell executor,
// the webcontent-backed page browser and step browser, and the default
// per-check, dev-server, and excerpt bounds. The caller wires the explicit
// project configuration source (ConfigurationCommands from a merged
// configuration.Config) when one exists.
func New() *Runner {
	return &Runner{
		Manifest:         starterstore.LoadStarterManifest,
		Plans:            planstore.New().Load,
		Exec:             &ShellExecutor{},
		Browser:          NewWebcontentPageBrowser(),
		StepBrowser:      NewWebcontentStepBrowser(),
		Timeout:          DefaultTimeout,
		DevServerTimeout: DefaultDevServerTimeout,
		MaxExcerptBytes:  DefaultMaxExcerptBytes,
	}
}

// Run executes the verification checks for the project rooted at root and
// returns the structured result. It is implemented as
// a snapshot-then-run: it captures a fresh Snapshot of the manifest and plan
// and executes RunSnapshot against it, so a single Run call
// reads each file exactly once.
//
//   - With an active plan, the runner runs one check per acceptance kind it
//     implements (build and test, page, and interaction), one check per kind
//     for build/test/page (each covering every
//     acceptance item of that kind) and one check per interaction item. A
//     manual check is listed but never executed (it is pre-filled skipped).
//   - Without a plan, the build and test commands run as a baseline. A
//     baseline run never produces a page, interaction, or manual check, even
//     when the manifest declares a dev command, port, and routes.
//
// Command resolution is per check kind: the starter manifest's command
// wins where it is set, the explicit project configuration fills the gap,
// and a check with no trusted command is skipped (never guessed). The
// plan's acceptance Check fields are never read for a command, a route, or a
// step: a page check's routes and an interaction check's
// dev command come only from the manifest, and an interaction check's steps
// come only from the plan's frozen interaction items.
//
// Run only returns a non-nil error for setup problems (no executor,
// empty root, nil runner). Run-level findings (a corrupt manifest or
// plan, an executor failure) are recorded on the result, never
// swallowed.
func (r *Runner) Run(ctx context.Context, root string) (*Result, error) {
	if r == nil {
		return nil, errors.New("verify: nil runner")
	}
	return r.RunSnapshot(ctx, root, r.Snapshot(root))
}

// Snapshot is the frozen input to a turn's verification runs:
// the starter manifest and the active plan captured once, so that
// every verification run of the turn (every repair round) executes against
// the same trusted inputs rather than re-reading the files. A model that
// edits .sprout/starter.json or .sprout/plan.json mid-turn cannot change
// what "passing" means: the commands, the acceptance set, and the plan's
// presence are exactly what the turn started with.
//
// Snapshot loads the manifest and the plan once and records their load
// findings the way Run does today (a corrupt manifest or plan is a
// run-level finding, never swallowed). It freezes:
//   - the manifest's commands (build, test, dev, port, routes);
//   - the plan's acceptance (ids, kinds, and the interaction steps); and
//   - the plan's presence. A plan present at snapshot time gates its checks
//     for the whole turn even if the plan file is later deleted or
//     corrupted; a project with no plan at snapshot time runs the baseline
//     for the whole turn.
//
// Snapshot reads nothing else and runs nothing; it is safe to call at the
// turn's start, before any verification run.
type Snapshot struct {
	// Manifest is the starter manifest captured at snapshot time, or nil
	// when the project has no usable one.
	Manifest *startermanifest.StarterManifest
	// ManifestError is the run-level finding a corrupt manifest produces
	// ("" when the manifest is present or absent).
	ManifestError string
	// Plan is the active plan captured at snapshot time, or nil when the
	// project has no usable plan (absent → baseline; corrupt → baseline
	// plus PlanError).
	Plan *plancontract.Plan
	// PlanError is the run-level finding a corrupt plan produces ("" when
	// the plan is present or absent).
	PlanError string
}

// Snapshot captures the manifest and plan a turn's verification runs will
// use (see the Snapshot type for what is frozen and why). It takes a
// snapshot-then-run contract: call it once at the turn's start and pass the
// result to RunSnapshot for every verification run of the turn.
func (r *Runner) Snapshot(root string) *Snapshot {
	snap := &Snapshot{}
	snap.Manifest, snap.ManifestError = r.manifestLoad(root)
	snap.Plan, snap.PlanError = r.planLoad(root)
	return snap
}

// RunSnapshot executes the verification checks against the frozen inputs a
// turn captured with Snapshot: the manifest's commands and
// the plan's acceptance come from the snapshot, never from a re-read of the
// files on disk. It is the implementation Run delegates to after taking a
// fresh snapshot; the two behave identically for a snapshot captured
// immediately before the run.
//
// Like Run, it returns a non-nil error only for setup problems (no executor,
// empty root, nil runner, or a nil snapshot). Run-level findings (a corrupt
// manifest or plan, an executor failure) are recorded on the result, never
// swallowed; the snapshot's stored load findings are recorded exactly as Run
// would have recorded them at load time.
func (r *Runner) RunSnapshot(ctx context.Context, root string, snap *Snapshot) (*Result, error) {
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
	if snap == nil {
		return nil, errors.New("verify: snapshot is required (call Snapshot first)")
	}

	result := &Result{Checks: []Check{}}

	// The snapshot's load findings stand in for a mid-run load: manifest
	// first, then plan, then the configuration source (resolveCommands) —
	// the same order a live load records them.
	if snap.ManifestError != "" {
		result.Errors = append(result.Errors, snap.ManifestError)
	}
	if snap.PlanError != "" {
		result.Errors = append(result.Errors, snap.PlanError)
	}
	manifest := snap.Manifest
	plan := snap.Plan
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
		case plancontract.KindInteraction:
			r.runInteractionCheck(ctx, root, &result.Checks[i], manifest)
		case plancontract.KindManual:
			// Pre-filled skipped by planChecks; never executed and never
			// gates the result (manual is listed, not gated).
		default:
			r.runCheck(ctx, root, &result.Checks[i])
		}
	}

	// The require-a-test-for-new-behavior check is appended after the
	// executed checks, so a failing check is fed back through the same repair
	// loop and report as the build/test checks. It is off by default and adds
	// no check when it is satisfied or not applicable.
	if check := RequireTestCheck(r.RequireTest); check != nil {
		result.Checks = append(result.Checks, *check)
	}
	return result, nil
}

// manifestLoad loads the project's starter manifest and returns it with the
// run-level finding a corrupt manifest produces ("" when the manifest is
// present or missing). Missing is the normal "no starter" case, not a
// finding; a corrupt manifest discards its (invalid) commands and records
// the finding so the run proceeds with the configuration source.
func (r *Runner) manifestLoad(root string) (*startermanifest.StarterManifest, string) {
	if r.Manifest == nil {
		return nil, ""
	}
	manifest, err := r.Manifest(root)
	switch {
	case err == nil:
		return manifest, ""
	case errors.Is(err, starterstore.ErrNoManifest):
		return nil, ""
	default:
		return nil, "starter manifest: " + err.Error()
	}
}

// planLoad loads the active plan and returns it with the run-level finding a
// corrupt plan produces ("" when the plan is present or missing). Missing is
// the baseline case, not a finding; a corrupt plan discards its (invalid)
// acceptance and records the finding so the run proceeds as a baseline.
func (r *Runner) planLoad(root string) (*plancontract.Plan, string) {
	if r.Plans == nil {
		return nil, ""
	}
	plan, err := r.Plans(root)
	switch {
	case err == nil:
		return plan, ""
	case errors.Is(err, planstore.ErrNoPlan):
		return nil, ""
	default:
		return nil, "plan: " + err.Error()
	}
}

// resolveCommands returns the trusted commands for this project:
// per kind, the starter manifest's command wins where it is set,
// the explicit project configuration fills the gap, and nothing else is
// ever consulted. A manifest that exists but is invalid was already
// recorded as a run-level finding by the manifest load, so its (discarded)
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

// planChecks builds the checks an active plan gates: one check per
// build, test, and page item group the plan declares (build and test, and
// page — one check per kind, each covering every acceptance
// item of that kind), one check per interaction item (each is an
// independent scripted flow with its own steps and expected outcome), and a
// single pre-filled skipped manual check listing every manual item (listed,
// never gated). A kind the plan declares but no trusted command
// exists for still gets its check — skipped, with a reason — so the result
// says plainly what could not be verified. The page check carries the
// manifest's dev command and its routes come only from the manifest;
// each interaction check carries the manifest's dev command and its
// steps come only from the plan's interaction item. The plan's acceptance
// Check fields are never read for a command, route, or step.
func (r *Runner) planChecks(plan *plancontract.Plan, manifest *startermanifest.StarterManifest, buildCmd, testCmd string) []Check {
	var buildItems, testItems, pageItems, manualItems []string
	var interactionChecks []Check
	for _, a := range plan.Acceptance {
		switch a.Kind {
		case plancontract.KindBuild:
			buildItems = append(buildItems, a.ID)
		case plancontract.KindTest:
			testItems = append(testItems, a.ID)
		case plancontract.KindPage:
			pageItems = append(pageItems, a.ID)
		case plancontract.KindInteraction:
			interactionChecks = append(interactionChecks, Check{
				Kind:    plancontract.KindInteraction,
				Items:   []string{a.ID},
				Steps:   a.Steps,
				Command: manifestDev(manifest),
			})
		case plancontract.KindManual:
			manualItems = append(manualItems, a.ID)
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
	// One check per interaction item, in plan order.
	checks = append(checks, interactionChecks...)
	// A single manual check listing every manual item, pre-filled skipped:
	// manual verification is human, never machine-gated.
	if len(manualItems) > 0 {
		checks = append(checks, Check{
			Kind:    plancontract.KindManual,
			Items:   manualItems,
			Skipped: true,
			Reason:  "manual: verified by a human, not machine-gated",
		})
	}
	return checks
}

// runCheck executes one check and fills in its outcome. A check with no
// trusted command is skipped, not failed: there is nothing to verify,
// and the runner never invents a command.
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
