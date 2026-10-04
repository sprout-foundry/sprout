package agent

// Unit tests for the SP-149 §149c report builder and the per-check
// attempt key (item 149.5). They are pure — no agent, no fixture, no
// shell — so they run in every build including js/wasm.

import (
	"context"
	"errors"
	"testing"

	core "github.com/sprout-foundry/seed/core"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// ---------------------------------------------------------------------------
// checkAttemptKey
// ---------------------------------------------------------------------------

// TestCheckAttemptKey pins the counter keys (SP-149 §149c): build/test/
// page are keyed by kind; interaction checks carry their item id so two
// scripted flows count their own attempts separately.
func TestCheckAttemptKey(t *testing.T) {
	cases := []struct {
		name  string
		check verify.Check
		want  string
	}{
		{"build", verify.Check{Kind: plancontract.KindBuild}, "build"},
		{"build with items", verify.Check{Kind: plancontract.KindBuild, Items: []string{"a1", "a2"}}, "build"},
		{"test", verify.Check{Kind: plancontract.KindTest}, "test"},
		{"page", verify.Check{Kind: plancontract.KindPage}, "page"},
		{"page with items", verify.Check{Kind: plancontract.KindPage, Items: []string{"p1"}}, "page"},
		{"interaction carries the item id", verify.Check{Kind: plancontract.KindInteraction, Items: []string{"i1"}}, "interaction:i1"},
		{"a second flow is a separate counter", verify.Check{Kind: plancontract.KindInteraction, Items: []string{"i2"}}, "interaction:i2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := checkAttemptKey(c.check); got != c.want {
				t.Errorf("checkAttemptKey(%+v) = %q, want %q", c.check, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildVerificationReport
// ---------------------------------------------------------------------------

// vhFailedBuildResult is a deterministic failed baseline run: the build
// check failed (its output excerpt carries the evidence) and the test
// check skipped itself (the project declares no test command).
func vhFailedBuildResult() *verify.Result {
	return &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{
				Kind:    plancontract.KindBuild,
				Command: "make build",
				Excerpt: "error: undefined reference to `main`\n",
			},
			{
				Kind:    plancontract.KindTest,
				Skipped: true,
				Reason:  "no test command available",
			},
		},
	}
}

const vhFailedBuildSummary = "baseline: build: failed (make build); test: skipped — no test command available"

// TestBuildVerificationReport_Repairable pins the exact report for the
// repairable case: the envelope, the framing line, the summary, one
// bullet per repairable failing check (key, attempts used n/limit,
// reason, indented excerpt).
func TestBuildVerificationReport_Repairable(t *testing.T) {
	res := vhFailedBuildResult()
	got := buildVerificationReport(res, map[string]int{"build": 1}, 3)
	want := `<verification-report>
Turn-end verification found failing checks. Fix the repairable ones, then finish the turn.
Summary: ` + vhFailedBuildSummary + `

Failing checks:
- build 1/3 — command failed
  error: undefined reference to ` + "`main`" + `

</verification-report>
`
	if got != want {
		t.Errorf("report mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestBuildVerificationReport_Exhausted pins the stop-rule report (SP-149
// §149d): an exhausted check moves from the repairable bullets to the
// explicit DONE section telling the model to stop and state the failure.
func TestBuildVerificationReport_Exhausted(t *testing.T) {
	res := vhFailedBuildResult()
	got := buildVerificationReport(res, map[string]int{"build": 3}, 3)
	want := `<verification-report>
Turn-end verification found failing checks. Fix the repairable ones, then finish the turn.
Summary: ` + vhFailedBuildSummary + `

Checks that are DONE after the repair limit — stop trying to fix them and state the remaining failure plainly in your final reply:
- build 3/3

</verification-report>
`
	if got != want {
		t.Errorf("report mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestBuildVerificationReport_MixedRepairableAndExhausted pins the
// interaction key (item id in the counter) and both sections side by
// side.
func TestBuildVerificationReport_MixedRepairableAndExhausted(t *testing.T) {
	res := &verify.Result{
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "make build", Excerpt: "boom\n"},
			{
				Kind:    plancontract.KindInteraction,
				Items:   []string{"i1"},
				Reason:  "expected outcome not observed",
				Excerpt: "step 2: click #login\nassert failed: no element",
			},
		},
	}
	got := buildVerificationReport(res, map[string]int{"build": 1, "interaction:i1": 3}, 3)
	want := `<verification-report>
Turn-end verification found failing checks. Fix the repairable ones, then finish the turn.
Summary: plan rev 0: build: failed (make build); interaction: failed — expected outcome not observed

Failing checks:
- build 1/3 — command failed
  boom

Checks that are DONE after the repair limit — stop trying to fix them and state the remaining failure plainly in your final reply:
- interaction:i1 3/3

</verification-report>
`
	if got != want {
		t.Errorf("report mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestBuildVerificationReport_ReasonAndEmptyExcerpt pins the two
// degenerate bullet cases: a check-level reason replaces the plain
// "command failed", and an empty excerpt renders no indented block.
func TestBuildVerificationReport_ReasonAndEmptyExcerpt(t *testing.T) {
	res := &verify.Result{
		Checks: []verify.Check{
			{
				Kind:    plancontract.KindBuild,
				Command: "make build",
				Reason:  "command did not finish in time (timeout or cancellation)",
			},
		},
	}
	got := buildVerificationReport(res, map[string]int{"build": 1}, 3)
	want := `<verification-report>
Turn-end verification found failing checks. Fix the repairable ones, then finish the turn.
Summary: plan rev 0: build: failed (make build) — command did not finish in time (timeout or cancellation)

Failing checks:
- build 1/3 — command did not finish in time (timeout or cancellation)

</verification-report>
`
	if got != want {
		t.Errorf("report mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestBuildVerificationReport_SkippedManualCheckNeverAppears pins
// §149a: a manual check is listed in the result but never gated, so it
// never shows up in the repair report (it can never be "failing").
func TestBuildVerificationReport_SkippedManualCheckNeverAppears(t *testing.T) {
	res := vhFailedBuildResult()
	res.Checks = append(res.Checks, verify.Check{
		Kind:    plancontract.KindManual,
		Items:   []string{"m1"},
		Skipped: true,
		Reason:  "manual: verified by a human, not machine-gated (SP-149 §149a)",
	})
	got := buildVerificationReport(res, map[string]int{"build": 1}, 3)
	want := `<verification-report>
Turn-end verification found failing checks. Fix the repairable ones, then finish the turn.
Summary: baseline: build: failed (make build); test: skipped — no test command available; manual: skipped [1 manual item] — manual: verified by a human, not machine-gated (SP-149 §149a)

Failing checks:
- build 1/3 — command failed
  error: undefined reference to ` + "`main`" + `

</verification-report>
`
	if got != want {
		t.Errorf("report mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestVerificationHook_CancelledRunContextReportsInterrupt pins the stop
// window: a run context cancelled between the turn's answer and the verify
// run (or between repair rounds) reports an interrupt handleQueryResult
// can classify, instead of treating the turn as completed.
func TestVerificationHook_CancelledRunContextReportsInterrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Verification = &configuration.VerificationConfig{Enabled: true}
		return nil
	}); err != nil {
		t.Fatalf("configure verification: %v", err)
	}

	ag := NewTestAgent()
	ag.configManager = mgr
	tracker := NewChangeTracker(nil, "guard-turn")
	tracker.MarkTurnStart()
	if err := tracker.TrackFileWriteState("/ws/app.go", "old", "new", true); err != nil {
		t.Fatalf("TrackFileWriteState: %v", err)
	}
	ag.changeTracker = tracker

	qc := &queryRunContext{runCtx: ctx}
	res, err := ag.runTurnEndVerification(qc, "turn answer")
	if !errors.Is(err, core.ErrInterrupted) {
		t.Fatalf("runTurnEndVerification err = %v, want an ErrInterrupted the result handler classifies", err)
	}
	if res != "turn answer" {
		t.Errorf("result = %q, want the turn's answer (the turn's conversation state keeps it)", res)
	}
}
