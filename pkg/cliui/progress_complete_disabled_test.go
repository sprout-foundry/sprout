//go:build !js

package cliui

// progress_complete_disabled_test.go — the rule-breaker test for "a
// disabled turn shows no not-verified notice on any path": a scripted
// turn with verification disabled (the CLI default) drives the real
// emit path (agent → event bus → captured payload) and that exact
// payload must render nothing in the CLI — the terminal subscriber
// prints no line. The counter-case (an enabled turn that changed no
// code) still shows the notice. The web UI renderer is pinned by
// webui/src/utils/progressSummary.test.ts on the same payloads.

import (
	"bytes"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// disabledTurnAgent builds a bare agent wired to a captured event bus,
// with the given configuration manager (nil for the no-config case) and
// session id "run-1", so the real emit path (PublishTurnProgressComplete
// → publishEvent) can be driven and its events asserted.
func disabledTurnAgent(t *testing.T, mgr *configuration.Manager) (*agent.Agent, <-chan events.UIEvent) {
	t.Helper()

	ag := agent.NewTestAgentWithConfigManager(mgr)
	ag.SetSessionID("run-1")
	bus := events.NewEventBus()
	ch := bus.Subscribe("disabled-turn-test")
	t.Cleanup(func() { bus.Unsubscribe("disabled-turn-test") })
	ag.SetEventBus(bus)
	return ag, ch
}

// disabledTurnNextEvent reads the next event from ch (or fails on the
// deadline).
func disabledTurnNextEvent(t *testing.T, ch <-chan events.UIEvent) events.UIEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a progress event")
		return events.UIEvent{}
	}
}

// disabledTurnData returns an event's payload as a map (the payloads are
// built as map[string]interface{} so the metadata merge applies).
func disabledTurnData(t *testing.T, ev events.UIEvent) map[string]interface{} {
	t.Helper()
	m, ok := ev.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("payload is %T, want map[string]interface{}", ev.Data)
	}
	return m
}

// TestDisabledTurnNoNotVerifiedNotice is the disabled half of the rule: a
// scripted turn with verification disabled (the default) emits a
// progress_complete event WITHOUT not_verified_reason, and the CLI
// renderer says nothing on that exact payload — the template returns ""
// and the terminal handler (whose empty-summary guard suppresses the
// line) prints nothing.
func TestDisabledTurnNoNotVerifiedNotice(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	ag, ch := disabledTurnAgent(t, mgr)
	ag.PublishTurnProgressComplete()

	ev := disabledTurnNextEvent(t, ch)
	if ev.Type != events.EventTypeProgressComplete {
		t.Fatalf("event = %q, want progress_complete", ev.Type)
	}
	data := disabledTurnData(t, ev)
	if _, ok := data["not_verified_reason"]; ok {
		t.Errorf("a disabled turn must not carry not_verified_reason, got %v", data["not_verified_reason"])
	}
	if _, ok := data["verified"]; ok {
		t.Errorf("verified must be omitted when there is no result, got %v", data["verified"])
	}
	if data["run_id"] != "run-1" {
		t.Errorf("run_id = %v, want run-1", data["run_id"])
	}

	// The CLI renderer on that exact payload: nothing to say.
	if got := ProgressCompleteSummary(data); got != "" {
		t.Errorf("ProgressCompleteSummary(disabled payload) = %q, want \"\"", got)
	}

	// And the terminal handler prints nothing for it.
	t.Setenv("NO_COLOR", "1")
	state := NewTerminalSubscriberState(nil, nil)
	indicator := console.NewActivityIndicator(&bytes.Buffer{})
	footer := console.NewStatusFooter(&bytes.Buffer{}, nil)
	out := captureStdout(t, func() {
		state.HandleProgressEvent(events.EventTypeProgressComplete, data, indicator, footer)
	})
	if out != "" {
		t.Errorf("a disabled-turn progress_complete must print nothing in the terminal; got %q", out)
	}
}

// TestEnabledNoChangeTurnStillNotices is the counter-case of the rule:
// verification enabled, turn changed no code → the progress_complete
// event carries the reason and the CLI renderer shows the notice.
func TestEnabledNoChangeTurnStillNotices(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.Verification = &configuration.VerificationConfig{Enabled: true}
		return nil
	}); err != nil {
		t.Fatalf("configure verification: %v", err)
	}

	ag, ch := disabledTurnAgent(t, mgr)
	ag.PublishTurnProgressComplete()

	ev := disabledTurnNextEvent(t, ch)
	if ev.Type != events.EventTypeProgressComplete {
		t.Fatalf("event = %q, want progress_complete", ev.Type)
	}
	data := disabledTurnData(t, ev)
	if data["not_verified_reason"] != "no code changes this turn" {
		t.Fatalf("not_verified_reason = %v, want %q", data["not_verified_reason"], "no code changes this turn")
	}

	want := "Run complete — not verified (no code changes this turn)"
	if got := ProgressCompleteSummary(data); got != want {
		t.Errorf("ProgressCompleteSummary(enabled no-change payload) = %q, want %q", got, want)
	}
}
