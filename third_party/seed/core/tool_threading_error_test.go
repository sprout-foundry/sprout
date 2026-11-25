package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// --- ToolThreadingError.Error() formatting ---

func TestToolThreadingError_Error_NoProviderNoViolationsNoWrapped(t *testing.T) {
	e := &ToolThreadingError{}
	got := e.Error()
	if got != "tool call threading error" {
		t.Errorf("got %q, want %q", got, "tool call threading error")
	}
}

func TestToolThreadingError_Error_WithProvider(t *testing.T) {
	e := &ToolThreadingError{Provider: "minimax"}
	got := e.Error()
	if !strings.Contains(got, "minimax") {
		t.Errorf("message %q should contain provider 'minimax'", got)
	}
}

func TestToolThreadingError_Error_WithViolations(t *testing.T) {
	e := &ToolThreadingError{
		Violations: []ToolThreadingViolation{
			{Kind: "orphan_result", Index: 2, Detail: "stray"},
			{Kind: "missing_result", Index: 0, Detail: "no result"},
		},
	}
	got := e.Error()
	if !strings.Contains(got, "2 violation(s)") {
		t.Errorf("message %q should contain '2 violation(s)'", got)
	}
}

func TestToolThreadingError_Error_WithWrapped(t *testing.T) {
	wrapped := fmt.Errorf("HTTP 400: invalid params")
	e := &ToolThreadingError{Wrapped: wrapped}
	got := e.Error()
	if !strings.Contains(got, "HTTP 400: invalid params") {
		t.Errorf("message %q should contain wrapped error text", got)
	}
}

func TestToolThreadingError_Error_AllFields(t *testing.T) {
	wrapped := fmt.Errorf("HTTP 400: invalid params, tool call result does not follow tool call (2013)")
	e := &ToolThreadingError{
		Provider:   "minimax",
		Violations: []ToolThreadingViolation{{Kind: "out_of_order", Index: 3, Detail: "reversed"}},
		Wrapped:    wrapped,
	}
	got := e.Error()
	if !strings.Contains(got, "minimax") {
		t.Errorf("message %q should contain 'minimax'", got)
	}
	if !strings.Contains(got, "1 violation(s)") {
		t.Errorf("message %q should contain '1 violation(s)'", got)
	}
	if !strings.Contains(got, "HTTP 400") {
		t.Errorf("message %q should contain wrapped error", got)
	}
}

// --- ToolThreadingError.Unwrap() ---

func TestToolThreadingError_Unwrap_Nil(t *testing.T) {
	e := &ToolThreadingError{}
	if e.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil", e.Unwrap())
	}
}

func TestToolThreadingError_Unwrap_ReturnsWrapped(t *testing.T) {
	original := fmt.Errorf("original error")
	e := &ToolThreadingError{Wrapped: original}
	if e.Unwrap() != original {
		t.Errorf("Unwrap() = %v, want %v", e.Unwrap(), original)
	}
}

func TestToolThreadingError_ErrorsIs(t *testing.T) {
	original := fmt.Errorf("original error")
	e := &ToolThreadingError{Wrapped: original}
	if !errors.Is(e, original) {
		t.Error("errors.Is should find original through Unwrap")
	}
}

// --- IsToolThreadingError ---

func TestIsToolThreadingError_True(t *testing.T) {
	e := &ToolThreadingError{Provider: "minimax"}
	if !IsToolThreadingError(e) {
		t.Error("IsToolThreadingError(*ToolThreadingError) should be true")
	}
}

func TestIsToolThreadingError_FalseForOtherTypes(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"ClientError", &ClientError{Provider: "openai"}},
		{"TransientError", &TransientError{Op: "chat"}},
		{"RateLimitError", &RateLimitError{Provider: "openai"}},
		{"AuthError", &AuthError{Provider: "openai"}},
		{"plain error", fmt.Errorf("something went wrong")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsToolThreadingError(tc.err) {
				t.Errorf("IsToolThreadingError(%T) should be false", tc.err)
			}
		})
	}
}

func TestIsToolThreadingError_FalseForNil(t *testing.T) {
	if IsToolThreadingError(nil) {
		t.Error("IsToolThreadingError(nil) should be false")
	}
}

func TestIsToolThreadingError_ThroughWrap(t *testing.T) {
	original := &ToolThreadingError{Provider: "minimax"}
	wrapped := fmt.Errorf("outer: %w", original)
	if !IsToolThreadingError(wrapped) {
		t.Error("IsToolThreadingError should find *ToolThreadingError through fmt.Errorf wrap")
	}
}

// --- ClassifyError: MiniMax-style threading error ---

func TestClassifyError_ToolThreading_MiniMax(t *testing.T) {
	err := fmt.Errorf("HTTP 400: invalid params, tool call result does not follow tool call (2013)")
	wrapped := ClassifyError(err, "minimax")

	if !IsToolThreadingError(wrapped) {
		t.Fatalf("expected *ToolThreadingError, got %T: %v", wrapped, wrapped)
	}
	if IsClientError(wrapped) {
		t.Error("MiniMax threading error should NOT be classified as *ClientError")
	}

	var tte *ToolThreadingError
	if !errors.As(wrapped, &tte) {
		t.Fatal("errors.As failed for *ToolThreadingError")
	}
	if tte.Provider != "minimax" {
		t.Errorf("Provider = %q, want %q", tte.Provider, "minimax")
	}
	if tte.Wrapped != err {
		t.Errorf("Wrapped not preserved")
	}
}

// --- ClassifyError: DeepSeek-style threading error ---

func TestClassifyError_ToolThreading_DeepSeek(t *testing.T) {
	err := fmt.Errorf("tool messages must follow assistant messages with tool calls")
	wrapped := ClassifyError(err, "deepseek")

	if !IsToolThreadingError(wrapped) {
		t.Fatalf("expected *ToolThreadingError, got %T: %v", wrapped, wrapped)
	}
	if IsClientError(wrapped) {
		t.Error("DeepSeek threading error should NOT be classified as *ClientError")
	}

	var tte *ToolThreadingError
	if !errors.As(wrapped, &tte) {
		t.Fatal("errors.As failed for *ToolThreadingError")
	}
	if tte.Provider != "deepseek" {
		t.Errorf("Provider = %q, want %q", tte.Provider, "deepseek")
	}
}

// --- ClassifyError: threading patterns (each pattern individually) ---

func TestClassifyError_ToolThreading_Patterns(t *testing.T) {
	patterns := []string{
		"tool call result does not follow",
		"does not follow tool call",
		"tool result does not follow",
		"tool call does not match",
		"tool_use_ids must match",
		"tool messages must follow",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			err := fmt.Errorf("provider error: %s", pattern)
			wrapped := ClassifyError(err, "test-provider")

			if !IsToolThreadingError(wrapped) {
				t.Errorf("expected *ToolThreadingError for %q, got %T", pattern, wrapped)
			}
			if IsClientError(wrapped) {
				t.Errorf("threading pattern %q should NOT be *ClientError", pattern)
			}
		})
	}
}

// --- ClassifyError: idempotent for already-typed ToolThreadingError ---

func TestClassifyError_ToolThreadingError_Idempotent(t *testing.T) {
	original := &ToolThreadingError{
		Provider:   "minimax",
		Violations: []ToolThreadingViolation{{Kind: "out_of_order", Index: 3}},
		Wrapped:    fmt.Errorf("original wrapped"),
	}

	result := ClassifyError(original, "other-provider")

	if result != original {
		t.Error("ClassifyError should return *ToolThreadingError unchanged")
	}
}

// --- ClassifyError: generic 400 that is NOT a threading error ---

func TestClassifyError_ToolThreading_Generic400NotThreading(t *testing.T) {
	err := fmt.Errorf("HTTP 400: bad model name")
	wrapped := ClassifyError(err, "openai")

	if IsToolThreadingError(wrapped) {
		t.Error("generic 400 should NOT be classified as *ToolThreadingError")
	}
	if !IsClientError(wrapped) {
		t.Fatalf("expected *ClientError, got %T", wrapped)
	}
}

func TestClassifyError_ToolThreading_Generic400NotThreading_InvalidParameter(t *testing.T) {
	err := fmt.Errorf("HTTP 400: invalid parameter: model does not exist")
	wrapped := ClassifyError(err, "openai")

	if IsToolThreadingError(wrapped) {
		t.Error("generic 400 should NOT be classified as *ToolThreadingError")
	}
	if !IsClientError(wrapped) {
		t.Fatalf("expected *ClientError, got %T", wrapped)
	}
}

// --- ClassifyError: threading error takes precedence over generic 4xx ---

func TestClassifyError_ToolThreading_Beats400(t *testing.T) {
	// A message that contains both "400" and a threading pattern.
	// Threading check runs before the generic 4xx check.
	err := fmt.Errorf("HTTP 400: tool call result does not follow tool call")
	wrapped := ClassifyError(err, "minimax")

	if !IsToolThreadingError(wrapped) {
		t.Fatalf("expected *ToolThreadingError, got %T: %v", wrapped, wrapped)
	}
	if IsClientError(wrapped) {
		t.Error("threading error should NOT also be *ClientError")
	}
}

// --- ClassifyError: ToolThreadingError passthrough with violations ---

func TestClassifyError_ToolThreadingError_PassthroughPreservesViolations(t *testing.T) {
	original := &ToolThreadingError{
		Provider: "minimax",
		Violations: []ToolThreadingViolation{
			{Kind: "out_of_order", Index: 3, Detail: "reversed"},
		},
		Wrapped: fmt.Errorf("HTTP 400: tool call result does not follow"),
	}

	result := ClassifyError(original, "ignored-provider")

	var tte *ToolThreadingError
	if !errors.As(result, &tte) {
		t.Fatal("expected *ToolThreadingError")
	}
	if len(tte.Violations) != 1 {
		t.Errorf("violations count = %d, want 1", len(tte.Violations))
	}
	if tte.Provider != "minimax" {
		t.Errorf("Provider = %q, want %q (passthrough should not change provider)", tte.Provider, "minimax")
	}
}
