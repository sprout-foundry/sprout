package utils

import (
	"context"
	"fmt"
	"sync"
)

// ErrorManager provides centralized error management and handling
type ErrorManager struct {
	mu              sync.RWMutex
	errorHandler    *ErrorHandler
	errorCounts     map[string]int64
	recentErrors    []error
	maxRecentErrors int
	observers       []ErrorObserver
}

// ErrorObserver can observe error events
type ErrorObserver interface {
	OnError(err error, context string)
	OnRecovery(success bool, err error, strategy string)
}

// ErrorStats contains error statistics
type ErrorStats struct {
	TotalErrors      int64
	ErrorsByCode     map[string]int64
	ErrorsByCategory map[ErrorCategory]int64
	RecoveryRate     float64
}

// NewErrorManager creates a new error manager
func NewErrorManager(logger *Logger) *ErrorManager {
	return &ErrorManager{
		errorHandler:    NewErrorHandler(logger),
		errorCounts:     make(map[string]int64),
		recentErrors:    make([]error, 0),
		maxRecentErrors: 100,
		observers:       make([]ErrorObserver, 0),
	}
}

// HandleError handles an error with optional recovery
func (em *ErrorManager) HandleError(ctx context.Context, err error, context string, enableRecovery bool) error {
	if err == nil {
		return nil
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	// Record error
	em.recordError(err)

	// Notify observers
	em.notifyError(err, context)

	// Handle with error handler
	handledErr := em.errorHandler.HandleError(ctx, err, context, enableRecovery)

	// Record recovery success/failure
	if enableRecovery && handledErr == nil {
		em.notifyRecovery(true, err, "unknown")
	} else if enableRecovery {
		em.notifyRecovery(false, err, "none")
	}

	return handledErr
}

// HandleFatalError handles a fatal error
func (em *ErrorManager) HandleFatalError(err error, context string) {
	em.mu.Lock()
	em.recordError(err)
	em.mu.Unlock()

	em.notifyError(err, context)
	em.errorHandler.HandleFatalError(err, context)
}

// HandleValidationError handles validation errors
func (em *ErrorManager) HandleValidationError(err error, field string) error {
	validationErr := NewValidationError(field, fmt.Sprintf("validation failed: %v", err))
	return em.HandleError(context.Background(), validationErr, fmt.Sprintf("validation:%s", field), false)
}

// HandleSystemError handles system errors with recovery
func (em *ErrorManager) HandleSystemError(ctx context.Context, err error, component string) error {
	return em.errorHandler.HandleSystemError(ctx, err, component)
}

// HandleNetworkError handles network errors with retry
func (em *ErrorManager) HandleNetworkError(ctx context.Context, err error, operation string) error {
	return em.errorHandler.HandleNetworkError(ctx, err, operation)
}

// HandleFileSystemError handles filesystem errors
func (em *ErrorManager) HandleFileSystemError(ctx context.Context, err error, operation, path string) error {
	return em.errorHandler.HandleFileSystemError(ctx, err, operation, path)
}

// AddObserver adds an error observer
func (em *ErrorManager) AddObserver(observer ErrorObserver) {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.observers = append(em.observers, observer)
}

// RemoveObserver removes an error observer
func (em *ErrorManager) RemoveObserver(observer ErrorObserver) {
	em.mu.Lock()
	defer em.mu.Unlock()

	for i, obs := range em.observers {
		if obs == observer {
			em.observers = append(em.observers[:i], em.observers[i+1:]...)
			break
		}
	}
}

// GetStats returns error statistics
func (em *ErrorManager) GetStats() ErrorStats {
	em.mu.RLock()
	defer em.mu.RUnlock()

	stats := ErrorStats{
		TotalErrors:      0,
		ErrorsByCode:     make(map[string]int64),
		ErrorsByCategory: make(map[ErrorCategory]int64),
		RecoveryRate:     0.0,
	}

	for code, count := range em.errorCounts {
		stats.ErrorsByCode[code] = count
		stats.TotalErrors += count
	}

	// Calculate recovery rate (this is a simplified implementation)
	// In a real system, you'd track successful recoveries
	if stats.TotalErrors > 0 {
		stats.RecoveryRate = float64(stats.TotalErrors/2) / float64(stats.TotalErrors) // Placeholder
	}

	return stats
}

// GetRecentErrors returns recent errors
func (em *ErrorManager) GetRecentErrors(limit int) []error {
	em.mu.RLock()
	defer em.mu.RUnlock()

	if limit <= 0 || limit > len(em.recentErrors) {
		limit = len(em.recentErrors)
	}

	result := make([]error, limit)
	copy(result, em.recentErrors[:limit])
	return result
}

// ClearStats clears error statistics
func (em *ErrorManager) ClearStats() {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.errorCounts = make(map[string]int64)
	em.recentErrors = make([]error, 0)
}

// AddRecoveryStrategy adds a custom recovery strategy
func (em *ErrorManager) AddRecoveryStrategy(strategy RecoveryStrategy) {
	em.errorHandler.AddRecoveryStrategy(strategy)
}

// GetErrorHandler returns the underlying error handler
func (em *ErrorManager) GetErrorHandler() *ErrorHandler {
	return em.errorHandler
}

// recordError records an error for statistics
func (em *ErrorManager) recordError(err error) {
	var code string

	if structuredErr, ok := err.(*StructuredError); ok {
		code = structuredErr.Code
	} else {
		code = "UNKNOWN"
	}

	em.errorCounts[code]++

	// Add to recent errors
	em.recentErrors = append(em.recentErrors, err)
	if len(em.recentErrors) > em.maxRecentErrors {
		em.recentErrors = em.recentErrors[1:]
	}
}

// notifyError notifies all observers of an error
func (em *ErrorManager) notifyError(err error, context string) {
	for _, observer := range em.observers {
		// In a real implementation, you'd want to do this asynchronously
		// to avoid blocking the error handling flow
		observer.OnError(err, context)
	}
}

// notifyRecovery notifies all observers of a recovery attempt
func (em *ErrorManager) notifyRecovery(success bool, err error, strategy string) {
	for _, observer := range em.observers {
		observer.OnRecovery(success, err, strategy)
	}
}

// LogErrorObserver is an observer that logs errors
type LogErrorObserver struct {
	logger *Logger
}

// NewLogErrorObserver creates a new logging error observer
func NewLogErrorObserver(logger *Logger) *LogErrorObserver {
	return &LogErrorObserver{logger: logger}
}

// OnError handles error notifications
func (o *LogErrorObserver) OnError(err error, context string) {
	if o.logger != nil {
		o.logger.Logf("Error [%s]: %v", context, err)
	}
}

// OnRecovery handles recovery notifications
func (o *LogErrorObserver) OnRecovery(success bool, err error, strategy string) {
	if o.logger != nil {
		if success {
			o.logger.Logf("Error recovery successful: %v (strategy: %s)", err, strategy)
		} else {
			o.logger.Logf("Error recovery failed: %v (strategy: %s)", err, strategy)
		}
	}
}

// MetricsErrorObserver is an observer that collects error metrics
type MetricsErrorObserver struct {
	metrics map[string]int64
}

// NewMetricsErrorObserver creates a new metrics error observer
func NewMetricsErrorObserver() *MetricsErrorObserver {
	return &MetricsErrorObserver{
		metrics: make(map[string]int64),
	}
}

// OnError handles error notifications
func (o *MetricsErrorObserver) OnError(err error, context string) {
	o.metrics["total_errors"]++
	if structuredErr, ok := err.(*StructuredError); ok {
		o.metrics[fmt.Sprintf("errors_by_code_%s", structuredErr.Code)]++
		o.metrics[fmt.Sprintf("errors_by_category_%d", structuredErr.Category)]++
	}
}

// OnRecovery handles recovery notifications
func (o *MetricsErrorObserver) OnRecovery(success bool, err error, strategy string) {
	if success {
		o.metrics["successful_recoveries"]++
	} else {
		o.metrics["failed_recoveries"]++
	}
}

// GetMetrics returns the collected metrics
func (o *MetricsErrorObserver) GetMetrics() map[string]int64 {
	result := make(map[string]int64)
	for k, v := range o.metrics {
		result[k] = v
	}
	return result
}
