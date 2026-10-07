//go:build !js

// verification_hook_guards_test.go — the guard-rail tests: the
// per-turn reset (a previous turn's stored state never attaches to a later
// reply) and the belt-and-braces guards (subagent turns skip the hook, a
// runner setup error does not gate the turn, a provider error in a repair
// round is propagated to handleQueryResult). The fixtures and helpers live in
// verification_hook_test.go (same package); the gate and repair-loop
// acceptance tests live there too.

package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestVerificationReply_PerTurnResetNoStaleAttachment pins the per-turn
// reset (prepareQueryRun): a previous turn's stored verification state
// never attaches to a later turn's reply. Turn 1 ends on a failing run
// (the attachment is present); turn 2 is text-only (no code change, the
// hook is a no-op) and its reply stands byte-identical to the model's
// answer.
func TestVerificationReply_PerTurnResetNoStaleAttachment(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const (
		turnOneFinal = "The build still fails; the remaining failure is fixture-broken-build."
		turnTwo      = "Turn two: nothing to change."
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse("Turn one answer."),
		NewScriptedTextResponse("Repair one."),
		NewScriptedTextResponse(turnOneFinal),
		NewScriptedTextResponse(turnTwo),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	first, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("turn 1 ProcessQuery: %v", err)
	}
	if !strings.Contains(first, "Verification: FAILED after the stopping rule") {
		t.Fatalf("turn 1 reply = %q, want the failing attachment (the broken build stops at N=2)", first)
	}

	// Turn 2 changed no file: prepareQueryRun reset the stored state and
	// the hook's gate (TurnChangedPaths) is empty — turn 1's stored
	// result must not attach to turn 2's reply.
	second, err := ag.ProcessQuery("Anything else?")
	if err != nil {
		t.Fatalf("turn 2 ProcessQuery: %v", err)
	}
	if second != turnTwo {
		t.Errorf("turn 2 reply = %q, want the untouched text-only answer %q (no stale attachment)", second, turnTwo)
	}
	if tv := ag.currentTurnVerification(); tv.result != nil {
		t.Errorf("stored verification state after turn 2 = %+v, want empty (the hook never ran)", tv)
	}
}

// ---------------------------------------------------------------------------
// The guard rails
// ---------------------------------------------------------------------------

// TestVerificationHook_SkipsSubagents pins the belt-and-braces guard:
// subagent turns never own the final reply, so the hook is a no-op even
// when verification is enabled and the turn changed a file.
func TestVerificationHook_SkipsSubagents(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	const turnAnswer = "Subagent done."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})
	ag.subagentDepth = 1

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched subagent answer %q", result, turnAnswer)
	}
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (subagents never run the turn-end hook)", calls)
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("LastVerificationResult = %+v, want nil (subagent turns skip the hook)", res)
	}
}

// TestVerificationHook_RunnerSetupErrorDoesNotGateTurn pins that a
// runner setup problem (empty project root: "project root is required")
// is logged and the turn stands — it never gates the turn, and nothing
// is stored. The tracked file change proves the hook's own gate passed
// (the setup error — not the no-change no-op — is what was exercised).
func TestVerificationHook_RunnerSetupErrorDoesNotGateTurn(t *testing.T) {
	dir := t.TempDir()
	client := NewScriptedClient(
		NewScriptedToolCallResponse("vh_wf_1", "write_file",
			fmt.Sprintf(`{"path":%q,"content":"x"}`, filepath.Join(dir, "app.js")), "Writing."),
		NewScriptedTextResponse("Done."),
	)
	// No workspace root: the runner cannot start ("project root is
	// required") — the turn must not be gated on it.
	ag := vhAgent(t, client, "", &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	result, err := ag.ProcessQuery("Write the file.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v (a runner setup error must not gate the turn)", err)
	}
	if result != "Done." {
		t.Errorf("result = %q, want the untouched turn answer", result)
	}
	if ag.GetChangeCount() == 0 {
		t.Fatal("no tracked change: the hook's setup-error path was not reached (the file write was not tracked)")
	}
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (no repair round after a setup error)", calls)
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("LastVerificationResult = %+v, want nil (the run never started)", res)
	}
}

// TestVerificationHook_ProviderErrorDuringRepairIsPropagated pins the
// repair-round error path: a provider error in a repair round stops the
// loop and is classified by handleQueryResult (exactly as a first-run
// error would be), with the last verification result already stored.
func TestVerificationHook_ProviderErrorDuringRepairIsPropagated(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse("Turn answer."),
		&ScriptedResponse{Error: &core.ClientError{Provider: "scripted", Wrapped: errors.New("HTTP 400: invalid request")}},
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 3})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err == nil {
		t.Fatal("ProcessQuery = nil error, want the classified provider error from the repair round")
	}
	var ce *core.ClientError
	if !errors.As(err, &ce) {
		t.Fatalf("ProcessQuery error = %v, want a classified client error", err)
	}

	// The error path never reaches the attachment — the turn
	// reports as an error, and the stored failing verification result
	// must not leak into the reply as a final-result attachment.
	if strings.Contains(result, "Verification:") {
		t.Errorf("error-path result = %q, must not carry the verification attachment", result)
	}

	// One repair round was attempted (its call failed); the turn's two
	// calls plus that one repair call is the whole run.
	if calls := len(client.GetSentRequests()); calls != 3 {
		t.Errorf("model calls = %d, want 3 (the turn + one repair attempt)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 1 {
		t.Errorf("verification-report messages = %d, want 1 (the failed repair round's report)", len(reports))
	}

	// The last (failing) verification run was stored before the repair
	// round errored.
	if res := ag.LastVerificationResult(); res == nil || !res.Failed() {
		t.Errorf("LastVerificationResult = %+v, want the last failing run", res)
	}
}
