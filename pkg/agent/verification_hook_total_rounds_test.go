//go:build !js

// verification_hook_total_rounds_test.go — the total repair-rounds cap of
// the verification loop: the per-check attempt counters stop
// the loop only when every currently-failing check has exhausted its own
// attempts, so two failure patterns can outrun them — checks alternating
// failures between rounds (each counter grows at half speed), and
// interaction checks whose item id (and therefore counter key) is new
// every round. This file pins the turn-level cap that bounds both: the
// loop stops when either the per-check rule or the total cap fires,
// whichever comes first, and the honest failure report still
// attaches when the total cap is what ended the loop.
//
// The fixture follows verification_hook_test.go: a scripted model, a real
// workspace, and the default shell executor (sh -c), so the tests skip
// where sh is absent.

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// vtrShAvailable mirrors vhShAvailable (the fixture commands run through
// sh -c), kept local so this file stays self-contained.
func vtrShAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh available on this platform (the fixture commands run through sh -c)")
	}
}

// vtrWriteBuildTestManifest writes a starter manifest fixture declaring
// both a build and a test command, so a baseline verification run gates on
// two checks that can fail independently of each other.
func vtrWriteBuildTestManifest(t *testing.T, root, buildCommand, testCommand string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0o755); err != nil {
		t.Fatalf("mkdir .sprout: %v", err)
	}
	manifest := fmt.Sprintf(`{"starter":{"id":"fixture","version":"1.0.0"},"build":%q,"test":%q}`,
		buildCommand, testCommand)
	if err := os.WriteFile(starterstore.StarterManifestPath(root), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write starter manifest: %v", err)
	}
}

// vtrWriteFlipScript writes a fixture command script that alternates
// pass/fail per key: each invocation bumps a per-key counter under
// .sprout/ and the script fails when that counter is in its failing
// phase — even counts for key "b", odd counts for key "t" (the test check
// is anti-phased). Both manifest checks run the script with their own
// key, so each round exactly one of them fails and the failure swaps
// sides every round: a pattern the per-check counters cannot stop
// (neither key reaches its limit while the other is failing).
func vtrWriteFlipScript(t *testing.T, root string) {
	t.Helper()
	script := filepath.Join(root, "flip.sh")
	content := "#!/bin/sh\n" +
		"key=\"${1:-x}\"\n" +
		"c=\"$PWD/.sprout/flip-$key.count\"\n" +
		"r=$(cat \"$c\" 2>/dev/null || echo 0)\n" +
		"echo $((r+1)) > \"$c\"\n" +
		"p=$r\n" +
		"[ \"$key\" = \"t\" ] && p=$((r+1))\n" +
		"[ $((p % 2)) -eq 0 ] && exit 1\n" +
		"exit 0\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write flip script: %v", err)
	}
}

// TestVerificationHook_TotalCapStopsAlternatingFailures pins the
// alternating-failure termination: build and test take turns failing, so
// neither per-check counter reaches the limit while the loop is running,
// and the total-rounds cap is what ends the turn — exactly at the cap,
// with the failure report attached to the final reply.
func TestVerificationHook_TotalCapStopsAlternatingFailures(t *testing.T) {
	vtrShAvailable(t)
	root := t.TempDir()
	vtrWriteFlipScript(t, root)
	vtrWriteBuildTestManifest(t, root, "sh flip.sh b", "sh flip.sh t")

	const (
		turnAnswer = "I made the change."
		finalReply = "The build still alternates; the remaining failure is the build check."
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		NewScriptedTextResponse("Repair one."),
		NewScriptedTextResponse("Repair two."),
		NewScriptedTextResponse("Repair three."),
		NewScriptedTextResponse(finalReply),
	)
	// N=3 per check, total cap 4: the rotation gives each key 2 attempts
	// by the time the cap fires, so the per-check rule alone would keep
	// the loop running past this point.
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{
		Enabled:           true,
		RepairAttempts:    3,
		TotalRepairRounds: 4,
	})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The turn: the tool-call iteration + the answer, plus exactly four
	// repair rounds (one per cap round). A fifth repair round would
	// consume a seventh call.
	if calls := len(client.GetSentRequests()); calls != 6 {
		t.Errorf("model calls = %d, want 6 (the turn + 4 repair rounds: the total cap must end the loop)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 4 {
		t.Fatalf("verification-report user messages = %d, want 4 (one per repair round)", len(reports))
	}

	// The rotation: round 0 the build failed, round 1 the test, round 2
	// the build, round 3 the test. No report may carry the DONE section —
	// no check ever reached its limit, which is exactly why the per-check
	// rule cannot stop this loop.
	for i, report := range vhReportMessages(ag) {
		if strings.Contains(report, "DONE") {
			t.Errorf("report %d carries the DONE section; no check may exhaust its limit under the rotation", i)
		}
	}
	reports := vhReportMessages(ag)
	if !strings.Contains(reports[0], "build 1/3") || strings.Contains(reports[0], "test 1/3") {
		t.Errorf("round 0 report must fail the build only:\n%s", reports[0])
	}
	if !strings.Contains(reports[1], "test 1/3") || strings.Contains(reports[1], "build 1/3") {
		t.Errorf("round 1 report must fail the test only:\n%s", reports[1])
	}
	if !strings.Contains(reports[2], "build 2/3") {
		t.Errorf("round 2 report must show the build's second attempt:\n%s", reports[2])
	}
	if !strings.Contains(reports[3], "test 2/3") {
		t.Errorf("round 3 report must show the test's second attempt:\n%s", reports[3])
	}

	// The final reply carries the failure report even though
	// the total cap (not the per-check rule) ended the loop. The last run
	// failed the build (test passed), each with 2 of N=3 attempts used.
	const want = finalReply + "\n\n" + "Verification: FAILED after the stopping rule (3 repair attempts)\n" +
		"Passed: test\n" +
		"Failed: build — command failed\n" +
		"Tried: build: 2/3 repair attempts"
	if result != want {
		t.Errorf("result = %q,\nwant %q (the failure report after the total cap fired)", result, want)
	}

	// The stored state carries the cap round count and the per-check
	// counters the last run saw — both strictly under the limit, proving
	// the cap (not the per-check rule) stopped the loop.
	tv := ag.LastTurnVerification()
	if tv == nil {
		t.Fatal("LastTurnVerification = nil, want the stored failing run")
	}
	if tv.Rounds != 4 {
		t.Errorf("stored rounds = %d, want 4 (the total cap)", tv.Rounds)
	}
	if tv.Attempts["build"] != 2 || tv.Attempts["test"] != 2 {
		t.Errorf("stored attempts = %v, want build=2 test=2 (both keys under the limit N=3)", tv.Attempts)
	}

	res := ag.LastVerificationResult()
	if res == nil || !res.Failed() {
		t.Fatalf("LastVerificationResult = %+v, want the last failing run", res)
	}
	for _, c := range res.Checks {
		if c.Kind != plancontract.KindBuild {
			continue
		}
		if c.Passed || c.Skipped {
			t.Errorf("stored build check = %+v, want failed (the last round's failing check)", c)
		}
	}
}

// TestVerificationHook_TotalCapDefaultScalesAbovePerCheckLimit pins that
// the derived default total cap never cuts a healthy per-check repair
// sequence short: with N=2 and no explicit total, the default resolves to
// 4, and a build failing on every round exhausts its per-check attempts
// first — the loop stops at N=2 rounds, not at the cap.
func TestVerificationHook_TotalCapDefaultScalesAbovePerCheckLimit(t *testing.T) {
	vtrShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse("Turn answer."),
		NewScriptedTextResponse("Repair one."),
		NewScriptedTextResponse("Repair two."),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	if _, err := ag.ProcessQuery("Implement the app entry point."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The derived default (2 * N = 4) must exceed N so the per-check rule
	// stays the binding constraint for the single-check case.
	if got := ag.configManager.GetConfig().VerificationRepairTotalRounds(); got != 4 {
		t.Errorf("VerificationRepairTotalRounds() = %d, want 4 (twice the explicit N=2)", got)
	}
	if calls := len(client.GetSentRequests()); calls != 4 {
		t.Errorf("model calls = %d, want 4 (the per-check rule stopped the loop at N=2 rounds, before the cap)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 2 {
		t.Errorf("verification-report messages = %d, want 2 (N=2, not the total cap)", len(reports))
	}
	if tv := ag.LastTurnVerification(); tv == nil || tv.Rounds != 2 {
		t.Errorf("stored rounds = %+v, want 2 (the per-check rule fired first)", tv)
	}
}

// TestVerificationHook_ExplicitTotalCapBelowDefaultStopsEarly pins an
// explicit total tighter than the derived default: with N=2 and
// total_repair_rounds=1, one repair round runs and the loop stops at the
// cap — before the per-check limit — with the failing run stored and the
// report attached.
func TestVerificationHook_ExplicitTotalCapBelowDefaultStopsEarly(t *testing.T) {
	vtrShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "echo fixture-broken-build; exit 1")

	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse("Turn answer."),
		NewScriptedTextResponse("Repair one."),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{
		Enabled:           true,
		RepairAttempts:    2,
		TotalRepairRounds: 1,
	})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if calls := len(client.GetSentRequests()); calls != 3 {
		t.Errorf("model calls = %d, want 3 (the turn + exactly one repair round: the cap is 1)", calls)
	}
	if reports := vhReportMessages(ag); len(reports) != 1 {
		t.Errorf("verification-report messages = %d, want 1", len(reports))
	}
	if tv := ag.LastTurnVerification(); tv == nil || tv.Rounds != 1 {
		t.Errorf("stored rounds = %+v, want 1 (the total cap fired before the per-check limit)", tv)
	}
	// The failure report still names what failed and what was tried; the
	// per-check counter shows the attempt the one round consumed.
	const wantSub = "Verification: FAILED after the stopping rule (2 repair attempts)\n" +
		"Passed: none\n" +
		"Failed: build — command failed\n" +
		"Tried: build: 1/2 repair attempts"
	if !strings.HasSuffix(result, wantSub) {
		t.Errorf("result = %q,\nwant it to end with the failure report %q", result, wantSub)
	}
}
