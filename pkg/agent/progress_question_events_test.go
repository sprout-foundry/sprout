//go:build !js

// progress_question_events_test.go — the SP-151 §151a item-151.4 acceptance
// tests: the plan context a progress_question is correlated to (the active
// scope item + plan revision), the exact payload shape of the emitted event,
// and the "alongside ask_user_request" wiring at the ask_user entry points.
//
// The pure plan-context builder is asserted directly (planQuestionContext on a
// bare agent whose tracker is seeded). The emit path is driven through the
// real method — publishProgressQuestion on a bare Agent wired to a captured
// EventBus — mirroring the bare-`*Agent` + captured-`EventBus` convention in
// progress_verification_events_test.go (151.3) and scope_milestones_test.go
// (151.2). The "alongside" pairing is by construction: the emit sits at the
// same entry point as ask_user_request and before the channel, so the
// progress_question always precedes it for one question.

package agent

import (
	"context"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// Fixtures + helpers
// ---------------------------------------------------------------------------

// pqTracker seeds a scope tracker with the given active (started, not
// finished) scope ids and a cached plan revision, so planQuestionContext /
// publishProgressQuestion can be asserted without driving a todo_write.
func pqTracker(activeScopes []string, planRev int) *scopeMilestoneTracker {
	tr := newScopeMilestoneTracker()
	now := time.Now()
	for i, id := range activeScopes {
		// Distinct, monotonically increasing startedAt so the most recently
		// started scope is unambiguously the last one in the slice.
		tr.scopes[id] = &scopeMilestoneState{started: true, startedAt: now.Add(time.Duration(i) * time.Second)}
	}
	tr.planRev = planRev
	tr.planLoaded = true
	return tr
}

// pqAgent builds a bare agent with session id "run-1" and a captured EventBus,
// so publishProgressQuestion runs the real emit path and its events can be
// asserted.
func pqAgent(t *testing.T) (*Agent, <-chan events.UIEvent) {
	t.Helper()

	ag := &Agent{state: NewAgentStateManager(false)}
	ag.initSubManagers()
	ag.SetSessionID("run-1")

	bus := events.NewEventBus()
	ch := bus.Subscribe("progress-question-test")
	t.Cleanup(func() { bus.Unsubscribe("progress-question-test") })
	ag.SetEventBus(bus)
	return ag, ch
}

// pqNextEvent reads the next event from ch (or fails on the deadline).
func pqNextEvent(t *testing.T, ch <-chan events.UIEvent) events.UIEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a progress_question event")
		return events.UIEvent{}
	}
}

// pqData returns an event's payload as a map (the payloads are built as
// map[string]interface{} so the metadata merge applies).
func pqData(t *testing.T, ev events.UIEvent) map[string]interface{} {
	t.Helper()
	m, ok := ev.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("payload is %T, want map[string]interface{}", ev.Data)
	}
	return m
}

// ---------------------------------------------------------------------------
// planQuestionContext: the active scope + plan revision
// ---------------------------------------------------------------------------

// TestPlanQuestionContext pins the plan context a progress_question is
// correlated to: the active scope (the most recently started scope that has
// not finished) and the cached plan revision.
func TestPlanQuestionContext(t *testing.T) {
	t.Run("activeScopeWithPlan", func(t *testing.T) {
		ag := &Agent{state: NewAgentStateManager(false)}
		ag.scopeMilestones = pqTracker([]string{"signup-form"}, 2)

		scope, rev := ag.planQuestionContext()
		if scope != "signup-form" || rev != 2 {
			t.Errorf("planQuestionContext = (%q, %d), want (signup-form, 2)", scope, rev)
		}
	})

	t.Run("mostRecentlyStartedWins", func(t *testing.T) {
		ag := &Agent{state: NewAgentStateManager(false)}
		// "billing" is the most recently started of the two active scopes.
		ag.scopeMilestones = pqTracker([]string{"signup-form", "billing"}, 1)

		scope, _ := ag.planQuestionContext()
		if scope != "billing" {
			t.Errorf("active scope = %q, want billing (the most recently started)", scope)
		}
	})

	t.Run("noActiveScope", func(t *testing.T) {
		ag := &Agent{state: NewAgentStateManager(false)}
		ag.scopeMilestones = pqTracker(nil, 3)

		scope, rev := ag.planQuestionContext()
		if scope != "" || rev != 3 {
			t.Errorf("planQuestionContext = (%q, %d), want (, 3)", scope, rev)
		}
	})

	t.Run("noActiveScopeNoPlan", func(t *testing.T) {
		ag := &Agent{state: NewAgentStateManager(false)}
		ag.scopeMilestones = pqTracker(nil, 0)

		scope, rev := ag.planQuestionContext()
		if scope != "" || rev != 0 {
			t.Errorf("planQuestionContext = (%q, %d), want (, 0)", scope, rev)
		}
	})

	t.Run("finishedScopeNotActive", func(t *testing.T) {
		ag := &Agent{state: NewAgentStateManager(false)}
		ag.scopeMilestones = newScopeMilestoneTracker()
		ag.scopeMilestones.scopes["done"] = &scopeMilestoneState{started: true, finished: true, startedAt: time.Now()}
		ag.scopeMilestones.planRev = 1
		ag.scopeMilestones.planLoaded = true

		if scope, _ := ag.planQuestionContext(); scope != "" {
			t.Errorf("active scope = %q, want empty (the scope finished)", scope)
		}
	})

	t.Run("nilTracker", func(t *testing.T) {
		ag := &Agent{state: NewAgentStateManager(false)}
		// scopeMilestones stays nil (a bare test agent).

		scope, rev := ag.planQuestionContext()
		if scope != "" || rev != 0 {
			t.Errorf("planQuestionContext = (%q, %d), want (, 0)", scope, rev)
		}
	})
}

// ---------------------------------------------------------------------------
// publishProgressQuestion: exact payload (the emit path)
// ---------------------------------------------------------------------------

// TestPublishProgressQuestion_Payload is the item's acceptance test: on a bare
// agent with session id "run-1" and an active scope "signup-form" under plan
// revision 2, publishProgressQuestion emits EXACTLY one progress_question with
// the full payload — run_id, question, header, plan_revision, scope_id, and
// the options (each carrying only its non-empty sub-keys) — and never
// why_it_matters.
func TestPublishProgressQuestion_Payload(t *testing.T) {
	ag, ch := pqAgent(t)
	ag.scopeMilestones = pqTracker([]string{"signup-form"}, 2)

	ag.publishProgressQuestion(tools.AskUserRequest{
		Question: "Which DB?",
		Header:   "Storage",
		Options: []tools.AskUserOption{
			{Label: "Postgres"},
			{Label: "SQLite", Value: "sqlite", Description: "local"},
		},
	})

	ev := pqNextEvent(t, ch)
	if ev.Type != events.EventTypeProgressQuestion {
		t.Fatalf("event = %q, want progress_question", ev.Type)
	}
	data := pqData(t, ev)

	if data["run_id"] != "run-1" {
		t.Errorf("run_id = %v, want run-1 (the session id)", data["run_id"])
	}
	if data["question"] != "Which DB?" {
		t.Errorf("question = %v, want %q", data["question"], "Which DB?")
	}
	if data["header"] != "Storage" {
		t.Errorf("header = %v, want Storage", data["header"])
	}
	if data["plan_revision"] != 2 {
		t.Errorf("plan_revision = %v, want 2", data["plan_revision"])
	}
	if data["scope_id"] != "signup-form" {
		t.Errorf("scope_id = %v, want signup-form", data["scope_id"])
	}

	options, ok := data["options"].([]map[string]interface{})
	if !ok {
		t.Fatalf("options = %T, want []map[string]interface{}", data["options"])
	}
	if len(options) != 2 {
		t.Fatalf("options length = %d, want 2", len(options))
	}
	if options[0]["label"] != "Postgres" {
		t.Errorf("options[0] = %#v, want {label: Postgres}", options[0])
	}
	if _, ok := options[0]["value"]; ok {
		t.Errorf("options[0] must omit value when empty, got %#v", options[0])
	}
	if _, ok := options[0]["description"]; ok {
		t.Errorf("options[0] must omit description when empty, got %#v", options[0])
	}
	if options[1]["label"] != "SQLite" || options[1]["value"] != "sqlite" || options[1]["description"] != "local" {
		t.Errorf("options[1] = %#v, want {label: SQLite, value: sqlite, description: local}", options[1])
	}

	if _, ok := data["why_it_matters"]; ok {
		t.Errorf("why_it_matters must be absent (no source field), got %v", data["why_it_matters"])
	}
}

// TestPublishProgressQuestion_OmitsOptionalKeys pins the omitempty contract:
// a freeform question on a plan-less bare agent carries only run_id and
// question — plan_revision, scope_id, header, and options are all omitted.
func TestPublishProgressQuestion_OmitsOptionalKeys(t *testing.T) {
	ag, ch := pqAgent(t)
	ag.scopeMilestones = pqTracker(nil, 0)

	ag.publishProgressQuestion(tools.AskUserRequest{Question: "Continue?"})

	ev := pqNextEvent(t, ch)
	if ev.Type != events.EventTypeProgressQuestion {
		t.Fatalf("event = %q, want progress_question", ev.Type)
	}
	data := pqData(t, ev)

	if data["run_id"] != "run-1" || data["question"] != "Continue?" {
		t.Errorf("run_id/question = (%v, %v), want (run-1, Continue?)", data["run_id"], data["question"])
	}
	for _, key := range []string{"plan_revision", "scope_id", "header", "options", "why_it_matters"} {
		if _, ok := data[key]; ok {
			t.Errorf("%q must be omitted, got %v", key, data[key])
		}
	}
}

// TestPublishProgressQuestion_NilReceiver pins the no-op contract: a nil
// receiver emits nothing and does not panic.
func TestPublishProgressQuestion_NilReceiver(t *testing.T) {
	var ag *Agent
	// Must not panic.
	ag.publishProgressQuestion(tools.AskUserRequest{Question: "Which DB?"})
}

// ---------------------------------------------------------------------------
// Alongside ask_user_request: the wiring at the entry point
// ---------------------------------------------------------------------------

// TestPublishProgressQuestion_WiredIntoAskService is the "alongside" proof:
// driving the ask flow through the AskUserService entry point emits a
// progress_question (before the channel is engaged). The bare agent has no
// WebUI client and stdin is not a TTY, so the ask channel is unavailable and
// Ask returns ErrAskUserNoChannel — but the progress_question was published
// before the channel, so it still lands on the bus. The pairing is by
// construction: the emit sits at the same entry point, before the channel.
func TestPublishProgressQuestion_WiredIntoAskService(t *testing.T) {
	ag, ch := pqAgent(t)
	ag.scopeMilestones = pqTracker([]string{"signup-form"}, 2)

	svc := newAgentAskUserService(ag)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// The ask itself fails (no channel) — that is expected here; we are
	// asserting the progress_question that precedes it, not the answer.
	_, _ = svc.Ask(ctx, tools.AskUserRequest{Question: "Which DB?", Header: "Storage"})

	ev := pqNextEvent(t, ch)
	if ev.Type != events.EventTypeProgressQuestion {
		t.Fatalf("event = %q, want progress_question", ev.Type)
	}
	data := pqData(t, ev)
	if data["run_id"] != "run-1" || data["question"] != "Which DB?" {
		t.Errorf("payload = %#v, want run_id=run-1, question=Which DB?", data)
	}
	if data["scope_id"] != "signup-form" {
		t.Errorf("scope_id = %v, want signup-form", data["scope_id"])
	}
	if data["plan_revision"] != 2 {
		t.Errorf("plan_revision = %v, want 2", data["plan_revision"])
	}
}
