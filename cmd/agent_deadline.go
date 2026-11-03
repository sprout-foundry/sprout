package cmd

import (
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// deadlineWarnBefore is how long before a run's deadline the agent is told to
// finish.
const deadlineWarnBefore = 2 * time.Minute

// deadlineWarning is the message the agent receives near its deadline.
const deadlineWarning = "Time check: about two minutes remain before this task is stopped. Stop exploring and verifying now. Make sure every change is written to its file, then give your final answer with what is done and anything left unfinished."

// steerer is the part of the agent the deadline warning needs.
type steerer interface {
	InjectInputContext(input string) error
}

// startDeadlineWarning arranges for the agent to be told to wrap up shortly
// before the run's deadline, when the host that started it set one
// (SPROUT_TASK_DEADLINE, Unix seconds). A run with no deadline, or one too
// close to warn usefully, gets no warning. The returned function cancels it.
func startDeadlineWarning(a steerer, now time.Time) func() {
	raw := strings.TrimSpace(configuration.GetEnvSimple("TASK_DEADLINE"))
	if raw == "" || a == nil {
		return func() {}
	}
	secs, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || secs <= 0 {
		return func() {}
	}
	wait := time.Unix(secs, 0).Sub(now) - deadlineWarnBefore
	if wait <= 0 {
		return func() {}
	}
	t := time.AfterFunc(wait, func() { _ = a.InjectInputContext(deadlineWarning) })
	return func() { t.Stop() }
}
