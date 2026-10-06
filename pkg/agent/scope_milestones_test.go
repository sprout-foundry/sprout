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
// One-write pending→completed: the whole lifecycle in a single todo_write
// ---------------------------------------------------------------------------

// smFixturePlanV2JSON is the fixture plan after a mid-run write-back: the
// same scope ids, revision bumped to 2, and both titles renamed so a stale
// cache (revision or titles) is detectable in the payload.
const smFixturePlanV2JSON = `{
  "version": 1,
  "revision": 2,
  "created": "2026-10-05T00:00:00Z",
  "updated": "2026-10-05T01:00:00Z",
  "goal": "Ship the sign-up flow",
  "scope": [
    {"id": "signup-form", "title": "Sign-up form (revised)", "description": "Front-end sign-up form"},
    {"id": "billing", "title": "Billing integration (revised)"}
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

// TestScopeMilestones_OneWritePendingToCompleted pins the never-seen-in-
// progress lifecycle: a scope whose todos go from open (or from nothing)
// straight to all-terminal in one todo_write emits BOTH phases, started
// before finished, in that order. The started event of the pair carries
// neither files_touched nor elapsed_ms; the finished event carries elapsed_ms
// (the wall time from the shared start instant, ~0) — files_touched measures
// the tracked-file delta since that same instant, so it is 0 and omitted here
// (the omitempty contract the nil-tracker test pins for the slow path; the
// transitions table test proves the shared delta computation).
func TestScopeMilestones_OneWritePendingToCompleted(t *testing.T) {
	t.Run("directlyTerminalFromNothing", func(t *testing.T) {
		root := t.TempDir()
		smWritePlanFile(t, root, smFixturePlanJSON)

		ct := changes.NewChangeTracker(nil, "")
		ag, ch := smAgent(t, root, ct)
		smSeedTrackedFiles(t, ct, root, 4, 0)

		// The scope's todo is written completed without ever being seen
		// in_progress (billing stays pending and must not emit).
		smTodoWrite(t, ag,
			[3]string{"Build the sign-up form", "completed", "signup-form"},
			[3]string{"Wire billing", "pending", "billing"},
		)

		started := smMilestoneData(t, smMilestoneEvent(t, ch))
		if started["phase"] != events.MilestonePhaseStarted {
			t.Errorf("first phase = %v, want %q (started before finished)", started["phase"], events.MilestonePhaseStarted)
		}
		if started["scope_id"] != "signup-form" {
			t.Errorf("started.scope_id = %v, want signup-form", started["scope_id"])
		}
		for _, key := range []string{"files_touched", "elapsed_ms"} {
			if _, ok := started[key]; ok {
				t.Errorf("started event must not carry %q, got %v", key, started[key])
			}
		}

		finished := smMilestoneData(t, smMilestoneEvent(t, ch))
		if finished["phase"] != events.MilestonePhaseFinished {
			t.Errorf("second phase = %v, want %q", finished["phase"], events.MilestonePhaseFinished)
		}
		if finished["scope_id"] != "signup-form" {
			t.Errorf("finished.scope_id = %v, want signup-form", finished["scope_id"])
		}
		elapsed, ok := finished["elapsed_ms"].(int64)
		if !ok || elapsed < 0 {
			t.Errorf("finished.elapsed_ms = %v, want an int64 >= 0", finished["elapsed_ms"])
		}
		if _, ok := finished["files_touched"]; ok {
			t.Errorf("files_touched must be omitted when the same-write delta is 0, got %v", finished["files_touched"])
		}
		if finished["scope_title"] != "Sign-up form" {
			t.Errorf("scope_title = %v, want %q", finished["scope_title"], "Sign-up form")
		}
		// The pair correlates: same run id, same scope id.
		if finished["run_id"] != started["run_id"] {
			t.Errorf("finished.run_id = %v, want the started run_id %v", finished["run_id"], started["run_id"])
		}

		// billing was pending throughout — no third event.
		smNoMilestone(t, ch, 300*time.Millisecond)

		// De-duplication for the both-events state: re-writing the same
		// terminal snapshot emits nothing.
		smTodoWrite(t, ag,
			[3]string{"Build the sign-up form", "completed", "signup-form"},
			[3]string{"Wire billing", "pending", "billing"},
		)
		smNoMilestone(t, ch, 300*time.Millisecond)
	})

	t.Run("pendingWriteThenCompletedWrite", func(t *testing.T) {
		root := t.TempDir()
		smWritePlanFile(t, root, smFixturePlanJSON)

		ct := changes.NewChangeTracker(nil, "")
		ag, ch := smAgent(t, root, ct)

		// A pending-only write is no transition: nothing emits.
		smTodoWrite(t, ag, [3]string{"Wire billing", "pending", "billing"})
		smNoMilestone(t, ch, 300*time.Millisecond)

		// Now the same scope flips pending→completed in one write: both
		// phases, in order.
		smTodoWrite(t, ag, [3]string{"Wire billing", "completed", "billing"})

		started := smMilestoneData(t, smMilestoneEvent(t, ch))
		if started["phase"] != events.MilestonePhaseStarted || started["scope_id"] != "billing" {
			t.Errorf("first event = %v/%v, want started/billing", started["phase"], started["scope_id"])
		}
		finished := smMilestoneData(t, smMilestoneEvent(t, ch))
		if finished["phase"] != events.MilestonePhaseFinished || finished["scope_id"] != "billing" {
			t.Errorf("second event = %v/%v, want finished/billing", finished["phase"], finished["scope_id"])
		}
		if _, ok := finished["elapsed_ms"]; !ok {
			t.Error("finished event must carry elapsed_ms")
		}
		smNoMilestone(t, ch, 300*time.Millisecond)
	})
}

// ---------------------------------------------------------------------------
// Plan snapshot refresh: a mid-run revision bump is carried by the next event
// ---------------------------------------------------------------------------

// TestScopeMilestones_PlanSnapshotRefresh pins the not-frozen-at-first-load
// contract: a scope write-back that bumps .sprout/plan.json from revision 1
// to 2 mid-run is carried by the very next milestone (plan_revision 2, the
// updated title), a plan that disappears refreshes the cache to "no plan"
// (revision 0, no title), and a todo_write that emits no milestone still
// never touches the plan (the load-only-when-an-emit-is-certain property).
func TestScopeMilestones_PlanSnapshotRefresh(t *testing.T) {
	root := t.TempDir()
	smWritePlanFile(t, root, smFixturePlanJSON)

	ct := changes.NewChangeTracker(nil, "")
	ag, ch := smAgent(t, root, ct)

	// A non-emitting write must not load the plan at all.
	smTodoWrite(t, ag, [3]string{"Wire billing", "pending", "billing"})
	smNoMilestone(t, ch, 300*time.Millisecond)
	if ag.scopeMilestones.planLoaded {
		t.Fatal("plan was loaded on a todo_write that emitted no milestone")
	}

	// First milestone under revision 1.
	smTodoWrite(t, ag,
		[3]string{"Build the sign-up form", "in_progress", "signup-form"},
		[3]string{"Wire billing", "pending", "billing"},
	)
	started := smMilestoneData(t, smMilestoneEvent(t, ch))
	if started["plan_revision"] != 1 {
		t.Errorf("plan_revision = %v, want 1 (the fixture plan revision)", started["plan_revision"])
	}
	if started["scope_title"] != "Sign-up form" {
		t.Errorf("scope_title = %v, want %q", started["scope_title"], "Sign-up form")
	}

	// Mid-run write-back: the plan is rewritten at revision 2 with renamed
	// scope titles (what write_plan does on a scope change).
	smWritePlanFile(t, root, smFixturePlanV2JSON)

	// The next milestone-bearing write must carry the new revision and
	// title — billing starts here (signup-form is re-written unchanged, so
	// it emits nothing).
	smTodoWrite(t, ag,
		[3]string{"Build the sign-up form", "in_progress", "signup-form"},
		[3]string{"Wire billing", "in_progress", "billing"},
	)
	next := smMilestoneData(t, smMilestoneEvent(t, ch))
	if next["phase"] != events.MilestonePhaseStarted || next["scope_id"] != "billing" {
		t.Fatalf("event = %v/%v, want started/billing", next["phase"], next["scope_id"])
	}
	if next["plan_revision"] != 2 {
		t.Errorf("plan_revision = %v, want 2 (the written-back revision)", next["plan_revision"])
	}
	if next["scope_title"] != "Billing integration (revised)" {
		t.Errorf("scope_title = %v, want %q (the revised title)", next["scope_title"], "Billing integration (revised)")
	}

	// The refreshed cache serves the question context too.
	if _, rev := ag.planQuestionContext(); rev != 2 {
		t.Errorf("planQuestionContext revision = %d, want 2 (the refreshed cache)", rev)
	}

	// The plan disappears: the next milestone reports the plan as it stands
	// — revision 0, no title — instead of the last readable snapshot.
	if err := os.Remove(filepath.Join(root, ".sprout", "plan.json")); err != nil {
		t.Fatalf("remove plan: %v", err)
	}
	smTodoWrite(t, ag, [3]string{"Stray task", "in_progress", "orphan-scope"})
	after := smMilestoneData(t, smMilestoneEvent(t, ch))
	if after["plan_revision"] != 0 {
		t.Errorf("plan_revision = %v, want 0 (the plan is gone)", after["plan_revision"])
	}
	if after["scope_id"] != "orphan-scope" {
		t.Errorf("scope_id = %v, want orphan-scope", after["scope_id"])
	}
	if _, ok := after["scope_title"]; ok {
		t.Errorf("scope_title must be absent when the plan is gone, got %v", after["scope_title"])
	}
	if _, rev := ag.planQuestionContext(); rev != 0 {
		t.Errorf("planQuestionContext revision = %d, want 0 (the refreshed cache)", rev)
	}

	// The plan comes back: the cache picks it up again.
	smWritePlanFile(t, root, smFixturePlanV2JSON)
	smTodoWrite(t, ag, [3]string{"Stray task", "in_progress", "orphan-scope"})
	smNoMilestone(t, ch, 300*time.Millisecond) // already started — no new event

	// billing finishes under the restored plan: revision 2 and the revised
	// title are reported.
	smTodoWrite(t, ag, [3]string{"Wire billing", "completed", "billing"})
	finished := smMilestoneData(t, smMilestoneEvent(t, ch))
	if finished["phase"] != events.MilestonePhaseFinished {
		t.Errorf("phase = %v, want %q", finished["phase"], events.MilestonePhaseFinished)
	}
	if finished["plan_revision"] != 2 {
		t.Errorf("plan_revision = %v, want 2 (the restored plan)", finished["plan_revision"])
	}
	if finished["scope_title"] != "Billing integration (revised)" {
		t.Errorf("scope_title = %v, want %q", finished["scope_title"], "Billing integration (revised)")
	}
}

// ---------------------------------------------------------------------------
// Transition decisions: the pure per-scope state machine
// ---------------------------------------------------------------------------

// TestScopeMilestoneTransitions pins the per-scope transition decisions as a
// table over (aggregated statuses, previous state): which phases emit and in
// what order, and how the finished payload is computed. The table covers the
// invariants the event tests exercise end-to-end plus the ones hard to stage
// on the bus (negative clamping, already-finished no-ops).
func TestScopeMilestoneTransitions(t *testing.T) {
	now := time.Now()
	started := func(startFiles int, age time.Duration) *scopeMilestoneState {
		return &scopeMilestoneState{started: true, startedAt: now.Add(-age), startFiles: startFiles}
	}

	tests := []struct {
		name        string
		status      scopeTodoStatus
		prev        *scopeMilestoneState
		filesNow    int
		wantPhases  []string
		wantFiles   int // -1: the finished event must omit files_touched
		wantElapsed time.Duration
		wantState   *scopeMilestoneState // nil: state must be unchanged
	}{
		{
			name:       "neverStartedAllTerminalEmitsStartedThenFinished",
			status:     scopeTodoStatus{},
			prev:       nil,
			filesNow:   7,
			wantPhases: []string{events.MilestonePhaseStarted, events.MilestonePhaseFinished},
			wantFiles:  -1, // same-instant delta is 0 → omitted on the wire
			wantState:  &scopeMilestoneState{started: true, finished: true, startedAt: now, startFiles: 7},
		},
		{
			name:        "startedEarlierAllTerminalEmitsFinishedOnly",
			status:      scopeTodoStatus{},
			prev:        started(5, 2*time.Second),
			filesNow:    7,
			wantPhases:  []string{events.MilestonePhaseFinished},
			wantFiles:   2,
			wantElapsed: 2 * time.Second,
			wantState:   &scopeMilestoneState{started: true, finished: true, startedAt: now.Add(-2 * time.Second), startFiles: 5},
		},
		{
			name:       "inProgressNeverStartedEmitsStartedOnly",
			status:     scopeTodoStatus{hasInProgress: true, hasOpen: true},
			prev:       nil,
			filesNow:   3,
			wantPhases: []string{events.MilestonePhaseStarted},
			wantState:  &scopeMilestoneState{started: true, startedAt: now, startFiles: 3},
		},
		{
			name:       "alreadyStartedWithOpenTodosEmitsNothing",
			status:     scopeTodoStatus{hasInProgress: true, hasOpen: true},
			prev:       started(1, time.Second),
			wantPhases: nil,
			wantState:  nil,
		},
		{
			name:       "alreadyFinishedRecompletedEmitsNothing",
			status:     scopeTodoStatus{},
			prev:       &scopeMilestoneState{started: true, finished: true, startedAt: now, startFiles: 1},
			wantPhases: nil,
			wantState:  nil,
		},
		{
			name:       "alreadyFinishedReopenedEmitsNothing",
			status:     scopeTodoStatus{hasInProgress: true, hasOpen: true},
			prev:       &scopeMilestoneState{started: true, finished: true, startedAt: now, startFiles: 1},
			wantPhases: nil,
			wantState:  nil,
		},
		{
			name:       "openTodosNeverStartedEmitsNothing",
			status:     scopeTodoStatus{hasOpen: true},
			prev:       nil,
			wantPhases: nil,
			wantState:  nil,
		},
		{
			name:        "negativeFileDeltaClampsToZero",
			status:      scopeTodoStatus{},
			prev:        started(10, time.Second),
			filesNow:    4,
			wantPhases:  []string{events.MilestonePhaseFinished},
			wantFiles:   0,
			wantElapsed: time.Second,
			wantState:   &scopeMilestoneState{started: true, finished: true, startedAt: now.Add(-time.Second), startFiles: 10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var prevCopy scopeMilestoneState
			hadPrev := tt.prev != nil
			if hadPrev {
				prevCopy = *tt.prev
			}
			gotState, gotEmits := scopeMilestoneTransitions("s1", tt.status, tt.prev, now, tt.filesNow)

			phases := make([]string, len(gotEmits))
			for i, e := range gotEmits {
				phases[i] = e.phase
				if e.scopeID != "s1" {
					t.Errorf("emit[%d].scopeID = %q, want s1", i, e.scopeID)
				}
			}
			if len(phases) != len(tt.wantPhases) {
				t.Fatalf("phases = %v, want %v", phases, tt.wantPhases)
			}
			for i := range phases {
				if phases[i] != tt.wantPhases[i] {
					t.Fatalf("phases = %v, want %v (order matters)", phases, tt.wantPhases)
				}
			}

			for _, e := range gotEmits {
				if e.phase != events.MilestonePhaseFinished {
					if e.files != 0 || e.elapsedMs != 0 {
						t.Errorf("started event carries files=%d elapsed=%d, want both zero", e.files, e.elapsedMs)
					}
					continue
				}
				if tt.wantFiles < 0 {
					if e.files != 0 {
						t.Errorf("finished.files = %d, want 0 (same-instant delta)", e.files)
					}
				} else if e.files != tt.wantFiles {
					t.Errorf("finished.files = %d, want %d", e.files, tt.wantFiles)
				}
				if want := tt.wantElapsed.Milliseconds(); e.elapsedMs != want {
					t.Errorf("finished.elapsedMs = %d, want %d", e.elapsedMs, want)
				}
			}

			if tt.wantState == nil {
				if gotState != nil {
					t.Errorf("state = %+v, want nil (unchanged)", gotState)
				} else if hadPrev && *tt.prev != prevCopy {
					t.Errorf("previous state mutated: %+v, want %+v", *tt.prev, prevCopy)
				}
				return
			}
			if gotState == nil {
				t.Fatalf("state = nil, want %+v", tt.wantState)
			}
			if *gotState != *tt.wantState {
				t.Errorf("state = %+v, want %+v", *gotState, *tt.wantState)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Session rotation resets the tracker
// ---------------------------------------------------------------------------

// TestScopeMilestones_ResetOnRotate pins the cross-session isolation: the
// prior run's started/finished state must not leak into the new run. After
// RotateSession the tracker is empty, so the first write of the new run that
// takes the scope terminal is a never-started→terminal transition *of the new
// run* and emits the full lifecycle under the NEW run id, with fresh timing
// (elapsed ~0) and a fresh file delta — never the old run's id, its startedAt,
// or its file counts. Re-writing the same snapshot afterwards emits nothing.
func TestScopeMilestones_ResetOnRotate(t *testing.T) {
	root := t.TempDir()
	smWritePlanFile(t, root, smFixturePlanJSON)

	ct := changes.NewChangeTracker(nil, "")
	ag, ch := smAgent(t, root, ct)

	smTodoWrite(t, ag, [3]string{"Task", "in_progress", "signup-form"})
	oldStarted := smMilestoneData(t, smMilestoneEvent(t, ch)) // started in run-1
	if oldStarted["run_id"] != "run-1" {
		t.Fatalf("run_id = %v, want run-1", oldStarted["run_id"])
	}

	newID, err := ag.RotateSession()
	if err != nil {
		t.Fatalf("RotateSession: %v", err)
	}
	if newID == "" || newID == "run-1" {
		t.Fatalf("RotateSession returned %q, want a fresh session id", newID)
	}

	// In the new run the tracker is empty: the terminal write is a fresh
	// never-started→terminal transition, so the whole lifecycle fires —
	// correlated to the NEW run id, timed from the new write.
	smTodoWrite(t, ag, [3]string{"Task", "completed", "signup-form"})

	freshStarted := smMilestoneData(t, smMilestoneEvent(t, ch))
	if freshStarted["phase"] != events.MilestonePhaseStarted || freshStarted["scope_id"] != "signup-form" {
		t.Errorf("first event = %v/%v, want started/signup-form", freshStarted["phase"], freshStarted["scope_id"])
	}
	freshFinished := smMilestoneData(t, smMilestoneEvent(t, ch))
	if freshFinished["phase"] != events.MilestonePhaseFinished || freshFinished["scope_id"] != "signup-form" {
		t.Errorf("second event = %v/%v, want finished/signup-form", freshFinished["phase"], freshFinished["scope_id"])
	}

	// No leak from the old run: both events carry the new run id, and the
	// finish is timed from the new run's own start (no old elapsed, no old
	// file delta — the tracker saw 0 tracked files at its fresh start).
	for i, ev := range []map[string]interface{}{freshStarted, freshFinished} {
		if ev["run_id"] != newID {
			t.Errorf("event %d run_id = %v, want %q (the new session id)", i, ev["run_id"], newID)
		}
		if ev["run_id"] == "run-1" {
			t.Errorf("event %d carries the pre-rotation run id", i)
		}
	}
	if _, ok := freshFinished["files_touched"]; ok {
		t.Errorf("files_touched must be omitted (the fresh delta is 0), got %v", freshFinished["files_touched"])
	}
	if _, ok := freshFinished["elapsed_ms"]; !ok {
		t.Error("finished event must carry elapsed_ms (timed from the fresh start)")
	}

	// De-duplication in the new run: the same terminal snapshot again is a
	// no-op.
	smTodoWrite(t, ag, [3]string{"Task", "completed", "signup-form"})
	smNoMilestone(t, ch, 300*time.Millisecond)
}
