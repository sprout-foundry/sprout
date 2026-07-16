package agent

import (
	"errors"
	"fmt"
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// ---------------------------------------------------------------------------
// ClassifyError: Nil
// ---------------------------------------------------------------------------

func TestClassifyError_NilError(t *testing.T) {
	action := ClassifyError(nil)
	if action != ActionRetry {
		t.Errorf("expected ActionRetry for nil error, got %s", action)
	}
}

// ---------------------------------------------------------------------------
// ClassifyError: Each typed error
// ---------------------------------------------------------------------------

func TestClassifyError_TransientError(t *testing.T) {
	err := agenterrors.NewTransientError("network timeout", nil)
	action := ClassifyError(err)
	if action != ActionRetry {
		t.Errorf("expected ActionRetry for TransientError, got %s", action)
	}
}

func TestClassifyError_RateLimitError(t *testing.T) {
	err := agenterrors.NewRateLimitError("rate limited", nil, "openai")
	action := ClassifyError(err)
	if action != ActionRetry {
		t.Errorf("expected ActionRetry for RateLimitError, got %s", action)
	}
}

func TestClassifyError_SecurityError(t *testing.T) {
	err := agenterrors.NewSecurityError("path traversal detected", nil)
	action := ClassifyError(err)
	if action != ActionEscalate {
		t.Errorf("expected ActionEscalate for SecurityError, got %s", action)
	}
}

func TestClassifyError_PermissionError(t *testing.T) {
	err := agenterrors.NewPermission("shell_command rejected (timed_out): approval timed out", nil)
	action := ClassifyError(err)
	if action != ActionFail {
		t.Errorf("expected ActionFail for PermissionError, got %s", action)
	}
}

func TestClassifyError_InvalidInputError(t *testing.T) {
	err := agenterrors.NewInvalidInputError("bad argument", nil)
	action := ClassifyError(err)
	if action != ActionFail {
		t.Errorf("expected ActionFail for InvalidInputError, got %s", action)
	}
}

func TestClassifyError_ContextError(t *testing.T) {
	err := agenterrors.NewContextError("conversation too long", nil)
	action := ClassifyError(err)
	if action != ActionFail {
		t.Errorf("expected ActionFail for ContextError, got %s", action)
	}
}

func TestClassifyError_PermanentError(t *testing.T) {
	err := agenterrors.NewPermanentError("not supported", nil)
	action := ClassifyError(err)
	if action != ActionFail {
		t.Errorf("expected ActionFail for PermanentError, got %s", action)
	}
}

func TestClassifyError_ProviderError(t *testing.T) {
	// ProviderError with an auth-related cause is non-retryable;
	// ClassifyError maps ProviderError → ActionFail.
	err := agenterrors.NewProviderError("auth failed", errors.New("unauthorized"), "openai", "gpt-4")
	action := ClassifyError(err)
	if action != ActionFail {
		t.Errorf("expected ActionFail for ProviderError, got %s", action)
	}
}

// ---------------------------------------------------------------------------
// ClassifyError: Wrapped / priority / unknown
// ---------------------------------------------------------------------------

func TestClassifyError_WrappedTransientError(t *testing.T) {
	// A TransientError wrapped in a generic error should still be detected
	// because the helper functions use errors.As which unwraps.
	inner := agenterrors.NewTransientError("network timeout", nil)
	wrapped := fmt.Errorf("tool failed: %w", inner)

	action := ClassifyError(wrapped)
	if action != ActionRetry {
		t.Errorf("expected ActionRetry for wrapped TransientError, got %s", action)
	}
}

func TestClassifyError_UnknownError(t *testing.T) {
	err := errors.New("some random error")
	action := ClassifyError(err)
	if action != ActionRetry {
		t.Errorf("expected ActionRetry for unknown error (default), got %s", action)
	}
}

func TestClassifyError_SecurityTakesPriority(t *testing.T) {
	// Wrapping a SecurityError inside another error must still escalate
	// since Security is checked first in ClassifyError.
	inner := agenterrors.NewSecurityError("injection detected", nil)
	wrapped := fmt.Errorf("processing failed: %w", inner)

	action := ClassifyError(wrapped)
	if action != ActionEscalate {
		t.Errorf("expected ActionEscalate for wrapped SecurityError, got %s", action)
	}
}

// ---------------------------------------------------------------------------
// RetryAction.String()
// ---------------------------------------------------------------------------

func TestRetryAction_String(t *testing.T) {
	tests := []struct {
		action   RetryAction
		expected string
	}{
		{ActionRetry, "retry"},
		{ActionFail, "fail"},
		{ActionEscalate, "escalate"},
		{RetryAction(99), "unknown"},
		{RetryAction(-1), "unknown"},
	}

	for _, tc := range tests {
		got := tc.action.String()
		if got != tc.expected {
			t.Errorf("RetryAction(%d).String() = %q, want %q", tc.action, got, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Table-driven: All error types in one pass
// ---------------------------------------------------------------------------

func TestClassifyError_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected RetryAction
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: ActionRetry,
		},
		{
			name:     "transient error",
			err:      agenterrors.NewTransientError("timeout", nil),
			expected: ActionRetry,
		},
		{
			name:     "rate limit error",
			err:      agenterrors.NewRateLimitError("too many requests", nil, "anthropic"),
			expected: ActionRetry,
		},
		{
			name:     "security error",
			err:      agenterrors.NewSecurityError("blocked command", nil),
			expected: ActionEscalate,
		},
		{
			name:     "permission error (TypedError)",
			err:      agenterrors.NewPermission("approval timed out", nil),
			expected: ActionFail,
		},
		{
			name:     "wrapped permission error",
			err:      fmt.Errorf("layer: %w", agenterrors.NewPermission("rejected", nil)),
			expected: ActionFail,
		},
		{
			name:     "invalid input error",
			err:      agenterrors.NewInvalidInputError("missing field", nil),
			expected: ActionFail,
		},
		{
			name:     "context error",
			err:      agenterrors.NewContextError("context overflow", nil),
			expected: ActionFail,
		},
		{
			name:     "permanent error",
			err:      agenterrors.NewPermanentError("feature disabled", nil),
			expected: ActionFail,
		},
		{
			name:     "provider error with auth cause",
			err:      agenterrors.NewProviderError("api error", errors.New("401 unauthorized"), "openai", "gpt-4"),
			expected: ActionFail,
		},
		{
			name:     "provider error with server error cause",
			err:      agenterrors.NewProviderError("api error", errors.New("503 overloaded"), "openai", "gpt-4"),
			expected: ActionRetry,
		},
		{
			name:     "wrapped transient error",
			err:      fmt.Errorf("layer: %w", agenterrors.NewTransientError("timeout", nil)),
			expected: ActionRetry,
		},
		{
			name:     "wrapped security error",
			err:      fmt.Errorf("layer: %w", agenterrors.NewSecurityError("injection", nil)),
			expected: ActionEscalate,
		},
		{
			name:     "generic unknown error",
			err:      errors.New("mystery failure"),
			expected: ActionRetry,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyError(tc.err)
			if got != tc.expected {
				t.Errorf("ClassifyError() = %s, want %s", got, tc.expected)
			}
		})
	}
}
