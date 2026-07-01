//go:build !js

package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// RetryOptions — configuration for DoVisionRetry.
//
// Precedence for numeric fields:
//   1. If the field is non-zero in opts, that value is used (opts wins).
//   2. Otherwise the corresponding VISION_RETRY_* env var is read.
//   3. If the env var is absent or invalid, the documented default is used.
//
// MaxAttempts special values:
//   - 1 → retries are disabled (single attempt only).
//   - 0 → ignore env var and use the default (3).
// ---------------------------------------------------------------------------

// RetryOptions configures DoVisionRetry.
type RetryOptions struct {
	MaxAttempts int           // total attempts (including first); 1 disables; 0 falls back to default
	BaseDelay   time.Duration // base for exponential backoff (200ms default)
	MaxDelay    time.Duration // cap on backoff (1600ms default)
	JitterPct   int           // ± jitter percent (20 default)
	IsRetryable func(error) bool // optional classifier; uses default if nil
	OpName      string        // for logging
}

// defaultRetryOptions returns the effective retry options after applying
// env-var overrides. opts fields that are non-zero take precedence over
// environment variables.
func defaultRetryOptions(opts RetryOptions) RetryOptions {
	// MaxAttempts
	if opts.MaxAttempts == 0 {
		if raw := configuration.GetEnvSimple("VISION_RETRY_ATTEMPTS"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v != 0 {
				opts.MaxAttempts = v
			}
		}
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 3
	}

	// BaseDelay
	if opts.BaseDelay == 0 {
		if raw := configuration.GetEnvSimple("VISION_RETRY_BASE_MS"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				opts.BaseDelay = time.Duration(v) * time.Millisecond
			}
		}
	}
	if opts.BaseDelay == 0 {
		opts.BaseDelay = 200 * time.Millisecond
	}

	// MaxDelay
	if opts.MaxDelay == 0 {
		if raw := configuration.GetEnvSimple("VISION_RETRY_MAX_MS"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				opts.MaxDelay = time.Duration(v) * time.Millisecond
			}
		}
	}
	if opts.MaxDelay == 0 {
		opts.MaxDelay = 1600 * time.Millisecond
	}

	// JitterPct
	if opts.JitterPct == 0 {
		if raw := configuration.GetEnvSimple("VISION_RETRY_JITTER_PCT"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
				opts.JitterPct = v
			}
		}
	}
	if opts.JitterPct == 0 {
		opts.JitterPct = 20
	}

	// OpName
	if opts.OpName == "" {
		opts.OpName = "vision"
	}

	return opts
}

// ---------------------------------------------------------------------------
// HTTP status code extraction from errors.
//
// Errors from the provider layer are formatted as "HTTP <code>[: <detail>]".
// We parse the code to decide whether to retry.
// ---------------------------------------------------------------------------

var httpStatusCodeRe = regexp.MustCompile(`HTTP (\d{3})`)

// extractHTTPStatusCode returns the HTTP status code embedded in the error
// message, or 0 if none is found.
func extractHTTPStatusCode(err error) int {
	if err == nil {
		return 0
	}
	matches := httpStatusCodeRe.FindStringSubmatch(err.Error())
	if len(matches) < 2 {
		return 0
	}
	code, _ := strconv.Atoi(matches[1])
	return code
}

// ---------------------------------------------------------------------------
// isRetryableError — default classifier for vision-related errors.
// ---------------------------------------------------------------------------

// isRetryableError returns true if the error is likely transient and worth
// retrying. It covers:
//
// - 5xx HTTP errors
// - 408 (Request Timeout), 429 (Too Many Requests) HTTP errors
// - Network errors (net.Error with Timeout() or Temporary())
// - syscall.ECONNRESET, syscall.ETIMEDOUT
// - io.EOF, io.ErrUnexpectedEOF
//
// Non-retryable errors include other 4xx responses (400, 401, 403, 404, etc.)
// and any error that doesn't match the patterns above.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Fast-path: RetryableHTTPError is always retryable by definition.
	if _, ok := IsRetryableHTTPError(err); ok {
		return true
	}

	// Check HTTP status code from error message.
	code := extractHTTPStatusCode(err)
	if code > 0 {
		// Retry on 5xx and specific 4xx (408, 429).
		if code >= 500 {
			return true
		}
		if code == 408 || code == 429 {
			return true
		}
		// Other 4xx are NOT retryable.
		return false
	}

	// Network errors.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	// Connection resets / timeouts from syscall.
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}

	// Unexpected EOF conditions.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	// Context cancellation is NOT retryable.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	return false
}

// ---------------------------------------------------------------------------
// Backoff computation with jitter and Retry-After support.
// ---------------------------------------------------------------------------

// computeBackoff calculates the exponential backoff duration for the given
// attempt number, applying jitter and capping at maxDelay.
func computeBackoff(attempt int, baseDelay, maxDelay time.Duration, jitterPct int) time.Duration {
	// Exponential: baseDelay * 2^(attempt-1), capped at maxDelay.
	delay := baseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}

	// Apply ±jitterPct% jitter.
	if jitterPct > 0 && delay > 0 {
		jitterRange := int(float64(delay) * float64(jitterPct) / 100)
		if jitterRange > 0 {
			jitter := time.Duration(rand.Intn(2*jitterRange+1) - jitterRange)
			delay += jitter
			if delay < 0 {
				delay = 0
			}
		}
	}

	return delay
}

// ---------------------------------------------------------------------------
// DoVisionRetry — main retry loop.
// ---------------------------------------------------------------------------

// DoVisionRetry runs op with retries, respecting ctx cancellation.
//
// The op function is called with ctx so it can be cancelled independently.
// Between failed attempts, DoVisionRetry sleeps with an exponential backoff
// (plus jitter) and checks ctx.Done() before each sleep.
//
// Returns nil on success, or the last error after exhausting all attempts.
func DoVisionRetry(ctx context.Context, op func(ctx context.Context) error, opts RetryOptions) error {
	opts = defaultRetryOptions(opts)

	retryable := opts.IsRetryable
	if retryable == nil {
		retryable = isRetryableError
	}

	opName := opts.OpName
	maxAttempts := opts.MaxAttempts
	baseDelay := opts.BaseDelay
	maxDelay := opts.MaxDelay
	jitterPct := opts.JitterPct

	// Single-attempt mode: no retries.
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = op(ctx)
		if lastErr == nil {
			return nil
		}

		// Don't sleep after the last attempt.
		if attempt >= maxAttempts {
			break
		}

		// Check if the error is retryable.
		if !retryable(lastErr) {
			break
		}

		// Compute backoff delay.
		delay := computeBackoff(attempt, baseDelay, maxDelay, jitterPct)

		// Honor server-provided Retry-After header if present.
		if ra, ok := IsRetryableHTTPError(lastErr); ok && ra.RetryAfter > 0 {
			delay = ra.RetryAfter
			if delay > opts.MaxDelay {
				delay = opts.MaxDelay
			}
		}

		// Check for context cancellation before sleeping.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Log the retry attempt.
		logVisionRetry(attempt+1, delay, opName, lastErr)

		// Sleep with context awareness.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	// Log give-up message.
	logVisionGiveUp(opName, maxAttempts, lastErr)

	return lastErr
}

// ---------------------------------------------------------------------------
// Logging helpers (using slog).
// ---------------------------------------------------------------------------

// logVisionRetry logs an INFO message for a retry attempt.
func logVisionRetry(attempt int, delay time.Duration, opName string, err error) {
	msg := fmt.Sprintf("vision_retry attempt=%d next_backoff=%dms op=%s err=%s",
		attempt, delay.Milliseconds(), opName, compactErrorMessage(err))
	fmt.Println(msg)
}

// logVisionGiveUp logs a WARN message when all retries are exhausted.
func logVisionGiveUp(opName string, attempts int, err error) {
	msg := fmt.Sprintf("[WARN] vision_retry giving up op=%s attempts=%d last_err=%s",
		opName, attempts, compactErrorMessage(err))
	fmt.Println(msg)
}

// compactErrorMessage returns a truncated error message suitable for logging.
func compactErrorMessage(err error) string {
	if err == nil {
		return "none"
	}
	msg := err.Error()
	// Truncate very long messages (e.g., data URLs embedded in errors).
	const maxLen = 200
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "... (truncated)"
	}
	return msg
}

// ---------------------------------------------------------------------------
// RetryableHTTPError — typed error for retryable HTTP failures.
// ---------------------------------------------------------------------------

// RetryableHTTPError describes a retryable HTTP failure with optional
// server-supplied retry hints (Retry-After header, parsed as a duration).
type RetryableHTTPError struct {
	StatusCode int
	Status     string
	Method     string
	URL        string
	RetryAfter time.Duration // 0 means server didn't provide one
	Err        error         // underlying cause (for HTTP errors wrapping a network failure)
}

func (e *RetryableHTTPError) Error() string {
	base := fmt.Sprintf("HTTP %d %s", e.StatusCode, e.Status)
	if e.Method != "" && e.URL != "" {
		base = fmt.Sprintf("HTTP %d %s: %s %s", e.StatusCode, e.Status, e.Method, e.URL)
	}
	if e.Err != nil {
		return base + ": " + e.Err.Error()
	}
	return base
}

func (e *RetryableHTTPError) Unwrap() error {
	return e.Err
}

// IsRetryableHTTPError reports whether err is a RetryableHTTPError that
// should be retried. It returns the unwrapped error and true if so.
func IsRetryableHTTPError(err error) (*RetryableHTTPError, bool) {
	if err == nil {
		return nil, false
	}
	var r *RetryableHTTPError
	if errors.As(err, &r) {
		return r, true
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Retry-After header parsing.
// ---------------------------------------------------------------------------

// parseRetryAfter parses the Retry-After header value into a duration.
//
// Supported formats:
//   - Numeric value (seconds) → time.Duration
//   - HTTP-date (e.g., "Wed, 21 Oct 2015 07:28:00 GMT") → duration from now
//   - Empty / unparseable → 0
func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}

	// Try numeric seconds first.
	if secs, err := strconv.ParseInt(header, 10, 64); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}

	// Try HTTP-date format.
	if t, err := http.ParseTime(header); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}

	return 0
}

// ---------------------------------------------------------------------------
// HTTP error helpers for retry-after support.
// ---------------------------------------------------------------------------

// isHTTPError checks if the error message indicates an HTTP error response.
func isHTTPError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP ")
}
