package utils

import (
	"context"
	"fmt"
	"log"
)

// Global error manager instance
var globalErrorManager *ErrorManager

// InitErrorHandling initializes the global error handling system
func InitErrorHandling(logger *Logger) {
	globalErrorManager = NewErrorManager(logger)

	// Add default observers
	globalErrorManager.AddObserver(NewLogErrorObserver(logger))
	globalErrorManager.AddObserver(NewMetricsErrorObserver())
}

// GetErrorManager returns the global error manager
func GetErrorManager() *ErrorManager {
	if globalErrorManager == nil {
		// Initialize with a basic logger if not already initialized
		InitErrorHandling(GetLogger(true))
	}
	return globalErrorManager
}

// HandleError is a convenience function for handling errors globally
func HandleError(ctx context.Context, err error, context string, enableRecovery bool) error {
	return GetErrorManager().HandleError(ctx, err, context, enableRecovery)
}

// HandleFatalError is a convenience function for handling fatal errors
func HandleFatalError(err error, context string) {
	GetErrorManager().HandleFatalError(err, context)
}

// HandleValidationError is a convenience function for validation errors
func HandleValidationError(err error, field string) error {
	return GetErrorManager().HandleValidationError(err, field)
}

// WrapError wraps an error with additional context
func WrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

// WrapErrorWithContext wraps an error with structured context
func WrapErrorWithContext(err error, code, message string, severity ErrorSeverity, category ErrorCategory) error {
	if err == nil {
		return nil
	}

	structuredErr := NewStructuredError(code, message, severity, category, err)
	return structuredErr
}

// RecoverableError creates a recoverable error
func RecoverableError(err error, message string) error {
	if err == nil {
		return nil
	}

	return NewStructuredError(
		"RECOVERABLE_ERROR",
		message,
		SeverityMedium,
		CategorySystem,
		err,
	)
}

// UnrecoverableError creates an unrecoverable error
func UnrecoverableError(err error, message string) error {
	if err == nil {
		return nil
	}

	return NewStructuredError(
		"UNRECOVERABLE_ERROR",
		message,
		SeverityCritical,
		CategorySystem,
		err,
	).MakeUnrecoverable()
}

// SafeExecute executes a function safely and handles any panics
func SafeExecute(fn func() error, recoveryMsg string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered: %v", r)
			if recoveryMsg != "" {
				err = WrapError(err, recoveryMsg)
			}
		}
	}()

	return fn()
}

// SafeGo starts a goroutine with panic recovery
func SafeGo(fn func(), recoveryMsg string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("goroutine panic recovered: %v", r)
				if recoveryMsg != "" {
					err = WrapError(err, recoveryMsg)
				}
				log.Printf("SafeGo recovery: %v", err)
			}
		}()

		fn()
	}()
}

// WithErrorHandling wraps a function with error handling
func WithErrorHandling(fn func() error, ctx string) error {
	return HandleError(context.Background(), fn(), ctx, true)
}

// LogAndContinue logs an error but continues execution
func LogAndContinue(err error, ctx string) {
	if err != nil {
		GetErrorManager().HandleError(context.Background(), err, ctx, false)
	}
}

// LogAndExit logs an error and exits (for use in main functions)
func LogAndExit(err error, ctx string) {
	if err != nil {
		GetErrorManager().HandleFatalError(err, ctx)
	}
}

// ValidateNotNil validates that a value is not nil
func ValidateNotNil(value interface{}, name string) error {
	if value == nil {
		return NewValidationError(name, "cannot be nil")
	}
	return nil
}

// ValidateNotEmpty validates that a string is not empty
func ValidateNotEmpty(value, name string) error {
	if value == "" {
		return NewValidationError(name, "cannot be empty")
	}
	return nil
}

// ValidatePositive validates that a number is positive
func ValidatePositive(value int, name string) error {
	if value <= 0 {
		return NewValidationError(name, "must be positive")
	}
	return nil
}

// ValidateRange validates that a number is within a range
func ValidateRange(value, min, max int, name string) error {
	if value < min || value > max {
		return NewValidationError(name, fmt.Sprintf("must be between %d and %d", min, max))
	}
	return nil
}

// ValidateFileExists validates that a file exists
func ValidateFileExists(path, name string) error {
	// This would use filesystem operations to check if file exists
	// For now, just validate the path is not empty
	if path == "" {
		return NewValidationError(name, "file path cannot be empty")
	}
	return nil
}

// RetryableError creates a retryable error
func RetryableError(err error, maxRetries int) error {
	if structuredErr, ok := err.(*StructuredError); ok {
		return structuredErr
	}

	return NewStructuredError(
		"RETRYABLE_ERROR",
		"Operation can be retried",
		SeverityMedium,
		CategoryExecution,
		err,
	).WithMetadata("max_retries", maxRetries)
}

// NetworkTimeoutError creates a network timeout error
func NetworkTimeoutError(operation string, timeout int) error {
	return NewStructuredError(
		"NETWORK_TIMEOUT",
		fmt.Sprintf("Network operation %s timed out after %d seconds", operation, timeout),
		SeverityMedium,
		CategoryNetwork,
		nil,
	).WithOperation(operation).WithMetadata("timeout_seconds", timeout)
}

// FileNotFoundError creates a file not found error
func FileNotFoundError(path string) error {
	return NewStructuredError(
		"FILE_NOT_FOUND",
		fmt.Sprintf("File not found: %s", path),
		SeverityLow,
		CategoryFileSystem,
		nil,
	).WithResource(path)
}

// PermissionDeniedError creates a permission denied error
func PermissionDeniedError(resource string) error {
	return NewStructuredError(
		"PERMISSION_DENIED",
		fmt.Sprintf("Permission denied for: %s", resource),
		SeverityMedium,
		CategorySystem,
		nil,
	).WithResource(resource)
}

// ResourceExhaustedError creates a resource exhausted error
func ResourceExhaustedError(resource string) error {
	return NewStructuredError(
		"RESOURCE_EXHAUSTED",
		fmt.Sprintf("Resource exhausted: %s", resource),
		SeverityHigh,
		CategorySystem,
		nil,
	).WithResource(resource)
}

// InvalidArgumentError creates an invalid argument error
func InvalidArgumentError(argument, reason string) error {
	return NewStructuredError(
		"INVALID_ARGUMENT",
		fmt.Sprintf("Invalid argument %s: %s", argument, reason),
		SeverityLow,
		CategoryValidation,
		nil,
	).WithResource(argument)
}

// DeadlineExceededError creates a deadline exceeded error
func DeadlineExceededError(operation string) error {
	return NewStructuredError(
		"DEADLINE_EXCEEDED",
		fmt.Sprintf("Deadline exceeded for operation: %s", operation),
		SeverityMedium,
		CategoryExecution,
		nil,
	).WithOperation(operation)
}

// ContextCancelledError creates a context cancelled error
func ContextCancelledError(operation string) error {
	return NewStructuredError(
		"CONTEXT_CANCELLED",
		fmt.Sprintf("Context cancelled for operation: %s", operation),
		SeverityLow,
		CategoryExecution,
		nil,
	).WithOperation(operation)
}
