//go:build !js

package tools

// vision_retry_http_error.go — the typed HTTP retry error, split out of
// vision_retry.go. RetryableHTTPError carries a retryable HTTP failure plus a
// server-supplied Retry-After hint; parseRetryAfter parses that header;
// isHTTPError sniffs an HTTP failure out of an error message.
import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

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
