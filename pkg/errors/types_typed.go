// Package errors provides agent-specific error types and classification for intelligent retry/recovery logic.
//
// This package defines a taxonomy of errors that the agent can use to make informed decisions
// about how to handle failures. Unlike the generic StructuredError in pkg/utils, these errors
// are specifically designed to support the agent's retry logic and recovery strategies.
//
// Categories:
//   - CategoryTransient: Temporary failures (network, timeout, provider overload) - retryable
//   - CategoryRateLimited: Rate limit/quota exhaustion - retryable with backoff
//   - CategorySecurity: Security violations (blocked commands, unauthorized access) - not retryable
//   - CategoryInvalidInput: Invalid parameters, malformed requests - not retryable
//   - CategoryProvider: Provider-specific failures (auth, model not found) - depends on cause
//   - CategoryContext: Context window exceeded, compaction needed - retryable after compaction
//   - CategoryPermanent: Non-recoverable errors - not retryable
//
// Example usage:
//
//	err := errors.NewTransientError("network timeout", originalErr)
//	if errors.IsRetryable(err) {
//	    // Retry with backoff
//	}
//
//	if errors.IsContextError(err) {
//	    // Trigger conversation compaction
//	}
package errors

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// types_typed.go — the SP-094 typed-error hierarchy: ErrorCode / Severity /
// TypedError, the typed constructors (NewValidation, NewNotFound, ...), and
// the Wrap/Wrapf helpers. Split out of types.go; coexists with the legacy
// taxonomy in types.go.

// =============================================================================
// SP-094 typed-error hierarchy (additive, coexists with the legacy Category
// taxonomy above). New call sites should prefer these constructors; existing
// call sites continue to use the legacy Category-based API until migration.
// =============================================================================

// ErrorCode is the stable wire-level identifier for an error type.
// Codes are stable strings suitable for serialization across process boundaries.
type ErrorCode string

const (
	// CodeUnknown indicates an unrecognized or unclassified error.
	CodeUnknown ErrorCode = "unknown"
	// CodeValidation indicates the input failed validation (bad parameters, malformed request).
	CodeValidation ErrorCode = "validation"
	// CodeNotFound indicates the requested resource does not exist.
	CodeNotFound ErrorCode = "not_found"
	// CodePermission indicates the caller lacks authorization for the operation.
	CodePermission ErrorCode = "permission"
	// CodeTimeout indicates the operation exceeded its time limit.
	CodeTimeout ErrorCode = "timeout"
	// CodeNetwork indicates a network-level failure (connect, DNS, transport).
	CodeNetwork ErrorCode = "network"
	// CodeConfig indicates a configuration error (missing, invalid, or conflicting settings).
	CodeConfig ErrorCode = "config"
	// CodeAgent indicates an agent-level failure (runner crash, internal error).
	CodeAgent ErrorCode = "agent"
	// CodeTool indicates a tool execution failure.
	CodeTool ErrorCode = "tool"
	// CodeApproval indicates an approval gate blocked the operation.
	CodeApproval ErrorCode = "approval"
)

// Severity represents the operational severity of an error.
type Severity string

const (
	// SeverityInfo indicates informational events.
	// Reserved for future non-error informational events. No constructor currently emits this severity.
	SeverityInfo Severity = "info"
	// SeverityWarning indicates a recoverable issue that may need attention.
	SeverityWarning Severity = "warning"
	// SeverityError indicates a standard error condition.
	SeverityError Severity = "error"
	// SeverityCritical indicates a configuration or system-level failure requiring immediate attention.
	SeverityCritical Severity = "critical"
)

// TypedError is the base of the new typed-error hierarchy added in SP-094.
// It complements the legacy AgentError (above) with a wire-stable ErrorCode,
// HTTP status code, structured Details, and a Component field for source
// attribution. Call sites should prefer TypedError for new code; migration
// of the legacy AgentError call sites is tracked separately.
type TypedError struct {
	Code      ErrorCode      // stable wire-level identifier
	Severity  Severity       // operational severity
	Message   string         // human-readable message
	Cause     error          // wrapped cause (may be nil)
	Component string         // source attribution, e.g. "agent.Runner", "tool.shell_command"
	Retryable bool           // whether the operation may be retried
	Status    int            // HTTP status code (0 if not applicable)
	Time      time.Time      // when the error was created
	Details   map[string]any // structured context (operation IDs, attempt counts, etc.)
}

// Error implements the error interface. Format:
//
//	[code] message
//
// When a cause is present:
//
//	[code] message: <cause>
func (e *TypedError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the wrapped cause for errors.Is / errors.As traversal.
func (e *TypedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is enables errors.Is(err, sentinel) comparisons by Code.
// A TypedError matches a sentinel TypedError if their Codes are equal.
func (e *TypedError) Is(target error) bool {
	if e == nil {
		return false
	}
	t, ok := target.(*TypedError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// WithDetail sets a key/value on Details (chainable). Returns e.
func (e *TypedError) WithDetail(key string, value any) *TypedError {
	if e == nil {
		return &TypedError{Details: map[string]any{key: value}}
	}
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

// WithComponent sets the Component field (chainable). Returns e.
func (e *TypedError) WithComponent(component string) *TypedError {
	if e == nil {
		return &TypedError{Component: component}
	}
	e.Component = component
	return e
}

// AsTypedError extracts a *TypedError from anywhere in the error chain.
// Returns nil if no TypedError is present.
func AsTypedError(err error) *TypedError {
	var te *TypedError
	if errors.As(err, &te) {
		return te
	}
	return nil
}

// SeverityFor returns the canonical Severity for a given ErrorCode.
func SeverityFor(code ErrorCode) Severity {
	switch code {
	case CodeTimeout:
		return SeverityWarning
	case CodeConfig:
		return SeverityCritical
	default:
		return SeverityError
	}
}

// StatusFor returns the canonical HTTP status code for a given ErrorCode.
func StatusFor(code ErrorCode) int {
	switch code {
	case CodeValidation:
		return http.StatusBadRequest
	case CodeNotFound:
		return http.StatusNotFound
	case CodePermission:
		return http.StatusForbidden
	case CodeTimeout:
		return http.StatusRequestTimeout
	case CodeNetwork:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

// RetryableFor returns whether a given ErrorCode is considered retryable.
func RetryableFor(code ErrorCode) bool {
	switch code {
	case CodeTimeout, CodeNetwork:
		return true
	default:
		return false
	}
}

// NewValidation creates a TypedError for input validation failures.
func NewValidation(msg string, details map[string]any) *TypedError {
	return &TypedError{
		Code:      CodeValidation,
		Severity:  SeverityFor(CodeValidation),
		Status:    StatusFor(CodeValidation),
		Retryable: RetryableFor(CodeValidation),
		Message:   msg,
		Time:      time.Now(),
		Details:   cloneDetails(details),
	}
}

// NewNotFound creates a TypedError indicating a resource was not found.
func NewNotFound(what string) *TypedError {
	return &TypedError{
		Code:      CodeNotFound,
		Severity:  SeverityFor(CodeNotFound),
		Status:    StatusFor(CodeNotFound),
		Retryable: RetryableFor(CodeNotFound),
		Message:   what + " not found",
		Time:      time.Now(),
	}
}

// NewNotFoundCause creates a TypedError indicating a resource was not found,
// preserving the original error as the cause for errors.Is/errors.As traversal.
// Use this when wrapping a *PathError, *os.LinkError, or other syscall-level
// error so the underlying errno and operation context remain accessible.
func NewNotFoundCause(what string, cause error) *TypedError {
	return &TypedError{
		Code:      CodeNotFound,
		Severity:  SeverityFor(CodeNotFound),
		Status:    StatusFor(CodeNotFound),
		Retryable: RetryableFor(CodeNotFound),
		Message:   what + " not found",
		Cause:     cause,
		Time:      time.Now(),
	}
}

// NewPermission creates a TypedError for permission or authorization failures.
func NewPermission(msg string, details map[string]any) *TypedError {
	return &TypedError{
		Code:      CodePermission,
		Severity:  SeverityFor(CodePermission),
		Status:    StatusFor(CodePermission),
		Retryable: RetryableFor(CodePermission),
		Message:   msg,
		Time:      time.Now(),
		Details:   cloneDetails(details),
	}
}

// NewTimeout creates a TypedError for operation timeouts.
func NewTimeout(op string, dur time.Duration) *TypedError {
	return &TypedError{
		Code:      CodeTimeout,
		Severity:  SeverityFor(CodeTimeout),
		Status:    StatusFor(CodeTimeout),
		Retryable: RetryableFor(CodeTimeout),
		Message:   fmt.Sprintf("%s timed out after %s", op, dur),
		Time:      time.Now(),
	}
}

// NewNetwork creates a TypedError for network-related failures.
func NewNetwork(msg string, cause error) *TypedError {
	return &TypedError{
		Code:      CodeNetwork,
		Severity:  SeverityFor(CodeNetwork),
		Status:    StatusFor(CodeNetwork),
		Retryable: RetryableFor(CodeNetwork),
		Message:   msg,
		Cause:     cause,
		Time:      time.Now(),
	}
}

// NewConfig creates a TypedError for configuration errors.
func NewConfig(msg string, cause error) *TypedError {
	return &TypedError{
		Code:      CodeConfig,
		Severity:  SeverityFor(CodeConfig),
		Status:    StatusFor(CodeConfig),
		Retryable: RetryableFor(CodeConfig),
		Message:   msg,
		Cause:     cause,
		Time:      time.Now(),
	}
}

// NewAgent creates a TypedError for agent-level failures.
func NewAgent(component, msg string, cause error) *TypedError {
	return &TypedError{
		Code:      CodeAgent,
		Severity:  SeverityFor(CodeAgent),
		Status:    StatusFor(CodeAgent),
		Retryable: RetryableFor(CodeAgent),
		Message:   msg,
		Cause:     cause,
		Component: component,
		Time:      time.Now(),
	}
}

// NewTool creates a TypedError for tool execution failures.
func NewTool(toolName string, msg string, cause error) *TypedError {
	return &TypedError{
		Code:      CodeTool,
		Severity:  SeverityFor(CodeTool),
		Status:    StatusFor(CodeTool),
		Retryable: RetryableFor(CodeTool),
		Message:   msg,
		Cause:     cause,
		Component: "tool." + toolName,
		Time:      time.Now(),
	}
}

// NewApproval creates a TypedError for approval-related errors.
func NewApproval(msg string, details map[string]any) *TypedError {
	return &TypedError{
		Code:      CodeApproval,
		Severity:  SeverityFor(CodeApproval),
		Status:    StatusFor(CodeApproval),
		Retryable: RetryableFor(CodeApproval),
		Message:   msg,
		Time:      time.Now(),
		Details:   cloneDetails(details),
	}
}

// Wrap returns a *TypedError with Code=CodeAgent wrapping cause with msg.
// If cause is already a *TypedError, it is returned unchanged (no double-wrap).
// To add context to an existing TypedError, use WithDetail instead.
// If cause is nil, returns NewAgent("", msg, nil).
func Wrap(cause error, msg string) error {
	if te := AsTypedError(cause); te != nil {
		return te
	}
	if cause == nil {
		return NewAgent("", msg, nil)
	}
	return NewAgent("", msg, cause)
}

// Wrapf is Wrap with a formatted message.
// If cause is already a *TypedError, it is returned unchanged (no double-wrap).
// To add context to an existing TypedError, use WithDetail instead.
func Wrapf(cause error, format string, args ...any) error {
	return Wrap(cause, fmt.Sprintf(format, args...))
}

// cloneDetails returns a defensive copy of details, or an empty non-nil map if
// details is nil. This prevents callers from mutating (or being surprised by
// mutations of) the details map after constructing a TypedError.
func cloneDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(details))
	for k, v := range details {
		out[k] = v
	}
	return out
}
