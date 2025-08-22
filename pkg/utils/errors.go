package utils

import (
	"fmt"
	"runtime"
	"strings"
)

// ErrorSeverity represents the severity level of an error
type ErrorSeverity int

const (
	SeverityLow ErrorSeverity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// ErrorCategory represents the category of an error
type ErrorCategory int

const (
	CategorySystem ErrorCategory = iota
	CategoryNetwork
	CategoryFileSystem
	CategoryConfiguration
	CategoryValidation
	CategoryExecution
	CategoryUser
)

// ErrorContext provides additional context for errors
type ErrorContext struct {
	Component string
	Operation string
	UserID    string
	RequestID string
	Resource  string
	Metadata  map[string]interface{}
}

// StructuredError represents a standardized error with rich context
type StructuredError struct {
	Code        string
	Message     string
	Severity    ErrorSeverity
	Category    ErrorCategory
	Context     *ErrorContext
	RootCause   error
	StackTrace  string
	Timestamp   int64
	Recoverable bool
}

// Error implements the error interface
func (e *StructuredError) Error() string {
	if e.RootCause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.RootCause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying error for compatibility with errors.Is and errors.As
func (e *StructuredError) Unwrap() error {
	return e.RootCause
}

// NewStructuredError creates a new structured error
func NewStructuredError(code, message string, severity ErrorSeverity, category ErrorCategory, rootCause error) *StructuredError {
	err := &StructuredError{
		Code:        code,
		Message:     message,
		Severity:    severity,
		Category:    category,
		RootCause:   rootCause,
		Timestamp:   GetCurrentTimestamp(),
		Recoverable: true,
	}

	// Capture stack trace for medium/high severity errors
	if severity >= SeverityMedium {
		err.StackTrace = captureStackTrace()
	}

	return err
}

// NewSystemError creates a system-level error
func NewSystemError(operation string, rootCause error) *StructuredError {
	return NewStructuredError(
		"SYS_ERROR",
		fmt.Sprintf("System error during %s", operation),
		SeverityHigh,
		CategorySystem,
		rootCause,
	).WithContext(&ErrorContext{Operation: operation})
}

// NewNetworkError creates a network-related error
func NewNetworkError(operation string, rootCause error) *StructuredError {
	return NewStructuredError(
		"NET_ERROR",
		fmt.Sprintf("Network error during %s", operation),
		SeverityMedium,
		CategoryNetwork,
		rootCause,
	).WithContext(&ErrorContext{Operation: operation})
}

// NewFileSystemError creates a filesystem-related error
func NewFileSystemError(operation, path string, rootCause error) *StructuredError {
	return NewStructuredError(
		"FS_ERROR",
		fmt.Sprintf("Filesystem error during %s", operation),
		SeverityMedium,
		CategoryFileSystem,
		rootCause,
	).WithContext(&ErrorContext{Operation: operation, Resource: path})
}

// NewConfigError creates a configuration-related error
func NewConfigError(key string, rootCause error) *StructuredError {
	return NewStructuredError(
		"CFG_ERROR",
		fmt.Sprintf("Configuration error for %s", key),
		SeverityMedium,
		CategoryConfiguration,
		rootCause,
	).WithContext(&ErrorContext{Resource: key})
}

// NewValidationError creates a validation error
func NewValidationError(field, reason string) *StructuredError {
	return NewStructuredError(
		"VAL_ERROR",
		fmt.Sprintf("Validation failed for %s: %s", field, reason),
		SeverityLow,
		CategoryValidation,
		nil,
	).WithContext(&ErrorContext{Resource: field})
}

// NewExecutionError creates an execution error
func NewExecutionError(component, operation string, rootCause error) *StructuredError {
	return NewStructuredError(
		"EXEC_ERROR",
		fmt.Sprintf("Execution failed in %s during %s", component, operation),
		SeverityHigh,
		CategoryExecution,
		rootCause,
	).WithContext(&ErrorContext{Component: component, Operation: operation})
}

// NewUserError creates a user-facing error
func NewUserError(message string, rootCause error) *StructuredError {
	return NewStructuredError(
		"USER_ERROR",
		message,
		SeverityLow,
		CategoryUser,
		rootCause,
	)
}

// WithContext adds context to the error
func (e *StructuredError) WithContext(ctx *ErrorContext) *StructuredError {
	e.Context = ctx
	return e
}

// WithComponent adds component context
func (e *StructuredError) WithComponent(component string) *StructuredError {
	if e.Context == nil {
		e.Context = &ErrorContext{}
	}
	e.Context.Component = component
	return e
}

// WithOperation adds operation context
func (e *StructuredError) WithOperation(operation string) *StructuredError {
	if e.Context == nil {
		e.Context = &ErrorContext{}
	}
	e.Context.Operation = operation
	return e
}

// WithResource adds resource context
func (e *StructuredError) WithResource(resource string) *StructuredError {
	if e.Context == nil {
		e.Context = &ErrorContext{}
	}
	e.Context.Resource = resource
	return e
}

// WithMetadata adds metadata to the error
func (e *StructuredError) WithMetadata(key string, value interface{}) *StructuredError {
	if e.Context == nil {
		e.Context = &ErrorContext{}
	}
	if e.Context.Metadata == nil {
		e.Context.Metadata = make(map[string]interface{})
	}
	e.Context.Metadata[key] = value
	return e
}

// MakeUnrecoverable marks the error as unrecoverable
func (e *StructuredError) MakeUnrecoverable() *StructuredError {
	e.Recoverable = false
	return e
}

// IsRecoverable checks if the error can be recovered from
func (e *StructuredError) IsRecoverable() bool {
	return e.Recoverable
}

// GetSeverity returns the error severity
func (e *StructuredError) GetSeverity() ErrorSeverity {
	return e.Severity
}

// GetCategory returns the error category
func (e *StructuredError) GetCategory() ErrorCategory {
	return e.Category
}

// GetCode returns the error code
func (e *StructuredError) GetCode() string {
	return e.Code
}

// GetContext returns the error context
func (e *StructuredError) GetContext() *ErrorContext {
	return e.Context
}

// GetStackTrace returns the stack trace if available
func (e *StructuredError) GetStackTrace() string {
	return e.StackTrace
}

// captureStackTrace captures the current stack trace
func captureStackTrace() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// GetCurrentTimestamp returns the current timestamp
func GetCurrentTimestamp() int64 {
	return 0 // Placeholder - would use time.Now().Unix() in real implementation
}

// LegacyErrorHandler provides centralized error handling functionality (deprecated)
type LegacyErrorHandler struct {
	logger     *Logger
	maxRetries int
	retryDelay int64 // milliseconds
}

// NewLegacyErrorHandler creates a new legacy error handler
func NewLegacyErrorHandler(logger *Logger, maxRetries int) *LegacyErrorHandler {
	return &LegacyErrorHandler{
		logger:     logger,
		maxRetries: maxRetries,
		retryDelay: 1000, // 1 second default
	}
}

// Error handling utilities

// RecoverableError wraps an error to make it recoverable
// RecoverableError moved to error_helpers.go

// IsCriticalError checks if an error is critical
func IsCriticalError(err error) bool {
	if structuredErr, ok := err.(*StructuredError); ok {
		return structuredErr.Severity >= SeverityCritical
	}
	return false
}

// IsNetworkError checks if an error is network-related
func IsNetworkError(err error) bool {
	if structuredErr, ok := err.(*StructuredError); ok {
		return structuredErr.Category == CategoryNetwork
	}
	return false
}

// IsValidationError checks if an error is validation-related
func IsValidationError(err error) bool {
	if structuredErr, ok := err.(*StructuredError); ok {
		return structuredErr.Category == CategoryValidation
	}
	return false
}

// FormatError formats an error for display
func FormatError(err error) string {
	if structuredErr, ok := err.(*StructuredError); ok {
		var parts []string

		parts = append(parts, fmt.Sprintf("Error [%s]: %s", structuredErr.Code, structuredErr.Message))

		if structuredErr.Context != nil {
			if structuredErr.Context.Component != "" {
				parts = append(parts, fmt.Sprintf("Component: %s", structuredErr.Context.Component))
			}
			if structuredErr.Context.Operation != "" {
				parts = append(parts, fmt.Sprintf("Operation: %s", structuredErr.Context.Operation))
			}
			if structuredErr.Context.Resource != "" {
				parts = append(parts, fmt.Sprintf("Resource: %s", structuredErr.Context.Resource))
			}
		}

		if structuredErr.RootCause != nil {
			parts = append(parts, fmt.Sprintf("Root Cause: %v", structuredErr.RootCause))
		}

		return strings.Join(parts, " | ")
	}

	return err.Error()
}

// LogError logs an error with context
func (h *ErrorHandler) LogError(err error, context string) {
	if h.logger != nil {
		message := FormatError(err)
		if context != "" {
			message = fmt.Sprintf("%s | Context: %s", message, context)
		}
		h.logger.LogProcessStep(fmt.Sprintf("ERROR: %s", message))
	}
}
