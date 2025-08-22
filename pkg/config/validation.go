package config

import (
	"fmt"
	"strings"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config validation failed for field '%s': %s", e.Field, e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

// ValidationResult contains the result of a configuration validation
type ValidationResult struct {
	Errors   []ValidationError
	Warnings []string
}

// IsValid returns true if there are no errors
func (r *ValidationResult) IsValid() bool {
	return len(r.Errors) == 0
}

// HasWarnings returns true if there are warnings
func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// ErrorMessages returns all error messages as a slice
func (r *ValidationResult) ErrorMessages() []string {
	messages := make([]string, len(r.Errors))
	for i, err := range r.Errors {
		messages[i] = err.Error()
	}
	return messages
}

// CombinedError returns all errors as a single error
func (r *ValidationResult) CombinedError() error {
	if len(r.Errors) == 0 {
		return nil
	}

	messages := r.ErrorMessages()
	return fmt.Errorf("configuration validation failed:\n%s", strings.Join(messages, "\n"))
}

// ValidateAll validates all configuration domains
func (c *Config) ValidateAll() *ValidationResult {
	result := &ValidationResult{}

	// Validate LLM config
	if c.LLM != nil {
		if err := c.LLM.Validate(); err != nil {
			if vErr, ok := err.(*ValidationError); ok {
				result.Errors = append(result.Errors, *vErr)
			} else {
				result.Errors = append(result.Errors, ValidationError{
					Field:   "llm",
					Message: err.Error(),
				})
			}
		}
	}

	// Validate Agent config
	if c.Agent != nil {
		if err := c.Agent.Validate(); err != nil {
			if vErr, ok := err.(*ValidationError); ok {
				result.Errors = append(result.Errors, *vErr)
			} else {
				result.Errors = append(result.Errors, ValidationError{
					Field:   "agent",
					Message: err.Error(),
				})
			}
		}
	}

	// Validate Security config
	if c.Security != nil {
		if err := c.Security.Validate(); err != nil {
			if vErr, ok := err.(*ValidationError); ok {
				result.Errors = append(result.Errors, *vErr)
			} else {
				result.Errors = append(result.Errors, ValidationError{
					Field:   "security",
					Message: err.Error(),
				})
			}
		}
	}

	// Validate Performance config
	if c.Performance != nil {
		if err := c.Performance.Validate(); err != nil {
			if vErr, ok := err.(*ValidationError); ok {
				result.Errors = append(result.Errors, *vErr)
			} else {
				result.Errors = append(result.Errors, ValidationError{
					Field:   "performance",
					Message: err.Error(),
				})
			}
		}
	}

	// Add warnings for potentially problematic configurations
	if c.LLM != nil && c.LLM.Temperature > 1.5 {
		result.Warnings = append(result.Warnings, "High temperature (>1.5) may lead to unpredictable outputs")
	}

	if c.Performance != nil && c.Performance.MaxConcurrentRequests > 20 {
		result.Warnings = append(result.Warnings, "High concurrency (>20) may lead to rate limiting")
	}

	return result
}

// Validate method for AgentConfig
func (c *AgentConfig) Validate() error {
	if c.OrchestrationMaxAttempts < 0 {
		return NewValidationError("orchestration_max_attempts", "cannot be negative")
	}

	if c.OrchestrationMaxAttempts > 10 {
		return NewValidationError("orchestration_max_attempts", "cannot be greater than 10")
	}

	return nil
}

// Validate method for SecurityConfig
func (c *SecurityConfig) Validate() error {
	if c.MaxCommandLength < 10 {
		return NewValidationError("max_command_length", "cannot be less than 10")
	}

	if c.MaxCommandLength > 10000 {
		return NewValidationError("max_command_length", "cannot be greater than 10000")
	}

	return nil
}

// Validate method for PerformanceConfig
func (c *PerformanceConfig) Validate() error {
	if c.FileBatchSize < 1 {
		return NewValidationError("file_batch_size", "cannot be less than 1")
	}

	if c.EmbeddingBatchSize < 1 {
		return NewValidationError("embedding_batch_size", "cannot be less than 1")
	}

	if c.MaxConcurrentRequests < 1 {
		return NewValidationError("max_concurrent_requests", "cannot be less than 1")
	}

	if c.RequestDelayMs < 0 {
		return NewValidationError("request_delay_ms", "cannot be negative")
	}

	return nil
}
