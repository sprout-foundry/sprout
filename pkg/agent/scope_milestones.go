// Package agent — SP-151 §151a item 151.2: plan-scope milestone events.
//
// When a todo_write changes the status of todos linked to a plan scope item
// (the todo "scope" field, SP-148 §148c), the runtime emits a
// progress_milestone event for that scope item: phase "started" when one of
// its todos first goes in_progress, phase "finished" when all of its todos
// reach a terminal status (completed/cancelled). The finished event carries
// the files-touched count (change-tracker delta since the scope started) and
// the scope item's elapsed wall time. Every event carries the stable run id
// (the session id), the plan revision, and the scope id so consumers can
// de-duplicate and correlate the started/finished pair (SP-151 §151a:
// "stable IDs (run, plan revision, scope item)").
//
// The plan snapshot behind the payload is not frozen at the first load: the
// milestone path re-reads .sprout/plan.json and refreshes the cached
// revision and titles whenever the stored revision differs, so a scope
// write-back that bumps the plan mid-run is reflected by the very next
// milestone instead of every event reporting the revision first seen. And a
// scope whose todos go from open straight to all-terminal inside one
// todo_write — never observed in_progress — emits its whole lifecycle,
// started then finished, in that order.
//
// An absent or corrupt plan never fails a todo_write: it means "no plan"
// (revision 0, no titles) and milestones still fire for the scope ids the
// todos carry.
package agent

import (
	"sort"
	"sync"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// scopeMilestoneTracker is the per-agent state that turns todo_write
// snapshots into progress_milestone events (SP-151 §151a, item 151.2). It
// remembers, per plan scope item id, whether the scope has started (and when,
// with how many tracked files) and whether it has finished, so a scope that
// starts in one turn and finishes in a later one produces exactly one started
// and one finished event for the run.
type scopeMilestoneTracker struct {
	mu sync.Mutex
	// scopes maps a plan scope item id to its milestone state.
	scopes map[string]*scopeMilestoneState

	planMu sync.Mutex
	// planLoaded records whether the plan has ever been looked up. The
	// cached revision and titles below are refreshed on the milestone path
	// whenever the stored plan's revision differs from planRev, so the
	// payloads always describe the plan as it stands: a mid-run write-back
	// that bumps the revision is carried by the very next milestone.
	planLoaded bool
	planRev    int
	// scopeTitles maps a plan scope item id to its title (the scope_title
	// payload field). nil when the project has no plan — or none anymore
	// (a deleted or corrupt plan refreshes the cache to "no plan").
	scopeTitles map[string]string
}

// scopeMilestoneState is one plan scope item's in-flight milestone.
type scopeMilestoneState struct {
	startedAt  time.Time
	startFiles int
	started    bool
	finished   bool
}

func newScopeMilestoneTracker() *scopeMilestoneTracker {
	return &scopeMilestoneTracker{scopes: make(map[string]*scopeMilestoneState)}
}

// Reset clears the scope state and the cached plan. Called on session
// rotation: the run id (session id) changes, so the previous run's
// started/finished state must not leak into the new one — it would emit a
// "finished" for a scope that merely looks terminal in the new run.
func (t *scopeMilestoneTracker) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.scopes = make(map[string]*scopeMilestoneState)
	t.mu.Unlock()
	t.planMu.Lock()
	t.planLoaded = false
	t.planRev = 0
	t.scopeTitles = nil
	t.planMu.Unlock()
}

// scopeTodoStatus is one plan scope item's aggregated todo statuses in a
// single todo_write snapshot.
type scopeTodoStatus struct {
	hasInProgress bool
	hasOpen       bool // any todo still pending or in_progress
}

// scopeEmit is one decided progress_milestone event: the scope id and phase,
// plus the finished-only payload (the files-touched delta and the elapsed
// wall time; both ignored for started events).
type scopeEmit struct {
	scopeID   string
	phase     string
	files     int
	elapsedMs int64
}

// observe reconciles the previous and next todo snapshots against the tracked
// scope state and emits a progress_milestone event for every scope item that
// just started or just finished.
//
// The transitions, per scope item present in next:
//
//   - Started: the scope has an in_progress todo in next, was not already
//     started, and was not already finished. The state is seeded with the
//     current tracked-file count so the finished event can report the delta.
//   - Started + finished, in that order: none of the scope's todos in next
//     is pending/in_progress (all terminal) but the scope was never started —
//     a scope written from open (or from nothing) straight to terminal in
//     this single write. Its whole lifecycle happened inside one write, so
//     both phases are emitted back-to-back, started first, and the finished
//     event's delta is measured from that same instant: files_touched 0
//     (omitted) and elapsed ~0.
//   - Finished: all-terminal in next, the scope was started in an earlier
//     write, and it was not already finished. files_touched is the
//     tracked-file delta since the scope started and elapsed_ms its wall
//     time, both clamped to >= 0.
//   - Nothing: the scope's state is unchanged — already started, or already
//     finished — so re-writing the same snapshot de-duplicates.
//
// Scope ids that appear only in prev (removed from next) are ignored:
// milestones are driven by the current snapshot, and the tracker's own state
// is the source of truth for what has already been emitted — prev is kept in
// the signature for the call site, not consulted.
func (t *scopeMilestoneTracker) observe(a *Agent, prev, next []tools.TodoItem) {
	if t == nil || a == nil {
		return
	}

	statuses := make(map[string]scopeTodoStatus)
	for _, td := range next {
		if td.Scope == "" {
			continue
		}
		st := statuses[td.Scope]
		switch td.Status {
		case "in_progress":
			st.hasInProgress = true
			st.hasOpen = true
		case "pending":
			st.hasOpen = true
		}
		statuses[td.Scope] = st
	}
	if len(statuses) == 0 {
		return
	}

	// Decide and record the transitions under t.mu; publish after releasing
	// it so publishEvent never runs under the tracker lock.
	now := time.Now()
	filesNow := 0
	if ct := a.GetChangeTracker(); ct != nil {
		filesNow = len(ct.GetTrackedFiles())
	}

	t.mu.Lock()
	var emits []scopeEmit
	ids := make([]string, 0, len(statuses))
	for id := range statuses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		nextState, evts := scopeMilestoneTransitions(id, statuses[id], t.scopes[id], now, filesNow)
		if nextState != nil {
			t.scopes[id] = nextState
		}
		emits = append(emits, evts...)
	}
	t.mu.Unlock()

	if len(emits) == 0 {
		return
	}

	// Refresh the plan snapshot only now that an emit is certain — a
	// todo_write with no milestone must not pay for plan I/O.
	planRev, titles := t.planSnapshot(a)
	runID := a.GetSessionID()
	for _, e := range emits {
		// map (not the events.ProgressMilestoneData struct) so
		// decorateEventPayload merges the event metadata, mirroring the
		// other event constructors; zero/empty optional keys are omitted
		// (the omitempty fields of events.ProgressMilestoneData).
		payload := map[string]interface{}{
			"run_id":        runID,
			"plan_revision": planRev,
			"scope_id":      e.scopeID,
			"phase":         e.phase,
		}
		if title := titles[e.scopeID]; title != "" {
			payload["scope_title"] = title
		}
		if e.phase == events.MilestonePhaseFinished {
			// started events carry neither files_touched nor elapsed_ms.
			if e.files > 0 {
				payload["files_touched"] = e.files
			}
			payload["elapsed_ms"] = e.elapsedMs
		}
		a.publishEvent(events.EventTypeProgressMilestone, payload)
	}
}

// scopeMilestoneTransitions decides the milestone events one todo_write
// produces for a single plan scope item, from the item's aggregated todo
// statuses in the next snapshot (st), the tracker's previous state for that
// scope (ms; nil when the scope was never seen), and the instant and
// tracked-file count of the write. It returns the scope's new state (nil
// means "unchanged — keep the previous state") and the events in lifecycle
// order: started before finished for a scope that starts and finishes in the
// same write.
//
// Pure: no locks, no I/O, no reads outside its arguments — the observe loop
// stays a thin record-and-collect shell over this decision.
func scopeMilestoneTransitions(id string, st scopeTodoStatus, ms *scopeMilestoneState, now time.Time, filesNow int) (*scopeMilestoneState, []scopeEmit) {
	fresh := ms == nil || !ms.started
	alreadyFinished := ms != nil && ms.finished

	switch {
	case st.hasInProgress && fresh && !alreadyFinished:
		// One of the scope's todos just went in_progress for the first
		// time: the scope starts.
		return &scopeMilestoneState{
			startedAt:  now,
			startFiles: filesNow,
			started:    true,
		}, []scopeEmit{{scopeID: id, phase: events.MilestonePhaseStarted}}

	case !st.hasOpen && !alreadyFinished:
		if fresh {
			// The scope went from never-started straight to all-terminal
			// in this one write. Report the whole lifecycle in order —
			// started, then finished — seeding the start at this instant
			// so the finish's delta is measured from it (files 0, elapsed
			// ~0). Skipping the started phase here would hide a scope
			// that was completed without ever being seen in_progress.
			seed := &scopeMilestoneState{
				startedAt:  now,
				startFiles: filesNow,
				started:    true,
			}
			files, elapsed := milestoneDelta(seed, now, filesNow)
			seed.finished = true
			return seed, []scopeEmit{
				{scopeID: id, phase: events.MilestonePhaseStarted},
				{scopeID: id, phase: events.MilestonePhaseFinished, files: files, elapsedMs: elapsed},
			}
		}
		// Started in an earlier write, now all-terminal: report the finish
		// with the delta since that start. The state pointer is kept (and
		// marked finished) so a re-write of the same snapshot is a no-op.
		files, elapsed := milestoneDelta(ms, now, filesNow)
		ms.finished = true
		return ms, []scopeEmit{{scopeID: id, phase: events.MilestonePhaseFinished, files: files, elapsedMs: elapsed}}
	}
	return nil, nil
}

// milestoneDelta computes the finished-event payload for a scope that started
// at ms: the tracked-file delta since the start and the elapsed wall time,
// both clamped to >= 0. A nil ms means the scope is starting and finishing in
// the same write, so both values are measured from now: 0 and 0.
func milestoneDelta(ms *scopeMilestoneState, now time.Time, filesNow int) (int, int64) {
	if ms == nil {
		return 0, 0
	}
	files := filesNow - ms.startFiles
	if files < 0 {
		files = 0
	}
	elapsed := now.Sub(ms.startedAt).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	return files, elapsed
}

// planSnapshot returns the plan's revision and scope id→title map for the
// milestone payloads, refreshing the cached snapshot when the stored plan's
// revision differs from the cached one.
//
// The plan is re-read on every call: this sits on the milestone path only —
// observe calls it after the transitions are decided, at most once per
// todo_write and only when at least one milestone is about to be published —
// so the cost is one read of a tiny JSON document per milestone-bearing
// write, a handful of times per run. Stat-based change detection would add a
// second syscall per read plus invalidation state, and cannot tell a
// rewritten-but-identical file from an untouched one; loading outright is the
// simpler and equally cheap choice here.
//
// The refresh decision is the loaded revision against the cached one:
//
//   - A plan that appeared, or was written back at a higher revision,
//     refreshes the cache so the next payload carries the revision and
//     titles the plan has now.
//   - A plan that vanished or became unreadable refreshes the cache to "no
//     plan" (revision 0, no titles): a milestone's plan_revision should
//     describe the plan as it stands, not the last one that happened to be
//     readable. Load errors are never surfaced — "no plan" is a state, not
//     a failure.
//   - An unchanged revision keeps the cached titles: the store bumps the
//     revision on every write, so a same-revision rewrite is outside this
//     cache's contract, and keeping the map avoids rebuilding it.
func (t *scopeMilestoneTracker) planSnapshot(a *Agent) (int, map[string]string) {
	t.planMu.Lock()
	defer t.planMu.Unlock()
	t.refreshPlanLocked(a)
	return t.planRev, t.scopeTitles
}

// cachedPlanSnapshot returns the cached plan revision and titles without
// refreshing them, loading the plan once if it was never looked up. It is
// the read side for the question context: answering a mid-run decision must
// not pay for disk I/O, and the cache is already kept current by the
// milestone path, which refreshes it at every milestone-bearing todo_write.
func (t *scopeMilestoneTracker) cachedPlanSnapshot(a *Agent) (int, map[string]string) {
	t.planMu.Lock()
	defer t.planMu.Unlock()
	if !t.planLoaded {
		t.refreshPlanLocked(a)
	}
	return t.planRev, t.scopeTitles
}

// refreshPlanLocked re-reads the plan from the agent's workspace and updates
// the cached revision and titles when the loaded revision differs from the
// cached one. planMu must be held.
func (t *scopeMilestoneTracker) refreshPlanLocked(a *Agent) {
	rev, titles := 0, map[string]string(nil)
	if root := a.currentWorkspaceRoot(); root != "" {
		if plan, err := planstore.New().Load(root); err == nil {
			rev = plan.Revision
			if len(plan.Scope) > 0 {
				titles = make(map[string]string, len(plan.Scope))
				for _, s := range plan.Scope {
					titles[s.ID] = s.Title
				}
			}
		}
	}
	t.planLoaded = true
	if rev != t.planRev {
		t.planRev = rev
		t.scopeTitles = titles
	}
}

// observeScopeMilestones records the milestone transitions a todo_write just
// made (SP-151 §151a, item 151.2). prev is the todo snapshot before the
// write, next the snapshot just written.
func (a *Agent) observeScopeMilestones(prev, next []tools.TodoItem) {
	if a == nil {
		return
	}
	if a.scopeMilestones == nil {
		a.scopeMilestones = newScopeMilestoneTracker()
	}
	a.scopeMilestones.observe(a, prev, next)
}

// questionContext returns the plan context a decision is correlated to (SP-151
// §151a, item 151.4): the active scope item (the most recently started scope
// that has not finished) and the plan revision. It is the read-side companion
// to observe: observe records scope transitions, questionContext reports which
// scope a mid-run decision belongs to.
//
// The active scope is the one with the latest startedAt among scopes that have
// started but not finished. Scopes started in the same todo_write share a
// startedAt (observe seeds them all with one time.Now()); on that tie the
// lexicographically greater scope id wins, keeping the choice deterministic
// regardless of map iteration order. With no active scope it is "".
//
// Locking mirrors observe and Reset: cachedPlanSnapshot is called first (it
// takes t.planMu and releases it before returning — it serves the cache,
// loading once if the plan was never looked up), then t.mu is taken to read
// scopes. The two locks are never held together, so there is no deadlock and
// no plan I/O runs under the scope lock.
func (t *scopeMilestoneTracker) questionContext(a *Agent) (scopeID string, planRev int) {
	if t == nil {
		return "", 0
	}
	planRev, _ = t.cachedPlanSnapshot(a)
	t.mu.Lock()
	active := ""
	var latest time.Time
	for id, s := range t.scopes {
		if s == nil || !s.started || s.finished {
			continue
		}
		switch {
		case active == "":
			active, latest = id, s.startedAt
		case s.startedAt.After(latest):
			active, latest = id, s.startedAt
		case s.startedAt.Equal(latest) && id > active:
			active = id
		}
	}
	t.mu.Unlock()
	return active, planRev
}

// planQuestionContext returns the plan context (active scope item id + plan
// revision) a progress_question event is correlated to (SP-151 §151a, item
// 151.4). A bare agent with no tracker (the minimal test agents) reports no
// context: ("", 0).
func (a *Agent) planQuestionContext() (scopeID string, planRev int) {
	if a == nil || a.scopeMilestones == nil {
		return "", 0
	}
	return a.scopeMilestones.questionContext(a)
}
