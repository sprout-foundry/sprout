// seed_provider_retry_classify.go — what the model-call retry loops will try
// again: the empty completion, connection-level failures reaching the model
// endpoint, gateway statuses, and the error kinds that were retryable
// before (rate limits, retryable provider and network/timeout errors).

package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"

	core "github.com/sprout-foundry/seed/core"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// isRetryableProviderError reports whether the model-call retry loops retry
// err:
//   - an empty completion (seed's core.ErrZeroChoices) — a gateway in front
//     of the model answers with an empty body while it redeploys;
//   - connection failures reaching the model endpoint: refused, reset, an
//     abrupt "unexpected EOF" (a bare io.EOF is an orderly end, not an
//     outage), timeouts;
//   - a bad gateway (HTTP 502), an unavailable service (503) or a gateway
//     timeout (504);
//   - everything retryable before: rate limits, and retryable provider /
//     network / timeout errors.
//
// A 400, 401, 403, 404, a context-length overflow and other permanent errors
// stay non-retryable and fail at once.
func isRetryableProviderError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, core.ErrZeroChoices) {
		return true
	}
	if isRetryableProviderStatusCode(extractHTTPStatusCode(err.Error())) {
		return true
	}
	if isTransientNetworkError(err) {
		return true
	}
	if agenterrors.IsRateLimited(err) {
		return true
	}
	if agenterrors.IsProviderError(err) && agenterrors.IsRetryable(err) {
		return true
	}
	// Typed network/timeout failures carry their retryability on the
	// TypedError itself (set from RetryableFor at construction). Only
	// network and timeout are retryable in the LLM retry loops; a
	// non-retryable typed error (validation, not_found, ...) still fails at
	// once, as a 401 does.
	if te := agenterrors.AsTypedError(err); te != nil {
		return te.Retryable && (te.Code == agenterrors.CodeNetwork || te.Code == agenterrors.CodeTimeout)
	}
	return false
}

// isRetryableProviderStatusCode reports whether an HTTP status returned by the
// model endpoint means "try again shortly".
func isRetryableProviderStatusCode(code int) bool {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// isTransientNetworkError matches the connection-level failures a provider
// deploy produces on the wire: a refused or reset connection, an abrupt EOF,
// and a timeout. Cancellations are never retried.
func isTransientNetworkError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// Errors formatted by the provider layer rather than wrapping a typed
	// cause still name the fault in their text.
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"connection refused",
		"connection reset",
		"unexpected eof",
		"i/o timeout",
		"timeout awaiting response headers",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}
