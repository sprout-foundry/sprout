package utils

import (
	"context"
	"fmt"
	"time"
)

// RecoveryStrategy defines how to recover from different types of errors
type RecoveryStrategy interface {
	CanRecover(err error) bool
	Recover(ctx context.Context, err error) error
	GetName() string
}

// RetryStrategy implements retry-based recovery
type RetryStrategy struct {
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
	multiplier float64
}

// NewRetryStrategy creates a new retry strategy
func NewRetryStrategy(maxRetries int, baseDelay time.Duration) *RetryStrategy {
	return &RetryStrategy{
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
		maxDelay:   time.Minute,
		multiplier: 2.0,
	}
}

// CanRecover checks if the error can be recovered with retry
func (r *RetryStrategy) CanRecover(err error) bool {
	if structuredErr, ok := err.(*StructuredError); ok {
		// Don't retry critical or unrecoverable errors
		if structuredErr.Severity >= SeverityCritical || !structuredErr.Recoverable {
			return false
		}

		// Retry network and system errors
		switch structuredErr.Category {
		case CategoryNetwork, CategorySystem:
			return true
		case CategoryExecution:
			// Only retry execution errors if they're not user-related
			return structuredErr.Severity < SeverityHigh
		}
	}

	return false
}

// Recover attempts to recover by retrying the operation
func (r *RetryStrategy) Recover(ctx context.Context, err error) error {
	// This is a simplified implementation
	// In a real system, you'd have the actual operation to retry
	return fmt.Errorf("retry recovery not implemented for: %w", err)
}

// GetName returns the strategy name
func (r *RetryStrategy) GetName() string {
	return "retry"
}

// FallbackStrategy implements fallback-based recovery
type FallbackStrategy struct {
	fallbackValues map[string]interface{}
}

// NewFallbackStrategy creates a new fallback strategy
func NewFallbackStrategy(fallbacks map[string]interface{}) *FallbackStrategy {
	return &FallbackStrategy{
		fallbackValues: fallbacks,
	}
}

// CanRecover checks if fallback is available for the error
func (f *FallbackStrategy) CanRecover(err error) bool {
	if structuredErr, ok := err.(*StructuredError); ok {
		if structuredErr.Context != nil && structuredErr.Context.Resource != "" {
			_, exists := f.fallbackValues[structuredErr.Context.Resource]
			return exists
		}
	}
	return false
}

// Recover attempts to recover using fallback values
func (f *FallbackStrategy) Recover(ctx context.Context, err error) error {
	if structuredErr, ok := err.(*StructuredError); ok {
		if structuredErr.Context != nil && structuredErr.Context.Resource != "" {
			if fallback, exists := f.fallbackValues[structuredErr.Context.Resource]; exists {
				return fmt.Errorf("fallback recovery attempted with value: %v", fallback)
			}
		}
	}
	return fmt.Errorf("no fallback available for: %w", err)
}

// GetName returns the strategy name
func (f *FallbackStrategy) GetName() string {
	return "fallback"
}

// CircuitBreakerStrategy implements circuit breaker pattern
type CircuitBreakerStrategy struct {
	failureThreshold int
	resetTimeout     time.Duration
	failures         int
	lastFailureTime  time.Time
	state            string // "closed", "open", "half-open"
}

// NewCircuitBreakerStrategy creates a new circuit breaker strategy
func NewCircuitBreakerStrategy(failureThreshold int, resetTimeout time.Duration) *CircuitBreakerStrategy {
	return &CircuitBreakerStrategy{
		failureThreshold: failureThreshold,
		resetTimeout:     resetTimeout,
		state:            "closed",
	}
}

// CanRecover checks if the circuit breaker allows recovery
func (c *CircuitBreakerStrategy) CanRecover(err error) bool {
	now := time.Now()

	switch c.state {
	case "closed":
		return true
	case "open":
		if now.Sub(c.lastFailureTime) > c.resetTimeout {
			c.state = "half-open"
			return true
		}
		return false
	case "half-open":
		return true
	default:
		return false
	}
}

// Recover attempts to recover using circuit breaker logic
func (c *CircuitBreakerStrategy) Recover(ctx context.Context, err error) error {
	c.failures++
	c.lastFailureTime = time.Now()

	if c.failures >= c.failureThreshold {
		c.state = "open"
		return fmt.Errorf("circuit breaker opened after %d failures", c.failures)
	}

	return fmt.Errorf("circuit breaker recovery not implemented for: %w", err)
}

// GetName returns the strategy name
func (c *CircuitBreakerStrategy) GetName() string {
	return "circuit_breaker"
}

// RecoveryManager manages error recovery strategies
type RecoveryManager struct {
	strategies []RecoveryStrategy
	logger     *Logger
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(logger *Logger) *RecoveryManager {
	return &RecoveryManager{
		logger: logger,
	}
}

// AddStrategy adds a recovery strategy
func (r *RecoveryManager) AddStrategy(strategy RecoveryStrategy) {
	r.strategies = append(r.strategies, strategy)
}

// Recover attempts to recover from an error using available strategies
func (r *RecoveryManager) Recover(ctx context.Context, err error) error {
	for _, strategy := range r.strategies {
		if strategy.CanRecover(err) {
			if r.logger != nil {
				r.logger.Logf("Attempting recovery with strategy: %s", strategy.GetName())
			}

			if recoveryErr := strategy.Recover(ctx, err); recoveryErr == nil {
				if r.logger != nil {
					r.logger.Logf("Recovery successful with strategy: %s", strategy.GetName())
				}
				return nil
			} else if r.logger != nil {
				r.logger.Logf("Recovery failed with strategy %s: %v", strategy.GetName(), recoveryErr)
			}
		}
	}

	return fmt.Errorf("no recovery strategy succeeded for: %w", err)
}

// CanRecover checks if any strategy can recover from the error
func (r *RecoveryManager) CanRecover(err error) bool {
	for _, strategy := range r.strategies {
		if strategy.CanRecover(err) {
			return true
		}
	}
	return false
}

// GetApplicableStrategies returns strategies that can handle the error
func (r *RecoveryManager) GetApplicableStrategies(err error) []RecoveryStrategy {
	var applicable []RecoveryStrategy
	for _, strategy := range r.strategies {
		if strategy.CanRecover(err) {
			applicable = append(applicable, strategy)
		}
	}
	return applicable
}

// ErrorHandler provides high-level error handling with recovery
type ErrorHandler struct {
	recoveryManager *RecoveryManager
	logger          *Logger
}

// NewErrorHandler creates a new error handler with recovery capabilities
func NewErrorHandler(logger *Logger) *ErrorHandler {
	recoveryManager := NewRecoveryManager(logger)

	// Add default strategies
	recoveryManager.AddStrategy(NewRetryStrategy(3, time.Second))
	recoveryManager.AddStrategy(NewCircuitBreakerStrategy(5, time.Minute))

	return &ErrorHandler{
		recoveryManager: recoveryManager,
		logger:          logger,
	}
}

// HandleError handles an error with optional recovery
func (e *ErrorHandler) HandleError(ctx context.Context, err error, context string, enableRecovery bool) error {
	if err == nil {
		return nil
	}

	// Log the error
	if e.logger != nil {
		e.logger.Logf("Error [%s]: %v", context, err)
	}

	// Attempt recovery if enabled
	if enableRecovery && e.recoveryManager.CanRecover(err) {
		if recoveryErr := e.recoveryManager.Recover(ctx, err); recoveryErr == nil {
			if e.logger != nil {
				e.logger.Logf("Error recovered successfully: %v", err)
			}
			return nil
		} else if e.logger != nil {
			e.logger.Logf("Error recovery failed: %v", recoveryErr)
		}
	}

	// Return the original error if recovery failed or wasn't attempted
	return err
}

// HandleFatalError handles a fatal error that should terminate the application
func (e *ErrorHandler) HandleFatalError(err error, context string) {
	if e.logger != nil {
		e.logger.Logf("FATAL ERROR [%s]: %v", context, err)
	}

	// In a real application, you might want to:
	// - Send error to monitoring system
	// - Attempt graceful shutdown
	// - Log stack traces
	// - Exit with appropriate code

	// For now, just re-panic with the error
	panic(err)
}

// HandleValidationError handles validation errors specifically
func (e *ErrorHandler) HandleValidationError(err error, field string) error {
	if e.logger != nil {
		e.logger.Logf("Validation error for field '%s': %v", field, err)
	}

	// Validation errors are typically user-facing and don't need recovery
	return err
}

// HandleSystemError handles system-level errors with recovery attempts
func (e *ErrorHandler) HandleSystemError(ctx context.Context, err error, component string) error {
	systemErr := NewSystemError(component, err)
	return e.HandleError(ctx, systemErr, fmt.Sprintf("system error in %s", component), true)
}

// HandleNetworkError handles network errors with retry logic
func (e *ErrorHandler) HandleNetworkError(ctx context.Context, err error, operation string) error {
	networkErr := NewNetworkError(operation, err)
	return e.HandleError(ctx, networkErr, fmt.Sprintf("network operation %s", operation), true)
}

// HandleFileSystemError handles filesystem errors
func (e *ErrorHandler) HandleFileSystemError(ctx context.Context, err error, operation, path string) error {
	fsErr := NewFileSystemError(operation, path, err)
	return e.HandleError(ctx, fsErr, fmt.Sprintf("filesystem operation %s on %s", operation, path), true)
}

// AddRecoveryStrategy adds a custom recovery strategy
func (e *ErrorHandler) AddRecoveryStrategy(strategy RecoveryStrategy) {
	e.recoveryManager.AddStrategy(strategy)
}

// GetRecoveryManager returns the recovery manager
func (e *ErrorHandler) GetRecoveryManager() *RecoveryManager {
	return e.recoveryManager
}
