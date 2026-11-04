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

// startDeadlineWarning arranges for the agent to be told to wrap up shortly
// before the run's deadline, when the host that started it set one
// (SPROUT_TASK_DEADLINE, Unix seconds). A run with no deadline, or one too
// close to warn usefully, gets no warning. A handoff note left in the
// workspace by an earlier run is removed, so only this run's note counts.
// The returned function cancels the warning.
func startDeadlineWarning(a steerer, now time.Time) func() {
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
