//go:build !js

package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// RetryableHTTPError tests
// ---------------------------------------------------------------------------

func TestRetryableHTTPError_Error(t *testing.T) {
	t.Parallel()

	err := &RetryableHTTPError{
		StatusCode: 429,
		Status:     "Too Many Requests",
		Method:     "GET",
		URL:        "https://example.com/img.png",
		RetryAfter: 2 * time.Second,
	}

	msg := err.Error()
	if !strings.Contains(msg, "HTTP 429") {
		t.Errorf("error message should contain status code, got: %s", msg)
	}
	if !strings.Contains(msg, "Too Many Requests") {
		t.Errorf("error message should contain status text, got: %s", msg)
	}
}

func TestRetryableHTTPError_Unwrap(t *testing.T) {
	t.Parallel()

	inner := fmt.Errorf("inner error")
	err := &RetryableHTTPError{
		StatusCode: 503,
		Status:     "Service Unavailable",
		Err:        inner,
	}

	if err.Unwrap() != inner {
		t.Error("Unwrap() should return the inner error")
	}
}

func TestIsRetryableHTTPError(t *testing.T) {
	t.Parallel()

	inner := &RetryableHTTPError{StatusCode: 429, Status: "Too Many Requests"}
	wrapped := fmt.Errorf("wrapped: %w", inner)

	// Direct error.
	if r, ok := IsRetryableHTTPError(inner); !ok || r != inner {
		t.Error("IsRetryableHTTPError should recognize direct error")
	}

	// Wrapped error.
	if r, ok := IsRetryableHTTPError(wrapped); !ok || r != inner {
		t.Error("IsRetryableHTTPError should recognize wrapped error")
	}

	// Non-retryable error.
	plain := fmt.Errorf("plain error")
	if _, ok := IsRetryableHTTPError(plain); ok {
		t.Error("IsRetryableHTTPError should not recognize plain error")
	}

	// Nil error.
	if _, ok := IsRetryableHTTPError(nil); ok {
		t.Error("IsRetryableHTTPError should not recognize nil")
	}
}

// ---------------------------------------------------------------------------
// parseRetryAfter tests
// ---------------------------------------------------------------------------

func TestParseRetryAfter_Numeric(t *testing.T) {
	t.Parallel()

	d := parseRetryAfter("5")
	if d != 5*time.Second {
		t.Errorf("expected 5s, got %v", d)
	}

	d = parseRetryAfter("0")
	if d != 0 {
		t.Errorf("expected 0 for '0', got %v", d)
	}

	d = parseRetryAfter("-1")
	if d != 0 {
		t.Errorf("expected 0 for '-1', got %v", d)
	}
}

func TestParseRetryAfter_EmptyAndMalformed(t *testing.T) {
	t.Parallel()

	if d := parseRetryAfter(""); d != 0 {
		t.Errorf("expected 0 for empty, got %v", d)
	}
	if d := parseRetryAfter("   "); d != 0 {
		t.Errorf("expected 0 for whitespace, got %v", d)
	}
	if d := parseRetryAfter("not-a-number"); d != 0 {
		t.Errorf("expected 0 for garbage, got %v", d)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	t.Parallel()

	// Future date (1 hour from now). Use UTC to match http.TimeFormat's "GMT" label.
	future := time.Now().UTC().Add(1 * time.Hour).Format(http.TimeFormat)
	d := parseRetryAfter(future)
	if d <= 0 {
		t.Errorf("expected positive duration for future HTTP-date, got %v", d)
	}
	// Should be roughly 1 hour (within 10s tolerance).
	if d < 55*time.Minute || d > 65*time.Minute {
		t.Errorf("expected ~1h duration, got %v", d)
	}

	// Past date should return 0.
	past := time.Now().UTC().Add(-1 * time.Hour).Format(http.TimeFormat)
	d = parseRetryAfter(past)
	if d != 0 {
		t.Errorf("expected 0 for past HTTP-date, got %v", d)
	}
}

// ---------------------------------------------------------------------------
// isRetryableError fast-path for RetryableHTTPError
// ---------------------------------------------------------------------------

func TestIsRetryableError_RetryableHTTPErrorFastPath(t *testing.T) {
	t.Parallel()

	err := &RetryableHTTPError{
		StatusCode: 429,
		Status:     "Too Many Requests",
	}
	if !isRetryableError(err) {
		t.Error("RetryableHTTPError should be retryable")
	}

	// Wrapped in fmt.Errorf.
	wrapped := fmt.Errorf("outer: %w", err)
	if !isRetryableError(wrapped) {
		t.Error("wrapped RetryableHTTPError should be retryable")
	}
}

// ---------------------------------------------------------------------------
// DoVisionRetry honors Retry-After header tests
// ---------------------------------------------------------------------------

func TestRetryAfter_HonorsHeader429(t *testing.T) {
	t.Parallel()

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ctx := context.Background()
	start := time.Now()
	err := DoVisionRetry(ctx, func(ctx context.Context) error {
		resp, err := http.Get(srv.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			return &RetryableHTTPError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Method:     "GET",
				URL:        srv.URL,
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			}
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil
	}, RetryOptions{
		OpName:      "test_retry_after",
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    2000 * time.Millisecond,
		JitterPct:   0,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
	// The delay should be ~1s (the Retry-After value), not the 10ms base.
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected delay ~1s (Retry-After), but elapsed only %v", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("delay too long: %v", elapsed)
	}
}

func TestRetryAfter_IgnoresMalformedHeader(t *testing.T) {
	t.Parallel()

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "not-a-number")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ctx := context.Background()
	start := time.Now()
	err := DoVisionRetry(ctx, func(ctx context.Context) error {
		resp, err := http.Get(srv.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			return &RetryableHTTPError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Method:     "GET",
				URL:        srv.URL,
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			}
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil
	}, RetryOptions{
		OpName:      "test_malformed_retry_after",
		MaxAttempts: 3,
		BaseDelay:   50 * time.Millisecond,
		MaxDelay:    200 * time.Millisecond,
		JitterPct:   0,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
	// With malformed header, should fall back to exponential backoff (50ms).
	// Total elapsed should be much less than 1s.
	if elapsed > 2*time.Second {
		t.Errorf("expected fallback to exponential backoff, but elapsed %v (too long)", elapsed)
	}
}

func TestRetryAfter_HTTPDateParsed(t *testing.T) {
	t.Parallel()

	attempts := 0
	// Set Retry-After to 5 seconds from now as an HTTP-date.
	// Use UTC to match http.TimeFormat's "GMT" label so the parsed time
	// is accurate regardless of the local timezone.
	futureDate := time.Now().UTC().Add(5 * time.Second).Format(http.TimeFormat)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", futureDate)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ctx := context.Background()
	start := time.Now()
	err := DoVisionRetry(ctx, func(ctx context.Context) error {
		resp, err := http.Get(srv.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			return &RetryableHTTPError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Method:     "GET",
				URL:        srv.URL,
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			}
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil
	}, RetryOptions{
		OpName:      "test_http_date_retry_after",
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    10000 * time.Millisecond,
		JitterPct:   0,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
	// Delay should be ~5s (the HTTP-date parsed duration), significantly more
	// than the 10ms base backoff. Allow generous tolerance for -race overhead.
	if elapsed < 4*time.Second {
		t.Errorf("expected delay ~5s (HTTP-date), but elapsed only %v", elapsed)
	}
	if elapsed > 12*time.Second {
		t.Errorf("delay too long: %v", elapsed)
	}
}

func TestRetryAfter_CappedAtMaxDelay(t *testing.T) {
	t.Parallel()

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ctx := context.Background()
	start := time.Now()
	err := DoVisionRetry(ctx, func(ctx context.Context) error {
		resp, err := http.Get(srv.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			return &RetryableHTTPError{
				StatusCode: resp.StatusCode,
				Status:     resp.Status,
				Method:     "GET",
				URL:        srv.URL,
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			}
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil
	}, RetryOptions{
		OpName:      "test_capped_retry_after",
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    100 * time.Millisecond,
		JitterPct:   0,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
	// Retry-After says 60s but MaxDelay is 100ms, so should cap to 100ms.
	if elapsed > 1*time.Second {
		t.Errorf("expected delay capped at 100ms (MaxDelay), but elapsed %v", elapsed)
	}
	// Should be at least ~80ms (allowing some slack for jitter=0).
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected delay ~100ms, but elapsed only %v", elapsed)
	}
}
