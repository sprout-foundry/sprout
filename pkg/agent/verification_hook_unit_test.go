package agent

// Unit tests for the verification machinery: the
// report builder, the per-check attempt key, and the
// final-reply attachment renderer. They are pure — no
// agent, no fixture, no shell — so they run in every build including
// js/wasm.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	core "github.com/sprout-foundry/seed/core"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// ---------------------------------------------------------------------------
// checkAttemptKey
// ---------------------------------------------------------------------------

// TestCheckAttemptKey pins the counter keys: build/test/
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

// TestBuildVerificationReport_Exhausted pins the stop-rule report:
// an exhausted check moves from the repairable bullets to the
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
// the manual-check rule: a manual check is listed in the result but never
// gated, so it never shows up in the repair report (it can never be
// "failing").
func TestBuildVerificationReport_SkippedManualCheckNeverAppears(t *testing.T) {
	res := vhFailedBuildResult()
	res.Checks = append(res.Checks, verify.Check{
		Kind:    plancontract.KindManual,
		Items:   []string{"m1"},
		Skipped: true,
		Reason:  "manual: verified by a human, not machine-gated",
	})
	got := buildVerificationReport(res, map[string]int{"build": 1}, 3)
	want := `<verification-report>
Turn-end verification found failing checks. Fix the repairable ones, then finish the turn.
Summary: baseline: build: failed (make build); test: skipped — no test command available; manual: skipped [1 manual item] — manual: verified by a human, not machine-gated

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

// ---------------------------------------------------------------------------
// verificationReplyAttachment (the final-reply contract)
// ---------------------------------------------------------------------------

// TestVerificationReplyAttachment_NilStateNoOp pins the behavioral
// baseline: when the turn-end hook never ran for the turn (verification
// disabled, no code change, subagent, or a runner setup error), the
// stored state is empty and the attachment renders nothing — the reply is
// byte-identical to the model's answer.
func TestVerificationReplyAttachment_NilStateNoOp(t *testing.T) {
	if got := verificationReplyAttachment(turnVerification{}); got != "" {
		t.Errorf("attachment for the empty state = %q, want \"\" (the hook never ran for the turn)", got)
	}
}

// TestVerificationReplyAttachment_Passing pins the passing shape: success
// is reported only with a passing result attached — the one-line block
// carrying the run's summary.
func TestVerificationReplyAttachment_Passing(t *testing.T) {
	res := &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Passed: true, Command: "make build"},
			{Kind: plancontract.KindTest, Skipped: true, Reason: "no test command available"},
		},
	}
	got := verificationReplyAttachment(turnVerification{result: res, limit: 3})
	want := "Verification: passed — baseline: build: passed (make build); test: skipped — no test command available"
	if got != want {
		t.Errorf("attachment = %q, want %q", got, want)
	}
}

// TestVerificationReplyAttachment_Failing pins the failure report
// for the canonical broken-build run: the header carries the configured
// repair limit N, the Passed section states nothing passed, the Failed
// line names the check and its reason, and the Tried line states the
// per-check repair attempts against N.
func TestVerificationReplyAttachment_Failing(t *testing.T) {
	got := verificationReplyAttachment(turnVerification{
		result:   vhFailedBuildResult(),
		attempts: map[string]int{"build": 2},
		limit:    2,
	})
	want := "Verification: FAILED after the stopping rule (2 repair attempts)\n" +
		"Passed: none\n" +
		"Failed: build — command failed\n" +
		"Tried: build: 2/2 repair attempts"
	if got != want {
		t.Errorf("attachment = %q,\nwant %q (what passes, what fails, what was tried)", got, want)
	}
}

// TestVerificationReplyAttachment_FailingMixed pins the sections side by
// side: a passing check under Passed, each failing check under Failed
// (keyed like the repair loop's counters, with its own reason), and one
// Tried line per failing check.
func TestVerificationReplyAttachment_FailingMixed(t *testing.T) {
	res := &verify.Result{
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild},
			{Kind: plancontract.KindTest, Passed: true, Command: "make test"},
			{
				Kind:   plancontract.KindInteraction,
				Items:  []string{"i1"},
				Reason: "expected outcome not observed",
			},
		},
	}
	got := verificationReplyAttachment(turnVerification{
		result:   res,
		attempts: map[string]int{"build": 3, "interaction:i1": 3},
		limit:    3,
	})
	want := "Verification: FAILED after the stopping rule (3 repair attempts)\n" +
		"Passed: test\n" +
		"Failed: build — command failed\n" +
		"Failed: interaction:i1 — expected outcome not observed\n" +
		"Tried: build: 3/3 repair attempts\n" +
		"Tried: interaction:i1: 3/3 repair attempts"
	if got != want {
		t.Errorf("attachment = %q,\nwant %q", got, want)
	}
}

// TestVerificationReplyAttachment_FailingRunLevelError pins the failure
// that comes from run-level errors alone (a corrupt manifest or plan):
// there is no failing check to repair, so the report names the run-level
// error and states that nothing was tried.
func TestVerificationReplyAttachment_FailingRunLevelError(t *testing.T) {
	res := &verify.Result{
		Errors: []string{"starter manifest: invalid JSON"},
	}
	got := verificationReplyAttachment(turnVerification{result: res, limit: 3})
	want := "Verification: FAILED after the stopping rule (3 repair attempts)\n" +
		"Passed: none\n" +
		"Failed: run — starter manifest: invalid JSON\n" +
		"Tried: none (run-level failure; nothing was repaired)"
	if got != want {
		t.Errorf("attachment = %q,\nwant %q", got, want)
	}
}

// TestVerificationReplyAttachment_AllSkipped pins the all-skipped shape:
// the run verified nothing, so the attachment states that no passing
// result exists (success is not corroborated) and lists what could not
// be verified.
func TestVerificationReplyAttachment_AllSkipped(t *testing.T) {
	res := &verify.Result{
		Baseline: true,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Skipped: true, Reason: "no build command available"},
			{Kind: plancontract.KindTest, Skipped: true, Reason: "no test command available"},
		},
	}
	if res.Passed() || res.Failed() {
		t.Fatalf("fixture run reports Passed()=%v Failed()=%v, want both false", res.Passed(), res.Failed())
	}
	got := verificationReplyAttachment(turnVerification{result: res, limit: 3})
	want := "Verification: ran, but no checks applied (all skipped) — no passing result\n" +
		"Skipped: build — no build command available\n" +
		"Skipped: test — no test command available"
	if got != want {
		t.Errorf("attachment = %q,\nwant %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// attachVerificationReply (placement: the stored state renders, nothing
// else touches the reply)
// ---------------------------------------------------------------------------

// TestAttachVerificationReply_PlaceAndNoOp pins the agent-level helper:
// an empty stored state leaves the reply byte-identical; a stored state
// appends the block after exactly one blank line, and a trailing
// newline on the model's answer normalizes to the same separator.
func TestAttachVerificationReply_PlaceAndNoOp(t *testing.T) {
	ag := NewTestAgent()

	if got := ag.attachVerificationReply("Done."); got != "Done." {
		t.Errorf("attachVerificationReply = %q, want the untouched reply %q (the hook never ran)", got, "Done.")
	}

	ag.setTurnVerification(turnVerification{
		result:   vhFailedBuildResult(),
		attempts: map[string]int{"build": 2},
		limit:    2,
	})
	const want = "The build is broken.\n\n" +
		"Verification: FAILED after the stopping rule (2 repair attempts)\n" +
		"Passed: none\n" +
		"Failed: build — command failed\n" +
		"Tried: build: 2/2 repair attempts"
	if got := ag.attachVerificationReply("The build is broken."); got != want {
		t.Errorf("attachVerificationReply = %q,\nwant %q", got, want)
	}
	if got := ag.attachVerificationReply("The build is broken.\n"); got != want {
		t.Errorf("attachVerificationReply (trailing newline) = %q,\nwant %q", got, want)
	}
}

// TestHandleQueryResult_InterruptPathOmitsVerificationAttachment pins the
// placement contract: the attachment is appended on the success path
// only (after the language guard). An interrupted turn — even with a
// stored failing verification state — reports as an interrupt, and its
// result never carries the attachment.
func TestHandleQueryResult_InterruptPathOmitsVerificationAttachment(t *testing.T) {
	ag := NewTestAgent()
	ag.setTurnVerification(turnVerification{
		result:   vhFailedBuildResult(),
		attempts: map[string]int{"build": 2},
		limit:    2,
	})

	seedAgent, err := core.NewAgent(core.Options{Provider: vhNoopProvider{}, Executor: core.NoopExecutor})
	if err != nil {
		t.Fatalf("core.NewAgent: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	qc := &queryRunContext{seedAgent: seedAgent, runCtx: ctx, processedQuery: "q"}

	result, err := ag.handleQueryResult(qc, "turn answer", fmt.Errorf("%w", core.ErrInterrupted))
	if !errors.Is(err, ErrRunInterrupted) {
		t.Fatalf("handleQueryResult err = %v, want the ErrRunInterrupted classification", err)
	}
	if result != "" {
		t.Errorf("interrupt result = %q, want the empty string (the turn reports as an interrupt)", result)
	}
	if strings.Contains(result, "Verification:") {
		t.Errorf("interrupt result = %q, must not carry the verification attachment", result)
	}
}

// vhNoopProvider is the minimal core.Provider for constructing a seed
// agent in result-handler tests: no chat happens on those paths, so
// every method is a no-op.
type vhNoopProvider struct{}

func (vhNoopProvider) Chat(context.Context, *core.ChatRequest) (*core.ChatResponse, error) {
	return &core.ChatResponse{}, nil
}

func (vhNoopProvider) ChatStream(context.Context, *core.ChatRequest, core.StreamHandler) error {
	return nil
}

func (vhNoopProvider) Info() core.ProviderInfo { return core.ProviderInfo{} }

func (vhNoopProvider) EstimateTokens(*core.ChatRequest) int { return 0 }
