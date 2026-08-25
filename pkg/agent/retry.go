// Package agent provides typed error classification for tool error retry decisions.
//
// The retry package defines a classification system that uses the typed AgentError
// system from pkg/errors to determine appropriate retry behavior instead of
// relying on string matching against error messages.
//
// Classification is driven by the error category:
//
//   - SecurityError       → Escalate (ask user/LLM)
//   - PermissionError     → Fail (approval denied/timeout — not retryable)
//   - TransientError      → Retry (backoff)
//   - RateLimitError      → Retry (longer backoff)
//   - InvalidInputError   → Fail (input must be fixed)
//   - ContextError        → Fail (context overflow, needs compaction)
//   - PermanentError      → Fail (non-recoverable)
//   - ProviderError       → Fail (auth/config) or Retry (server errors) depending on Retryable field
//   - Unknown errors      → Retry once then Fail
package agent

import (
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// RetryAction represents the action to take when a tool error occurs.
type RetryAction int

const (
	// ActionRetry indicates the error is transient and the tool call should be retried.
	// Covers TransientError, RateLimitError, retryable ProviderError, and unknown/untyped errors.
	ActionRetry RetryAction = iota
	// ActionFail indicates the error is permanent and should not be retried.
	ActionFail
	// ActionEscalate indicates the error needs human/LLM review before proceeding.
	ActionEscalate
)

// String returns a human-readable name for the retry action.
func (a RetryAction) String() string {
	switch a {
	case ActionRetry:
		return "retry"
	case ActionFail:
		return "fail"
	case ActionEscalate:
		return "escalate"
	default:
		return "unknown"
	}
}

// ClassifyError examines an error and returns the appropriate RetryAction.
//
// It uses typed error checks from pkg/errors (errors.As via helper functions)
// rather than string matching on error messages. This provides more reliable
// classification as the error types are structural rather than text-based.
//
// Classification rules (checked in priority order):
//   - SecurityError → ActionEscalate (ask user/LLM)
//   - PermissionError → ActionFail (approval denied/timeout — not retryable)
//   - TransientError → ActionRetry (with backoff)
//   - RateLimitError → ActionRetry (with longer backoff)
//   - InvalidInputError → ActionFail (fix the input)
//   - ContextError (ContextOverflow) → ActionFail (need context compaction)
//   - ProviderError → ActionFail (auth/config) or ActionRetry (server errors) depending on Retryable
//   - PermanentError → ActionFail
//   - Retryable AgentError → ActionRetry
//   - Default (unknown/untyped errors) → ActionRetry once, then ActionFail
func ClassifyError(err error) RetryAction {
	if err == nil {
		return ActionRetry // no error, proceed
	}

	// Check specific categories first (in priority order), then fall back to
	// generic retryability. Security errors are escalated before anything else
	// because they need human/LLM review before the system proceeds.
	if agenterrors.IsSecurity(err) {
		return ActionEscalate
	}
	if agenterrors.IsPermission(err) {
		return ActionFail
	}
	if agenterrors.IsTransient(err) {
		return ActionRetry
	}
	if agenterrors.IsRateLimited(err) {
		return ActionRetry
	}
	if agenterrors.IsInvalidInput(err) {
		return ActionFail
	}
	if agenterrors.IsContextError(err) {
		return ActionFail
	}
	if agenterrors.IsProviderError(err) {
		if agenterrors.IsRetryable(err) {
			return ActionRetry // Retryable provider error (server error, overload)
		}
		return ActionFail // Auth failure, model not found — not retryable
	}
	if agenterrors.IsPermanent(err) {
		return ActionFail
	}

	// For AgentError types that are retryable but didn't match a specific
	// category above, retry.
	if agenterrors.IsRetryable(err) {
		return ActionRetry
	}

	// Default: unknown/untyped errors get one retry attempt before failing.
	// The risk is retrying a permanent error, but the alternative (failing
	// unknown errors) risks giving up on transient network/provider issues.
	// If a new error type should not be retried, wrap it in a typed AgentError
	// with the appropriate category.
	return ActionRetry
}
