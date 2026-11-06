package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"syscall"
	"testing"
	"time"

	core "github.com/sprout-foundry/seed/core"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// isolateTaskDeadline clears the run's task deadline (SPROUT_TASK_DEADLINE),
// which a platform task exports to its tests too, so the retry loop's
// deadline checks see only what a test sets.
func isolateTaskDeadline(t *testing.T) {
	t.Helper()
	t.Setenv("SPROUT_TASK_DEADLINE", "")
}

// fakeProviderRetryClock advances virtual time on every sleep, so retry
// policy tests run in milliseconds and still see a realistic timeline.
type fakeProviderRetryClock struct {
	now     time.Time
	slept   []time.Duration
	onSleep func(ctx context.Context, d time.Duration) error
}

func newFakeProviderRetryClock() *fakeProviderRetryClock {
	return &fakeProviderRetryClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeProviderRetryClock) clock() providerRetryClock {
	return providerRetryClock{
		now: func() time.Time { return c.now },
		sleep: func(ctx context.Context, d time.Duration) error {
			if c.onSleep != nil {
				if err := c.onSleep(ctx, d); err != nil {
					return err
				}
			}
			c.slept = append(c.slept, d)
			c.now = c.now.Add(d)
			return nil
		},
	}
}

// failures returns the attempt bodies' errors in order and then repeats the
// last one forever, so a test never needs to know the attempt count.
type scriptedAttempts struct {
	errs   []error
	repeat error
	seen   int
}

func (s *scriptedAttempts) attempt(int) (error, bool) {
	s.seen++
	if s.seen <= len(s.errs) {
		return s.errs[s.seen-1], false
	}
	return s.repeat, false
}

var (
	zeroChoicesErr  = core.ErrZeroChoices
	connResetErr    = errors.New("Post \"https://model.example/v1/chat/completions\": read tcp 10.0.0.1:443: connection reset by peer")
	unauthorizedErr = errors.New("HTTP 401: invalid API key")
)

// TestRunWithProviderRetryRidesOutOutage: an empty completion, then a
// connection reset, then success within the budget — the call succeeds.
func TestRunWithProviderRetryRidesOutOutage(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	attempts := &scriptedAttempts{
		errs: []error{zeroChoicesErr, connResetErr},
	}
	var waits []string
	hooks := providerRetryHooks{wait: func(msg string) { waits = append(waits, msg) }}

	if err := runWithProviderRetry(context.Background(), clock.clock(), attempts.attempt, hooks); err != nil {
		t.Fatalf("expected the call to ride out the outage, got: %v", err)
	}
	if attempts.seen != 3 {
		t.Errorf("attempts = %d, want 3", attempts.seen)
	}
	want := []time.Duration{providerRetryBaseDelay, 2 * providerRetryBaseDelay}
	if len(clock.slept) != 2 || clock.slept[0] != want[0] || clock.slept[1] != want[1] {
		t.Fatalf("backoff waits = %v, want %v", clock.slept, want)
	}
	if len(waits) != 2 {
		t.Fatalf("wait notices = %d, want one per retry (2)", len(waits))
	}
	if waits[0] != "The model service did not answer; retrying in 1 s." {
		t.Errorf("unexpected first wait notice: %q", waits[0])
	}
}

// TestRunWithProviderRetryPermanentErrorFailsAtOnce: a 401 is not retried.
func TestRunWithProviderRetryPermanentErrorFailsAtOnce(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	attempts := &scriptedAttempts{repeat: unauthorizedErr}
	var willRetry []bool
	hooks := providerRetryHooks{failed: func(_ error, _ int, retry bool) { willRetry = append(willRetry, retry) }}

	err := runWithProviderRetry(context.Background(), clock.clock(), attempts.attempt, hooks)
	if !errors.Is(err, unauthorizedErr) {
		t.Fatalf("err = %v, want the 401 itself", err)
	}
	if attempts.seen != 1 {
		t.Errorf("attempts = %d, want exactly 1 (no retry)", attempts.seen)
	}
	if len(clock.slept) != 0 {
		t.Errorf("slept %v, want no wait before a permanent error", clock.slept)
	}
	if len(willRetry) != 1 || willRetry[0] {
		t.Errorf("notice should say no retry will follow, got %v", willRetry)
	}
}

// TestRunWithProviderRetryStopsAtBudget: a provider that keeps failing stops
// when the budget since its first failure is spent, and does not retry past
// it.
func TestRunWithProviderRetryStopsAtBudget(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	attempts := &scriptedAttempts{repeat: zeroChoicesErr}
	start := clock.now
	var stopped string
	hooks := providerRetryHooks{stopped: func(reason string) { stopped = reason }}

	err := runWithProviderRetry(context.Background(), clock.clock(), attempts.attempt, hooks)
	if !errors.Is(err, core.ErrZeroChoices) {
		t.Fatalf("err = %v, want the zero-choices error", err)
	}
	if stopped == "" {
		t.Error("expected a stop notice explaining why retrying ended")
	}
	if elapsed := clock.now.Sub(start); elapsed < providerRetryBudget || elapsed > providerRetryBudget+providerRetryMaxDelay {
		t.Errorf("retried for %s, want about the %s budget", elapsed, providerRetryBudget)
	}
	// The loop must not keep going for long after the budget is spent.
	if attempts.seen > 40 {
		t.Errorf("attempts = %d, want the budget to bound them", attempts.seen)
	}
	for _, d := range clock.slept {
		if d > providerRetryMaxDelay {
			t.Errorf("wait %s exceeds the %s cap", d, providerRetryMaxDelay)
		}
	}
}

// TestRunWithProviderRetryStopsAtContextDeadline: retries end when the
// context's deadline passes, the context's error is returned, and no third
// attempt starts after it.
func TestRunWithProviderRetryStopsAtContextDeadline(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The context ends during the second wait (the 1 s and 2 s waits fit).
	clock.onSleep = func(_ context.Context, d time.Duration) error {
		if len(clock.slept) == 1 {
			cancel()
			return context.DeadlineExceeded
		}
		return nil
	}

	attempts := &scriptedAttempts{repeat: zeroChoicesErr}
	err := runWithProviderRetry(ctx, clock.clock(), attempts.attempt, providerRetryHooks{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if attempts.seen != 2 {
		t.Errorf("attempts = %d, want 2 (none after the deadline)", attempts.seen)
	}
}

// TestNextProviderRetryWaitRefusesToCrossContextDeadline: the loop declines a
// retry whose wait would reach the context's deadline instead of starting it.
func TestNextProviderRetryWaitRefusesToCrossContextDeadline(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	ctx, cancel := context.WithDeadline(context.Background(), clock.now.Add(4*time.Second))
	defer cancel()

	// 1 s and 2 s waits fit before the 4 s deadline; the 4 s one does not.
	if wait, stop, _ := nextProviderRetryWait(ctx, clock.clock(), clock.now, 1); stop || wait != time.Second {
		t.Fatalf("first retry: wait = %s stop = %v, want 1s and a retry", wait, stop)
	}
	clock.now = clock.now.Add(time.Second + 2*time.Second)
	if _, stop, reason := nextProviderRetryWait(ctx, clock.clock(), clock.now.Add(-2*time.Second), 3); !stop {
		t.Fatalf("a 4s wait would cross a deadline 1s away; want a stop, reason %q", reason)
	}
}

// TestRunWithProviderRetryReturnsContextDeadlineAfterWait: a wait cut short by
// the context ending returns the context's error, not the provider's.
func TestRunWithProviderRetryReturnsContextDeadlineAfterWait(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	ctx, cancel := context.WithCancel(context.Background())
	clock.onSleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	attempts := &scriptedAttempts{repeat: zeroChoicesErr}
	err := runWithProviderRetry(ctx, clock.clock(), attempts.attempt, providerRetryHooks{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestProviderRetryDelayIsCapped: the backoff doubles from the base delay and
// stops at the cap.
func TestProviderRetryDelayIsCapped(t *testing.T) {
	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}
	for i, w := range want {
		if got := providerRetryDelay(i + 1); got != w {
			t.Errorf("providerRetryDelay(%d) = %s, want %s", i+1, got, w)
		}
	}
	if providerRetryMaxDelay != 30*time.Second {
		t.Errorf("cap = %s, want 30s", providerRetryMaxDelay)
	}
	if providerRetryBudget != 5*time.Minute {
		t.Errorf("budget = %s, want 5m", providerRetryBudget)
	}
}

// TestIsRetryableProviderErrorOutageTable pins what an outage of the model
// endpoint looks like: empty completions, connection-level failures and
// gateway statuses retry; client errors do not.
func TestIsRetryableProviderErrorOutageTable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"zero choices", core.ErrZeroChoices, true},
		{"wrapped zero choices", &zeroChoicesWrap{core.ErrZeroChoices}, true},
		{"connection reset", connResetErr, true},
		{"connection refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true},
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"connection reset, wrapped", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, true},
		{"bare EOF", io.EOF, false},
		{"502", errors.New("HTTP 502: bad gateway"), true},
		{"503", errors.New("HTTP 503: unavailable"), true},
		{"504", errors.New("HTTP 504: gateway timeout"), true},
		{"rate limited", agenterrors.NewRateLimitError("slow down", nil, "test"), true},
		{"401", unauthorizedErr, false},
		{"400", errors.New("HTTP 400: bad request"), false},
		{"403", errors.New("HTTP 403: forbidden"), false},
		{"404", errors.New("HTTP 404: not found"), false},
		{"context overflow", errors.New("HTTP 400: context_length_exceeded"), false},
	}
	for _, c := range cases {
		if got := isRetryableProviderError(c.err); got != c.want {
			t.Errorf("%s: isRetryableProviderError = %v, want %v", c.name, got, c.want)
		}
	}
}

// zeroChoicesWrap mimics seed's "transient error during chat (managed):
// %w: provider returned empty response" wrapping of the sentinel.
type zeroChoicesWrap struct{ err error }

func (e *zeroChoicesWrap) Error() string { return "zero choices" }
func (e *zeroChoicesWrap) Unwrap() error { return e.err }

// TestRunWithProviderRetryInPlaceRetriesAreBounded: a path that keeps asking
// for an in-place retry (an endpoint that always answers 413) must not loop
// forever; the attempt func bounds it, and the loop ends with the error.
func TestRunWithProviderRetryInPlaceRetriesAreBounded(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	bodyTooLarge := errors.New("HTTP 413: body too large")

	seen := 0
	attempt := func(attempt int) (error, bool) {
		seen++
		// Mirrors the 413 branch: repair until the in-place budget is spent.
		return bodyTooLarge, attempt < maxInPlaceRetries
	}

	err := runWithProviderRetry(context.Background(), clock.clock(), attempt, providerRetryHooks{})
	if !errors.Is(err, bodyTooLarge) {
		t.Fatalf("err = %v, want the 413", err)
	}
	if seen != maxInPlaceRetries+1 {
		t.Errorf("attempts = %d, want %d (initial + the in-place budget)", seen, maxInPlaceRetries+1)
	}
	if len(clock.slept) != 0 {
		t.Errorf("in-place retries should not back off, slept %v", clock.slept)
	}
}

// TestRunWithProviderRetryStopsBeforeTaskDeadline: when the run has a task
// deadline (SPROUT_TASK_DEADLINE), retries stop early enough for the agent to
// hand off rather than spend the run's last minutes waiting on the endpoint.
func TestRunWithProviderRetryStopsBeforeTaskDeadline(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	// The run's deadline is 20 s from now; the agent must stop retrying a
	// minute before it, which is already past.
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(clock.now.Add(20*time.Second).Unix(), 10))

	attempts := &scriptedAttempts{repeat: zeroChoicesErr}
	var stopped string
	hooks := providerRetryHooks{stopped: func(reason string) { stopped = reason }}

	err := runWithProviderRetry(context.Background(), clock.clock(), attempts.attempt, hooks)
	if !errors.Is(err, core.ErrZeroChoices) {
		t.Fatalf("err = %v, want the provider error", err)
	}
	if attempts.seen != 1 || len(clock.slept) != 0 {
		t.Errorf("attempts = %d, waits = %v; want no retry once the handoff time has come", attempts.seen, clock.slept)
	}
	if stopped == "" {
		t.Error("expected a stop notice saying the run must hand off")
	}
}

// TestRunWithProviderRetryKeepsRetryingWhileTaskDeadlineIsFar: a task deadline
// hours away leaves the normal budget in charge.
func TestRunWithProviderRetryKeepsRetryingWhileTaskDeadlineIsFar(t *testing.T) {
	isolateTaskDeadline(t)
	clock := newFakeProviderRetryClock()
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(clock.now.Add(2*time.Hour).Unix(), 10))

	attempts := &scriptedAttempts{errs: []error{zeroChoicesErr}}
	if err := runWithProviderRetry(context.Background(), clock.clock(), attempts.attempt, providerRetryHooks{}); err != nil {
		t.Fatalf("expected the single failure to be retried, got: %v", err)
	}
	if len(clock.slept) != 1 {
		t.Errorf("waits = %v, want one retry", clock.slept)
	}
}
