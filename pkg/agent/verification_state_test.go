//go:build !js

// verification_state_test.go — the accessor tests
// for the turn-end hook's stored per-turn verification state
// (Agent.LastTurnVerification): the nil contract (a nil receiver and a
// fresh agent with no completed turn), the stored state of a scripted
// failing-verification turn (result, per-check attempts, limit, and the
// new repair-round count), the attempts-map copy contract, the passing
// mirror case, and the unchanged LastVerificationResult behavior. The
// fixture mirrors verification_hook_test.go (scripted model, real
// workspace, real shell execution of the manifest's build command).

package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestLastTurnVerification_NilAndFreshAgent pins the nil contract: a
// nil receiver and a fresh agent (no completed turn, the hook never
// ran) both report nil — and LastVerificationResult keeps reporting
// nil alongside it (no behavior change).
func TestLastTurnVerification_NilAndFreshAgent(t *testing.T) {
	// The nil-receiver contract is the new accessor's (LastTurnVerification);
	// LastVerificationResult keeps its exact prior behavior (no nil
	// handling), so it is not exercised on a nil receiver here.
	var nilAgent *Agent
	if tv := nilAgent.LastTurnVerification(); tv != nil {
		t.Errorf("nil receiver: LastTurnVerification = %+v, want nil", tv)
	}

	ag := NewTestAgent()
	t.Cleanup(ag.Shutdown)
	if tv := ag.LastTurnVerification(); tv != nil {
		t.Errorf("fresh agent: LastTurnVerification = %+v, want nil (the hook never ran)", tv)
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("fresh agent: LastVerificationResult = %+v, want nil", res)
	}
}

// TestLastTurnVerification_FailingTurnStoresState pins the stored state
// of a scripted failing-verification turn (the shared fixture pattern,
// N=1): the last (still failing) verification run, the per-check repair
// attempts consumed against the stopping rule, the configured limit,
// and the one repair round the hook ran. The returned Attempts map is
// a copy (mutating it never touches the agent's stored state), and
// LastVerificationResult keeps returning the same stored result.
func TestLastTurnVerification_FailingTurnStoresState(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse("I made the change."),
		NewScriptedTextResponse("The build still fails; the remaining failure is fixture-broken-build."),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1})

	if _, err := ag.ProcessQuery("Implement the app entry point."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	tv := ag.LastTurnVerification()
	if tv == nil {
		t.Fatal("LastTurnVerification = nil, want the stored state (the hook ran)")
	}
	if tv.Result == nil || !tv.Result.Failed() {
		t.Errorf("stored Result = %+v, want the last failing verification run", tv.Result)
	}
	if len(tv.Attempts) != 1 || tv.Attempts["build"] != 1 {
		t.Errorf("stored Attempts = %v, want {build: 1} (one repair round consumed the N=1 budget)", tv.Attempts)
	}
	if tv.Limit != 1 {
		t.Errorf("stored Limit = %d, want 1 (the configured repair limit)", tv.Limit)
	}
	if tv.Rounds != 1 {
		t.Errorf("stored Rounds = %d, want 1 (the report was fed back once)", tv.Rounds)
	}

	// The Attempts map is a copy: mutating it must not touch the
	// agent's stored state.
	tv.Attempts["build"] = 99
	tv2 := ag.LastTurnVerification()
	if tv2 == nil {
		t.Fatal("re-read LastTurnVerification = nil, want the stored state")
	}
	if tv2.Attempts["build"] != 1 {
		t.Errorf("after mutating the returned map, re-read Attempts = %v, want {build: 1} (a copy, not the stored state)", tv2.Attempts)
	}

	// LastVerificationResult keeps its exact behavior (the preceding
	// tests depend on it): the same stored result.
	if res := ag.LastVerificationResult(); res != tv.Result {
		t.Errorf("LastVerificationResult = %+v, want the same result the state carries (%+v)", res, tv.Result)
	}
}

// TestLastTurnVerification_PassingTurnStoresZeroRounds pins the mirror
// case: a passing verification run stores the state with no repair
// rounds and no per-check attempts — and the configured limit is
// stored on a passing run too.
func TestLastTurnVerification_PassingTurnStoresZeroRounds(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-build-ok")

	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse("Done, the build passes."),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	if _, err := ag.ProcessQuery("Implement the app entry point."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	tv := ag.LastTurnVerification()
	if tv == nil {
		t.Fatal("LastTurnVerification = nil, want the stored state (the hook ran)")
	}
	if tv.Result == nil || !tv.Result.Passed() {
		t.Errorf("stored Result = %+v, want the passing verification run", tv.Result)
	}
	if len(tv.Attempts) != 0 {
		t.Errorf("stored Attempts = %v, want empty (no repair round ran)", tv.Attempts)
	}
	if tv.Limit != 2 {
		t.Errorf("stored Limit = %d, want 2 (the configured limit, stored on a passing run too)", tv.Limit)
	}
	if tv.Rounds != 0 {
		t.Errorf("stored Rounds = %d, want 0 (a passing run needs no repair)", tv.Rounds)
	}
}

// TestNotVerifiedReason_ExportedAccessor pins the exported
// Agent.NotVerifiedReason accessor: it reports the same reason as the
// unexported event builder for every branch (nil receiver, disabled
// verification, enabled with no code change, enabled with a code
// change), so a consumer outside pkg/agent — the benchmark report —
// records exactly what the turn-completion event says.
func TestNotVerifiedReason_ExportedAccessor(t *testing.T) {
	var nilAgent *Agent
	if got := nilAgent.NotVerifiedReason(); got != "" {
		t.Errorf("nil receiver = %q, want \"\" (no config, verification disabled)", got)
	}

	// No configuration manager: verification disabled.
	if got := NewTestAgent().NotVerifiedReason(); got != "" {
		t.Errorf("no config manager = %q, want \"\"", got)
	}

	// Verification enabled, no code change.
	mgrEnabled, cleanupEnabled := configuration.NewTestManager(t)
	t.Cleanup(cleanupEnabled)
	if err := mgrEnabled.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Verification = &configuration.VerificationConfig{Enabled: true}
		return nil
	}); err != nil {
		t.Fatalf("configure verification: %v", err)
	}
	agNoChanges := NewTestAgentWithConfigManager(mgrEnabled)
	if got := agNoChanges.NotVerifiedReason(); got != "no code changes this turn" {
		t.Errorf("enabled + no change = %q, want %q", got, "no code changes this turn")
	}

	// Verification enabled and the turn changed code, no setup error: the
	// hook still did not run.
	tracker := NewChangeTracker(nil, "nvr-exported")
	tracker.MarkTurnStart()
	if err := tracker.TrackFileWriteState("/ws/app.go", "old", "new", true); err != nil {
		t.Fatalf("TrackFileWriteState: %v", err)
	}
	agChanged := NewTestAgentWithConfigManager(mgrEnabled)
	agChanged.changeTracker = tracker
	if got := agChanged.NotVerifiedReason(); got != "verification did not run this turn" {
		t.Errorf("enabled + change = %q, want %q", got, "verification did not run this turn")
	}

	// The exported accessor and the event builder agree.
	if got, want := agChanged.NotVerifiedReason(), agChanged.notVerifiedReason(); got != want {
		t.Errorf("NotVerifiedReason = %q, notVerifiedReason = %q (must agree)", got, want)
	}
}

// TestNotVerifiedReason_SetupErrorBranch pins that a hook setup error is
// distinguishable from "no code changes": with verification enabled and
// the turn window empty, the marker flips the reason from the no-code
// branch to "verification setup error" — the hook entered and failed to
// set up, which is a different remedy than "write code".
func TestNotVerifiedReason_SetupErrorBranch(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Verification = &configuration.VerificationConfig{Enabled: true}
		return nil
	}); err != nil {
		t.Fatalf("configure verification: %v", err)
	}

	ag := NewTestAgentWithConfigManager(mgr)
	if got := ag.NotVerifiedReason(); got != "no code changes this turn" {
		t.Fatalf("before the marker = %q, want %q", got, "no code changes this turn")
	}

	ag.markTurnVerificationSetupError()
	if got := ag.NotVerifiedReason(); got != "verification setup error" {
		t.Errorf("after setup error = %q, want %q", got, "verification setup error")
	}
	if got := ag.notVerifiedReason(); got != "verification setup error" {
		t.Errorf("event builder after setup error = %q, want %q", got, "verification setup error")
	}

	// A turn start clears the marker with the rest of the per-turn state,
	// so a stale setup error never attaches to a later turn.
	ag.resetTurnVerification()
	if got := ag.NotVerifiedReason(); got != "no code changes this turn" {
		t.Errorf("after turn reset = %q, want %q (the marker is per-turn)", got, "no code changes this turn")
	}
}
