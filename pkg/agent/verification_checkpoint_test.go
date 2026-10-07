//go:build !js

// verification_checkpoint_test.go — the automatic checkpoint seam on the
// turn-end verification hook: a verification run that passed captures a
// checkpoint of the state the turn left behind. The seam itself
// (CaptureVerificationCheckpoint) is pure enough to pin without a shell: it
// gates on Result.Passed() and delegates the store write through the
// package-level captureCheckpoint hook.

package agent

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// vcPassingResult is a minimal passing run: one executed build check that
// passed.
func vcPassingResult() *verify.Result {
	return &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "make build", Passed: true},
		},
	}
}

// TestCaptureVerificationCheckpoint_PassingCaptures pins the seam: a passing
// result calls the capture hook once, with the agent's workspace.
func TestCaptureVerificationCheckpoint_PassingCaptures(t *testing.T) {
	root := t.TempDir()
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	ag, err := NewAgentWithClient(NewScriptedClient(), "test", mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(ag.Shutdown)
	ag.SetWorkspaceRoot(root)

	var capturedWorkspace string
	prev := captureCheckpoint
	captureCheckpoint = func(workspace, revisionID string, res *verify.Result) error {
		capturedWorkspace = workspace
		if res == nil || !res.Passed() {
			t.Errorf("capture hook called with a non-passing result: %+v", res)
		}
		return nil
	}
	t.Cleanup(func() { captureCheckpoint = prev })

	ag.CaptureVerificationCheckpoint(vcPassingResult())
	if capturedWorkspace != root {
		t.Errorf("captured workspace = %q, want %q", capturedWorkspace, root)
	}
}

// TestCaptureVerificationCheckpoint_NotPassingIsNoOp asserts a nil or
// non-passing result captures nothing.
func TestCaptureVerificationCheckpoint_NotPassingIsNoOp(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	ag, err := NewAgentWithClient(NewScriptedClient(), "test", mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(ag.Shutdown)

	calls := 0
	prev := captureCheckpoint
	captureCheckpoint = func(workspace, revisionID string, res *verify.Result) error {
		calls++
		return nil
	}
	t.Cleanup(func() { captureCheckpoint = prev })

	ag.CaptureVerificationCheckpoint(nil)
	// An all-skipped run verified nothing — not a pass.
	skipped := &verify.Result{Checks: []verify.Check{{Kind: plancontract.KindBuild, Skipped: true}}}
	ag.CaptureVerificationCheckpoint(skipped)
	// A failed run.
	failed := &verify.Result{Checks: []verify.Check{{Kind: plancontract.KindBuild}}}
	ag.CaptureVerificationCheckpoint(failed)
	if calls != 0 {
		t.Errorf("capture hook calls = %d, want 0 (nothing passed)", calls)
	}
}

// TestCaptureVerificationCheckpoint_StoreFailureIsSwallowed asserts a store
// failure is logged and swallowed — a passing run stands.
func TestCaptureVerificationCheckpoint_StoreFailureIsSwallowed(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	ag, err := NewAgentWithClient(NewScriptedClient(), "test", mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(ag.Shutdown)

	prev := captureCheckpoint
	captureCheckpoint = func(workspace, revisionID string, res *verify.Result) error {
		return errors.New("store unavailable")
	}
	t.Cleanup(func() { captureCheckpoint = prev })

	// Must not panic; the error is swallowed (logged at debug).
	ag.CaptureVerificationCheckpoint(vcPassingResult())
}

// TestCaptureCheckpointForWorkspace_WritesStore pins the production capture:
// a checkpoint lands in the workspace's checkpoint store, capturing the
// current revision.
func TestCaptureCheckpointForWorkspace_WritesStore(t *testing.T) {
	root := t.TempDir()

	prevC, prevR := history.GetPathsForTesting()
	prevCp := history.GetCheckpointsDir()
	tmp := t.TempDir()
	history.SetPathsForTesting(filepath.Join(tmp, "changes"), filepath.Join(tmp, "revisions"))
	history.SetCheckpointsDirForTesting(filepath.Join(tmp, "checkpoints"))
	t.Cleanup(func() {
		history.SetPathsForTesting(prevC, prevR)
		history.SetCheckpointsDirForTesting(prevCp)
	})

	if _, err := history.RecordBaseRevision("rev-1", "prompt", "resp", nil); err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}
	if err := history.RecordChangeWithDetails("rev-1", "a.go", "x\n", "x\ny\n", "edit", "", "", "", "m"); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	if err := captureCheckpointForWorkspace(root, "rev-1", vcPassingResult()); err != nil {
		t.Fatalf("captureCheckpointForWorkspace: %v", err)
	}

	// The production capture writes under the workspace, not the
	// process-wide store.
	prevStore := history.GetCheckpointsDir()
	history.SetCheckpointsDirForTesting(filepath.Join(root, ".sprout", "checkpoints"))
	t.Cleanup(func() { history.SetCheckpointsDirForTesting(prevStore) })

	cps, err := history.ListCheckpoints(false)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 1 {
		t.Fatalf("got %d checkpoints, want 1", len(cps))
	}
	if cps[0].Origin != history.CheckpointVerification {
		t.Errorf("origin = %q, want verification", cps[0].Origin)
	}
	if cps[0].RevisionID != "rev-1" {
		t.Errorf("revision = %q, want rev-1", cps[0].RevisionID)
	}
}

// TestCaptureCheckpointForWorkspace_UsesGivenRevisionNotMostRecent pins the
// rule that the automatic capture identifies the revision it was given, not
// whichever revision happens to be most recent in the history store. The
// turn knows the revision it produced; capturing the store's head instead
// would point the restore marker at the wrong state.
func TestCaptureCheckpointForWorkspace_UsesGivenRevisionNotMostRecent(t *testing.T) {
	root := t.TempDir()

	prevC, prevR := history.GetPathsForTesting()
	prevCp := history.GetCheckpointsDir()
	tmp := t.TempDir()
	history.SetPathsForTesting(filepath.Join(tmp, "changes"), filepath.Join(tmp, "revisions"))
	history.SetCheckpointsDirForTesting(filepath.Join(tmp, "checkpoints"))
	t.Cleanup(func() {
		history.SetPathsForTesting(prevC, prevR)
		history.SetCheckpointsDirForTesting(prevCp)
	})

	// Two revisions: an older one the turn produced, and a newer unrelated
	// one that is the store's head. The capture must name the older one.
	if _, err := history.RecordBaseRevision("rev-old", "prompt", "resp", nil); err != nil {
		t.Fatalf("RecordBaseRevision(rev-old): %v", err)
	}
	if err := history.RecordChangeWithDetails("rev-old", "old.go", "a\n", "a\nb\n", "edit", "", "", "", "m"); err != nil {
		t.Fatalf("RecordChangeWithDetails(rev-old): %v", err)
	}
	if _, err := history.RecordBaseRevision("rev-new", "prompt", "resp", nil); err != nil {
		t.Fatalf("RecordBaseRevision(rev-new): %v", err)
	}
	if err := history.RecordChangeWithDetails("rev-new", "new.go", "c\n", "c\nd\n", "edit", "", "", "", "m"); err != nil {
		t.Fatalf("RecordChangeWithDetails(rev-new): %v", err)
	}

	// The turn produced rev-old; capture names exactly that.
	if err := captureCheckpointForWorkspace(root, "rev-old", vcPassingResult()); err != nil {
		t.Fatalf("captureCheckpointForWorkspace: %v", err)
	}

	prevStore := history.GetCheckpointsDir()
	history.SetCheckpointsDirForTesting(filepath.Join(root, ".sprout", "checkpoints"))
	t.Cleanup(func() { history.SetCheckpointsDirForTesting(prevStore) })

	cps, err := history.ListCheckpoints(false)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 1 {
		t.Fatalf("got %d checkpoints, want 1", len(cps))
	}
	if cps[0].RevisionID != "rev-old" {
		t.Errorf("revision = %q, want rev-old (the given revision, not the store head rev-new)", cps[0].RevisionID)
	}
}
