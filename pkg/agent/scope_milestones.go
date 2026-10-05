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

	planMu     sync.Mutex
	planLoaded bool
	planRev    int
	// scopeTitles maps a plan scope item id to its title (the scope_title
	// payload field). nil when the project has no plan.
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

// observe reconciles the previous and next todo snapshots against the tracked
// scope state and emits a progress_milestone event for every scope item that
// just started or just finished.
//
//   - Started: the scope has an in_progress todo in next, was not already
//     started, and was not already finished. The state is seeded with the
//     current tracked-file count so the finished event can report the delta.
//   - Finished: none of the scope's todos in next is pending/in_progress
//     (all terminal), the scope was started, and it was not already finished.
//     files_touched is the tracked-file delta since the scope started and
//     elapsed_ms its wall time, both clamped to >= 0.
//
// Scope ids that appear only in prev (removed from next) are ignored:
// milestones are driven by the current snapshot, and the tracker's own state
// is the source of truth for what has already been emitted — prev is kept in
// the signature for the call site, not consulted.
func (t *scopeMilestoneTracker) observe(a *Agent, prev, next []tools.TodoItem) {
	if t == nil || a == nil {
		return
	}

	type scopeStatus struct {
		hasInProgress bool
		hasOpen       bool // any todo still pending or in_progress
	}
	statuses := make(map[string]*scopeStatus)
	for _, td := range next {
		if td.Scope == "" {
			continue
		}
		st := statuses[td.Scope]
		if st == nil {
			st = &scopeStatus{}
			statuses[td.Scope] = st
		}
		switch td.Status {
		case "in_progress":
			st.hasInProgress = true
			st.hasOpen = true
		case "pending":
			st.hasOpen = true
		}
	}
	if len(statuses) == 0 {
		return
	}

	// Decide and record the transitions under t.mu; publish after releasing
	// it so publishEvent never runs under the tracker lock.
	type emit struct {
		scopeID   string
		phase     string
		files     int
		elapsedMs int64
	}

	now := time.Now()
	filesNow := 0
	if ct := a.GetChangeTracker(); ct != nil {
		filesNow = len(ct.GetTrackedFiles())
	}

	t.mu.Lock()
	var emits []emit
	ids := make([]string, 0, len(statuses))
	for id := range statuses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		st := statuses[id]
		ms := t.scopes[id]
		switch {
		case st.hasInProgress && (ms == nil || !ms.started) && (ms == nil || !ms.finished):
			t.scopes[id] = &scopeMilestoneState{
				startedAt:  now,
				startFiles: filesNow,
				started:    true,
			}
			emits = append(emits, emit{scopeID: id, phase: events.MilestonePhaseStarted})
		case !st.hasOpen && ms != nil && ms.started && !ms.finished:
			ms.finished = true
			files := filesNow - ms.startFiles
			if files < 0 {
				files = 0
			}
			elapsed := now.Sub(ms.startedAt).Milliseconds()
			if elapsed < 0 {
				elapsed = 0
			}
			emits = append(emits, emit{scopeID: id, phase: events.MilestonePhaseFinished, files: files, elapsedMs: elapsed})
		}
	}
	t.mu.Unlock()

	if len(emits) == 0 {
		return
	}

	// Load the plan (once per session) only now that an emit is certain — a
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

// planSnapshot lazily loads the project plan once (revision + scope
// id→title) and caches it for the session. It mirrors the planContextSummary
// guard: an absent or unreadable plan means "no plan" (rev 0, no titles),
// never an error.
func (t *scopeMilestoneTracker) planSnapshot(a *Agent) (int, map[string]string) {
	t.planMu.Lock()
	defer t.planMu.Unlock()
	if t.planLoaded {
		return t.planRev, t.scopeTitles
	}
	t.planLoaded = true
	root := a.currentWorkspaceRoot()
	if root == "" {
		return 0, nil
	}
	plan, err := planstore.New().Load(root)
	if err != nil {
		return 0, nil
	}
	t.planRev = plan.Revision
	if len(plan.Scope) > 0 {
		t.scopeTitles = make(map[string]string, len(plan.Scope))
		for _, s := range plan.Scope {
			t.scopeTitles[s.ID] = s.Title
		}
	}
	return t.planRev, t.scopeTitles
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
// Locking mirrors observe and Reset: planSnapshot is called first (it takes
// t.planMu and releases it before returning — and returns the cached revision
// without I/O once loaded), then t.mu is taken to read scopes. The two locks
// are never held together, so there is no deadlock and no plan I/O runs under
// the scope lock.
func (t *scopeMilestoneTracker) questionContext(a *Agent) (scopeID string, planRev int) {
	if t == nil {
		return "", 0
	}
	planRev, _ = t.planSnapshot(a)
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
