package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// manifestFreezeFixture declares build, test, and a dev server (command,
// port, and routes) so the snapshot tests can freeze every manifest field a
// check consumes. The build and test commands run through the fake executor
// (no real build), and the page check is forced to skip (no browser) so no
// dev server is started — the dev command is still visible on the page
// check's Command field, which is what the freeze assertion reads.
const manifestFreezeFixture = `{
  "starter": {"id": "web-app", "version": "1.0.0"},
  "build": "make build",
  "test": "make test",
  "dev": "make dev",
  "dev_port": 4321,
  "routes": ["/"]
}`

// planFreezeFixture returns a valid plan with a build, test, and page item
// (one per gated kind) plus a manual item, so a snapshot captures a full
// acceptance set to freeze.
func planFreezeFixture(t *testing.T) *plancontract.Plan {
	t.Helper()
	p := newPlan(t)
	p.Acceptance = []plancontract.Acceptance{
		{ID: "a1", Scope: "s1", Kind: plancontract.KindBuild},
		{ID: "a2", Scope: "s1", Kind: plancontract.KindTest},
		{ID: "a3", Scope: "s1", Kind: plancontract.KindPage},
		{ID: "a4", Scope: "s1", Kind: plancontract.KindManual},
	}
	return p
}

// snapshotRunner builds a Runner wired for snapshot tests: the fake executor
// records command execution, and a nil browser forces page and interaction
// checks to skip (no dev server, no real browser).
func snapshotRunner(exec *fakeExecutor) *Runner {
	r := New()
	r.Exec = exec
	r.Browser = nil
	r.StepBrowser = nil
	return r
}

// TestSnapshotFreezesManifestAndPlan is the pkg/verify anchor for the
// snapshot contract: a Snapshot captured before the files change on disk
// keeps the original manifest commands (build, test, dev) and the original plan
// acceptance, and RunSnapshot executes against the snapshot — never the
// tampered on-disk files.
func TestSnapshotFreezesManifestAndPlan(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFreezeFixture)
	original := writePlanFile(t, root, planFreezeFixture(t))

	snap := New().Snapshot(root)

	// The snapshot holds the original manifest (all fields) and the original
	// plan (revision + acceptance).
	require.NotNil(t, snap.Manifest)
	assert.Equal(t, "make build", snap.Manifest.Build)
	assert.Equal(t, "make test", snap.Manifest.Test)
	assert.Equal(t, "make dev", snap.Manifest.Dev)
	assert.Equal(t, []string{"/"}, snap.Manifest.Routes)
	require.NotNil(t, snap.Plan)
	assert.Equal(t, original.Revision, snap.Plan.Revision)
	require.Len(t, snap.Plan.Acceptance, 4)

	// Tamper both files on disk after the snapshot.
	require.NoError(t, os.WriteFile(starterstore.StarterManifestPath(root),
		[]byte(`{"starter":{"id":"web-app","version":"1.0.0"},"build":"true","test":"true","dev":"make tampered","dev_port":9999,"routes":["/x"]}`), 0o644))
	tamperedPlan := planFreezeFixture(t)
	tamperedPlan.Acceptance = []plancontract.Acceptance{{ID: "a1", Scope: "s1", Kind: plancontract.KindBuild}}
	writePlanFile(t, root, tamperedPlan)

	// Prove the on-disk files actually changed (so the freeze below is
	// meaningful).
	tamperedBytes, err := os.ReadFile(starterstore.StarterManifestPath(root))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(tamperedBytes), "make tampered"), "the on-disk manifest must be tampered for the test to be meaningful")

	exec := &fakeExecutor{}
	res, err := snapshotRunner(exec).RunSnapshot(context.Background(), root, snap)
	require.NoError(t, err)

	// The run used the snapshot's manifest commands, not the tampered ones.
	assert.Equal(t, []string{"make build", "make test"}, exec.executed,
		"RunSnapshot must execute the snapshot's build/test commands, not the tampered on-disk commands")
	assert.False(t, res.Baseline, "the plan was present at snapshot time: no baseline")
	assert.Equal(t, original.Revision, res.PlanRevision, "the plan revision is the snapshot's, not the tampered plan's")

	// Every check is built from the snapshot's manifest + acceptance: build
	// and test run the original commands, the page check carries the original
	// dev command (skipped here because no browser), and the manual item is
	// listed. The tampered plan (only a1) and manifest ("true"/"make
	// tampered") are never consulted.
	require.Len(t, res.Checks, 4)
	assert.Equal(t, plancontract.KindBuild, res.Checks[0].Kind)
	assert.Equal(t, "make build", res.Checks[0].Command)
	assert.Equal(t, []string{"a1"}, res.Checks[0].Items)
	assert.Equal(t, plancontract.KindTest, res.Checks[1].Kind)
	assert.Equal(t, "make test", res.Checks[1].Command)
	assert.Equal(t, []string{"a2"}, res.Checks[1].Items)
	assert.Equal(t, plancontract.KindPage, res.Checks[2].Kind)
	assert.Equal(t, "make dev", res.Checks[2].Command, "the page check must carry the snapshot's dev command, not the tampered one")
	assert.Equal(t, []string{"a3"}, res.Checks[2].Items)
	assert.True(t, res.Checks[2].Skipped, "no browser: the page check skips, but its Command is still the snapshot's dev command")
	assert.Equal(t, plancontract.KindManual, res.Checks[3].Kind)
	assert.Equal(t, []string{"a4"}, res.Checks[3].Items)
}

// TestSnapshotNoPlanFreezesBaseline pins the plan-presence half of the
// freeze: with no plan at snapshot time the run is a baseline for the whole
// turn, and a manifest edit between rounds changes nothing — every
// RunSnapshot round uses the snapshot's commands and stays a baseline.
func TestSnapshotNoPlanFreezesBaseline(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)

	// No plan on disk → the snapshot records a nil plan.
	snap := New().Snapshot(root)
	assert.Nil(t, snap.Plan, "no plan at snapshot time")
	assert.Empty(t, snap.PlanError)

	// Round 1: a baseline build+test run with the manifest's commands.
	exec1 := &fakeExecutor{}
	res1, err := snapshotRunner(exec1).RunSnapshot(context.Background(), root, snap)
	require.NoError(t, err)
	assert.True(t, res1.Baseline, "no plan at snapshot time: round 1 is a baseline")
	assert.Equal(t, []string{"make build", "make test"}, exec1.executed)

	// Tamper the manifest on disk between rounds: rewrite both commands.
	require.NoError(t, os.WriteFile(starterstore.StarterManifestPath(root),
		[]byte(`{"starter":{"id":"web-app","version":"1.0.0"},"build":"true","test":"true"}`), 0o644))

	// Round 2 (a fresh runner, same snapshot): still a baseline, still the
	// snapshot's original commands — the on-disk edit is never re-read.
	exec2 := &fakeExecutor{}
	res2, err := snapshotRunner(exec2).RunSnapshot(context.Background(), root, snap)
	require.NoError(t, err)
	assert.True(t, res2.Baseline, "round 2 is still a baseline (the plan's absence is frozen)")
	assert.Equal(t, []string{"make build", "make test"}, exec2.executed,
		"round 2 must reuse the snapshot's commands, not the tampered on-disk ones")
	require.Len(t, res2.Checks, 2)
	assert.Equal(t, "make build", res2.Checks[0].Command)
	assert.Equal(t, "make test", res2.Checks[1].Command)
}

// TestSnapshotPlanCorruptionStillGates pins the plan-side defense: a plan
// present at snapshot time gates its checks for the whole turn even if the
// plan file is corrupted or deleted mid-turn — RunSnapshot runs the plan's
// checks (not a baseline) with no plan error, because the snapshot holds the
// original valid plan.
func TestSnapshotPlanCorruptionStillGates(t *testing.T) {
	root := t.TempDir()
	writeManifestFile(t, root, manifestFull)
	original := writePlanFile(t, root, newPlan(t))

	snap := New().Snapshot(root)
	require.NotNil(t, snap.Plan, "a plan is present at snapshot time")

	// Corrupt the plan file on disk after the snapshot.
	planPath := filepath.Join(root, ".sprout", "plan.json")
	require.NoError(t, os.WriteFile(planPath, []byte(`{ not valid json`), 0o644))

	exec := &fakeExecutor{}
	res, err := snapshotRunner(exec).RunSnapshot(context.Background(), root, snap)
	require.NoError(t, err)

	assert.False(t, res.Baseline, "the plan was present at snapshot time: still plan mode, not a baseline")
	assert.Equal(t, original.Revision, res.PlanRevision)
	assert.Empty(t, res.Errors, "the snapshot captured a valid plan, so no plan error is recorded")
	// The original acceptance (a1 build, a2 test) still gates.
	require.Len(t, res.Checks, 2)
	assert.Equal(t, plancontract.KindBuild, res.Checks[0].Kind)
	assert.Equal(t, []string{"a1"}, res.Checks[0].Items)
	assert.Equal(t, plancontract.KindTest, res.Checks[1].Kind)
	assert.Equal(t, []string{"a2"}, res.Checks[1].Items)
}

// TestRunSnapshotNilSnapshotErrors pins the setup contract: a nil snapshot is
// a setup problem (call Snapshot first), not a silent baseline.
func TestRunSnapshotNilSnapshotErrors(t *testing.T) {
	root := t.TempDir()
	r := New()
	r.Exec = &fakeExecutor{}
	_, err := r.RunSnapshot(context.Background(), root, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot is required")
}
