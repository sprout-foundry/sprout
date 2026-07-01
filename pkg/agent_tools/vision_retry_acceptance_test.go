//go:build !js

package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Acceptance-criteria tests for SP-103-A1 DoVisionRetry.
//
// Each test maps to one acceptance scenario from the spec.  Tests that
// manipulate environment variables do NOT use t.Parallel() to avoid
// cross-contamination.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Helper: clear an env var for the duration of the test, restoring on cleanup.
// ---------------------------------------------------------------------------

func clearEnvVar(t *testing.T, key string) {
	t.Helper()
	orig, wasSet := os.LookupEnv("SPROUT_" + key)
	os.Unsetenv("SPROUT_" + key)
	t.Cleanup(func() {
		if wasSet {
			os.Setenv("SPROUT_"+key, orig)
		} else {
			os.Unsetenv("SPROUT_" + key)
		}
	})
}

// ---------------------------------------------------------------------------
// Custom net.Error with Temporary() == true.
//
// net.OpError.Temporary() always returns false since Go 1.18, so we use a
// custom type to exercise the isRetryableError net.Error.Temporary() path.
// ---------------------------------------------------------------------------

type tempNetError struct{ msg string }

func (e *tempNetError) Error() string     { return e.msg }
func (e *tempNetError) Timeout() bool     { return false }
func (e *tempNetError) Temporary() bool   { return true }

// ---------------------------------------------------------------------------
// 1. Success on first try
// ---------------------------------------------------------------------------

func TestAcceptance_SuccessOnFirstTry(t *testing.T) {
	t.Parallel()
	var calls int
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return nil
	}, RetryOptions{OpName: "test", MaxAttempts: 3})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// 2. Success after transient failure (503 then OK)
// ---------------------------------------------------------------------------

func TestAcceptance_SuccessAfterTransientFailure(t *testing.T) {
	t.Parallel()
	var calls int
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable"}
		}
		return nil
	}, RetryOptions{
		OpName:      "test",
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// 3. Give up after max attempts
// ---------------------------------------------------------------------------

func TestAcceptance_GiveUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	const maxAttempts = 3
	var calls int
	sentinelErr := &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable"}
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return sentinelErr
	}, RetryOptions{
		OpName:      "test",
		MaxAttempts: maxAttempts,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != maxAttempts {
		t.Fatalf("expected %d calls, got %d", maxAttempts, calls)
	}
	// The returned error should be the sentinel RetryableHTTPError.
	if _, ok := IsRetryableHTTPError(err); !ok {
		t.Errorf("expected RetryableHTTPError, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// 4. No retry on 4xx (not 408 or 429) — 404
//
// Use a plain error with "HTTP 404" text (not RetryableHTTPError) because
// RetryableHTTPError is always retryable via the fast-path.
// ---------------------------------------------------------------------------

func TestAcceptance_NoRetryOn4xx_Not408Or429(t *testing.T) {
	t.Parallel()
	var calls int
	err404 := fmt.Errorf("HTTP 404: Not Found")
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return err404
	}, RetryOptions{OpName: "test", MaxAttempts: 3})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry), got %d", calls)
	}
	if err != err404 {
		t.Errorf("expected original 404 error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 5. Retry on 408 (Request Timeout)
// ---------------------------------------------------------------------------

func TestAcceptance_RetryOn4xx_408(t *testing.T) {
	t.Parallel()
	var calls int
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return &RetryableHTTPError{StatusCode: 408, Status: "Request Timeout"}
		}
		return nil
	}, RetryOptions{
		OpName:      "test",
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// 6. Retry on 429 (Too Many Requests)
// ---------------------------------------------------------------------------

func TestAcceptance_RetryOn4xx_429(t *testing.T) {
	t.Parallel()
	var calls int
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return &RetryableHTTPError{StatusCode: 429, Status: "Too Many Requests", RetryAfter: 0}
		}
		return nil
	}, RetryOptions{
		OpName:      "test",
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// 7. Context cancelled mid-backoff
// ---------------------------------------------------------------------------

func TestAcceptance_CtxCancelledMidBackoff(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	firstCallDone := make(chan struct{})
	var calls int

	// Cancel context after the first call returns but before the backoff sleep.
	go func() {
		<-firstCallDone
		time.Sleep(5 * time.Millisecond) // ensure cancel fires before ctx.Done() check
		cancel()
	}()

	err := DoVisionRetry(ctx, func(ctx context.Context) error {
		calls++
		if calls == 1 {
			close(firstCallDone)
		}
		return &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable"}
	}, RetryOptions{
		OpName:      "test",
		MaxAttempts: 3,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    1000 * time.Millisecond,
		JitterPct:   0,
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// 8. Env var: default 3 attempts when env is unset
// ---------------------------------------------------------------------------

func TestAcceptance_EnvVarAttempts_Default3(t *testing.T) {
	clearEnvVar(t, "VISION_RETRY_ATTEMPTS")

	var calls int
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable"}
	}, RetryOptions{
		OpName:      "test",
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts (default), got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// 9. Env var: set to 1 disables retries
// ---------------------------------------------------------------------------

func TestAcceptance_EnvVarAttempts_SetTo1(t *testing.T) {
	t.Setenv("SPROUT_VISION_RETRY_ATTEMPTS", "1")

	var calls int
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable"}
	}, RetryOptions{
		OpName:      "test",
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("expected 1 attempt (retries disabled), got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// 10. Env var: zero falls back to default 3
// ---------------------------------------------------------------------------

func TestAcceptance_EnvVarAttempts_ZeroFallsBackTo3(t *testing.T) {
	t.Setenv("SPROUT_VISION_RETRY_ATTEMPTS", "0")

	var calls int
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable"}
	}, RetryOptions{
		OpName:      "test",
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts (zero falls back to default), got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// 11. Env var: base and max delay honoured
// ---------------------------------------------------------------------------

func TestAcceptance_EnvVarBaseAndMax(t *testing.T) {
	t.Setenv("SPROUT_VISION_RETRY_BASE_MS", "50")
	t.Setenv("SPROUT_VISION_RETRY_MAX_MS", "500")

	var calls int
	start := time.Now()
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable"}
		}
		return nil
	}, RetryOptions{
		OpName:      "test",
		MaxAttempts: 3,
		JitterPct:   20, // ±20% jitter
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	// Base delay is 50ms with ±20% jitter → [40, 60]ms.
	// Allow ±10ms tolerance for CI overhead.
	if elapsed < 30*time.Millisecond {
		t.Errorf("expected delay ~50ms (±20%% jitter), but elapsed only %v", elapsed)
	}
	if elapsed > 70*time.Millisecond {
		t.Errorf("expected delay ~50ms (±20%% jitter), but elapsed %v (too long)", elapsed)
	}
}

// ---------------------------------------------------------------------------
// 12. Opts wins over env
// ---------------------------------------------------------------------------

func TestAcceptance_OptsWinsOverEnv(t *testing.T) {
	t.Setenv("SPROUT_VISION_RETRY_ATTEMPTS", "5")

	var calls int
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return &RetryableHTTPError{StatusCode: 503, Status: "Service Unavailable"}
	}, RetryOptions{
		OpName:      "test",
		MaxAttempts: 2, // opts wins over env's "5"
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts (opts wins), got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// 13. Non-retryable plain error — no retry
// ---------------------------------------------------------------------------

func TestAcceptance_NonRetryableContextError(t *testing.T) {
	t.Parallel()
	var calls int
	parseErr := fmt.Errorf("parse error: invalid JSON")
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return parseErr
	}, RetryOptions{OpName: "test", MaxAttempts: 3})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry), got %d", calls)
	}
	if err != parseErr {
		t.Errorf("expected original parse error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 14. Network error with Temporary() == true is retried
// ---------------------------------------------------------------------------

func TestAcceptance_NetworkErrorRetried(t *testing.T) {
	t.Parallel()
	var calls int
	netErr := &tempNetError{msg: "dial tcp: connection refused"}
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		return netErr
	}, RetryOptions{
		OpName:      "test",
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if calls < 2 {
		t.Fatalf("expected >= 2 calls (network error should be retried), got %d", calls)
	}
	// After exhausting retries, should return the last error.
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Verify the error is our tempNetError.
	var matched net.Error
	if !errors.As(err, &matched) {
		t.Errorf("expected net.Error, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// 15. EOF is retried
// ---------------------------------------------------------------------------

func TestAcceptance_EOFRetried(t *testing.T) {
	t.Parallel()
	var calls int
	err := DoVisionRetry(context.Background(), func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return io.EOF
		}
		return nil
	}, RetryOptions{
		OpName:      "test",
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}
