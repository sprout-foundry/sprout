package agent

import (
	"context"
	"time"
)

// bgStopWait bounds how long rotation waits for a cancelled background
// task's goroutine to observe the cancellation. Bounded so a wedged task
// cannot hold /clear hostage; the generation gate below suppresses the
// straggler's completion notification regardless.
const bgStopWait = 5 * time.Second

// wakeupResumeBatch is one wakeup turn stashed for the REPL: the
// formatted batch and the background generation it drained in.
// DrainWakeupForREPL drops batches whose generation moved — a rotation
// (/clear) between the stash and the REPL's drain means the completions
// belong to a conversation the user closed, and the batch must not run
// as a turn in the new session.
type wakeupResumeBatch struct {
	gen uint64
	msg string
}

// BackgroundGeneration returns the session's background-work generation.
// Producers capture it when their background work starts and compare at
// completion: a mismatch means the session rotated away underneath the
// work, and the completion must not be delivered into the new session.
func (a *Agent) BackgroundGeneration() uint64 {
	if a == nil {
		return 0
	}
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	return a.bgGeneration
}

// generationUnchanged reports whether gen is still the current generation.
func (a *Agent) generationUnchanged(gen uint64) bool {
	if a == nil {
		return true
	}
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	return a.bgGeneration == gen
}

// trackInProcessWorkflow registers an in-process automate workflow's cancel
// so session rotation can end it. ID is the workflow session ID.
func (a *Agent) trackInProcessWorkflow(id string, cancel context.CancelFunc) {
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	if a.inProcessWorkflowCancels == nil {
		a.inProcessWorkflowCancels = make(map[string]context.CancelFunc)
	}
	a.inProcessWorkflowCancels[id] = cancel
}

// untrackInProcessWorkflow removes the registration when the workflow's
// own goroutine finishes.
func (a *Agent) untrackInProcessWorkflow(id string) {
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	delete(a.inProcessWorkflowCancels, id)
}

// cancelInProcessWorkflows cancels every tracked in-process workflow.
func (a *Agent) cancelInProcessWorkflows() {
	a.notifMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(a.inProcessWorkflowCancels))
	for _, cancel := range a.inProcessWorkflowCancels {
		cancels = append(cancels, cancel)
	}
	a.notifMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// stopSessionBackgroundWork ends the background work this session started
// and silences its completions, so a rotated-to session starts quiet.
// Called by RotateSession — /clear in the CLI and the WebUI's New Session
// button both land here.
//
// Three layers:
//  1. Bump the generation, so completions that land after this point are
//     recognized as stale by their producers (subagent tasks, automate
//     watchers) and by the shell wakeup watcher's gate.
//  2. Cancel the work itself: background subagent tasks (whose
//     "cancelled" status already suppresses their notification), BPM
//     processes (killed as whole process groups), in-process automate
//     workflows (run context cancelled, ending their LLM calls), and the
//     chat's WebUI background PTY sessions.
//  3. Discard everything already queued, so a completion that landed just
//     before rotation cannot auto-resume into the new conversation, and
//     drop any wakeup batch stashed for the REPL.
//
// Layers 1–3 up through the cancels run synchronously: they are cheap and
// their ordering against the new session's first turn is load-bearing (a
// poller that drains after the bump but before the drain would carry
// old-session completions into a resume turn under the new generation).
// Everything that can block — the cancelled tasks' goroutines observing
// cancellation (bgStopWait), BPM grace periods, the PTY close sequence,
// seconds per live process — runs in a goroutine: rotation is on the WebUI
// HTTP path, and stragglers' completions are already silenced by the
// generation bump, so nothing user-visible waits on any of it.
func (a *Agent) stopSessionBackgroundWork() {
	a.notifMu.Lock()
	a.bgGeneration++
	a.notifMu.Unlock()

	// Field read, not GetSubagentRunner: rotation must not lazily create a
	// runner (plus its shared state) for an agent that never ran a task.
	if runner := a.subagentRunner; runner != nil {
		runner.cancelBackgroundTasks()
		go runner.waitBackgroundDone(bgStopWait)
	}

	// In-process automate workflows: their loops derive cancellation from
	// the run context registered at launch, so cancelling ends their LLM
	// calls. Completion stays silent via the generation gate.
	a.cancelInProcessWorkflows()

	// A completion may have queued (or a poller drained into a stash) in
	// the window before the stops above took effect; drop whatever is left.
	// A poller that still slips between the bump and this drain is caught
	// by TryAutoResume's pre-drain generation capture (notifications.go) —
	// this drain is the belt, that capture is the suspenders.
	a.DrainNotifications()

	a.wakeupMu.Lock()
	a.pendingWakeupResume = nil
	a.wakeupMu.Unlock()
	a.notifMu.Lock()
	a.pendingQueryDisplay = ""
	a.notifMu.Unlock()

	// Physical teardown, off the rotation's critical path (see above).
	// backgroundProcessManager is set once during agent setup and never
	// re-set, so the unlocked read here cannot race a rotation.
	bpm := a.GetBackgroundProcessManager()
	chatID := a.GetChatID()
	if chatID != "" {
		// Read under webuiMu now: the goroutine must not race a concurrent
		// SetTerminalManager.
		tm := a.GetTerminalManager()
		if tm != nil {
			go tm.StopBackgroundSessionsForChat(chatID)
		}
	}
	if bpm != nil {
		go bpm.StopAll()
	}
}
