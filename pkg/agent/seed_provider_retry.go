// seed_provider_retry.go — the sproutProvider retry / backoff layer: the
// HTTP-status extraction, exponential-backoff delay, retryable-error
// classification, and the provider-error record / clear. Split out of
// seed_provider.go.

package agent

import (
	"strconv"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// extractHTTPStatusCode parses common HTTP error patterns to extract the status code.
// Handles formats like "HTTP 400: msg", "HTTP 400", "400 Bad Request", "error 429", etc.
func extractHTTPStatusCode(msg string) int {
	lower := strings.ToLower(msg)
	// "HTTP 400: ..." or "HTTP 400"
	if idx := strings.Index(lower, "http "); idx >= 0 {
		rest := msg[idx+5:]
		if i, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && i >= 100 && i < 1000 {
			return i
		}
	}
	// "error 429" or "response error: 400" — look for a standalone 3-digit number
	for _, word := range strings.FieldsFunc(lower, func(r rune) bool { return !((r >= '0' && r <= '9') || r == '_') }) {
		if len(word) == 3 {
			if i, err := strconv.Atoi(word); err == nil && i >= 100 && i < 1000 {
				return i
			}
		}
	}
	return 0
}

// exponentialBackoffDelay calculates the delay for a given retry attempt.
// Formula: 2^attempt * baseDelay, capped at maxDelay.
// Base delay is 100ms, max delay is 10s.
func exponentialBackoffDelay(attempt int) time.Duration {
	const baseDelay = 100 * time.Millisecond
	const maxDelay = 10 * time.Second

	delay := baseDelay
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

// isRetryableProviderError returns true if the error should trigger a provider retry.
// Retries are performed on:
//   - RateLimitError (always retryable)
//   - ProviderError with a retryable cause (server errors, overload)
func isRetryableProviderError(err error) bool {
	if err == nil {
		return false
	}
	if agenterrors.IsRateLimited(err) {
		return true
	}
	if agenterrors.IsProviderError(err) && agenterrors.IsRetryable(err) {
		return true
	}
	return false
}

// recordProviderError stores error info in the agent's state for observability.
func (sp *sproutProvider) recordProviderError(err error, retries int) {
	if sp.agent == nil || err == nil {
		return
	}
	msg := err.Error()
	sp.agent.state.SetLastProviderError(&ProviderErrorInfo{
		Timestamp:  time.Now().Format(time.RFC3339),
		Provider:   sp.agent.GetProvider(),
		Model:      sp.agent.GetModel(),
		StatusCode: extractHTTPStatusCode(msg),
		Message:    msg,
		Retries:    retries,
	})
}

// clearProviderError clears the last provider error (on success).
func (sp *sproutProvider) clearProviderError() {
	if sp.agent == nil {
		return
	}
	sp.agent.state.SetLastProviderError(nil)
}

// isBodyTooLargeError reports whether err is an HTTP 413 — the provider
// rejected the request body as oversized. Distinct from context overflow:
// the remedy is shedding payload (inline images), not compacting history.
func isBodyTooLargeError(err error) bool {
	if err == nil {
		return false
	}
	// Provider HTTP errors are formatted as "HTTP <code>" (optionally
	// followed by ": <body>" or " (empty body, ...)"). Match the code
	// precisely — the next character must not be a digit — so
	// "HTTP 4130" never matches.
	msg := err.Error()
	idx := strings.Index(msg, "HTTP 413")
	if idx < 0 {
		return false
	}
	end := idx + len("HTTP 413")
	return end == len(msg) || (msg[end] < '0' || msg[end] > '9')
}
