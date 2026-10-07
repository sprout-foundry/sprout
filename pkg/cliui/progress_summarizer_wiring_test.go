//go:build !js

package cliui

// progress_summarizer_wiring_test.go — the call-site tests: the terminal
// subscriber's progress-event handler invokes the optional summarizer
// where the spec says (the progress event path), through the
// summarizer-role metering seam, using an injected stub rather than a
// live model. They also pin the two fallbacks the spec demands — a
// timeout or an error falls back to the deterministic template — and the
// invariant that no success summary is emitted without a passing
// verification event, via the real render path (HandleProgressEvent →
// summarizeProgressEvent → stub).

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// errSummarizerDown is the injected summarizer failure for the error
// fallback test.
var errSummarizerDown = errors.New("summarizer provider down")

// summaryHarness drives the terminal render path with an injected
// summarizer stub and a usage recorder, so the call site is exercised
// end to end without a real provider.
type summaryHarness struct {
	state    *TerminalSubscriberState
	stub     *fakeModelClient
	usage    []SummaryUsage
	indicate *console.ActivityIndicator
	footer   *console.StatusFooter
	output   func() string
}

func newSummaryHarness(t *testing.T, stub *fakeModelClient) *summaryHarness {
	t.Helper()
	state := NewTerminalSubscriberState(nil, nil)
	state.SetProgressSummarizerModelClient(stub)
	h := &summaryHarness{
		state:    state,
		stub:     stub,
		indicate: console.NewActivityIndicator(&bytes.Buffer{}),
		footer:   console.NewStatusFooter(&bytes.Buffer{}, nil),
	}
	state.SetProgressSummarizerUsage(func(u SummaryUsage) { h.usage = append(h.usage, u) })
	return h
}

func (h *summaryHarness) render(t *testing.T, evtType string, data map[string]interface{}) string {
	t.Helper()
	var out string
	out = captureStdout(t, func() {
		h.state.HandleProgressEvent(evtType, data, h.indicate, h.footer)
	})
	return out
}

// TestHandleProgressEventInvokesSummarizerAtCallSite pins the wiring
// rule: the progress-event render path calls the summarizer (the injected
// stub), routes the call through the meter, and prints the model-written
// summary, not the template.
func TestHandleProgressEventInvokesSummarizerAtCallSite(t *testing.T) {
	stub := &fakeModelClient{result: "Run complete — every check green."}
	h := newSummaryHarness(t, stub)

	out := h.render(t, events.EventTypeProgressComplete, summarizerCompleteVerified)

	if stub.calls != 1 {
		t.Fatalf("summarizer calls = %d, want 1 (the call site must invoke it)", stub.calls)
	}
	if !strings.Contains(out, "Run complete — every check green.") {
		t.Errorf("terminal output %q does not carry the model-written summary", out)
	}
	if len(h.usage) != 1 {
		t.Fatalf("metered usage entries = %d, want 1", len(h.usage))
	}
	if h.usage[0].PromptTokens <= 0 || h.usage[0].CompletionTokens <= 0 {
		t.Errorf("metered usage = %+v, want positive token counts", h.usage[0])
	}
}

// TestHandleProgressEventSummarizerErrorFallsBackToTemplate proves a
// summarizer error does not surface — the deterministic template is
// printed instead.
func TestHandleProgressEventSummarizerErrorFallsBackToTemplate(t *testing.T) {
	stub := &fakeModelClient{err: errSummarizerDown}
	h := newSummaryHarness(t, stub)

	out := h.render(t, events.EventTypeProgressComplete, summarizerCompleteVerified)

	if stub.calls != 1 {
		t.Fatalf("summarizer calls = %d, want 1", stub.calls)
	}
	if !strings.Contains(out, "Run complete — verified (Checks: 3/3 passed)") {
		t.Errorf("terminal output %q does not carry the fallback template", out)
	}
	if len(h.usage) != 0 {
		t.Errorf("metered usage = %+v, want none for a failed call", h.usage)
	}
}

// TestHandleProgressEventSummarizerTimeoutFallsBackToTemplate proves a
// hung summarizer cannot stall the render path: the call is bounded and
// the template is printed when the bound elapses.
func TestHandleProgressEventSummarizerTimeoutFallsBackToTemplate(t *testing.T) {
	// Shrink the bound so the render-path timeout is exercised in
	// milliseconds rather than the production 5s.
	orig := progressSummaryTimeout
	progressSummaryTimeout = 50 * time.Millisecond
	t.Cleanup(func() { progressSummaryTimeout = orig })

	stub := &fakeModelClient{result: "unused", block: make(chan struct{})}
	h := newSummaryHarness(t, stub)

	start := time.Now()
	out := h.render(t, events.EventTypeProgressComplete, summarizerCompleteVerified)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("render took %s, want it bounded near %s", elapsed, progressSummaryTimeout)
	}
	if !strings.Contains(out, "Run complete — verified (Checks: 3/3 passed)") {
		t.Errorf("terminal output %q does not carry the timeout fallback template", out)
	}
	if len(h.usage) != 0 {
		t.Errorf("metered usage = %+v, want none for a timed-out call", h.usage)
	}
}

// TestHandleProgressEventNoSuccessWithoutPassingVerification is the
// rule-breaker: a stub that insists on success must not produce a
// success summary when the event carries no passing verification — the
// model is never consulted, and the printed line is the non-success
// template.
func TestHandleProgressEventNoSuccessWithoutPassingVerification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		evtType   string
		data      map[string]interface{}
		wantLine  string
		wantNoSub string
	}{
		{
			name:      "failing checks",
			evtType:   events.EventTypeProgressVerification,
			data:      summarizerVerificationFailing,
			wantLine:  "Checks: 4/6 passed",
			wantNoSub: "Success",
		},
		{
			name:      "unverified complete",
			evtType:   events.EventTypeProgressComplete,
			data:      summarizerCompleteUnverified,
			wantLine:  "Run complete — not verified (verification disabled)",
			wantNoSub: "Success",
		},
		{
			name:      "complete without the verified flag",
			evtType:   events.EventTypeProgressComplete,
			data:      map[string]interface{}{"run_id": "run-1", "not_verified_reason": "no code changes this turn"},
			wantLine:  "Run complete — not verified (no code changes this turn)",
			wantNoSub: "Success",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &fakeModelClient{result: "Success! Everything passed and the run is complete."}
			h := newSummaryHarness(t, stub)

			out := h.render(t, tc.evtType, tc.data)

			if stub.calls != 0 {
				t.Errorf("summarizer calls = %d, want 0 (no success summary may be generated)", stub.calls)
			}
			if !strings.Contains(out, tc.wantLine) {
				t.Errorf("terminal output %q does not carry %q", out, tc.wantLine)
			}
			if strings.Contains(out, tc.wantNoSub) {
				t.Errorf("terminal output %q states success without a passing verification", out)
			}
			if len(h.usage) != 0 {
				t.Errorf("metered usage = %+v, want none when the model is not consulted", h.usage)
			}
		})
	}
}

// TestHandleProgressEventQuestionNeverConsulted pins that the
// progress_question path (which the terminal deliberately does not
// render) never invokes the summarizer.
func TestHandleProgressEventQuestionNeverConsulted(t *testing.T) {
	stub := &fakeModelClient{result: "Needs a decision: proceed?"}
	h := newSummaryHarness(t, stub)

	out := h.render(t, events.EventTypeProgressQuestion, map[string]interface{}{"question": "proceed?"})

	if stub.calls != 0 {
		t.Errorf("summarizer calls = %d, want 0 for a question event", stub.calls)
	}
	if out != "" {
		t.Errorf("question event printed %q, want nothing", out)
	}
}

// TestHandleProgressEventConfiguredRoleMetersThroughAgent proves the
// production metering path (no test recorder): with a configured
// summarizer role resolved to the in-memory test provider, a metered
// call is booked into the chat agent's per-role metrics under the
// summarizer role.
func TestHandleProgressEventConfiguredRoleMetersThroughAgent(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		if cfg.Roles == nil {
			cfg.Roles = make(map[string]configuration.RoleConfig)
		}
		cfg.Roles[configuration.RoleSummarizer] = configuration.RoleConfig{Provider: "test", Model: "summarizer-model"}
		return nil
	}); err != nil {
		t.Fatalf("configure summarizer role: %v", err)
	}

	ag := agent.NewTestAgentWithConfigManager(mgr)
	state := NewTerminalSubscriberState(mgr, ag)
	indicator := console.NewActivityIndicator(&bytes.Buffer{})
	footer := console.NewStatusFooter(&bytes.Buffer{}, nil)

	data := map[string]interface{}{
		"run_id":   "run-1",
		"verified": true,
		"verification": map[string]interface{}{
			"passed": true,
			"checks": []interface{}{
				map[string]interface{}{"kind": "build", "passed": true},
			},
		},
	}
	_ = captureStdout(t, func() {
		state.HandleProgressEvent(events.EventTypeProgressComplete, data, indicator, footer)
	})

	var found bool
	for _, ru := range ag.GetRoleUsage() {
		if ru.Role == configuration.RoleSummarizer {
			found = true
			if ru.Calls == 0 {
				t.Errorf("summarizer role recorded no calls: %+v", ru)
			}
			if ru.PromptTokens <= 0 || ru.CompletionTokens <= 0 {
				t.Errorf("summarizer role recorded no tokens: %+v", ru)
			}
		}
	}
	if !found {
		t.Errorf("summarizer role missing from role usage: %+v", ag.GetRoleUsage())
	}
}
