// seed_provider_retry_policy.go — the time-budget retry policy shared by the
// non-streaming (doChatWithRetry) and streaming (doChatWithRetryStreaming)
// model-call loops: the backoff schedule, the per-call time budget (and the
// run's task deadline), the plain-words wait notice, and the attempt loop
// that drives both paths.
//
// The policy is per model call, not per attempt count: a platform deploy can
// interrupt the model endpoint for tens of seconds, so a call keeps retrying
// its transient failures until the budget since its first failure is spent.

package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

const (
	// providerRetryBaseDelay is the first backoff delay after a failure.
	providerRetryBaseDelay = 1 * time.Second
	// providerRetryMaxDelay caps the exponential backoff.
	providerRetryMaxDelay = 30 * time.Second
	// providerRetryBudget is how long one model call keeps retrying
	// transient failures, measured from its first failure. A platform
	// deploy interrupts model calls for 30-90 s; five minutes rides that
	// out without stalling a run for long when the endpoint is really gone.
	providerRetryBudget = 5 * time.Minute
	// providerRetryMinDelay is the shortest wait worth taking before trying
	// again; a budget with less left than this ends the call instead.
	providerRetryMinDelay = 100 * time.Millisecond
	// providerTaskDeadlineEnvSuffix names the run's task deadline (Unix
	// seconds) in the environment; the host that started the run sets it
	// (SPROUT_TASK_DEADLINE, read by cmd/agent_deadline.go).
	providerTaskDeadlineEnvSuffix = "TASK_DEADLINE"
	// providerTaskDeadlineReserve is the time a run keeps back from its task
	// deadline so the agent can still hand off.
	providerTaskDeadlineReserve = time.Minute
)

// providerRetryClock is the retry policy's clock and sleeper, so tests can
// advance time instead of waiting it out.
type providerRetryClock struct {
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// retrySleep is the real sleeper: it waits d or until ctx is done, and
// reports ctx.Err() when the wait was cut short.
func retrySleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// defaultProviderRetryClock keeps the real time.
func defaultProviderRetryClock() providerRetryClock {
	return providerRetryClock{now: time.Now, sleep: retrySleep}
}

// providerRetryDelay is the backoff before the retry that follows the given
// failure number (1 = the first retry): base doubled per failure, capped at
// providerRetryMaxDelay.
func providerRetryDelay(failure int) time.Duration {
	delay := providerRetryBaseDelay
	for i := 1; i < failure; i++ {
		delay *= 2
		if delay >= providerRetryMaxDelay {
			return providerRetryMaxDelay
		}
	}
	return delay
}

// providerTaskDeadline is the run's task deadline, when the host set one.
func providerTaskDeadline() (time.Time, bool) {
	secs, err := strconv.ParseInt(strings.TrimSpace(configuration.GetEnvSimple(providerTaskDeadlineEnvSuffix)), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}, false
	}
	return time.Unix(secs, 0), true
}

// providerTaskDeadlineCutoff is the latest moment a retry wait may end while
// still leaving the agent time to hand off; false when no deadline is set.
func providerTaskDeadlineCutoff() (time.Time, bool) {
	deadline, ok := providerTaskDeadline()
	if !ok {
		return time.Time{}, false
	}
	return deadline.Add(-providerTaskDeadlineReserve), true
}

// nextProviderRetryWait decides whether a retry may start and how long its
// wait should be. stop reports that retrying is over — the context's deadline
// has passed, the budget is spent, or the wait would reach the context's
// deadline or the run's task deadline cutoff — and reason says which, for the
// log line.
func nextProviderRetryWait(ctx context.Context, clock providerRetryClock, firstFailure time.Time, failures int) (wait time.Duration, stop bool, reason string) {
	now := clock.now()
	remaining := providerRetryBudget - now.Sub(firstFailure)
	if remaining <= 0 {
		return 0, true, fmt.Sprintf("the retry budget of %s is spent", providerRetryBudget)
	}
	wait = providerRetryDelay(failures)
	if wait > remaining {
		wait = remaining
	}
	if wait < providerRetryMinDelay {
		return 0, true, fmt.Sprintf("the retry budget of %s is spent", providerRetryBudget)
	}
	retryAt := now.Add(wait)
	if deadline, ok := ctx.Deadline(); ok && !retryAt.Before(deadline) {
		return 0, true, "the call's own deadline comes first"
	}
	if cutoff, ok := providerTaskDeadlineCutoff(); ok && !retryAt.Before(cutoff) {
		return 0, true, "the run must hand off before then"
	}
	return wait, false, ""
}

// providerRetryWaitNotice is the plain-words message shown once per attempt
// while the call waits to retry: the model service is not answering and how
// long the agent waits, so a person and the platform can see the run is alive
// rather than stalled.
func providerRetryWaitNotice(wait time.Duration) string {
	return "The model service did not answer; retrying in " + formatWaitDuration(wait) + "."
}

// formatWaitDuration renders a wait in whole seconds, or in milliseconds when
// it is shorter than one.
func formatWaitDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return fmt.Sprintf("%.0f s", d.Seconds())
}

// providerAttempt runs one attempt of a model call. It reports an error, or
// retryOnly with no error to say the failure is handled in place (a shed
// body, a repaired request) and the loop should try again at once without
// counting a provider failure against the backoff. The attempt func owns the
// bound on in-place retries (maxInPlaceRetries), since only it knows whether
// the repair still has payload left to drop.
type providerAttempt func(attempt int) (err error, retryOnly bool)

// providerRetryHooks receives the per-attempt notices of the retry loop.
type providerRetryHooks struct {
	// failed is called once per failed provider attempt, with whether a
	// retry will follow.
	failed func(err error, attempt int, willRetry bool)
	// wait is called once per retry, before the backoff, with the
	// plain-words wait notice.
	wait func(message string)
	// stopped is called when retrying ends while the error is retryable,
	// to say why no further attempt is made.
	stopped func(reason string)
}

// runWithProviderRetry drives the model-call loop: attempts run until one
// succeeds (nil error), the error is not retryable, or the retry budget or a
// deadline ends the call. The attempt func decides the content of each
// attempt; the hooks report it.
func runWithProviderRetry(ctx context.Context, clock providerRetryClock, attempt providerAttempt, hooks providerRetryHooks) error {
	var firstFailure time.Time
	failures := 0
	for n := 0; ; n++ {
		err, retryOnly := attempt(n)
		if err == nil {
			return nil
		}
		if retryOnly {
			continue
		}
		failures++
		if firstFailure.IsZero() {
			firstFailure = clock.now()
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		retryable := isRetryableProviderError(err)
		wait, stop, reason := nextProviderRetryWait(ctx, clock, firstFailure, failures)
		willRetry := retryable && !stop
		if hooks.failed != nil {
			hooks.failed(err, n, willRetry)
		}
		if !retryable {
			return err
		}
		if stop {
			if hooks.stopped != nil {
				hooks.stopped(reason)
			}
			return err
		}
		if hooks.wait != nil {
			hooks.wait(providerRetryWaitNotice(wait))
		}
		if sleepErr := clock.sleep(ctx, wait); sleepErr != nil {
			return sleepErr
		}
	}
}
