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
	"strings"
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
