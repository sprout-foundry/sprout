package agent

import (
	"errors"
	"strings"
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// A transient browse failure must reach the agent as retryable, so a page that
// resets or times out once does not end the investigation. A deterministic
// failure must not be retried: the same URL fails the same way every time.
func TestClassifyBrowseError(t *testing.T) {
	t.Run("transient failures are retryable", func(t *testing.T) {
		for _, msg := range []string{
			"net::ERR_CONNECTION_RESET",
			"net::ERR_TIMEOUT",
			"net::ERR_EMPTY_RESPONSE",
		} {
			err := classifyBrowseError(errors.New(msg), "https://example.com", "text")
			if !agenterrors.IsTransient(err) && !agenterrors.IsRetryable(err) {
				t.Errorf("%q produced a non-retryable error: %v", msg, err)
			}
			if !strings.Contains(err.Error(), "https://example.com") {
				t.Errorf("%q error does not name the URL: %v", msg, err)
			}
			if !strings.Contains(err.Error(), "action=text") {
				t.Errorf("%q error does not name the action: %v", msg, err)
			}
		}
	})

	t.Run("deterministic failures are not retryable", func(t *testing.T) {
		for _, msg := range []string{
			"net::ERR_NAME_NOT_RESOLVED",
			"net::ERR_CONNECTION_REFUSED",
			"net::ERR_INVALID_URL",
		} {
			err := classifyBrowseError(errors.New(msg), "https://example.com", "dom")
			if agenterrors.IsRetryable(err) {
				t.Errorf("%q was classified retryable; retrying only delays the error", msg)
			}
		}
	})
}
