package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

func TestPersonaBudget_ReviewerDefaults(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	b := runner.personaBudget("code_reviewer")
	if b.iterations != 15 || b.duration != 8*time.Minute {
		t.Fatalf("reviewer budget = %+v, want 15 iterations / 8m", b)
	}
	if got := b.hardMaxIterations(); got != 15+subagentWrapUpIterations {
		t.Errorf("hardMaxIterations = %d", got)
	}
	if !runner.personaBudget("coder").isZero() {
		t.Error("coder should declare no budget")
	}
}

func TestCreateSubagent_MaxIterationsFollowsPersonaBudget(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	reviewer, err := runner.createSubagent(SubagentOptions{Persona: "reviewer"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reviewer.Shutdown()
	if reviewer.maxIterations != 15+subagentWrapUpIterations {
		t.Errorf("reviewer maxIterations = %d", reviewer.maxIterations)
	}

	coder, err := runner.createSubagent(SubagentOptions{Persona: "coder"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer coder.Shutdown()
	if coder.maxIterations != defaultSubagentMaxIterations {
		t.Errorf("coder maxIterations = %d, want default %d", coder.maxIterations, defaultSubagentMaxIterations)
	}
}

func TestResolveSubagentTimeout_PersonaBudget(t *testing.T) {
	_, runner := newReviewTestRunner(t)
	t.Setenv("SPROUT_TOOL_TIMEOUT", "")

	if got := runner.resolveSubagentTimeout(SubagentOptions{Persona: "reviewer"}); got != 8*time.Minute+subagentWrapUpGrace {
		t.Errorf("reviewer timeout = %s", got)
	}
	if got := runner.resolveSubagentTimeout(SubagentOptions{Persona: "coder"}); got != defaultSubagentTimeout {
		t.Errorf("coder timeout = %s, want %s", got, defaultSubagentTimeout)
	}
	if got := runner.resolveSubagentTimeout(SubagentOptions{Persona: "reviewer", Timeout: time.Minute}); got != time.Minute {
		t.Errorf("explicit timeout not honored: %s", got)
	}

	t.Setenv("SPROUT_TOOL_TIMEOUT", "3600")
	if got := runner.resolveSubagentTimeout(SubagentOptions{Persona: "reviewer"}); got != time.Hour {
		t.Errorf("env floor not applied: %s", got)
	}
}

func TestMonitorWrapUp_InjectsOnceAtIterationBudget(t *testing.T) {
	_, runner := newReviewTestRunner(t)
	sub, err := runner.createSubagent(SubagentOptions{Persona: "reviewer"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Shutdown()

	prev := subagentBudgetPollInterval
	subagentBudgetPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { subagentBudgetPollInterval = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var injected atomic.Bool
	done := make(chan struct{})
	go func() {
		monitorWrapUp(ctx, sub, subagentBudget{iterations: 3}, time.Now(), &injected)
		close(done)
	}()

	sub.state.SetCurrentIteration(1)
	select {
	case msg := <-sub.inputInjectionChan:
		t.Fatalf("injected before budget: %q", msg)
	case <-time.After(50 * time.Millisecond):
	}

	sub.state.SetCurrentIteration(3)
	select {
	case msg := <-sub.inputInjectionChan:
		if !strings.Contains(msg, "[budget]") || !strings.Contains(msg, "3-iteration") {
			t.Errorf("unexpected wrap-up message: %q", msg)
		}
	case <-ctx.Done():
		t.Fatal("wrap-up message never injected")
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("monitor did not exit after injecting")
	}
	if !injected.Load() {
		t.Error("injected flag not set")
	}
}

func TestMonitorWrapUp_TimeBudget(t *testing.T) {
	_, runner := newReviewTestRunner(t)
	sub, err := runner.createSubagent(SubagentOptions{Persona: "reviewer"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Shutdown()

	prev := subagentBudgetPollInterval
	subagentBudgetPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { subagentBudgetPollInterval = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go monitorWrapUp(ctx, sub, subagentBudget{duration: time.Minute}, time.Now().Add(-2*time.Minute), nil)

	select {
	case msg := <-sub.inputInjectionChan:
		if !strings.Contains(msg, "time budget") {
			t.Errorf("unexpected wrap-up message: %q", msg)
		}
	case <-ctx.Done():
		t.Fatal("time-budget wrap-up never injected")
	}
}

func TestComputerUseToolsBlockedInSubagents(t *testing.T) {
	_, runner := newReviewTestRunner(t)
	sub, err := runner.createSubagent(SubagentOptions{}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Shutdown()

	prevNames := computerUseToolNames
	computerUseToolNames = map[string]bool{"take_screenshot": true}
	t.Cleanup(func() { computerUseToolNames = prevNames })

	// Even a subagent whose active persona is computer_user (reachable only if
	// a config made it spawnable) must not drive the desktop.
	sub.state.SetActivePersona(personas.IDComputerUser)
	if !isComputerUseToolBlocked("take_screenshot", sub) {
		t.Error("computer-use tool allowed inside a subagent")
	}

	root := newIsolatedTestAgent(t)
	defer root.Shutdown()
	root.state.SetActivePersona(personas.IDComputerUser)
	if isComputerUseToolBlocked("take_screenshot", root) {
		t.Error("top-level computer_user should not be blocked")
	}
}

func TestLowContextSubagentStillNarrowedByPersona(t *testing.T) {
	parent, runner := newReviewTestRunner(t)
	sub, err := runner.createSubagent(SubagentOptions{Persona: "reviewer"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Shutdown()

	if len(sub.contextProfile.ToolAllowlist) == 0 {
		t.Skip("test client did not resolve to a low-context profile")
	}
	advertised := toolNames(sub.getOptimizedToolDefinitions(nil))
	for _, unwanted := range []string{"write_file", "edit_file", "commit", "web_search"} {
		if advertised[unwanted] {
			t.Errorf("low-context reviewer advertises %q", unwanted)
		}
	}
	if !advertised["read_file"] {
		t.Error("low-context reviewer lost read_file")
	}

	if len(parent.contextProfile.ToolAllowlist) > 0 && !toolNames(parent.getOptimizedToolDefinitions(nil))["write_file"] {
		t.Error("root agent's low-context allowlist was narrowed by persona")
	}
}

func TestEffectiveBudget_IterationOverrideRaisesCap(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	// The reviewer persona declares 15 iterations, which would otherwise cap
	// the run at 15+wrap-up. A caller asking for 500 must actually get 500.
	b := runner.effectiveBudget(SubagentOptions{Persona: "reviewer", MaxIterations: 500})
	if b.iterations != 500 {
		t.Errorf("soft budget = %d, want 500", b.iterations)
	}
	if got, want := b.hardMaxIterations(), 500+subagentWrapUpIterations; got != want {
		t.Errorf("hardMaxIterations = %d, want %d", got, want)
	}

	// A persona budget alone is still bounded by the default cap.
	if got := runner.effectiveBudget(SubagentOptions{Persona: "reviewer"}).hardMaxIterations(); got != 15+subagentWrapUpIterations {
		t.Errorf("persona hardMaxIterations = %d", got)
	}
}

func TestEffectiveBudget_OverrideClampedToCeiling(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	b := runner.effectiveBudget(SubagentOptions{MaxIterations: 5000})
	if b.iterations != subagentMaxIterationsCeiling {
		t.Errorf("soft budget = %d, want ceiling %d", b.iterations, subagentMaxIterationsCeiling)
	}
	// The wrap-up margin rides on top of the ceiling rather than being clamped
	// away, so even the largest permitted budget gets a final turn to report.
	if got, want := b.hardMaxIterations(), subagentMaxIterationsCeiling+subagentWrapUpIterations; got != want {
		t.Errorf("hardMaxIterations = %d, want %d", got, want)
	}
}

func TestEffectiveBudget_ExplicitTimeoutDrivesWrapUp(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	// Without an override the reviewer keeps its own 8-minute soft budget.
	if got := runner.effectiveBudget(SubagentOptions{Persona: "reviewer"}).duration; got != 8*time.Minute {
		t.Errorf("persona duration = %s, want 8m", got)
	}

	// An explicit timeout sets the soft budget a grace period before the hard
	// deadline, so the wrap-up fires instead of the run being cut off silently.
	b := runner.effectiveBudget(SubagentOptions{Persona: "reviewer", Timeout: 30 * time.Minute})
	if want := 30*time.Minute - subagentWrapUpGrace; b.duration != want {
		t.Errorf("override duration = %s, want %s", b.duration, want)
	}

	// A timeout shorter than the persona's time budget binds the hard deadline,
	// so the soft budget must follow it. Keeping the persona's 8m would put the
	// wrap-up after the 6m deadline, where it never fires.
	short := runner.effectiveBudget(SubagentOptions{Persona: "reviewer", Timeout: 6 * time.Minute})
	if want := 3 * time.Minute; short.duration != want {
		t.Errorf("short override duration = %s, want %s", short.duration, want)
	}

	// A timeout shorter than the grace leaves no room to wrap up.
	if got := runner.effectiveBudget(SubagentOptions{Timeout: time.Minute}).duration; got != 0 {
		t.Errorf("sub-grace timeout duration = %s, want 0", got)
	}
}

func TestCreateSubagent_HonorsIterationOverride(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	sub, err := runner.createSubagent(SubagentOptions{Persona: "reviewer", MaxIterations: 400}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Shutdown()
	if want := 400 + subagentWrapUpIterations; sub.maxIterations != want {
		t.Errorf("maxIterations = %d, want %d", sub.maxIterations, want)
	}
}

func TestPositiveIntArg(t *testing.T) {
	args := map[string]interface{}{
		"float":      float64(250),
		"fractional": float64(2.5),
		"int":        12,
		"zero":       float64(0),
		"negative":   float64(-5),
		"text":       "300",
	}
	for key, want := range map[string]int{
		"float": 250, "fractional": 0, "int": 12, "zero": 0, "negative": 0, "text": 0, "missing": 0,
	} {
		if got := positiveIntArg(args, key); got != want {
			t.Errorf("positiveIntArg(%q) = %d, want %d", key, got, want)
		}
	}
}

func TestSubagentRunRecord(t *testing.T) {
	base := &SubagentResult{ID: "t1", Elapsed: 1500 * time.Millisecond, Iterations: 12, ToolCalls: 20, TokensUsed: 900, Cost: 0.01}
	rec := subagentRunRecord("reviewer", base, 38, false, true)
	if rec["outcome"] != "completed" || rec["elapsed_ms"] != int64(1500) || rec["iterations"] != 12 ||
		rec["max_iterations"] != 38 || rec["wrap_up_injected"] != true || rec["persona"] != "reviewer" {
		t.Errorf("unexpected record: %v", rec)
	}

	for _, tc := range []struct {
		r    SubagentResult
		want string
	}{
		{SubagentResult{Cancelled: true, Error: errors.New("x")}, "cancelled"},
		{SubagentResult{Truncated: true}, "budget_exceeded"},
		{SubagentResult{Error: errors.New("boom")}, "error"},
	} {
		if got := subagentRunRecord("coder", &tc.r, 200, false, false)["outcome"]; got != tc.want {
			t.Errorf("outcome = %v, want %s", got, tc.want)
		}
	}
}

// The argument's contract is "raise this spawn's budget". A value below the
// persona's declared budget must not silently tighten it.
func TestEffectiveBudget_LowerOverrideIgnored(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	b := runner.effectiveBudget(SubagentOptions{Persona: "reviewer", MaxIterations: 5})
	if b.iterations != 15 {
		t.Errorf("soft budget = %d, want the persona's 15", b.iterations)
	}
	if got, want := b.hardMaxIterations(), 15+subagentWrapUpIterations; got != want {
		t.Errorf("hardMaxIterations = %d, want %d", got, want)
	}
}

// A long-but-plausible timeout is clamped to the ceiling, and an absurd one is
// rejected rather than overflowing the Duration multiplication into a
// negative value (which would silently restore the default timeout).
func TestSubagentTimeoutArg_ClampsAbsurdValues(t *testing.T) {
	if got := subagentTimeoutArg(map[string]interface{}{"t": float64(1e6)}, "t"); got != subagentTimeoutCeiling {
		t.Errorf("long timeout = %s, want ceiling %s", got, subagentTimeoutCeiling)
	}
	if got := subagentTimeoutArg(map[string]interface{}{"t": float64(1e18)}, "t"); got != 0 {
		t.Errorf("absurd timeout = %s, want rejected", got)
	}
	if got := subagentTimeoutArg(map[string]interface{}{"t": float64(90)}, "t"); got != 90*time.Second {
		t.Errorf("normal timeout = %s, want 90s", got)
	}
	if got := subagentTimeoutArg(map[string]interface{}{}, "t"); got != 0 {
		t.Errorf("missing timeout = %s, want zero", got)
	}
}
