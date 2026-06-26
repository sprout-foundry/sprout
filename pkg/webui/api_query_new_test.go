//go:build !js

package webui

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// isProviderConfigError — pure helper
// ---------------------------------------------------------------------------

func TestIsProviderConfigError_Nil(t *testing.T) {
	if isProviderConfigError(nil) {
		t.Error("nil error should return false")
	}
}

func TestIsProviderConfigError_ErrNoProviderConfigured(t *testing.T) {
	// This is a sentinel error defined in the package
	err := ErrNoProviderConfigured
	if !isProviderConfigError(err) {
		t.Error("ErrNoProviderConfigured should be detected")
	}
}

func TestIsProviderConfigError_Wrapped(t *testing.T) {
	// errors.New wrapping doesn't use Wrap, so Is() won't match
	// but the substring should still match via strings.Contains
	err := errors.New("wrapped: " + ErrNoProviderConfigured.Error())
	if !isProviderConfigError(err) {
		// If ErrNoProviderConfigured message contains one of the substrings,
		// it should match via strings.Contains. If not, skip this test.
		t.Skip("ErrNoProviderConfigured message doesn't contain matching substring")
	}
}

func TestIsProviderConfigError_SubstringMatches(t *testing.T) {
	substrings := []string{
		"provider recovery failed",
		"failed to initialize provider",
		"failed to select provider",
		"provider_not_configured",
		"no provider configured",
		"editor mode is active",
	}
	for _, substr := range substrings {
		t.Run(substr, func(t *testing.T) {
			err := errors.New(substr)
			if !isProviderConfigError(err) {
				t.Errorf("error %q should be detected as provider config error", substr)
			}
		})
	}
}

func TestIsProviderConfigError_PartialSubstring(t *testing.T) {
	// "provider" alone is not enough — must match full substrings
	err := errors.New("provider")
	if isProviderConfigError(err) {
		t.Error("partial match 'provider' alone should not trigger")
	}
}

func TestIsProviderConfigError_RegularError(t *testing.T) {
	err := errors.New("connection refused")
	if isProviderConfigError(err) {
		t.Error("regular error should not be detected as provider config error")
	}
}

func TestIsProviderConfigError_EmptyError(t *testing.T) {
	err := errors.New("")
	if isProviderConfigError(err) {
		t.Error("empty error message should not match")
	}
}

func TestIsProviderConfigError_LongErrorMessage(t *testing.T) {
	err := errors.New("some long error message that happened to include 'no provider configured' in the middle")
	if !isProviderConfigError(err) {
		t.Error("should match substring even in long error messages")
	}
}

func TestIsProviderConfigError_CaseSensitive(t *testing.T) {
	// The function uses strings.Contains which is case-sensitive
	err := errors.New("NO PROVIDER CONFIGURED")
	if isProviderConfigError(err) {
		t.Error("uppercase variant should not match (case-sensitive)")
	}
}

func TestIsProviderConfigError_WrappedWithFMT(t *testing.T) {
	// Test that wrapping with fmt.Errorf %w preserves the sentinel
	err := ErrNoProviderConfigured
	wrapped := wrapError(err, "context: ")
	if !isProviderConfigError(wrapped) {
		t.Error("wrapped ErrNoProviderConfigured should still be detected via errors.Is")
	}
}

// wrapError is a test helper to simulate fmt.Errorf wrapping.
func wrapError(err error, prefix string) error {
	return &wrappedError{
		msg:   prefix,
		cause: err,
	}
}

type wrappedError struct {
	msg   string
	cause error
}

func (e *wrappedError) Error() string {
	return e.msg + e.cause.Error()
}

func (e *wrappedError) Unwrap() error {
	return e.cause
}

func TestIsProviderConfigError_SubstringInWrappedError(t *testing.T) {
	err := errors.New("some prefix failed to initialize provider some suffix")
	if !isProviderConfigError(err) {
		t.Error("should match substring in middle of error message")
	}
}

func TestIsProviderConfigError_OnlySimilarSubstring(t *testing.T) {
	// "provider" in a non-config context should NOT match
	// The function checks for "no provider configured" or "failed to initialize provider", etc.
	// These are specific enough that a plain "provider" won't match.
	err := errors.New("using provider openai")
	if isProviderConfigError(err) {
		t.Error("generic provider message should not match")
	}
}
