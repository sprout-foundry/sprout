package errors

import (
	"testing"
	"time"
)

// TestGetCategoryUnderstandsTypedError pins the SP-094 bridge: a TypedError
// is classified the way an AgentError is — network and timeout read as the
// retryable Transient category, and every other typed code stays
// unclassified so the tool/security/retry paths are unchanged by the
// existence of a TypedError in the chain.
func TestGetCategoryUnderstandsTypedError(t *testing.T) {
	cases := []struct {
		err     error
		wantCat ErrorCategory
		wantOK  bool
	}{
		{NewNetwork("connection reset", nil), CategoryTransient, true},
		{NewTimeout("stream", 5*time.Second), CategoryTransient, true},
		// Non-retryable typed codes are left unclassified (ok=false) so they
		// are not suddenly read as a legacy category and fail immediately.
		{NewValidation("bad", nil), 0, false},
		{NewNotFound("model"), 0, false},
		{NewPermission("denied", nil), 0, false},
		{NewConfig("missing key", nil), 0, false},
		{NewAgent("runner", "crash", nil), 0, false},
		{NewTool("shell_command", "failed", nil), 0, false},
		{NewApproval("blocked", nil), 0, false},
	}
	for _, c := range cases {
		got, ok := GetCategory(c.err)
		if ok != c.wantOK {
			t.Errorf("GetCategory(%v) ok = %v, want %v", c.err, ok, c.wantOK)
		}
		if ok && got != c.wantCat {
			t.Errorf("GetCategory(%v) = %v, want %v", c.err, got, c.wantCat)
		}
	}
}

// TestIsRetryableUnderstandsTypedError pins that a network/timeout TypedError
// is retryable (the LLM retry loops can recover from a mid-stream reset or
// idle/first-token timeout) while other typed errors are not.
func TestIsRetryableUnderstandsTypedError(t *testing.T) {
	if !IsRetryable(NewNetwork("connection reset", nil)) {
		t.Error("network TypedError should be retryable")
	}
	if !IsRetryable(NewTimeout("idle", 5*time.Second)) {
		t.Error("timeout TypedError should be retryable")
	}
	if IsRetryable(NewValidation("bad", nil)) {
		t.Error("validation TypedError should not be retryable")
	}
	if IsRetryable(NewNotFound("model")) {
		t.Error("not-found TypedError should not be retryable")
	}
}

// TestIsTransientUnderstandsTypedError: the category helpers agree with the
// retry loops — a network/timeout TypedError is read as transient.
func TestIsTransientUnderstandsTypedError(t *testing.T) {
	if !IsTransient(NewNetwork("reset", nil)) {
		t.Error("network TypedError should read as transient")
	}
	if !IsTransient(NewTimeout("idle", 5*time.Second)) {
		t.Error("timeout TypedError should read as transient")
	}
	if IsTransient(NewValidation("bad", nil)) {
		t.Error("validation TypedError should not read as transient")
	}
}

// TestGetCategoryAgentErrorTakesPrecedenceOverTypedError: when both an
// AgentError and a TypedError are in the chain, the legacy AgentError
// classification wins so legacy callers see unchanged behaviour.
func TestGetCategoryAgentErrorTakesPrecedenceOverTypedError(t *testing.T) {
	agentErr := NewProviderError("auth failed", NewNetwork("underlying", nil), "p", "m")
	if cat, ok := GetCategory(agentErr); !ok || cat != CategoryProvider {
		t.Errorf("GetCategory = (%v, %v); want (Provider, true)", cat, ok)
	}
	if IsRetryable(agentErr) {
		// isProviderErrorRetryable reads the cause text; "underlying" is not
		// auth/model-not-found/5xx, so it is not retryable.
		t.Error("provider error with network cause should not be retryable by legacy path")
	}
}
