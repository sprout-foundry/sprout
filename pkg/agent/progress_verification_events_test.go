//go:build !js

// progress_verification_events_test.go — the SP-151 §151a item-151.3
// acceptance tests: at turn completion the runtime emits
// progress_verification (the SP-149 evidence) followed by
// progress_complete (the verdict), both carrying the same correlation ids,
// and emits only progress_complete with a not_verified_reason when the
// turn-end hook never ran.
//
// The pure payload builders are asserted directly (exact-map + omitempty
// pins). The emit path is driven through the real method —
// publishTurnProgressComplete on a bare Agent wired to a captured EventBus —
// mirroring the bare-`*Agent` + captured-`EventBus` convention in
// scope_milestones_test.go (151.2), so the full publish path
// (stored turn-verification → payload builder → publishEvent) is exercised.

package agent

import (
	"reflect"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// pveMixedResult is the canonical mixed run: a passing build check, a failing
// test check (its excerpt carries the evidence), a skipped page check, and one
// run-level error. res.Passed() is false (a check failed), res.PlanRevision is
// 3, and Baseline is false.
func pveMixedResult() *verify.Result {
	return &verify.Result{
		PlanRevision: 3,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Items: []string{"acc-1"}, Command: "make build", Passed: true},
			{Kind: plancontract.KindTest, Items: []string{"acc-2"}, Command: "go test ./...", Excerpt: "FAIL\tgithub.com/x 0.5s"},
			{Kind: plancontract.KindPage, Skipped: true, Reason: "no trusted page command"},
		},
		Errors: []string{"unreadable manifest"},
	}
}

// pvePassingResult is a clean passing run (nothing failed, at least one check
// ran), so res.Passed() is true.
func pvePassingResult() *verify.Result {
	return &verify.Result{
		PlanRevision: 3,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Items: []string{"acc-1"}, Command: "make build", Passed: true},
			{Kind: plancontract.KindTest, Items: []string{"acc-2"}, Command: "go test ./...", Passed: true},
		},
	}
}

// pveFailingResult is a failing run: the build check failed (its excerpt
// carries the evidence) and the test check skipped itself (no command).
func pveFailingResult() *verify.Result {
	return &verify.Result{
		PlanRevision: 3,
		Checks: []verify.Check{
			{Kind: plancontract.KindBuild, Command: "make build", Excerpt: "error: undefined reference to `main`\n"},
			{Kind: plancontract.KindTest, Skipped: true, Reason: "no test command available"},
		},
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// pveAgent builds a bare agent with session id "run-1" and a captured EventBus,
// so publishTurnProgressComplete runs the real emit path and its events can be
// asserted. The agent has no configuration manager (configManager == nil) so
// the "not verified" reason resolves to "verification disabled".
func pveAgent(t *testing.T) (*Agent, <-chan events.UIEvent) {
	t.Helper()

	ag := &Agent{state: NewAgentStateManager(false)}
	ag.initSubManagers()
	ag.SetSessionID("run-1")

	bus := events.NewEventBus()
	ch := bus.Subscribe("progress-verification-test")
	t.Cleanup(func() { bus.Unsubscribe("progress-verification-test") })
	ag.SetEventBus(bus)
	return ag, ch
}

// pveNextEvent reads the next event from ch (or fails on the deadline).
func pveNextEvent(t *testing.T, ch <-chan events.UIEvent) events.UIEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a progress event")
		return events.UIEvent{}
	}
}

// pveData returns an event's payload as a map (the payloads are built as
// map[string]interface{} so the metadata merge applies).
func pveData(t *testing.T, ev events.UIEvent) map[string]interface{} {
	t.Helper()
	m, ok := ev.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("payload is %T, want map[string]interface{}", ev.Data)
	}
	return m
}

// pveNoMoreEvents asserts that no further event arrives within the window —
// the "exactly N events" checks.
func pveNoMoreEvents(t *testing.T, ch <-chan events.UIEvent, window time.Duration) {
	t.Helper()
	deadline := time.After(window)
	for {
		select {
		case ev := <-ch:
			t.Fatalf("unexpected event %q", ev.Type)
		case <-deadline:
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Pure payload builders: exact map + omitempty pins
// ---------------------------------------------------------------------------

// TestProgressVerificationData_PayloadShape pins the exact map the
// progress_verification / nested verification payload carries for a mixed run:
// run_id, plan_revision, the three checks (kind/passed/skipped/excerpt), and
// errors — with baseline and passed omitted (both false).
func TestProgressVerificationData_PayloadShape(t *testing.T) {
	res := pveMixedResult()
	got := progressVerificationData(res, "run-1")

	want := map[string]interface{}{
		"run_id":        "run-1",
		"plan_revision": 3,
		"errors":        []string{"unreadable manifest"},
		"checks": []map[string]interface{}{
			{
				"kind":    "build",
				"items":   []string{"acc-1"},
				"command": "make build",
				"passed":  true,
			},
			{
				"kind":    "test",
				"items":   []string{"acc-2"},
				"command": "go test ./...",
				"excerpt": "FAIL\tgithub.com/x 0.5s",
			},
			{
				"kind":    "page",
				"skipped": true,
				"reason":  "no trusted page command",
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("progressVerificationData mismatch:\n got:  %#v\n want: %#v", got, want)
	}

	// Explicit omitempty pins (documenting intent beyond the DeepEqual above).
	if _, ok := got["baseline"]; ok {
		t.Errorf("baseline must be omitted when false, got %v", got["baseline"])
	}
	if _, ok := got["passed"]; ok {
		t.Errorf("passed must be omitted when false (a check failed), got %v", got["passed"])
	}
}

// TestProgressVerificationData_PassingResult pins the passing shape: passed is
// present (true) and the optional keys stay omitted when absent.
func TestProgressVerificationData_PassingResult(t *testing.T) {
	got := progressVerificationData(pvePassingResult(), "run-1")

	if got["passed"] != true {
		t.Errorf("passed = %v, want true (a clean passing run)", got["passed"])
	}
	if got["plan_revision"] != 3 {
		t.Errorf("plan_revision = %v, want 3", got["plan_revision"])
	}
	if got["run_id"] != "run-1" {
		t.Errorf("run_id = %v, want run-1", got["run_id"])
	}
	if _, ok := got["baseline"]; ok {
		t.Errorf("baseline must be absent when false, got %v", got["baseline"])
	}
	if _, ok := got["errors"]; ok {
		t.Errorf("errors must be absent when empty, got %v", got["errors"])
	}
	checks, ok := got["checks"].([]map[string]interface{})
	if !ok || len(checks) != 2 {
		t.Fatalf("checks = %#v, want a 2-element slice", got["checks"])
	}
}

// TestProgressVerificationData_NoChecksEmitsEmptySlice pins the never-null
// checks contract: a result with no checks emits an empty slice, not null.
func TestProgressVerificationData_NoChecksEmitsEmptySlice(t *testing.T) {
	res := &verify.Result{Errors: []string{"no checks resolved"}}
	got := progressVerificationData(res, "run-1")

	checks, ok := got["checks"].([]map[string]interface{})
	if !ok {
		t.Fatalf("checks = %T, want []map[string]interface{}", got["checks"])
	}
	if len(checks) != 0 {
		t.Errorf("checks length = %d, want 0 (an empty slice, not null)", len(checks))
	}
	if _, ok := got["plan_revision"]; ok {
		t.Errorf("plan_revision must be omitted when 0, got %v", got["plan_revision"])
	}
}

// TestProgressCompletePayload_NotVerified pins the not-verified progress_complete
// shape: run_id and not_verified_reason, with verified omitted (false) and no
// verification key.
func TestProgressCompletePayload_NotVerified(t *testing.T) {
	got := progressCompletePayload(nil, "run-2", "verification disabled")

	want := map[string]interface{}{
		"run_id":              "run-2",
		"not_verified_reason": "verification disabled",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("progressCompletePayload(nil) mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

// TestProgressCompletePayload_Verified pins the verified progress_complete
// shape: run_id, plan_revision, verified, and the nested verification map (the
// same shape as the standalone progress_verification payload).
func TestProgressCompletePayload_Verified(t *testing.T) {
	res := pvePassingResult()
	got := progressCompletePayload(res, "run-1", "")

	if got["verified"] != true {
		t.Errorf("verified = %v, want true", got["verified"])
	}
	if got["plan_revision"] != 3 {
		t.Errorf("plan_revision = %v, want 3", got["plan_revision"])
	}
	if got["run_id"] != "run-1" {
		t.Errorf("run_id = %v, want run-1", got["run_id"])
	}
	if _, ok := got["not_verified_reason"]; ok {
		t.Errorf("not_verified_reason must be absent for a result, got %v", got["not_verified_reason"])
	}
	// The nested verification is byte-consistent with the standalone payload.
	nested, ok := got["verification"].(map[string]interface{})
	if !ok {
		t.Fatalf("verification = %T, want map[string]interface{}", got["verification"])
	}
	if !reflect.DeepEqual(nested, progressVerificationData(res, "run-1")) {
		t.Errorf("nested verification = %#v, want the standalone progress_verification payload", nested)
	}
}

// ---------------------------------------------------------------------------
// Emit path: event order + correlating IDs (integration)
// ---------------------------------------------------------------------------

// TestPublishTurnProgressComplete_EventOrderAndCorrelation is the item's
// acceptance test: with a passing stored result, publishTurnProgressComplete
// emits EXACTLY [progress_verification, progress_complete] in that order; both
// carry the same run_id and plan_revision; and progress_complete has
// verified=true with a nested verification whose checks match the standalone
// progress_verification checks.
func TestPublishTurnProgressComplete_EventOrderAndCorrelation(t *testing.T) {
	ag, ch := pveAgent(t)
	ag.setTurnVerification(turnVerification{result: pvePassingResult()})

	ag.publishTurnProgressComplete()

	first := pveNextEvent(t, ch)
	second := pveNextEvent(t, ch)
	if first.Type != events.EventTypeProgressVerification {
		t.Fatalf("first event = %q, want progress_verification", first.Type)
	}
	if second.Type != events.EventTypeProgressComplete {
		t.Fatalf("second event = %q, want progress_complete", second.Type)
	}
	pveNoMoreEvents(t, ch, 300*time.Millisecond)

	standalone := pveData(t, first)
	complete := pveData(t, second)

	// Correlation: both events carry the same run_id and plan_revision.
	if standalone["run_id"] != "run-1" {
		t.Errorf("progress_verification run_id = %v, want run-1", standalone["run_id"])
	}
	if complete["run_id"] != standalone["run_id"] {
		t.Errorf("progress_complete run_id = %v, want the progress_verification run_id %v", complete["run_id"], standalone["run_id"])
	}
	if standalone["plan_revision"] != 3 || complete["plan_revision"] != 3 {
		t.Errorf("plan_revision = (%v, %v), want (3, 3) on both events", standalone["plan_revision"], complete["plan_revision"])
	}

	// The verdict: verified=true, and the nested verification matches the
	// standalone event's checks.
	if complete["verified"] != true {
		t.Errorf("verified = %v, want true", complete["verified"])
	}
	nested, ok := complete["verification"].(map[string]interface{})
	if !ok {
		t.Fatalf("progress_complete verification = %T, want map[string]interface{}", complete["verification"])
	}
	standaloneChecks, _ := standalone["checks"].([]map[string]interface{})
	nestedChecks, _ := nested["checks"].([]map[string]interface{})
	if !reflect.DeepEqual(standaloneChecks, nestedChecks) {
		t.Errorf("nested verification checks = %#v, want the standalone progress_verification checks %#v", nestedChecks, standaloneChecks)
	}
}

// TestPublishTurnProgressComplete_FailingResult pins the failing verdict:
// progress_complete still follows progress_verification, but verified is
// omitted (false) and the nested verification carries the failing checks.
func TestPublishTurnProgressComplete_FailingResult(t *testing.T) {
	ag, ch := pveAgent(t)
	ag.setTurnVerification(turnVerification{result: pveFailingResult()})

	ag.publishTurnProgressComplete()

	first := pveNextEvent(t, ch)
	second := pveNextEvent(t, ch)
	if first.Type != events.EventTypeProgressVerification {
		t.Fatalf("first event = %q, want progress_verification", first.Type)
	}
	if second.Type != events.EventTypeProgressComplete {
		t.Fatalf("second event = %q, want progress_complete", second.Type)
	}

	complete := pveData(t, second)
	if _, ok := complete["verified"]; ok {
		t.Errorf("verified must be omitted for a failing result, got %v", complete["verified"])
	}
	if complete["run_id"] != pveData(t, first)["run_id"] {
		t.Errorf("progress_complete run_id = %v, want the progress_verification run_id", complete["run_id"])
	}
	nested, ok := complete["verification"].(map[string]interface{})
	if !ok {
		t.Fatalf("progress_complete verification = %T, want map[string]interface{}", complete["verification"])
	}
	checks, _ := nested["checks"].([]map[string]interface{})
	if len(checks) != 2 {
		t.Fatalf("nested checks length = %d, want 2", len(checks))
	}
	if checks[0]["kind"] != "build" {
		t.Errorf("nested checks[0].kind = %v, want build", checks[0]["kind"])
	}
	if _, ok := checks[0]["passed"]; ok {
		t.Errorf("the failing build check must omit passed, got %v", checks[0]["passed"])
	}
}

// TestPublishTurnProgressComplete_NotVerified pins the no-result case: with an
// empty stored verification state (the hook never ran) exactly ONE event is
// emitted — progress_complete with verified omitted and
// not_verified_reason set — and NO progress_verification.
func TestPublishTurnProgressComplete_NotVerified(t *testing.T) {
	ag, ch := pveAgent(t) // configManager == nil, no stored result

	ag.publishTurnProgressComplete()

	ev := pveNextEvent(t, ch)
	if ev.Type != events.EventTypeProgressComplete {
		t.Fatalf("event = %q, want progress_complete (no result → no progress_verification)", ev.Type)
	}
	pveNoMoreEvents(t, ch, 300*time.Millisecond)

	data := pveData(t, ev)
	if _, ok := data["verified"]; ok {
		t.Errorf("verified must be omitted when there is no result, got %v", data["verified"])
	}
	if data["not_verified_reason"] != "verification disabled" {
		t.Errorf("not_verified_reason = %v, want %q", data["not_verified_reason"], "verification disabled")
	}
	if _, ok := data["verification"]; ok {
		t.Errorf("verification must be absent when there is no result, got %v", data["verification"])
	}
	if data["run_id"] != "run-1" {
		t.Errorf("run_id = %v, want run-1", data["run_id"])
	}
}

// TestPublishTurnProgressComplete_SubagentSilent pins the subagent guard: a
// subagent turn (subagentDepth > 0) shares the parent's event bus but carries a
// fresh (empty) session id, so it must emit NO progress events — a subagent is
// part of the parent's run, not an independent run. Even with a stored
// verification result, publishTurnProgressComplete stays silent.
func TestPublishTurnProgressComplete_SubagentSilent(t *testing.T) {
	ag, ch := pveAgent(t)
	ag.subagentDepth = 1
	ag.setTurnVerification(turnVerification{result: pvePassingResult()})

	ag.publishTurnProgressComplete()

	// No event of any kind may arrive for a subagent turn.
	pveNoMoreEvents(t, ch, 300*time.Millisecond)
}

// ---------------------------------------------------------------------------
// notVerifiedReason: the reason branches
// ---------------------------------------------------------------------------

// TestProgressComplete_NotVerifiedReason pins every branch of the
// not-verified reason (mirroring the SP-149 hook's guard conditions): a nil
// config manager or a config with verification disabled → "verification
// disabled"; verification enabled but no code change → "no code changes this
// turn"; verification enabled with a code change → "verification did not run
// this turn".
func TestProgressComplete_NotVerifiedReason(t *testing.T) {
	// Nil receiver and nil config manager.
	var nilAgent *Agent
	if got := nilAgent.notVerifiedReason(); got != "verification disabled" {
		t.Errorf("nil receiver reason = %q, want %q", got, "verification disabled")
	}
	if got := NewTestAgent().notVerifiedReason(); got != "verification disabled" {
		t.Errorf("nil config manager reason = %q, want %q", got, "verification disabled")
	}

	// Config present, verification disabled (the default).
	mgrDisabled, cleanupDisabled := configuration.NewTestManager(t)
	t.Cleanup(cleanupDisabled)
	agDisabled := NewTestAgent()
	agDisabled.configManager = mgrDisabled
	if got := agDisabled.notVerifiedReason(); got != "verification disabled" {
		t.Errorf("disabled-config reason = %q, want %q", got, "verification disabled")
	}

	// Config present, verification enabled.
	mgrEnabled, cleanupEnabled := configuration.NewTestManager(t)
	t.Cleanup(cleanupEnabled)
	if err := mgrEnabled.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Verification = &configuration.VerificationConfig{Enabled: true}
		return nil
	}); err != nil {
		t.Fatalf("configure verification: %v", err)
	}

	// Enabled but the turn changed no code.
	agNoChanges := NewTestAgent()
	agNoChanges.configManager = mgrEnabled
	if got := agNoChanges.notVerifiedReason(); got != "no code changes this turn" {
		t.Errorf("enabled+no-changes reason = %q, want %q", got, "no code changes this turn")
	}

	// Enabled and the turn changed code: the hook still did not run.
	tracker := NewChangeTracker(nil, "pve-reason")
	tracker.MarkTurnStart()
	if err := tracker.TrackFileWriteState("/ws/app.go", "old", "new", true); err != nil {
		t.Fatalf("TrackFileWriteState: %v", err)
	}
	agChanged := NewTestAgent()
	agChanged.configManager = mgrEnabled
	agChanged.changeTracker = tracker
	if got := agChanged.notVerifiedReason(); got != "verification did not run this turn" {
		t.Errorf("enabled+changed reason = %q, want %q", got, "verification did not run this turn")
	}
}
