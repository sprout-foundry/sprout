package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/personas"
)

func TestPersonaBudget_ReviewerDefaults(t *testing.T) {
	_, runner := newReviewTestRunner(t)

	b := runner.personaBudget("code_reviewer")
	if b.iterations != 30 || b.duration != 10*time.Minute {
		t.Fatalf("reviewer budget = %+v, want 30 iterations / 10m", b)
	}
	if got := b.hardMaxIterations(); got != 30+subagentWrapUpIterations {
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
	if reviewer.maxIterations != 30+subagentWrapUpIterations {
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

	if got := runner.resolveSubagentTimeout(SubagentOptions{Persona: "reviewer"}); got != 10*time.Minute+subagentWrapUpGrace {
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
	done := make(chan struct{})
	go func() {
		monitorWrapUp(ctx, sub, subagentBudget{iterations: 3}, time.Now())
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
	go monitorWrapUp(ctx, sub, subagentBudget{duration: time.Minute}, time.Now().Add(-2*time.Minute))

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
