//go:build !js

// verification_state_test.go — the SP-154 §154b (154.3) accessor tests
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
	// LastVerificationResult keeps its exact pre-154.3 behavior (no nil
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
// of a scripted failing-verification turn (the 149.5 fixture pattern,
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

	// LastVerificationResult keeps its exact behavior (the 149.5/149.6
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
