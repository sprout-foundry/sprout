package errors

import (
	"errors"
	"fmt"
	"testing"
)

// Test constructors for correct category and retryable flag
func TestNewTransientError(t *testing.T) {
	cause := fmt.Errorf("network timeout")
	err := NewTransientError("connection failed", cause)

	if err.Category != CategoryTransient {
		t.Errorf("Expected category %v, got %v", CategoryTransient, err.Category)
	}

	if !err.Retryable {
		t.Error("Expected retryable=true for transient error")
	}

	if err.Message != "connection failed" {
		t.Errorf("Expected message 'connection failed', got '%s'", err.Message)
	}

	if err.Cause != cause {
		t.Error("Cause not preserved")
	}
}

func TestNewTransientError_NilCause(t *testing.T) {
	err := NewTransientError("connection failed", nil)

	if err.Category != CategoryTransient {
		t.Errorf("Expected category %v, got %v", CategoryTransient, err.Category)
	}

	if !err.Retryable {
		t.Error("Expected retryable=true for transient error")
	}

	if err.Cause != nil {
		t.Error("Expected nil cause")
	}
}

func TestNewRateLimitError(t *testing.T) {
	cause := fmt.Errorf("rate limit exceeded")
	err := NewRateLimitError("too many requests", cause, "openai")

	if err.Category != CategoryRateLimited {
		t.Errorf("Expected category %v, got %v", CategoryRateLimited, err.Category)
	}

	if !err.Retryable {
		t.Error("Expected retryable=true for rate limit error")
	}

	if err.Message != "too many requests" {
		t.Errorf("Expected message 'too many requests', got '%s'", err.Message)
	}

	if provider := err.GetMetadata("provider"); provider != "openai" {
		t.Errorf("Expected provider 'openai', got '%s'", provider)
	}
}

func TestNewRateLimitError_NoProvider(t *testing.T) {
	err := NewRateLimitError("too many requests", nil, "")

	if provider := err.GetMetadata("provider"); provider != "" {
		t.Errorf("Expected empty provider, got '%s'", provider)
	}
}

func TestNewSecurityError(t *testing.T) {
	cause := fmt.Errorf("unauthorized access")
	err := NewSecurityError("blocked command", cause)

	if err.Category != CategorySecurity {
		t.Errorf("Expected category %v, got %v", CategorySecurity, err.Category)
	}

	if err.Retryable {
		t.Error("Expected retryable=false for security error")
	}
}

func TestNewInvalidInputError(t *testing.T) {
	cause := fmt.Errorf("invalid parameter")
	err := NewInvalidInputError("malformed request", cause)

	if err.Category != CategoryInvalidInput {
		t.Errorf("Expected category %v, got %v", CategoryInvalidInput, err.Category)
	}

	if err.Retryable {
		t.Error("Expected retryable=false for invalid input error")
	}
}

func TestNewProviderError(t *testing.T) {
	cause := fmt.Errorf("401 unauthorized")
	err := NewProviderError("auth failed", cause, "openai", "gpt-4")

	if err.Category != CategoryProvider {
		t.Errorf("Expected category %v, got %v", CategoryProvider, err.Category)
	}

	// Auth errors are not retryable
	if err.Retryable {
		t.Error("Expected retryable=false for auth error")
	}

	if provider := err.GetMetadata("provider"); provider != "openai" {
		t.Errorf("Expected provider 'openai', got '%s'", provider)
	}

	if model := err.GetMetadata("model"); model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%s'", model)
	}
}

func TestNewProviderError_Overload(t *testing.T) {
	cause := fmt.Errorf("503 service unavailable")
	err := NewProviderError("provider overloaded", cause, "openai", "gpt-4")

	// Server overload errors are retryable
	if !err.Retryable {
		t.Error("Expected retryable=true for provider overload error")
	}
}

func TestNewProviderError_NilCause(t *testing.T) {
	err := NewProviderError("unknown provider error", nil, "openai", "gpt-4")

	// Nil cause defaults to not retryable
	if err.Retryable {
		t.Error("Expected retryable=false for provider error with nil cause")
	}
}

func TestNewContextError(t *testing.T) {
	cause := fmt.Errorf("context window exceeded")
	err := NewContextError("conversation too long", cause)

	if err.Category != CategoryContext {
		t.Errorf("Expected category %v, got %v", CategoryContext, err.Category)
	}

	if !err.Retryable {
		t.Error("Expected retryable=true for context error")
	}
}

func TestNewPermanentError(t *testing.T) {
	cause := fmt.Errorf("configuration error")
	err := NewPermanentError("cannot recover", cause)

	if err.Category != CategoryPermanent {
		t.Errorf("Expected category %v, got %v", CategoryPermanent, err.Category)
	}

	if err.Retryable {
		t.Error("Expected retryable=false for permanent error")
	}
}

// Test category check helpers
func TestGetCategory(t *testing.T) {
	tests := []struct {
		name     string
		err      *AgentError
		expected ErrorCategory
	}{
		{"Transient", NewTransientError("test", nil), CategoryTransient},
		{"RateLimited", NewRateLimitError("test", nil, ""), CategoryRateLimited},
		{"Security", NewSecurityError("test", nil), CategorySecurity},
		{"InvalidInput", NewInvalidInputError("test", nil), CategoryInvalidInput},
		{"Provider", NewProviderError("test", nil, "", ""), CategoryProvider},
		{"Context", NewContextError("test", nil), CategoryContext},
		{"Permanent", NewPermanentError("test", nil), CategoryPermanent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category, ok := GetCategory(tt.err)
			if !ok {
				t.Error("GetCategory returned false for AgentError")
			}
			if category != tt.expected {
				t.Errorf("Expected category %v, got %v", tt.expected, category)
			}
		})
	}

	// Test with non-AgentError
	t.Run("Non-AgentError", func(t *testing.T) {
		err := fmt.Errorf("plain error")
		category, ok := GetCategory(err)
		if ok {
			t.Error("GetCategory returned true for non-AgentError")
		}
		if category != 0 {
			t.Errorf("Expected zero category, got %v", category)
		}
	})

	// Test with nil
	t.Run("NilError", func(t *testing.T) {
		category, ok := GetCategory(nil)
		if ok {
			t.Error("GetCategory returned true for nil error")
		}
		if category != 0 {
			t.Errorf("Expected zero category, got %v", category)
		}
	})
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		err      *AgentError
		expected bool
	}{
		{"Transient", NewTransientError("test", nil), true},
		{"RateLimited", NewRateLimitError("test", nil, ""), true},
		{"Security", NewSecurityError("test", nil), false},
		{"InvalidInput", NewInvalidInputError("test", nil), false},
		{"Context", NewContextError("test", nil), true},
		{"Permanent", NewPermanentError("test", nil), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.expected {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.expected)
			}
		})
	}

	// Provider error depends on cause
	t.Run("ProviderAuthError", func(t *testing.T) {
		cause := fmt.Errorf("401 unauthorized")
		err := NewProviderError("auth failed", cause, "openai", "gpt-4")
		if IsRetryable(err) {
			t.Error("Expected not retryable for auth error")
		}
	})

	t.Run("ProviderOverloadError", func(t *testing.T) {
		cause := fmt.Errorf("503 service unavailable")
		err := NewProviderError("overloaded", cause, "openai", "gpt-4")
		if !IsRetryable(err) {
			t.Error("Expected retryable for overload error")
		}
	})

	// Non-AgentError
	t.Run("Non-AgentError", func(t *testing.T) {
		err := fmt.Errorf("plain error")
		if IsRetryable(err) {
			t.Error("IsRetryable returned true for non-AgentError")
		}
	})

	// Nil
	t.Run("NilError", func(t *testing.T) {
		if IsRetryable(nil) {
			t.Error("IsRetryable returned true for nil")
		}
	})
}

func TestIsTransient(t *testing.T) {
	err := NewTransientError("test", nil)
	if !IsTransient(err) {
		t.Error("IsTransient returned false for Transient error")
	}

	other := NewSecurityError("test", nil)
	if IsTransient(other) {
		t.Error("IsTransient returned true for non-Transient error")
	}
}

func TestIsRateLimited(t *testing.T) {
	err := NewRateLimitError("test", nil, "openai")
	if !IsRateLimited(err) {
		t.Error("IsRateLimited returned false for RateLimited error")
	}

	other := NewTransientError("test", nil)
	if IsRateLimited(other) {
		t.Error("IsRateLimited returned true for non-RateLimited error")
	}
}

func TestIsSecurity(t *testing.T) {
	err := NewSecurityError("test", nil)
	if !IsSecurity(err) {
		t.Error("IsSecurity returned false for Security error")
	}

	other := NewTransientError("test", nil)
	if IsSecurity(other) {
		t.Error("IsSecurity returned true for non-Security error")
	}
}

func TestIsInvalidInput(t *testing.T) {
	err := NewInvalidInputError("test", nil)
	if !IsInvalidInput(err) {
		t.Error("IsInvalidInput returned false for InvalidInput error")
	}

	other := NewTransientError("test", nil)
	if IsInvalidInput(other) {
		t.Error("IsInvalidInput returned true for non-InvalidInput error")
	}
}

func TestIsProviderError(t *testing.T) {
	err := NewProviderError("test", nil, "openai", "gpt-4")
	if !IsProviderError(err) {
		t.Error("IsProviderError returned false for Provider error")
	}

	other := NewTransientError("test", nil)
	if IsProviderError(other) {
		t.Error("IsProviderError returned true for non-Provider error")
	}
}

func TestIsContextError(t *testing.T) {
	err := NewContextError("test", nil)
	if !IsContextError(err) {
		t.Error("IsContextError returned false for Context error")
	}

	other := NewTransientError("test", nil)
	if IsContextError(other) {
		t.Error("IsContextError returned true for non-Context error")
	}
}

func TestIsPermanent(t *testing.T) {
	err := NewPermanentError("test", nil)
	if !IsPermanent(err) {
		t.Error("IsPermanent returned false for Permanent error")
	}

	other := NewTransientError("test", nil)
	if IsPermanent(other) {
		t.Error("IsPermanent returned true for non-Permanent error")
	}
}

// Test error wrapping and Unwrap chain
func TestUnwrap(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := NewTransientError("wrapper", cause)

	if err.Unwrap() != cause {
		t.Error("Unwrap did not return the original cause")
	}
}

func TestUnwrap_NilCause(t *testing.T) {
	err := NewTransientError("wrapper", nil)

	if err.Unwrap() != nil {
		t.Error("Unwrap should return nil when cause is nil")
	}
}

func TestErrorChain(t *testing.T) {
	originalErr := fmt.Errorf("root cause")
	wrappedErr := NewTransientError("wrapper 1", originalErr)
	doubleWrappedErr := WrapWithCategory(wrappedErr, CategorySecurity, "wrapper 2")

	// Test Unwrap chain
	if doubleWrappedErr.Unwrap() != wrappedErr {
		t.Error("Double wrap did not maintain chain")
	}

	if wrappedErr.Unwrap() != originalErr {
		t.Error("Single wrap did not maintain chain")
	}
}

// Test errors.Is/errors.As compatibility
func TestErrorsAs(t *testing.T) {
	err := NewTransientError("test", nil)

	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Error("errors.As failed to extract AgentError")
	}

	if agentErr.Category != CategoryTransient {
		t.Errorf("errors.As extracted wrong category: got %v", agentErr.Category)
	}
}

func TestErrorsAs_NonAgentError(t *testing.T) {
	err := fmt.Errorf("plain error")

	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		t.Error("errors.As should not extract AgentError from non-AgentError")
	}
}

func TestErrorsIs_SameError(t *testing.T) {
	original := fmt.Errorf("original error")
	wrapped := NewTransientError("wrapped", original)

	if !errors.Is(wrapped, original) {
		t.Error("errors.Is did not find original error in chain")
	}
}

func TestErrorsIs_DifferentError(t *testing.T) {
	original := fmt.Errorf("original error")
	different := fmt.Errorf("different error")
	wrapped := NewTransientError("wrapped", original)

	if errors.Is(wrapped, different) {
		t.Error("errors.Is incorrectly matched different error")
	}
}

// Test WrapWithCategory
func TestWrapWithCategory(t *testing.T) {
	original := fmt.Errorf("original error")
	wrapped := WrapWithCategory(original, CategoryTransient, "wrapped message")

	if wrapped == nil {
		t.Fatal("WrapWithCategory returned nil")
	}

	if wrapped.Category != CategoryTransient {
		t.Errorf("Expected category %v, got %v", CategoryTransient, wrapped.Category)
	}

	if wrapped.Message != "wrapped message" {
		t.Errorf("Expected message 'wrapped message', got '%s'", wrapped.Message)
	}

	if wrapped.Unwrap() != original {
		t.Error("Wrapped error did not preserve original cause")
	}

	if !wrapped.Retryable {
		t.Error("Expected retryable=true for Transient category")
	}
}

func TestWrapWithCategory_NilError(t *testing.T) {
	wrapped := WrapWithCategory(nil, CategoryTransient, "wrapped message")

	if wrapped != nil {
		t.Error("WrapWithCategory should return nil for nil error")
	}
}

func TestWrapWithCategory_NonRetryableCategory(t *testing.T) {
	original := fmt.Errorf("original error")
	wrapped := WrapWithCategory(original, CategorySecurity, "wrapped message")

	if wrapped.Retryable {
		t.Error("Expected retryable=false for Security category")
	}
}

// Test metadata methods
func TestWithMetadata(t *testing.T) {
	err := NewTransientError("test", nil)
	err.WithMetadata("key1", "value1")
	err.WithMetadata("key2", "value2")

	if v := err.GetMetadata("key1"); v != "value1" {
		t.Errorf("Expected metadata key1='value1', got '%s'", v)
	}

	if v := err.GetMetadata("key2"); v != "value2" {
		t.Errorf("Expected metadata key2='value2', got '%s'", v)
	}
}

func TestWithProvider(t *testing.T) {
	err := NewTransientError("test", nil)
	err.WithProvider("openai")

	if v := err.GetMetadata("provider"); v != "openai" {
		t.Errorf("Expected provider='openai', got '%s'", v)
	}
}

func TestWithModel(t *testing.T) {
	err := NewTransientError("test", nil)
	err.WithModel("gpt-4")

	if v := err.GetMetadata("model"); v != "gpt-4" {
		t.Errorf("Expected model='gpt-4', got '%s'", v)
	}
}

func TestGetMetadata_NotFound(t *testing.T) {
	err := NewTransientError("test", nil)

	if v := err.GetMetadata("nonexistent"); v != "" {
		t.Errorf("Expected empty string for nonexistent key, got '%s'", v)
	}
}

// Test Error() method
func TestErrorFormat_WithCause(t *testing.T) {
	cause := fmt.Errorf("underlying error")
	err := NewTransientError("wrapper", cause)

	expected := "[Transient] wrapper: underlying error"
	if got := err.Error(); got != expected {
		t.Errorf("Error() = '%s', want '%s'", got, expected)
	}
}

func TestErrorFormat_WithoutCause(t *testing.T) {
	err := NewTransientError("wrapper", nil)

	expected := "[Transient] wrapper"
	if got := err.Error(); got != expected {
		t.Errorf("Error() = '%s', want '%s'", got, expected)
	}
}

// Test ErrorCategory String method
func TestErrorCategoryString(t *testing.T) {
	tests := []struct {
		category ErrorCategory
		expected string
	}{
		{CategoryTransient, "Transient"},
		{CategoryRateLimited, "RateLimited"},
		{CategorySecurity, "Security"},
		{CategoryInvalidInput, "InvalidInput"},
		{CategoryProvider, "Provider"},
		{CategoryContext, "Context"},
		{CategoryPermanent, "Permanent"},
		{ErrorCategory(999), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.category.String(); got != tt.expected {
				t.Errorf("ErrorCategory.String() = '%s', want '%s'", got, tt.expected)
			}
		})
	}
}

// Test provider error retryability logic
func TestIsProviderErrorRetryable(t *testing.T) {
	tests := []struct {
		name     string
		cause    string
		expected bool
	}{
		{"Unauthorized", "401 unauthorized", false},
		{"Unauthorized", "unauthorized access", false},
		{"Auth", "authentication failed", false},
		{"InvalidAPIKey", "invalid api key", false},
		{"ModelNotFound", "model not found", false},
		{"ModelNotExist", "model does not exist", false},
		{"InvalidModel", "invalid model", false},
		{"502", "502 Bad Gateway", true},
		{"503", "503 Service Unavailable", true},
		{"Overloaded", "service overloaded", true},
		{"InternalError", "internal server error", true},
		{"Generic", "some error", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cause := errors.New(tt.cause)
			got := isProviderErrorRetryable(cause)
			if got != tt.expected {
				t.Errorf("isProviderErrorRetryable() = %v, want %v for cause '%s'", got, tt.expected, tt.cause)
			}
		})
	}

	t.Run("NilCause", func(t *testing.T) {
		if isProviderErrorRetryable(nil) {
			t.Error("Expected false for nil cause")
		}
	})
}

// Test edge cases
func TestEmptyMessage(t *testing.T) {
	err := NewTransientError("", nil)

	if err.Message != "" {
		t.Errorf("Expected empty message, got '%s'", err.Message)
	}

	// Should still be a valid error
	if err.Error() != "[Transient] " {
		t.Errorf("Unexpected Error() output: '%s'", err.Error())
	}
}

func TestLongMessage(t *testing.T) {
	longMsg := string(make([]byte, 10000)) // 10KB message
	for i := range longMsg {
		longMsg = longMsg[:i] + "a" + longMsg[i+1:]
	}
	err := NewTransientError(longMsg, nil)

	if err.Message != longMsg {
		t.Error("Long message not preserved")
	}
}

func TestErrorChainCompatibility(t *testing.T) {
	// Test that we can chain multiple wraps
	root := fmt.Errorf("root error")
	level1 := NewTransientError("level 1", root)
	level2 := WrapWithCategory(level1, CategoryContext, "level 2")
	level3 := WrapWithCategory(level2, CategoryPermanent, "level 3")

	// errors.Is should find any error in the chain
	if !errors.Is(level3, root) {
		t.Error("errors.Is did not find root error in deep chain")
	}

	if !errors.Is(level3, level1) {
		t.Error("errors.Is did not find level1 error in chain")
	}

	// errors.As should extract any AgentError in the chain
	var agentErr *AgentError
	if !errors.As(level3, &agentErr) {
		t.Error("errors.As did not extract AgentError from chain")
	}

	// The topmost AgentError is level3
	if agentErr.Category != CategoryPermanent {
		t.Errorf("Expected category %v, got %v", CategoryPermanent, agentErr.Category)
	}
}

// Test that multiple WrapWithCategory calls preserve the chain
func TestMultipleWraps(t *testing.T) {
	original := fmt.Errorf("original")
	wrap1 := WrapWithCategory(original, CategoryTransient, "wrap 1")
	wrap2 := WrapWithCategory(wrap1, CategoryContext, "wrap 2")

	// Check that we can unwrap to get back to wrap1
	if wrap2.Unwrap() != wrap1 {
		t.Error("Multiple wraps did not preserve chain correctly")
	}

	// Check that wrap1 still points to original
	if wrap1.Unwrap() != original {
		t.Error("First wrap did not preserve original")
	}

	// Check categories
	if wrap1.Category != CategoryTransient {
		t.Errorf("wrap1 category: expected %v, got %v", CategoryTransient, wrap1.Category)
	}

	if wrap2.Category != CategoryContext {
		t.Errorf("wrap2 category: expected %v, got %v", CategoryContext, wrap2.Category)
	}
}

// Test metadata preservation through wraps
func TestMetadataThroughWraps(t *testing.T) {
	original := NewTransientError("original", nil)
	original.WithProvider("openai")

	wrapped := WrapWithCategory(original, CategoryContext, "wrapped")

	// Original metadata should still be accessible through the chain
	if provider := original.GetMetadata("provider"); provider != "openai" {
		t.Errorf("Expected provider 'openai' in original, got '%s'", provider)
	}

	// Wrapped should have its own metadata map
	if wrapped.Metadata == nil {
		t.Error("Wrapped error has nil metadata")
	}
}
