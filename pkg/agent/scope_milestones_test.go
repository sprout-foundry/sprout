//go:build !js

// scope_milestones_test.go — the SP-151 §151a item-151.2 acceptance tests:
// a todo_write that starts or finishes a plan scope item (the todo "scope"
// field, SP-148 §148c) emits a progress_milestone event carrying the stable
// correlation ids (run id, plan revision, scope id), and the finished event
// additionally carries the files-touched count and the scope item's elapsed
// time.
//
// The scenarios are driven through the real hook — handleTodoWrite on a bare
// Agent wired to a captured EventBus — so the full path is exercised:
// coercion → todo write → observeScopeMilestones → publishEvent. A fixture
// plan on disk (two scope items, revision 1) provides the scope ids and
// titles, mirroring the fixture conventions in todo_plan_scope_test.go
// (148.6) and plan_context_test.go (148.5).

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent/changes"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// smFixturePlanJSON is a minimal valid SP-148 plan with two scope items
// (signup-form, billing), each covered by one acceptance item (the validator
// rule "every scope item needs at least one acceptance item"). revision is 1,
// so the milestones carry plan_revision=1.
const smFixturePlanJSON = `{
  "version": 1,
  "revision": 1,
  "created": "2026-10-05T00:00:00Z",
  "updated": "2026-10-05T00:00:00Z",
  "goal": "Ship the sign-up flow",
  "scope": [
    {"id": "signup-form", "title": "Sign-up form", "description": "Front-end sign-up form"},
    {"id": "billing", "title": "Billing integration"}
  ],
  "steps": [
    {"scope": "signup-form", "description": "Build the sign-up form"},
    {"scope": "billing", "description": "Wire the billing integration"}
  ],
  "acceptance": [
    {"id": "acc-signup", "scope": "signup-form", "check": "form renders", "kind": "page"},
    {"id": "acc-billing", "scope": "billing", "check": "billing module builds", "kind": "build"}
  ],
  "out_of_scope": []
}`

// smWritePlanFile writes content to .sprout/plan.json under root, so the plan
// lives exactly where planstore looks.
func smWritePlanFile(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0o755); err != nil {
		t.Fatalf("create .sprout dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sprout", "plan.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// smAgent builds a bare agent rooted at workspaceRoot with session id
// "run-1" and a captured EventBus, so handleTodoWrite runs the real milestone
// hook and its events can be asserted. changeTracker, when non-nil, is a bare
// tracker (nil view) that the test seeds directly.
func smAgent(t *testing.T, workspaceRoot string, changeTracker *changes.ChangeTracker) (*Agent, <-chan events.UIEvent) {
	t.Helper()

	ag := &Agent{state: NewAgentStateManager(false)}
	ag.initSubManagers()
	ag.SetWorkspaceRoot(workspaceRoot)
	ag.SetSessionID("run-1")
	ag.changeTracker = changeTracker

	bus := events.NewEventBus()
	ch := bus.Subscribe("scope-milestone-test")
	t.Cleanup(func() { bus.Unsubscribe("scope-milestone-test") })
	ag.SetEventBus(bus)
	return ag, ch
}

// smMilestoneEvent reads from ch until a progress_milestone event arrives (or
// the deadline), skipping any other event type.
func smMilestoneEvent(t *testing.T, ch <-chan events.UIEvent) events.UIEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type == events.EventTypeProgressMilestone {
				return ev
			}
			t.Errorf("unexpected event type %q (want progress_milestone)", ev.Type)
		case <-deadline:
			t.Fatal("timed out waiting for a progress_milestone event")
			return events.UIEvent{}
		}
	}
}

// smMilestoneData returns the milestone payload as a map.
func smMilestoneData(t *testing.T, ev events.UIEvent) map[string]interface{} {
	t.Helper()
	m, ok := ev.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("milestone payload is %T, want map[string]interface{}", ev.Data)
	}
	return m
}

// smNoMilestone asserts that no progress_milestone event arrives within the
// window — the de-duplication and "no transition" checks.
func smNoMilestone(t *testing.T, ch <-chan events.UIEvent, window time.Duration) {
	t.Helper()
	deadline := time.After(window)
	for {
		select {
		case ev := <-ch:
			if ev.Type == events.EventTypeProgressMilestone {
				m := smMilestoneData(t, ev)
				t.Fatalf("unexpected progress_milestone event: %v", m)
			}
		case <-deadline:
			return
		}
	}
}

// smTodoWrite calls handleTodoWrite with a todos array built from the given
// (content, status, scope) triples.
func smTodoWrite(t *testing.T, ag *Agent, items ...[3]string) {
	t.Helper()
	todos := make([]interface{}, 0, len(items))
	for _, it := range items {
		todos = append(todos, map[string]interface{}{
			"content": it[0],
			"status":  it[1],
			"scope":   it[2],
		})
	}
	if _, err := handleTodoWrite(context.Background(), ag, map[string]interface{}{"todos": todos}); err != nil {
		t.Fatalf("handleTodoWrite: %v", err)
	}
}

// smSeedTrackedFiles adds n file changes (distinct paths) to the tracker.
func smSeedTrackedFiles(t *testing.T, ct *changes.ChangeTracker, root string, n int, offset int) {
	t.Helper()
	if ct == nil {
		return
	}
	for i := 0; i < n; i++ {
		path := filepath.Join(root, fmt.Sprintf("seed_%d_%d.go", offset, i))
		if err := ct.TrackFileWrite(path, "", "content"); err != nil {
			t.Fatalf("TrackFileWrite: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Fixture-plan lifecycle: started → finished with delta and correlation
// ---------------------------------------------------------------------------

// TestScopeMilestones_FixturePlanLifecycle is the item's acceptance test:
// with a fixture plan on disk, a todo_write that starts the signup-form scope
// emits progress_milestone phase=started (correlating ids, no files/elapsed),
// and a later todo_write that completes the scope emits phase=finished with
// the files-touched delta and a non-negative elapsed time. Re-writing the same
// snapshot emits nothing (de-duplication), and the started/finished pair
// shares run_id and scope_id.
func TestScopeMilestones_FixturePlanLifecycle(t *testing.T) {
	root := t.TempDir()
	smWritePlanFile(t, root, smFixturePlanJSON)

	ct := changes.NewChangeTracker(nil, "")
	ag, ch := smAgent(t, root, ct)

	// N = 3 tracked files before the scope starts.
	smSeedTrackedFiles(t, ct, root, 3, 0)

	// signup-form goes in_progress; billing stays pending.
	smTodoWrite(t, ag,
		[3]string{"Build the sign-up form", "in_progress", "signup-form"},
		[3]string{"Wire billing", "pending", "billing"},
	)

	started := smMilestoneData(t, smMilestoneEvent(t, ch))
	if started["phase"] != events.MilestonePhaseStarted {
		t.Errorf("phase = %v, want %q", started["phase"], events.MilestonePhaseStarted)
	}
	if started["scope_id"] != "signup-form" {
		t.Errorf("scope_id = %v, want %q", started["scope_id"], "signup-form")
	}
	if started["plan_revision"] != 1 {
		t.Errorf("plan_revision = %v, want 1 (the fixture plan revision)", started["plan_revision"])
	}
	if started["scope_title"] != "Sign-up form" {
		t.Errorf("scope_title = %v, want %q", started["scope_title"], "Sign-up form")
	}
	if started["run_id"] != "run-1" {
		t.Errorf("run_id = %v, want %q (the session id)", started["run_id"], "run-1")
	}
	for _, key := range []string{"files_touched", "elapsed_ms"} {
		if _, ok := started[key]; ok {
			t.Errorf("started event must not carry %q, got %v", key, started[key])
		}
	}
	// billing is still pending — no milestone for it.
	smNoMilestone(t, ch, 300*time.Millisecond)

	// M = 2 more tracked files, then the scope completes.
	smSeedTrackedFiles(t, ct, root, 2, 10)
	smTodoWrite(t, ag,
		[3]string{"Build the sign-up form", "completed", "signup-form"},
		[3]string{"Wire billing", "pending", "billing"},
	)

	finished := smMilestoneData(t, smMilestoneEvent(t, ch))
	if finished["phase"] != events.MilestonePhaseFinished {
		t.Errorf("phase = %v, want %q", finished["phase"], events.MilestonePhaseFinished)
	}
	if finished["scope_id"] != "signup-form" {
		t.Errorf("scope_id = %v, want %q", finished["scope_id"], "signup-form")
	}
	if finished["files_touched"] != 2 {
		t.Errorf("files_touched = %v, want 2 (the delta since the scope started)", finished["files_touched"])
	}
	elapsed, ok := finished["elapsed_ms"].(int64)
	if !ok || elapsed < 0 {
		t.Errorf("elapsed_ms = %v, want an int64 >= 0", finished["elapsed_ms"])
	}
	if finished["scope_title"] != "Sign-up form" {
		t.Errorf("scope_title = %v, want %q", finished["scope_title"], "Sign-up form")
	}
	if finished["plan_revision"] != 1 {
		t.Errorf("plan_revision = %v, want 1", finished["plan_revision"])
	}

	// Correlation: the pair shares run_id and scope_id.
	if finished["run_id"] != started["run_id"] {
		t.Errorf("finished.run_id = %v, want the started run_id %v", finished["run_id"], started["run_id"])
	}
	if finished["scope_id"] != started["scope_id"] {
		t.Errorf("finished.scope_id = %v, want the started scope_id %v", finished["scope_id"], started["scope_id"])
	}

	// De-duplication: re-writing the same completed snapshot emits nothing.
	smTodoWrite(t, ag,
		[3]string{"Build the sign-up form", "completed", "signup-form"},
		[3]string{"Wire billing", "pending", "billing"},
	)
	smNoMilestone(t, ch, 300*time.Millisecond)
}

// ---------------------------------------------------------------------------
// No plan: milestones still fire, with plan_revision=0 and no title
// ---------------------------------------------------------------------------

// TestScopeMilestones_NoPlan pins the "no plan is not an error" contract:
// with no .sprout/plan.json, milestones still fire for the todo scope ids,
// carry plan_revision=0, and omit scope_title.
func TestScopeMilestones_NoPlan(t *testing.T) {
	root := t.TempDir() // no plan file written

	ct := changes.NewChangeTracker(nil, "")
	ag, ch := smAgent(t, root, ct)

	smTodoWrite(t, ag, [3]string{"Lone task", "in_progress", "orphan-scope"})
	started := smMilestoneData(t, smMilestoneEvent(t, ch))
	if started["phase"] != events.MilestonePhaseStarted {
		t.Errorf("phase = %v, want %q", started["phase"], events.MilestonePhaseStarted)
	}
	if started["scope_id"] != "orphan-scope" {
		t.Errorf("scope_id = %v, want %q", started["scope_id"], "orphan-scope")
	}
	if started["plan_revision"] != 0 {
		t.Errorf("plan_revision = %v, want 0 (no plan)", started["plan_revision"])
	}
	if _, ok := started["scope_title"]; ok {
		t.Errorf("scope_title must be absent without a plan, got %v", started["scope_title"])
	}

	smSeedTrackedFiles(t, ct, root, 1, 0)
	smTodoWrite(t, ag, [3]string{"Lone task", "completed", "orphan-scope"})
	finished := smMilestoneData(t, smMilestoneEvent(t, ch))
	if finished["phase"] != events.MilestonePhaseFinished {
		t.Errorf("phase = %v, want %q", finished["phase"], events.MilestonePhaseFinished)
	}
	if finished["files_touched"] != 1 {
		t.Errorf("files_touched = %v, want 1", finished["files_touched"])
	}
	if _, ok := finished["scope_title"]; ok {
		t.Errorf("scope_title must be absent without a plan, got %v", finished["scope_title"])
	}
}

// ---------------------------------------------------------------------------
// De-duplication and removed scopes
// ---------------------------------------------------------------------------

// TestScopeMilestones_StartDedupAndRemovedScope pins the no-double-emit rules:
// re-writing an already-started snapshot emits no second "started"; a scope
// removed from the todo list (present only in prev) is ignored; todos with an
// empty scope never emit.
func TestScopeMilestones_StartDedupAndRemovedScope(t *testing.T) {
	root := t.TempDir()
	smWritePlanFile(t, root, smFixturePlanJSON)

	ct := changes.NewChangeTracker(nil, "")
	ag, ch := smAgent(t, root, ct)

	smTodoWrite(t, ag,
		[3]string{"Task A", "in_progress", "signup-form"},
		[3]string{"Unscoped task", "in_progress", ""},
	)
	smMilestoneData(t, smMilestoneEvent(t, ch)) // the single "started"

	// Same snapshot again — already started, no new event.
	smTodoWrite(t, ag,
		[3]string{"Task A", "in_progress", "signup-form"},
		[3]string{"Unscoped task", "in_progress", ""},
	)
	smNoMilestone(t, ch, 300*time.Millisecond)

	// Scope removed from the list entirely — milestones are driven by the
	// current snapshot, so nothing is emitted.
	smTodoWrite(t, ag, [3]string{"Other task", "pending", "billing"})
	smNoMilestone(t, ch, 300*time.Millisecond)
}

// ---------------------------------------------------------------------------
// Nil change tracker
// ---------------------------------------------------------------------------

// TestScopeMilestones_NilChangeTracker pins the minimal-agent contract: with
// no change tracker (nil ⇒ 0 files), milestones still fire and the finished
// event omits files_touched (zero ⇒ omitted, mirroring the omitempty) while
// still carrying elapsed_ms.
func TestScopeMilestones_NilChangeTracker(t *testing.T) {
	root := t.TempDir()
	smWritePlanFile(t, root, smFixturePlanJSON)

	ag, ch := smAgent(t, root, nil) // no change tracker

	smTodoWrite(t, ag, [3]string{"Task", "in_progress", "signup-form"})
	started := smMilestoneData(t, smMilestoneEvent(t, ch))
	if started["phase"] != events.MilestonePhaseStarted {
		t.Errorf("phase = %v, want %q", started["phase"], events.MilestonePhaseStarted)
	}

	smTodoWrite(t, ag, [3]string{"Task", "completed", "signup-form"})
	finished := smMilestoneData(t, smMilestoneEvent(t, ch))
	if finished["phase"] != events.MilestonePhaseFinished {
		t.Errorf("phase = %v, want %q", finished["phase"], events.MilestonePhaseFinished)
	}
	if _, ok := finished["files_touched"]; ok {
		t.Errorf("files_touched must be omitted when zero, got %v", finished["files_touched"])
	}
	if _, ok := finished["elapsed_ms"]; !ok {
		t.Error("finished event must carry elapsed_ms")
	}
}

// ---------------------------------------------------------------------------
// Session rotation resets the tracker
// ---------------------------------------------------------------------------

// TestScopeMilestones_ResetOnRotate pins the cross-session isolation: a scope
// started before RotateSession must not emit a "finished" after the rotation
// (the run id changed, so the stale state would misattribute the finish).
func TestScopeMilestones_ResetOnRotate(t *testing.T) {
	root := t.TempDir()
	smWritePlanFile(t, root, smFixturePlanJSON)

	ct := changes.NewChangeTracker(nil, "")
	ag, ch := smAgent(t, root, ct)

	smTodoWrite(t, ag, [3]string{"Task", "in_progress", "signup-form"})
	smMilestoneData(t, smMilestoneEvent(t, ch)) // started in run-1

	newID, err := ag.RotateSession()
	if err != nil {
		t.Fatalf("RotateSession: %v", err)
	}
	if newID == "" || newID == "run-1" {
		t.Fatalf("RotateSession returned %q, want a fresh session id", newID)
	}

	// In the new run the scope looks terminal — but it was never started in
	// this run, so no "finished" may fire (and certainly not under the old
	// run's timing).
	smTodoWrite(t, ag, [3]string{"Task", "completed", "signup-form"})
	smNoMilestone(t, ch, 300*time.Millisecond)
}
