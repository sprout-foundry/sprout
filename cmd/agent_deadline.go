package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// deadlineWarnBefore is how long before a run's deadline the agent is told to
// wrap up: enough to bring a change to a working state and write the handoff
// note.
const deadlineWarnBefore = 3 * time.Minute

// handoffNotePath is where a run that reaches its deadline leaves its note
// for the next run, relative to the workspace.
const handoffNotePath = ".sprout/handoff.md"

// maxHandoffNoteBytes bounds the note carried in the run's result.
const maxHandoffNoteBytes = 16 << 10

// deadlineWarning is the message the agent receives near its deadline. The
// host that set the deadline continues the task in a new run, so the agent
// hands off instead of rushing to finish.
const deadlineWarning = "Time check: about three minutes remain before this run is stopped. The task continues in a new run after this one, so do not rush to finish it. Now: bring whatever you are changing to a working state (no half-written files; the project should still build), then write a handoff note to " + handoffNotePath + " for the next run, with what is done, what is left (as steps), and anything it must know. If the whole task is already done, do not write the note. Then give your final answer."

// deadlineWarned records that this process's run was told to wrap up.
var deadlineWarned atomic.Bool

// steerer is the part of the agent the deadline warning needs.
type steerer interface {
	InjectInputContext(input string) error
}

// hasTaskDeadline reports whether the host that started this run set a task
// deadline (SPROUT_TASK_DEADLINE, Unix seconds). Such a run is the platform's
// to stop, so it is also the one that must leave no background work behind.
func hasTaskDeadline() bool {
	raw := strings.TrimSpace(configuration.GetEnvSimple("TASK_DEADLINE"))
	if raw == "" {
		return false
	}
	secs, err := strconv.ParseInt(raw, 10, 64)
	return err == nil && secs > 0
}

// startTaskRun prepares a run for the host's deadline, when it set one: the
// agent is warned to wrap up before it (startDeadlineWarning), and the run
// leaves a quiet machine for the next one. stopBackground stops the run's
// background sessions (builds, servers), killing them as whole process
// groups; it runs when the agent is warned and again from the returned
// function, which ends the run's preparations and is called when the run
// returns. A run without a deadline keeps its background sessions.
func startTaskRun(a steerer, stopBackground func(), now time.Time) func() {
	if !hasTaskDeadline() {
		stopBackground = nil
	}
	cancelWarning := startDeadlineWarning(a, now, stopBackground)
	return func() {
		cancelWarning()
		if stopBackground != nil {
			stopBackground()
		}
	}
}

// startDeadlineWarning arranges for the agent to be told to wrap up shortly
// before the run's deadline, when the host that started it set one
// (SPROUT_TASK_DEADLINE, Unix seconds). A run with no deadline, or one too
// close to warn usefully, gets no warning. A handoff note left in the
// workspace by an earlier run is removed, so only this run's note counts.
// stopBackground, when set, stops the run's background sessions once the
// agent is warned. The returned function cancels the warning.
func startDeadlineWarning(a steerer, now time.Time, stopBackground func()) func() {
	raw := strings.TrimSpace(configuration.GetEnvSimple("TASK_DEADLINE"))
	if raw == "" || a == nil {
		return func() {}
	}
	secs, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || secs <= 0 {
		return func() {}
	}
	_ = os.Remove(handoffNotePath)
	wait := time.Unix(secs, 0).Sub(now) - deadlineWarnBefore
	if wait <= 0 {
		return func() {}
	}
	t := time.AfterFunc(wait, func() {
		deadlineWarned.Store(true)
		_ = a.InjectInputContext(deadlineWarning)
		// The agent is wrapping up: a build it left running is noise now,
		// and the next run must start on an idle machine.
		if stopBackground != nil {
			stopBackground()
		}
	})
	return func() { t.Stop() }
}

// handoffNote is the note a run that was told to wrap up left for the next
// run in dir ("" when it was not told to, or finished the task instead).
func handoffNote(dir string) string {
	if !deadlineWarned.Load() {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, handoffNotePath))
	if err != nil {
		return ""
	}
	if len(b) > maxHandoffNoteBytes {
		b = b[:maxHandoffNoteBytes]
	}
	return strings.TrimSpace(string(b))
}
