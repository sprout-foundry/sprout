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
	"strings"
	"time"
)

// ErrorCategory represents the classification of an error for retry/recovery logic.
type ErrorCategory int

const (
	// CategoryTransient indicates a temporary failure that should be retried.
	// Examples: network timeout, connection reset, 502 gateway errors.
	CategoryTransient ErrorCategory = iota

	// CategoryRateLimited indicates rate limit or quota exhaustion.
	// Should be retried with exponential backoff and key rotation if available.
	CategoryRateLimited

	// CategorySecurity indicates a security violation.
	// Should NOT be retried. Examples: blocked commands, unauthorized access.
	CategorySecurity

	// CategoryInvalidInput indicates invalid parameters or malformed requests.
	// Should NOT be retried without fixing the input.
	CategoryInvalidInput

	// CategoryProvider indicates provider-specific failures.
	// Retryability depends on the underlying cause. Examples: auth errors, model not found.
	CategoryProvider

	// CategoryContext indicates context window exceeded.
	// Should be retried after conversation compaction.
	CategoryContext

	// CategoryPermanent indicates non-recoverable errors.
	// Should NOT be retried.
	CategoryPermanent
)

// String returns a human-readable representation of the error category.
func (c ErrorCategory) String() string {
	switch c {
	case CategoryTransient:
		return "Transient"
	case CategoryRateLimited:
		return "RateLimited"
	case CategorySecurity:
		return "Security"
	case CategoryInvalidInput:
		return "InvalidInput"
	case CategoryProvider:
		return "Provider"
	case CategoryContext:
		return "Context"
	case CategoryPermanent:
		return "Permanent"
	default:
		return "Unknown"
	}
}

// AgentError represents a structured error with classification for agent retry logic.
// It implements the error interface and supports error unwrapping for compatibility
// with errors.Is() and errors.As().
type AgentError struct {
	Category  ErrorCategory
	Message   string
	Cause     error
	Retryable bool
	Metadata  map[string]string
}

// Error returns a formatted error message.
// If a cause is present, it includes the wrapped error. The format is:
//
//	[Category] message: cause
//
// If no cause is present:
//
//	[Category] message
func (e *AgentError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Category, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Category, e.Message)
}

// Unwrap returns the underlying error for compatibility with errors.Is and errors.As.
func (e *AgentError) Unwrap() error {
	return e.Cause
}

// WithMetadata adds or updates a metadata key-value pair.
func (e *AgentError) WithMetadata(key, value string) *AgentError {
	if e.Metadata == nil {
		e.Metadata = make(map[string]string)
	}
	e.Metadata[key] = value
	return e
}

// WithProvider sets the provider in metadata.
func (e *AgentError) WithProvider(provider string) *AgentError {
	return e.WithMetadata("provider", provider)
}

// WithModel sets the model in metadata.
func (e *AgentError) WithModel(model string) *AgentError {
	return e.WithMetadata("model", model)
}

// GetMetadata returns the value for a metadata key, or empty string if not found.
func (e *AgentError) GetMetadata(key string) string {
	if e.Metadata == nil {
		return ""
	}
	return e.Metadata[key]
}

// NewTransientError creates a retryable transient error for temporary failures.
// Examples: network timeout, connection reset, provider overload.
func NewTransientError(message string, cause error) *AgentError {
	return &AgentError{
		Category:  CategoryTransient,
		Message:   message,
		Cause:     cause,
		Retryable: true,
		Metadata:  make(map[string]string),
	}
}

// NewRateLimitError creates a retryable rate limit error.
// Includes provider information for key rotation decisions.
func NewRateLimitError(message string, cause error, provider string) *AgentError {
	err := &AgentError{
		Category:  CategoryRateLimited,
		Message:   message,
		Cause:     cause,
		Retryable: true,
		Metadata:  make(map[string]string),
	}
	if provider != "" {
		err.WithProvider(provider)
	}
	return err
}

// NewSecurityError creates a non-retryable security error.
// Examples: blocked commands, unauthorized access.
func NewSecurityError(message string, cause error) *AgentError {
	return &AgentError{
		Category:  CategorySecurity,
		Message:   message,
		Cause:     cause,
		Retryable: false,
		Metadata:  make(map[string]string),
	}
}

// NewSecurityErrorWithAssessment creates a non-retryable security error
// that carries a RiskAssessment explanation for the --why diagnostic.
// The assessment is stored in the error's metadata under the "assessment" key.
func NewSecurityErrorWithAssessment(message string, assessment string, cause error) *AgentError {
	err := &AgentError{
		Category:  CategorySecurity,
		Message:   message,
		Cause:     cause,
		Retryable: false,
		Metadata:  make(map[string]string),
	}
	err.Metadata["assessment"] = assessment
	return err
}

// Why returns the RiskAssessment explanation attached to this error,
// or "" if no assessment was attached. Used by the --why CLI flag.
func (e *AgentError) Why() string {
	if e.Metadata == nil {
		return ""
	}
	return e.Metadata["assessment"]
}

// NewInvalidInputError creates a non-retryable invalid input error.
// Examples: invalid parameters, malformed requests.
func NewInvalidInputError(message string, cause error) *AgentError {
	return &AgentError{
		Category:  CategoryInvalidInput,
		Message:   message,
		Cause:     cause,
		Retryable: false,
		Metadata:  make(map[string]string),
	}
}

// NewProviderError creates a provider-specific error.
// Retryability depends on the underlying cause. Includes provider and model info.
// For auth errors, this is not retryable. For model not found, not retryable.
// For provider overload, may be retryable.
func NewProviderError(message string, cause error, provider, model string) *AgentError {
	err := &AgentError{
		Category: CategoryProvider,
		Message:  message,
		Cause:    cause,
		// Determine retryability based on cause
		Retryable: isProviderErrorRetryable(cause),
		Metadata:  make(map[string]string),
	}
	if provider != "" {
		err.WithProvider(provider)
	}
	if model != "" {
		err.WithModel(model)
	}
	return err
}

// NewContextError creates a retryable context error.
// Should be retried after conversation compaction.
func NewContextError(message string, cause error) *AgentError {
	return &AgentError{
		Category:  CategoryContext,
		Message:   message,
		Cause:     cause,
		Retryable: true,
		Metadata:  make(map[string]string),
	}
}

// NewPermanentError creates a non-retryable permanent error.
// Examples: non-recoverable failures, configuration errors.
func NewPermanentError(message string, cause error) *AgentError {
	return &AgentError{
		Category:  CategoryPermanent,
		Message:   message,
		Cause:     cause,
		Retryable: false,
		Metadata:  make(map[string]string),
	}
}

// isProviderErrorRetryable determines if a provider error is retryable based on the cause.
func isProviderErrorRetryable(cause error) bool {
	if cause == nil {
		return false
	}

	causeStr := strings.ToLower(cause.Error())

	// Auth errors are not retryable without changing credentials
	if strings.Contains(causeStr, "unauthorized") ||
		strings.Contains(causeStr, "401") ||
		strings.Contains(causeStr, "authentication") ||
		strings.Contains(causeStr, "invalid api key") {
		return false
	}

	// Model not found is not retryable
	if strings.Contains(causeStr, "model not found") ||
		strings.Contains(causeStr, "model not exist") ||
		strings.Contains(causeStr, "invalid model") {
		return false
	}

	// Provider overload or server errors may be retryable
	if strings.Contains(causeStr, "502") ||
		strings.Contains(causeStr, "503") ||
		strings.Contains(causeStr, "overloaded") ||
		strings.Contains(causeStr, "internal server error") {
		return true
	}

	// Default to not retryable for provider errors
	return false
}

// WrapWithCategory wraps an existing error with a specific category and message.
// The original error is preserved as the cause, maintaining the error chain.
func WrapWithCategory(err error, category ErrorCategory, message string) *AgentError {
	if err == nil {
		return nil
	}

	// Determine retryability based on category
	retryable := isCategoryRetryable(category)

	return &AgentError{
		Category:  category,
		Message:   message,
		Cause:     err,
		Retryable: retryable,
		Metadata:  make(map[string]string),
	}
}

// isCategoryRetryable returns true if the category is generally retryable.
func isCategoryRetryable(category ErrorCategory) bool {
	switch category {
	case CategoryTransient, CategoryRateLimited, CategoryContext:
		return true
	case CategorySecurity, CategoryInvalidInput, CategoryPermanent:
		return false
	case CategoryProvider:
		// Provider errors depend on the underlying cause
		return false
	default:
		return false
	}
}

// GetCategory extracts the error category from an AgentError.
// Returns the category and true if the error is an AgentError, or zero and false otherwise.
func GetCategory(err error) (ErrorCategory, bool) {
	if err == nil {
		return 0, false
	}

	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return agentErr.Category, true
	}

	return 0, false
}

// IsRetryable checks if an error is retryable.
// Returns true if the error is an AgentError with Retryable=true.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var agentErr *AgentError
	if errors.As(err, &agentErr) {
		return agentErr.Retryable
	}

	return false
}

// IsTransient checks if an error is in the Transient category.
func IsTransient(err error) bool {
	category, ok := GetCategory(err)
	return ok && category == CategoryTransient
}

// IsRateLimited checks if an error is in the RateLimited category.
func IsRateLimited(err error) bool {
	category, ok := GetCategory(err)
	return ok && category == CategoryRateLimited
}

// IsSecurity checks if an error is in the Security category.
func IsSecurity(err error) bool {
	category, ok := GetCategory(err)
	return ok && category == CategorySecurity
}

// IsInvalidInput checks if an error is in the InvalidInput category.
func IsInvalidInput(err error) bool {
	category, ok := GetCategory(err)
	return ok && category == CategoryInvalidInput
}

// IsProviderError checks if an error is in the Provider category.
func IsProviderError(err error) bool {
	category, ok := GetCategory(err)
	return ok && category == CategoryProvider
}

// IsContextError checks if an error is in the Context category.
func IsContextError(err error) bool {
	category, ok := GetCategory(err)
	return ok && category == CategoryContext
}

// IsPermanent checks if an error is in the Permanent category.
func IsPermanent(err error) bool {
	category, ok := GetCategory(err)
	return ok && category == CategoryPermanent
}

// IsPermission checks if an error is a TypedError with CodePermission.
// Permission errors arise from security approval denial, approval timeout,
// or no approval channel — none are retryable.
func IsPermission(err error) bool {
	if err == nil {
		return false
	}
	var typedErr *TypedError
	if errors.As(err, &typedErr) {
		return typedErr.Code == CodePermission
	}
	return false
}

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
