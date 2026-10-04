package verify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// TestRunPlanWithConfigCommands covers plan mode fed by the explicit
// configuration source: the configuration commands run, the plan items
// are attached, and the plan's model-proposed Check fields stay inert.
func TestRunPlanWithConfigCommands(t *testing.T) {
	root := t.TempDir()
	stored := writePlanFile(t, root, newPlan(t))

	exec := &fakeExecutor{}
	r := New()
	r.Exec = exec
	r.ConfigCommands = func(string) (Commands, error) {
		return Commands{Build: "make ci-build", Test: "make ci-test"}, nil
	}

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	assert.Equal(t, []string{"make ci-build", "make ci-test"}, exec.executed)
	assert.False(t, res.Baseline)
	assert.Equal(t, stored.Revision, res.PlanRevision)
	assert.Equal(t, []string{"a1"}, res.Checks[0].Items)
	assert.Equal(t, []string{"a2"}, res.Checks[1].Items)
	assert.True(t, res.Passed())
}

// TestRunPlanWithoutBuildOrTestItems pins the 149.3 scope: a plan that
// declares only a page item (plus a manual item) gates a single page
// check. The manifestFull manifest has no dev command, so the page check
// is skipped with a reason (honest failure, SP-149 §149d) rather than
// invented, and the manual item still gates nothing — no manual check is
// produced.
func TestRunPlanWithoutBuildOrTestItems(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)
	p := newPlan(t)
	p.Acceptance = []plancontract.Acceptance{
		{ID: "a1", Scope: "s1", Check: "/login renders", Kind: plancontract.KindPage},
		{ID: "a2", Scope: "s1", Check: "user confirms login", Kind: plancontract.KindManual},
	}
	stored := writePlanFile(t, root, p)

	exec := &fakeExecutor{}
	r := New()
	r.Exec = exec

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	assert.Empty(t, exec.executed, "a plan with no build/test items gates no command execution")
	require.Len(t, res.Checks, 1, "149.3 adds exactly one page check for the plan's page items")
	assert.Equal(t, plancontract.KindPage, res.Checks[0].Kind)
	assert.True(t, res.Checks[0].Skipped, "no dev command in the manifest: the page check is skipped, not invented")
	assert.Contains(t, res.Checks[0].Reason, "dev command")
	assert.Equal(t, []string{"a1"}, res.Checks[0].Items)
	// The manual item gates nothing: the single check is the page one, no
	// manual check exists.
	assert.False(t, res.Baseline, "a plan exists: no baseline")
	assert.Equal(t, stored.Revision, res.PlanRevision)
}

// TestRunCorruptManifestRecordsErrorAndFallsBack pins the starterstore
// contract ("an invalid manifest is a hard error, never a guessed
// one"): the manifest's commands are discarded, the problem is recorded
// on the result, and the configuration source stands in.
func TestRunCorruptManifestRecordsErrorAndFallsBack(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, `{"starter": {"id": "web-app"}, "build": "make build"}`)

	exec := &fakeExecutor{}
	r := New()
	r.Exec = exec
	r.ConfigCommands = func(string) (Commands, error) {
		return Commands{Build: "config build", Test: "config test"}, nil
	}

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	require.NotEmpty(t, res.Errors, "an invalid manifest is a run-level error")
	assert.Equal(t, []string{"config build", "config test"}, exec.executed,
		"the invalid manifest's commands must not be used")
	assert.True(t, res.Failed(), "a run with recorded errors is not a pass")
}

// TestRunCorruptPlanRunsBaselineAndRecordsError pins the planstore
// contract for verification: an unreadable plan means no usable plan,
// so the baseline runs and the problem is recorded.
func TestRunCorruptPlanRunsBaselineAndRecordsError(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".sprout"), 0o755))
	require.NoError(t, os.WriteFile(planstore.PlanJSONPath(root), []byte(`{"version": 99, "revision": 1}`), 0o644))

	exec := &fakeExecutor{}
	r := New()
	r.Exec = exec

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	assert.True(t, res.Baseline, "an unreadable plan means no usable plan: run the baseline")
	assert.NotEmpty(t, res.Errors)
	assert.Equal(t, []string{"make build", "make test"}, exec.executed)
	assert.True(t, res.Failed())
}

// TestRunExecutorErrorFailsCheck pins the executor-failure path: a
// launch failure fails the check (with the error as its reason) without
// stopping the remaining checks.
func TestRunExecutorErrorFailsCheck(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)

	exec := &fakeExecutor{errs: map[string]error{"make build": errors.New("shell not found")}}
	r := New()
	r.Exec = exec

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	assert.False(t, res.Checks[0].Passed)
	assert.Contains(t, res.Checks[0].Reason, "shell not found")
	assert.True(t, res.Checks[1].Passed, "a failure in one check must not skip the rest")
	assert.True(t, res.Failed())
	assert.False(t, res.Passed())
}

// TestRunFailingCheckCapturesExcerpt pins the evidence contract: a
// normal (non-zero-exit) failure carries its output in the excerpt.
func TestRunFailingCheckCapturesExcerpt(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)

	exec := &fakeExecutor{results: map[string]Outcome{
		"make test": {Passed: false, Output: "FAIL\ngit: fatal: not a repository\n"},
	}}
	r := New()
	r.Exec = exec

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	assert.False(t, res.Checks[1].Passed)
	assert.Empty(t, res.Checks[1].Reason, "a normal failure carries its evidence in the excerpt")
	assert.Contains(t, res.Checks[1].Excerpt, "git: fatal")
	assert.True(t, res.Failed())
}

// TestRunCancelledContextSkipsChecks pins the cancellation path: a
// cancelled run executes nothing and reports every check skipped.
func TestRunCancelledContextSkipsChecks(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)

	exec := &fakeExecutor{}
	r := New()
	r.Exec = exec

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := r.Run(ctx, root)
	require.NoError(t, err)

	assert.Empty(t, exec.executed, "a cancelled run executes nothing")
	require.Len(t, res.Checks, 2)
	for _, c := range res.Checks {
		assert.True(t, c.Skipped)
		assert.Contains(t, c.Reason, "cancelled")
	}
}

// TestRunRequiresRootAndExecutor pins the setup-failure contract: Run
// returns an error (not a result) when the root is empty or no executor
// is configured.
func TestRunRequiresRootAndExecutor(t *testing.T) {
	r := New()
	r.Exec = &fakeExecutor{}
	_, err := r.Run(context.Background(), "")
	require.Error(t, err, "an empty root must be rejected")

	zero := Runner{}
	_, err = zero.Run(context.Background(), t.TempDir())
	require.Error(t, err, "a zero Runner has no executor and must not run")

	var nilRunner *Runner
	_, err = nilRunner.Run(context.Background(), t.TempDir())
	require.Error(t, err, "a nil runner must not run")
}

// TestNewDefaults pins the production wiring: the starter manifest and
// plan loaders, the shell executor, and the default bounds.
func TestNewDefaults(t *testing.T) {
	r := New()
	require.NotNil(t, r.Manifest)
	require.NotNil(t, r.Plans)
	require.NotNil(t, r.Exec)
	assert.Equal(t, DefaultTimeout, r.Timeout)
	assert.Equal(t, DefaultMaxExcerptBytes, r.MaxExcerptBytes)
}

// TestResultJSONRoundTrip pins the "plain data for SP-151" contract: a
// Result marshals and unmarshals losslessly.
func TestResultJSONRoundTrip(t *testing.T) {
	res := &Result{
		Baseline: true,
		Checks: []Check{
			{Kind: plancontract.KindBuild, Command: "make build", Passed: true, Duration: 123 * time.Millisecond},
			{Kind: plancontract.KindTest, Skipped: true, Reason: "no test command configured"},
		},
		Errors: []string{"plan: invalid"},
	}
	data, err := json.Marshal(res)
	require.NoError(t, err)

	var out Result
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, *res, out, "a Result must round-trip as plain data")
	assert.True(t, out.Failed())
}

// TestResultSummary pins the deterministic summary rendering used by the
// final reply (SP-149 §149d).
func TestResultSummary(t *testing.T) {
	baseline := &Result{
		Baseline: true,
		Checks: []Check{
			{Kind: plancontract.KindBuild, Command: "make build", Passed: true},
			{Kind: plancontract.KindTest, Skipped: true, Reason: "no test command configured"},
		},
	}
	assert.Equal(t,
		"baseline: build: passed (make build); test: skipped — no test command configured",
		baseline.Summary())

	planned := &Result{
		PlanRevision: 4,
		Checks: []Check{
			{Kind: plancontract.KindBuild, Items: []string{"a1"}, Command: "make build", Passed: false, Reason: "timed out"},
		},
		Errors: []string{"plan: stale"},
	}
	assert.Equal(t,
		"plan rev 4: build: failed (make build) — timed out; plan: stale",
		planned.Summary())

	assert.Equal(t, "plan rev 0: ", (&Result{}).Summary())
	assert.Equal(t, "no verification result", (*Result)(nil).Summary())
}
