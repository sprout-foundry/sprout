//go:build !js

// errclass_repair_test.go — the scripted acceptance tests for the rule that
// the agent attempts a fix for a build/test failure before the turn reports
// it, and that a short classification explanation accompanies the raw output
// rather than replacing it. The turn-end verification hook already feeds any
// failing check back to the model (the repair round); a classified failure
// now reaches that repair round with the classification line attached to the
// check's bullet and the raw excerpt preserved below it.
//
//   - a classified build failure is fed back as a repair round; the report
//     the model sees carries both the classification and the raw output;
//   - a repair round that fixes the build ends the turn on the passing run
//     (the failure is never reported as final when a fix succeeded);
//   - an unclassified failure still follows the existing repair behavior;
//   - a persistent failure is bounded by the repair cap and then reported.

package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// erClassifiedBuildCommand is a failing build whose output matches a known
// classifier shape (missing dependency): a deterministic signal the report's
// classification line must name.
const erClassifiedBuildCommand = `echo './main.go:3:8: cannot find package "example.com/dep" in any of:'; exit 1`

// erUnknownBuildCommand is a failing build whose output matches no classifier
// shape, so the report must keep its pre-classifier form.
const erUnknownBuildCommand = `echo 'make: nothing to be done for all'; exit 1`

// erReportMessages returns the user-role transcript messages carrying a
// <verification-report> envelope, in order.
func erReportMessages(ag *Agent) []string {
	var reports []string
	for _, m := range ag.GetMessages() {
		if m.Role == "user" && strings.Contains(m.Content, "<verification-report>") {
			reports = append(reports, m.Content)
		}
	}
	return reports
}

// ---------------------------------------------------------------------------
// (1) A classified build failure triggers a fix attempt before reporting
// ---------------------------------------------------------------------------

// TestErrclassRepair_ClassifiedFailureTriggersFixBeforeReport pins rules (1)
// and (2): a build failure whose output classifies as a missing dependency
// is fed back to the model as a repair round (a fix attempt before the turn
// reports — the same repair round every failing check gets, now carrying the
// classification), and the failure the model/repair sees carries the short
// explanation AND the raw output.
func TestErrclassRepair_ClassifiedFailureTriggersFixBeforeReport(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, erClassifiedBuildCommand)

	const (
		turnAnswer  = "I made the change."
		repairOne   = "Repair one."
		repairFinal = "The dependency is still missing; the remaining failure stands."
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		NewScriptedTextResponse(repairOne),
		NewScriptedTextResponse(repairFinal),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The turn ran a fix attempt: the model was called again with the repair
	// report (the turn's two calls plus two repair rounds).
	if calls := len(client.GetSentRequests()); calls != 4 {
		t.Fatalf("model calls = %d, want 4 (the turn + repair rounds: a classified failure must trigger a fix attempt)", calls)
	}
	reports := erReportMessages(ag)
	if len(reports) != 2 {
		t.Fatalf("verification-report messages = %d, want 2 (the classified failure fed back as repair rounds)", len(reports))
	}
	report := reports[0]

	// The short explanation accompanies the raw output: the classification
	// line names the category and the explanation, and the raw excerpt is
	// still present (the repairable report carries both).
	if !strings.Contains(report, "Classification: missing-dependency:") {
		t.Errorf("report missing the classification line:\n%s", report)
	}
	if !strings.Contains(report, "cannot find package") {
		t.Errorf("report must still carry the raw output:\n%s", report)
	}
	for _, want := range []string{"<verification-report>", "build", "1/2", "example.com/dep"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}

	// The turn ends on the repair-round answer with the failure report (the
	// fix attempt was made; the failure persisted to the cap).
	if !strings.HasPrefix(result, repairFinal) {
		t.Errorf("result = %q, want it to start with the repair-round answer %q", result, repairFinal)
	}
	if !strings.Contains(result, "Verification: FAILED after the stopping rule (2 repair attempts)") {
		t.Errorf("result = %q, want the failure report after the fix attempt", result)
	}
}

// ---------------------------------------------------------------------------
// (2) A fix attempt that succeeds ends the turn on the passing run
// ---------------------------------------------------------------------------

// TestErrclassRepair_SuccessfulFixEndsTurnPassing pins that the fix attempt
// is real: a repair round that resolves the classified failure ends the turn
// on a passing verification run, never reporting the fixed failure as final.
func TestErrclassRepair_SuccessfulFixEndsTurnPassing(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	// The build fails until the repair round writes the marker file the
	// command looks for: a real fix the model performs.
	vhWriteStarterManifest(t, root, `if [ -f fixed.marker ]; then echo build-ok; else echo './main.go:1:1: cannot find package "x"'; exit 1; fi`)

	const (
		turnAnswer  = "I made the change."
		repairFinal = "I added the missing dependency so the build passes."
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		// The repair round writes the marker the build command checks for.
		NewScriptedToolCallResponse("er_fix_1", "write_file",
			`{"path":"`+root+`/fixed.marker","content":"1"}`, "Adding the dependency."),
		NewScriptedTextResponse(repairFinal),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 2})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// The fix attempt was made (one repair round), and the passing run ended
	// the turn: the reply carries the passing attachment, not the failure.
	if calls := len(client.GetSentRequests()); calls != 4 {
		t.Errorf("model calls = %d, want 4 (the turn + one successful repair round)", calls)
	}
	if !strings.Contains(result, "Verification: passed") {
		t.Errorf("result = %q, want the passing attachment after the successful fix", result)
	}
	if strings.Contains(result, "FAILED") {
		t.Errorf("result = %q, must not report the fixed failure as final", result)
	}
	res := ag.LastVerificationResult()
	if res == nil || !res.Passed() {
		t.Fatalf("LastVerificationResult = %+v, want the passing run after the fix", res)
	}
}

// ---------------------------------------------------------------------------
// (3) An unclassified failure still follows the existing behavior
// ---------------------------------------------------------------------------

// TestErrclassRepair_UnclassifiedFailureFollowsExistingBehavior pins rule
// (3): an unknown failure still triggers the existing repair round, but the
// report carries no classification line — the pre-classifier shape.
func TestErrclassRepair_UnclassifiedFailureFollowsExistingBehavior(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, erUnknownBuildCommand)

	const repairFinal = "The failure is not classifiable; it still stands."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse("Turn answer."),
		NewScriptedTextResponse(repairFinal),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if calls := len(client.GetSentRequests()); calls != 3 {
		t.Errorf("model calls = %d, want 3 (an unknown failure still triggers the existing repair round)", calls)
	}
	reports := erReportMessages(ag)
	if len(reports) != 1 {
		t.Fatalf("verification-report messages = %d, want 1", len(reports))
	}
	if strings.Contains(reports[0], "Classification:") {
		t.Errorf("unknown failure must not gain a classification line:\n%s", reports[0])
	}
	// The raw output is still carried (existing behavior).
	if !strings.Contains(reports[0], "nothing to be done") {
		t.Errorf("report must carry the raw output:\n%s", reports[0])
	}
	if !strings.Contains(result, "Verification: FAILED after the stopping rule") {
		t.Errorf("result = %q, want the failure report (existing behavior)", result)
	}
}

// ---------------------------------------------------------------------------
// (4) The attempt count is bounded; a persistent failure is reported
// ---------------------------------------------------------------------------

// TestErrclassRepair_PersistentClassifiedFailureBoundedByCap pins rule (4):
// a classified failure that the fix attempts never resolve is bounded by the
// configured repair cap and then reported — never an infinite loop.
func TestErrclassRepair_PersistentClassifiedFailureBoundedByCap(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, erClassifiedBuildCommand)

	const (
		n           = 2
		turnAnswer  = "Turn answer."
		repairOne   = "Repair one."
		repairFinal = "Still missing the dependency; the failure stands."
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		NewScriptedTextResponse(repairOne),
		NewScriptedTextResponse(repairFinal),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{Enabled: true, RepairAttempts: n})

	result, err := ag.ProcessQuery("Implement the app entry point.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// Bounded: exactly N repair rounds (the turn's two calls plus N), never
	// more. A third round would consume a fifth request.
	if calls := len(client.GetSentRequests()); calls != 2+n {
		t.Errorf("model calls = %d, want %d (bounded at N=%d repair rounds)", calls, 2+n, n)
	}
	reports := erReportMessages(ag)
	if len(reports) != n {
		t.Fatalf("verification-report messages = %d, want %d (one per bounded repair round)", len(reports), n)
	}
	// The first (repairable) report carries the classification and the raw
	// output; the last states the check is DONE after the cap.
	if !strings.Contains(reports[0], "Classification: missing-dependency:") {
		t.Errorf("first report missing the classification line:\n%s", reports[0])
	}
	if !strings.Contains(reports[0], "cannot find package") {
		t.Errorf("first report missing the raw output:\n%s", reports[0])
	}
	// The last report tells the model the check is DONE, and the final reply
	// reports the persistent failure.
	if !strings.Contains(reports[n-1], "DONE") {
		t.Errorf("last report must state the check is DONE after the cap:\n%s", reports[n-1])
	}
	if !strings.Contains(result, "Verification: FAILED after the stopping rule (2 repair attempts)") {
		t.Errorf("result = %q, want the persistent failure reported after the cap", result)
	}
}
