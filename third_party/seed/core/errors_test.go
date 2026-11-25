package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- Sentinel error tests ---

func TestSentinelErrors_NonNil(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrNoProvider", ErrNoProvider},
		{"ErrNoExecutor", ErrNoExecutor},
		{"ErrInterrupted", ErrInterrupted},
		{"ErrMaxIterations", ErrMaxIterations},
		{"ErrPaused", ErrPaused},
	}
	for _, s := range sentinels {
		t.Run(s.name, func(t *testing.T) {
			if s.err == nil {
				t.Fatal("sentinel error is nil")
			}
			if s.err.Error() == "" {
				t.Error("sentinel error message is empty")
			}
		})
	}
}

// --- TransientError tests ---

func TestTransientError_Error(t *testing.T) {
	wrapped := fmt.Errorf("connection refused")
	tests := []struct {
		name string
		err  *TransientError
		want string
	}{
		{
			name: "minimal",
			err:  &TransientError{},
			want: "transient error",
		},
		{
			name: "with op",
			err:  &TransientError{Op: "chat"},
			want: "transient error during chat",
		},
		{
			name: "with provider",
			err:  &TransientError{Provider: "openai"},
			want: "transient error (openai)",
		},
		{
			name: "with op and provider",
			err:  &TransientError{Op: "stream", Provider: "anthropic"},
			want: "transient error during stream (anthropic)",
		},
		{
			name: "with wrapped",
			err:  &TransientError{Op: "chat", Provider: "openai", Wrapped: wrapped},
			want: "transient error during chat (openai): connection refused",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTransientError_Unwrap(t *testing.T) {
	wrapped := fmt.Errorf("inner")
	err := &TransientError{Wrapped: wrapped}
	if got := errors.Unwrap(err); got != wrapped {
		t.Errorf("Unwrap() = %v, want %v", got, wrapped)
	}
}

func TestTransientError_ErrorsAs(t *testing.T) {
	wrapped := fmt.Errorf("inner")
	outer := &TransientError{Op: "chat", Wrapped: wrapped}
	var target *TransientError
	if !errors.As(outer, &target) {
		t.Error("errors.As failed for TransientError")
	}
	if target.Op != "chat" {
		t.Errorf("expected Op 'chat', got %q", target.Op)
	}
}

func TestTransientError_ErrorsIs(t *testing.T) {
	wrapped := fmt.Errorf("inner")
	outer := &TransientError{Wrapped: wrapped}
	if !errors.Is(outer, wrapped) {
		t.Error("errors.Is should find wrapped error")
	}
}

func TestIsTransient(t *testing.T) {
	if !IsTransient(&TransientError{}) {
		t.Error("IsTransient should return true for TransientError")
	}
	if IsTransient(fmt.Errorf("other")) {
		t.Error("IsTransient should return false for other errors")
	}
}

func TestTransientError_RetryAfter(t *testing.T) {
	d := 5 * time.Second
	err := &TransientError{RetryAfter: d}
	if err.RetryAfter != d {
		t.Errorf("RetryAfter = %v, want %v", err.RetryAfter, d)
	}
}

// --- RateLimitError tests ---

func TestRateLimitError_Error(t *testing.T) {
	wrapped := fmt.Errorf("429 too many requests")
	tests := []struct {
		name string
		err  *RateLimitError
		want string
	}{
		{
			name: "minimal",
			err:  &RateLimitError{},
			want: "rate limit exceeded",
		},
		{
			name: "with provider",
			err:  &RateLimitError{Provider: "openai"},
			want: "rate limit exceeded (openai)",
		},
		{
			name: "with attempt",
			err:  &RateLimitError{Attempt: 3},
			want: "rate limit exceeded, attempt 3",
		},
		{
			name: "with provider and attempt",
			err:  &RateLimitError{Provider: "anthropic", Attempt: 2},
			want: "rate limit exceeded (anthropic), attempt 2",
		},
		{
			name: "with wrapped",
			err:  &RateLimitError{Provider: "openai", Wrapped: wrapped},
			want: "rate limit exceeded (openai): 429 too many requests",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRateLimitError_Unwrap(t *testing.T) {
	wrapped := fmt.Errorf("inner")
	err := &RateLimitError{Wrapped: wrapped}
	if got := errors.Unwrap(err); got != wrapped {
		t.Errorf("Unwrap() = %v, want %v", got, wrapped)
	}
}

func TestRateLimitError_ErrorsAs(t *testing.T) {
	outer := &RateLimitError{Provider: "openai", Attempt: 5, Wrapped: fmt.Errorf("429")}
	var target *RateLimitError
	if !errors.As(outer, &target) {
		t.Error("errors.As failed for RateLimitError")
	}
	if target.Provider != "openai" || target.Attempt != 5 {
		t.Errorf("got Provider=%q Attempt=%d, want Provider=openai Attempt=5", target.Provider, target.Attempt)
	}
}

func TestIsRateLimit(t *testing.T) {
	if !IsRateLimit(&RateLimitError{}) {
		t.Error("IsRateLimit should return true for RateLimitError")
	}
	if IsRateLimit(fmt.Errorf("other")) {
		t.Error("IsRateLimit should return false for other errors")
	}
}

func TestRateLimitError_RetryAfter(t *testing.T) {
	d := 30 * time.Second
	err := &RateLimitError{RetryAfter: d}
	if err.RetryAfter != d {
		t.Errorf("RetryAfter = %v, want %v", err.RetryAfter, d)
	}
}

// --- ContextOverflowError tests ---

func TestContextOverflowError_Error(t *testing.T) {
	wrapped := fmt.Errorf("maximum context length exceeded")
	tests := []struct {
		name string
		err  *ContextOverflowError
		want string
	}{
		{
			name: "minimal",
			err:  &ContextOverflowError{},
			want: "context window exceeded",
		},
		{
			name: "with tokens",
			err:  &ContextOverflowError{TokensUsed: 130000, TokensLimit: 128000},
			want: "context window exceeded (130000/128000 tokens)",
		},
		{
			name: "with wrapped",
			err:  &ContextOverflowError{Wrapped: wrapped},
			want: "context window exceeded: maximum context length exceeded",
		},
		{
			name: "full",
			err:  &ContextOverflowError{TokensUsed: 130000, TokensLimit: 128000, Wrapped: wrapped},
			want: "context window exceeded (130000/128000 tokens): maximum context length exceeded",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContextOverflowError_Unwrap(t *testing.T) {
	wrapped := fmt.Errorf("inner")
	err := &ContextOverflowError{Wrapped: wrapped}
	if got := errors.Unwrap(err); got != wrapped {
		t.Errorf("Unwrap() = %v, want %v", got, wrapped)
	}
}

func TestContextOverflowError_ErrorsAs(t *testing.T) {
	outer := &ContextOverflowError{TokensUsed: 1000, TokensLimit: 500, Wrapped: fmt.Errorf("too long")}
	var target *ContextOverflowError
	if !errors.As(outer, &target) {
		t.Error("errors.As failed for ContextOverflowError")
	}
	if target.TokensUsed != 1000 || target.TokensLimit != 500 {
		t.Errorf("got TokensUsed=%d TokensLimit=%d, want 1000/500", target.TokensUsed, target.TokensLimit)
	}
}

func TestIsContextOverflow(t *testing.T) {
	if !IsContextOverflow(&ContextOverflowError{}) {
		t.Error("IsContextOverflow should return true for ContextOverflowError")
	}
	if IsContextOverflow(fmt.Errorf("other")) {
		t.Error("IsContextOverflow should return false for other errors")
	}
}

// --- AuthError tests ---

func TestAuthError_Error(t *testing.T) {
	wrapped := fmt.Errorf("401 unauthorized")
	tests := []struct {
		name string
		err  *AuthError
		want string
	}{
		{
			name: "minimal",
			err:  &AuthError{},
			want: "authentication failed",
		},
		{
			name: "with provider",
			err:  &AuthError{Provider: "openai"},
			want: "authentication failed (openai)",
		},
		{
			name: "with wrapped",
			err:  &AuthError{Provider: "anthropic", Wrapped: wrapped},
			want: "authentication failed (anthropic): 401 unauthorized",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthError_Unwrap(t *testing.T) {
	wrapped := fmt.Errorf("inner")
	err := &AuthError{Wrapped: wrapped}
	if got := errors.Unwrap(err); got != wrapped {
		t.Errorf("Unwrap() = %v, want %v", got, wrapped)
	}
}

func TestAuthError_ErrorsAs(t *testing.T) {
	outer := &AuthError{Provider: "openai", Wrapped: fmt.Errorf("invalid api key")}
	var target *AuthError
	if !errors.As(outer, &target) {
		t.Error("errors.As failed for AuthError")
	}
	if target.Provider != "openai" {
		t.Errorf("got Provider=%q, want openai", target.Provider)
	}
}

func TestAuthError_ErrorsIs(t *testing.T) {
	wrapped := fmt.Errorf("invalid api key")
	outer := &AuthError{Wrapped: wrapped}
	if !errors.Is(outer, wrapped) {
		t.Error("errors.Is should find wrapped error")
	}
}

func TestIsAuthError(t *testing.T) {
	if !IsAuthError(&AuthError{}) {
		t.Error("IsAuthError should return true for AuthError")
	}
	if IsAuthError(fmt.Errorf("other")) {
		t.Error("IsAuthError should return false for other errors")
	}
}

// --- Cross-type checks ---

func TestTypedErrors_DistinctTypes(t *testing.T) {
	// Ensure the helper functions don't cross-match.
	errs := []error{
		&TransientError{Op: "chat"},
		&RateLimitError{Provider: "openai"},
		&ContextOverflowError{TokensUsed: 100},
		&AuthError{Provider: "anthropic"},
	}
	names := []string{"Transient", "RateLimit", "ContextOverflow", "Auth"}
	checkers := []func(error) bool{IsTransient, IsRateLimit, IsContextOverflow, IsAuthError}

	for i, checker := range checkers {
		for j, err := range errs {
			expected := i == j
			if checker(err) != expected {
				t.Errorf("%s(%s) = %v, want %v",
					names[i], names[j], checker(err), expected)
			}
		}
	}
}

func TestTypedErrors_NestedUnwrap(t *testing.T) {
	// Verify that a typed error wrapped inside another error is still discoverable.
	inner := &AuthError{Provider: "openai", Wrapped: fmt.Errorf("bad key")}
	outer := fmt.Errorf("request failed: %w", inner)

	var auth *AuthError
	if !errors.As(outer, &auth) {
		t.Error("errors.As should find AuthError through fmt.Errorf wrapper")
	}
	if auth.Provider != "openai" {
		t.Errorf("got Provider=%q, want openai", auth.Provider)
	}
}

// --- Nil Unwrap tests ---

func TestTransientError_UnwrapNil(t *testing.T) {
	err := &TransientError{Op: "chat"}
	if errors.Unwrap(err) != nil {
		t.Errorf("Unwrap() = %v, want nil", errors.Unwrap(err))
	}
}

func TestRateLimitError_UnwrapNil(t *testing.T) {
	err := &RateLimitError{Provider: "openai"}
	if errors.Unwrap(err) != nil {
		t.Errorf("Unwrap() = %v, want nil", errors.Unwrap(err))
	}
}

func TestContextOverflowError_UnwrapNil(t *testing.T) {
	err := &ContextOverflowError{TokensUsed: 100}
	if errors.Unwrap(err) != nil {
		t.Errorf("Unwrap() = %v, want nil", errors.Unwrap(err))
	}
}

func TestAuthError_UnwrapNil(t *testing.T) {
	err := &AuthError{Provider: "openai"}
	if errors.Unwrap(err) != nil {
		t.Errorf("Unwrap() = %v, want nil", errors.Unwrap(err))
	}
}

// --- RateLimitError Attempt=0 edge case ---

func TestRateLimitError_AttemptZero(t *testing.T) {
	err := &RateLimitError{Provider: "openai", Attempt: 0}
	msg := err.Error()
	// Attempt 0 should not appear in the message (guarded by Attempt > 0)
	if strings.Contains(msg, "attempt") {
		t.Errorf("Attempt=0 should not appear in error message, got: %s", msg)
	}
}
