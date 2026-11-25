package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Helper to classify and check type without assertion noise.
func classify(err error, provider string) error {
	return ClassifyError(err, provider)
}

// --- nil handling ---

func TestClassifyError_Nil(t *testing.T) {
	if got := ClassifyError(nil, "openai"); got != nil {
		t.Errorf("ClassifyError(nil) = %v, want nil", got)
	}
}

// --- passthrough of typed errors ---

func TestClassifyError_Passthrough(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"transient", &TransientError{Op: "chat", Provider: "anthropic", Wrapped: fmt.Errorf("oops")}},
		{"rate-limit", &RateLimitError{Provider: "openai", Wrapped: fmt.Errorf("429")}},
		{"context-overflow", &ContextOverflowError{Wrapped: fmt.Errorf("too long")}},
		{"auth", &AuthError{Provider: "anthropic", Wrapped: fmt.Errorf("bad key")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.err, "openai")
			if got != tt.err {
				t.Errorf("ClassifyError(%T) returned different value", tt.err)
			}
		})
	}
}

// --- RateLimitError patterns ---

func TestClassifyError_RateLimit(t *testing.T) {
	provider := "openai"
	patterns := []string{
		"429 too many requests",
		"rate limit exceeded",
		"rate_limit hit",
		"Too Many Requests",
		"quota exceeded for this project",
		"QUOTA_EXCEEDED",
		"Rate Limit: please slow down",
		"insufficient_quota",
		"insufficient quota",
		"request rate limited",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			err := errors.New(pattern)
			wrapped := classify(err, provider)

			if !IsRateLimit(wrapped) {
				t.Errorf("expected RateLimitError for %q, got %T", pattern, wrapped)
			}

			var rateErr *RateLimitError
			if !errors.As(wrapped, &rateErr) {
				t.Fatalf("errors.As failed for RateLimitError on %q", pattern)
			}
			if rateErr.Provider != provider {
				t.Errorf("Provider = %q, want %q", rateErr.Provider, provider)
			}
			if rateErr.Wrapped != err {
				t.Errorf("Wrapped = %v, want %v", rateErr.Wrapped, err)
			}
		})
	}
}

// --- AuthError patterns ---

func TestClassifyError_Auth(t *testing.T) {
	provider := "anthropic"
	patterns := []string{
		"401 unauthorized",
		"Unauthorized access",
		"Authentication failed",
		"Authentication error",
		"INVALID_API_KEY",
		"invalid api key provided",
		"invalid_api_key",
		"API key is not valid",
		"api key invalid",
		"api key rejected",
		"permission denied",
		"Permission Denied",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			err := errors.New(pattern)
			wrapped := classify(err, provider)

			if !IsAuthError(wrapped) {
				t.Errorf("expected AuthError for %q, got %T", pattern, wrapped)
			}

			var authErr *AuthError
			if !errors.As(wrapped, &authErr) {
				t.Fatalf("errors.As failed for AuthError on %q", pattern)
			}
			if authErr.Provider != provider {
				t.Errorf("Provider = %q, want %q", authErr.Provider, provider)
			}
			if authErr.Wrapped != err {
				t.Errorf("Wrapped = %v, want %v", authErr.Wrapped, err)
			}
		})
	}
}

// --- ContextOverflowError patterns ---

func TestClassifyError_ContextOverflow(t *testing.T) {
	patterns := []string{
		"context window exceeded",
		"maximum context length exceeded",
		"max_tokens exceeded",
		"max context length reached",
		"exceed_context_size",
		"available context size is insufficient",
		"Maximum context length: 128000",
		"context_length_exceeded",
		"input is too long for model",
		"prompt is too long",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			err := errors.New(pattern)
			wrapped := classify(err, "openai")

			if !IsContextOverflow(wrapped) {
				t.Errorf("expected ContextOverflowError for %q, got %T", pattern, wrapped)
			}

			var ctxErr *ContextOverflowError
			if !errors.As(wrapped, &ctxErr) {
				t.Fatalf("errors.As failed for ContextOverflowError on %q", pattern)
			}
			if ctxErr.Wrapped != err {
				t.Errorf("Wrapped = %v, want %v", ctxErr.Wrapped, err)
			}
		})
	}
}

// TestClassifyError_ContextOverflowBeats400 pins the ordering invariant fixed
// in the bug where the classifier saw "HTTP 400: This model's maximum context
// length is 200000 tokens. …" and routed it to ClientError (because the bare
// "400" matched first) instead of ContextOverflowError. ClientError is on the
// fail-fast list, so the agent died instead of triggering compaction recovery.
//
// The fix moved the context-overflow check above the 4xx check in
// ClassifyError. This test pins that ordering: errors containing both a "400"
// status code and a context-overflow phrase must classify as ContextOverflow.
func TestClassifyError_ContextOverflowBeats400(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{
			name: "openai-style HTTP 400 with maximum context length",
			msg:  "client error (qwen3.6-27b): HTTP 400: This model's maximum context length is 200000 tokens. However, you requested 512 output tokens and your prompt contains at least 199489 input tokens, for a total of at least 200001 tokens.",
		},
		{
			name: "HTTP 400 with available context size",
			msg:  "HTTP 400: request (131306 tokens) exceeds the available context size (131072 tokens)",
		},
		{
			name: "HTTP 400 with context_length_exceeded",
			msg:  "HTTP 400: context_length_exceeded",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.New(tc.msg)
			wrapped := classify(err, "openai")
			if !IsContextOverflow(wrapped) {
				t.Fatalf("expected ContextOverflowError, got %T: %v", wrapped, wrapped)
			}
			if IsClientError(wrapped) {
				t.Errorf("error was classified as both ContextOverflow and ClientError; the context check must take precedence")
			}
		})
	}
}

// --- TransientError patterns ---

func TestClassifyError_Transient_Timeout(t *testing.T) {
	patterns := []string{
		"request timeout",
		"deadline exceeded",
		"context deadline exceeded",
		"Read timeout",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			err := errors.New(pattern)
			wrapped := classify(err, "openai")

			if !IsTransient(wrapped) {
				t.Errorf("expected TransientError for %q, got %T", pattern, wrapped)
			}

			var transErr *TransientError
			if !errors.As(wrapped, &transErr) {
				t.Fatalf("errors.As failed for TransientError on %q", pattern)
			}
			if transErr.Op != "chat" {
				t.Errorf("Op = %q, want %q", transErr.Op, "chat")
			}
			if transErr.Provider != "openai" {
				t.Errorf("Provider = %q, want %q", transErr.Provider, "openai")
			}
		})
	}
}

func TestClassifyError_Transient_Server(t *testing.T) {
	patterns := []string{
		"HTTP 500 Internal Server Error",
		"HTTP 502 Bad Gateway",
		"HTTP 503 Service Unavailable",
		"HTTP 504 Gateway Timeout",
		"status 500",
		"status 502",
		"status 503",
		"status 504",
		"internal server error",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
		"connection refused",
		"connection reset by peer",
		"etimedout",
		"etx: connection timed out",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			err := errors.New(pattern)
			wrapped := classify(err, "openai")

			if !IsTransient(wrapped) {
				t.Errorf("expected TransientError for %q, got %T", pattern, wrapped)
			}

			var transErr *TransientError
			if !errors.As(wrapped, &transErr) {
				t.Fatalf("errors.As failed for TransientError on %q", pattern)
			}
			if transErr.Op != "chat" {
				t.Errorf("Op = %q, want %q", transErr.Op, "chat")
			}
			if transErr.Provider != "openai" {
				t.Errorf("Provider = %q, want %q", transErr.Provider, "openai")
			}
		})
	}
}

// A server that does not offer what was asked (501) fails at once: a retry
// cannot change it.
func TestClassifyError_NotImplementedIsClientError(t *testing.T) {
	for _, pattern := range []string{
		"HTTP 501: No model is set up for this request",
		"status 501",
		"not implemented",
	} {
		t.Run(pattern, func(t *testing.T) {
			wrapped := classify(errors.New(pattern), "managed")
			if !IsClientError(wrapped) {
				t.Errorf("expected ClientError for %q, got %T", pattern, wrapped)
			}
			if IsTransient(wrapped) {
				t.Errorf("%q must not be retried as transient", pattern)
			}
		})
	}
}

func TestClassifyError_Transient_Default(t *testing.T) {
	// Patterns that don't match any specific category fall through to default TransientError.
	patterns := []string{
		"something went wrong",
		"unknown error",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			err := errors.New(pattern)
			wrapped := classify(err, "deepseek")

			if !IsTransient(wrapped) {
				t.Errorf("expected TransientError for %q, got %T", pattern, wrapped)
			}

			var transErr *TransientError
			if !errors.As(wrapped, &transErr) {
				t.Fatalf("errors.As failed for TransientError on %q", pattern)
			}
			if transErr.Op != "chat" {
				t.Errorf("Op = %q, want %q", transErr.Op, "chat")
			}
			if transErr.Provider != "deepseek" {
				t.Errorf("Provider = %q, want %q", transErr.Provider, "deepseek")
			}
		})
	}
}

// --- Cross-type isolation ---

func TestClassifyError_DistinctTypes(t *testing.T) {
	// Ensure no cross-matching between typed errors.
	tests := []struct {
		name      string
		err       error
		check     func(error) bool
		checkName string
	}{
		{"429 error", fmt.Errorf("429 too many requests"), IsRateLimit, "IsRateLimit"},
		{"401 error", fmt.Errorf("401 unauthorized"), IsAuthError, "IsAuthError"},
		{"context overflow", fmt.Errorf("context window exceeded"), IsContextOverflow, "IsContextOverflow"},
		{"timeout", fmt.Errorf("deadline exceeded"), IsTransient, "IsTransient"},
		{"500 error", fmt.Errorf("500 internal server error"), IsTransient, "IsTransient"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := classify(tt.err, "openai")
			if !tt.check(wrapped) {
				t.Errorf("expected %s to be true for %q, got false", tt.checkName, tt.err.Error())
			}
		})
	}
}

// --- Provider propagation ---

func TestClassifyError_ProviderPropagation(t *testing.T) {
	provider := "custom-provider"

	err := fmt.Errorf("timeout on upstream")
	wrapped := classify(err, provider)

	var transErr *TransientError
	if !errors.As(wrapped, &transErr) {
		t.Fatal("expected TransientError")
	}
	if transErr.Provider != provider {
		t.Errorf("Provider = %q, want %q", transErr.Provider, provider)
	}

	err2 := fmt.Errorf("429 too many")
	wrapped2 := classify(err2, provider)
	var rateErr *RateLimitError
	if !errors.As(wrapped2, &rateErr) {
		t.Fatal("expected RateLimitError")
	}
	if rateErr.Provider != provider {
		t.Errorf("Provider = %q, want %q", rateErr.Provider, provider)
	}

	err3 := fmt.Errorf("401 unauthorized")
	wrapped3 := classify(err3, provider)
	var authErr *AuthError
	if !errors.As(wrapped3, &authErr) {
		t.Fatal("expected AuthError")
	}
	if authErr.Provider != provider {
		t.Errorf("Provider = %q, want %q", authErr.Provider, provider)
	}
}

// --- Wrapped error preservation ---

func TestClassifyError_WrappedPreserved(t *testing.T) {
	original := errors.New("specific error code")

	tests := []struct {
		name  string
		err   error
		check func(error) bool
	}{
		{"transient", fmt.Errorf("timeout: %w", original), IsTransient},
		{"rate-limit", fmt.Errorf("429 too many: %w", original), IsRateLimit},
		{"auth", fmt.Errorf("401 unauthorized: %w", original), IsAuthError},
		{"context", fmt.Errorf("max context: %w", original), IsContextOverflow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := classify(tt.err, "openai")
			if !tt.check(wrapped) {
				t.Errorf("expected typed error for %q", tt.err.Error())
			}
			if !errors.Is(wrapped, original) {
				t.Errorf("wrapped error %v not accessible via errors.Is on %T", original, wrapped)
			}
		})
	}
}

// --- RetryAfter not set by default ---

func TestClassifyError_NoRetryAfter(t *testing.T) {
	err := fmt.Errorf("429 too many")
	wrapped := classify(err, "openai")

	var rateErr *RateLimitError
	if !errors.As(wrapped, &rateErr) {
		t.Fatal("expected RateLimitError")
	}
	if rateErr.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0 (not set by classifier)", rateErr.RetryAfter)
	}

	err2 := fmt.Errorf("timeout")
	wrapped2 := classify(err2, "openai")
	var transErr *TransientError
	if !errors.As(wrapped2, &transErr) {
		t.Fatal("expected TransientError")
	}
	if transErr.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0", transErr.RetryAfter)
	}
}

// --- Empty provider ---

func TestClassifyError_EmptyProvider(t *testing.T) {
	err := fmt.Errorf("HTTP 500 server error")
	wrapped := classify(err, "")

	var transErr *TransientError
	if !errors.As(wrapped, &transErr) {
		t.Fatal("expected TransientError")
	}
	if transErr.Provider != "" {
		t.Errorf("Provider = %q, want empty string", transErr.Provider)
	}
}

// --- Order matters: rate limit before timeout (edge case) ---

func TestClassifyError_RateLimitBeforeTimeout(t *testing.T) {
	// A string that could match both rate-limit and transient (timeout) patterns.
	// E.g., "429 timeout" should be classified as RateLimitError because rate-limit
	// is checked first.
	err := fmt.Errorf("429 timeout on upstream")
	wrapped := classify(err, "openai")

	if !IsRateLimit(wrapped) {
		t.Errorf("expected RateLimitError (first match wins), got %T", wrapped)
	}
}

// --- Multiple status codes in one message ---

func TestClassifyError_MultipleStatusCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantType func(error) bool
		wantName string
	}{
		{
			name:     "403 then 500 → client error (403 classified before transient)",
			err:      fmt.Errorf("HTTP 403: error 500 on upstream"),
			wantType: IsClientError,
			wantName: "IsClientError",
		},
		{
			name:     "429 and 500 → rate limit (rate limit checked before transient)",
			err:      fmt.Errorf("429 rate limit: 500 error"),
			wantType: IsRateLimit,
			wantName: "IsRateLimit",
		},
		{
			name:     "500 then 401 → auth (auth checked before transient)",
			err:      fmt.Errorf("500 error: 401 unauthorized"),
			wantType: IsAuthError,
			wantName: "IsAuthError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := classify(tt.err, "openai")
			if !tt.wantType(wrapped) {
				t.Errorf("expected %s for %q, got %T", tt.wantName, tt.err.Error(), wrapped)
			}
		})
	}
}

// --- Empty error message ---

func TestClassifyError_EmptyMessage(t *testing.T) {
	err := fmt.Errorf("")
	wrapped := classify(err, "openai")

	if !IsTransient(wrapped) {
		t.Errorf("expected TransientError (default) for empty message, got %T", wrapped)
	}
}

// --- False positive prevention ---

func TestClassifyError_NoFalsePositives(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantType func(error) bool
	}{
		{
			name:     "4011 not auth (longer number)",
			err:      fmt.Errorf("Error 4011 occurred"),
			wantType: IsTransient,
		},
		{
			name:     "request 40104 not auth",
			err:      fmt.Errorf("Request 40104 failed"),
			wantType: IsTransient,
		},
		{
			name:     "error 50003 not server error",
			err:      fmt.Errorf("Error 50003: internal failure"),
			wantType: IsTransient,
		},
		{
			name:     "request 4290 not rate limit",
			err:      fmt.Errorf("Request 4290 failed"),
			wantType: IsTransient,
		},
		{
			name:     "api key rotation policy not auth",
			err:      fmt.Errorf("The api key rotation policy changed"),
			wantType: IsTransient,
		},
		{
			name:     "api key stored not auth",
			err:      fmt.Errorf("api key is stored in environment variables"),
			wantType: IsTransient,
		},
		{
			name:     "authentication service down is transient",
			err:      fmt.Errorf("authentication service is down"),
			wantType: IsTransient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := classify(tt.err, "openai")
			if !tt.wantType(wrapped) {
				t.Errorf("expected transient for %q, got %T", tt.err.Error(), wrapped)
			}
		})
	}
}

// --- Integration: errors.As chain through classification ---

func TestClassifyError_ErrorsAsChain(t *testing.T) {
	// Raw error wrapped in multiple layers, then classified.
	original := fmt.Errorf("inner: connection refused")
	multi := fmt.Errorf("request: %w", original)

	wrapped := classify(multi, "openai")

	var transErr *TransientError
	if !errors.As(wrapped, &transErr) {
		t.Fatal("expected TransientError through errors.As")
	}

	// Original underlying error should be discoverable through the full chain.
	if !errors.Is(wrapped, original) {
		t.Error("original error not accessible through classification chain")
	}
}

// --- ContainsAny helper (internal) ---

func TestContainsAny_FindsMatch(t *testing.T) {
	if !containsAny("request timeout exceeded", "timeout") {
		t.Error("should find 'timeout' in 'request timeout exceeded'")
	}
}

func TestContainsAny_CaseInsensitive(t *testing.T) {
	// containsAny itself does NOT perform case folding.
	// Case-insensitivity comes from ClassifyError, which calls strings.ToLower
	// before passing to containsAny.
	if !containsAny("request timeout exceeded", "timeout") {
		t.Error("should find 'timeout' in 'request timeout exceeded'")
	}
	// Upper-case pattern won't match lower-case input.
	if containsAny("request timeout exceeded", "TIMEOUT") {
		t.Error("pattern matching is case-sensitive in containsAny")
	}
}

func TestContainsAny_NoMatch(t *testing.T) {
	if containsAny("something else", "timeout") {
		t.Error("should not find 'timeout' in 'something else'")
	}
}

func TestContainsAny_MultiplePatterns(t *testing.T) {
	if !containsAny("rate limit exceeded", "429", "rate limit", "timeout") {
		t.Error("should match 'rate limit' from multiple patterns")
	}
}

func TestContainsAny_EmptyPatterns(t *testing.T) {
	// No substrings to match — should return false unless s is also empty.
	if containsAny("hello") {
		t.Error("should return false with no patterns")
	}
}

func TestContainsAny_EmptyString(t *testing.T) {
	// Even with an empty pattern string, strings.Contains("", "") is true.
	// So this tests the actual behavior of strings.Contains.
	if !containsAny("hello", "") {
		t.Error("strings.Contains matches empty string in any string")
	}
}

// --- Error message format ---

func TestClassifyError_TransientErrorMessage(t *testing.T) {
	err := fmt.Errorf("connection refused")
	wrapped := classify(err, "openai")

	if !IsTransient(wrapped) {
		t.Fatal("expected TransientError")
	}

	got := wrapped.Error()
	wantContains := []string{"transient error", "chat", "openai", "connection refused"}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Errorf("message %q should contain %q", got, want)
		}
	}
}

func TestClassifyError_RateLimitErrorMessage(t *testing.T) {
	err := fmt.Errorf("429 too many requests")
	wrapped := classify(err, "openai")

	if !IsRateLimit(wrapped) {
		t.Fatal("expected RateLimitError")
	}

	got := wrapped.Error()
	if !strings.Contains(got, "rate limit") {
		t.Errorf("message %q should contain 'rate limit'", got)
	}
	if !strings.Contains(got, "openai") {
		t.Errorf("message %q should contain 'openai'", got)
	}
	if !strings.Contains(got, "429 too many requests") {
		t.Errorf("message %q should contain wrapped error", got)
	}
}

func TestClassifyError_ContextOverflowErrorMessage(t *testing.T) {
	err := fmt.Errorf("context window exceeded")
	wrapped := classify(err, "openai")

	if !IsContextOverflow(wrapped) {
		t.Fatal("expected ContextOverflowError")
	}

	got := wrapped.Error()
	if !strings.Contains(got, "context window exceeded") {
		t.Errorf("message %q should contain 'context window exceeded'", got)
	}
	if !strings.Contains(got, "context window") {
		t.Errorf("message %q should contain 'context window' (type name)", got)
	}
}

func TestClassifyError_AuthErrorMessage(t *testing.T) {
	err := fmt.Errorf("invalid api key")
	wrapped := classify(err, "anthropic")

	if !IsAuthError(wrapped) {
		t.Fatal("expected AuthError")
	}

	got := wrapped.Error()
	if !strings.Contains(got, "authentication failed") {
		t.Errorf("message %q should contain 'authentication failed'", got)
	}
	if !strings.Contains(got, "anthropic") {
		t.Errorf("message %q should contain 'anthropic'", got)
	}
	if !strings.Contains(got, "invalid api key") {
		t.Errorf("message %q should contain wrapped error", got)
	}
}

// --- Attempt field not set by classifier ---

func TestClassifyError_AttemptNotSet(t *testing.T) {
	err := fmt.Errorf("429 too many")
	wrapped := classify(err, "openai")

	var rateErr *RateLimitError
	if !errors.As(wrapped, &rateErr) {
		t.Fatal("expected RateLimitError")
	}
	if rateErr.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0 (classifier doesn't track attempt count)", rateErr.Attempt)
	}
}
