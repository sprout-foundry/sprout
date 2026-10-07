// runner_timeout.go — the runner's per-run timeout: the wall-clock limit
// one headless turn may take before the runner stops it. A benchmark run
// calls a real provider and a hung turn (a wedged HTTP call, a tool that
// never returns) would otherwise hold the whole suite hostage, so each run
// carries its own timer.
//
// The stop uses the agent's own interrupt mechanism — TriggerInterrupt, the
// same cancel the CLI's Ctrl+C path fires — rather than inventing a second
// cancel path: the interrupt context is what the in-flight provider request
// derives from, so firing it makes the turn actually unwind. The unwind is
// waited on with a bounded grace so the runner itself can never hang: if
// the turn will not stop, the runner records the failure and moves on (the
// goroutine's channel is buffered, so it finishes without blocking).
package benchmark

import (
	"errors"
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// defaultTaskTimeout is the per-run wall-clock limit a Runner uses when its
// Timeout is unset (zero or negative). A benchmark task is a small-to-medium
// change plus a verification pass, so ten minutes is generous headroom over
// a healthy turn while still bounding a wedged one.
const defaultTaskTimeout = 10 * time.Minute

// interruptGrace bounds the wait for the turn to unwind after the interrupt
// fires. Most turns stop promptly — the interrupt cancels the in-flight
// provider request — so the grace is only a backstop for a turn wedged
// inside something the cancel cannot reach (a stuck tool). The runner gives
// up waiting and records the timeout either way.
const interruptGrace = 15 * time.Second

// ErrRunTimeout is wrapped into a run's Run.Err when the run's turn was
// stopped for exceeding the runner's per-run timeout. Callers detect it
// with errors.Is (mirroring the package's ErrInvalidTask contract).
var ErrRunTimeout = errors.New("benchmark: run timed out")

// timeoutDuration is the effective per-run timeout: Timeout where positive,
// defaultTaskTimeout otherwise (the zero value and negative values resolve
// to the default, so a zero-value Runner stays usable).
func (r *Runner) timeoutDuration() time.Duration {
	if r != nil && r.Timeout > 0 {
		return r.Timeout
	}
	return defaultTaskTimeout
}

// runTurnWithTimeout issues the run's headless turn and stops it when it
// exceeds the runner's per-run timeout.
//
// The turn runs in a goroutine so the caller can select between its
// completion and the timeout. On timeout the runner fires the agent's real
// interrupt (TriggerInterrupt — the same cancel the CLI's Ctrl+C path uses:
// it cancels the interrupt context the in-flight provider request derives
// from, and preempts the turn's subagents), then waits a bounded grace for
// the goroutine to unwind. Either way the call returns: with the turn's own
// error on a normal completion, with an error wrapping ErrRunTimeout when
// the timer fired, and with the un-wound turn abandoned after the grace (its
// goroutine drains into the buffered channel and finishes on its own).
//
// A panicked turn is recovered (mirroring the CLI's query goroutine) so one
// panicking turn cannot take down a suite that may have hours of runs left.
func (r *Runner) runTurnWithTimeout(ag *agent.Agent, task *Task, runNumber int) error {
	timeout := r.timeoutDuration()

	type turnOutcome struct{ err error }
	done := make(chan turnOutcome, 1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				done <- turnOutcome{err: fmt.Errorf("agent turn panicked: %v", rec)}
			}
		}()
		_, err := ag.ProcessQueryWithContinuityAs(agent.QuerySourceCLI, task.Request)
		done <- turnOutcome{err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case outcome := <-done:
		return outcome.err
	case <-timer.C:
		// The turn hung: fire the real interrupt so the in-flight provider
		// request aborts and the turn unwinds, then give it the grace to do
		// so. Either way the run is recorded as a timeout failure.
		ag.TriggerInterrupt()
		grace := time.NewTimer(interruptGrace)
		defer grace.Stop()
		select {
		case outcome := <-done:
			return fmt.Errorf("%w: the turn exceeded %s (interrupt unwind: %v)",
				ErrRunTimeout, timeout, outcome.err)
		case <-grace.C:
			return fmt.Errorf("%w: the turn exceeded %s and did not stop within %s of the interrupt",
				ErrRunTimeout, timeout, interruptGrace)
		}
	}
}
