package layered

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string      `json:"field"`
	Value   interface{} `json:"value"`
	Message string      `json:"message"`
	Code    string      `json:"code"`
}

// Error implements the error interface
func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s", e.Field, e.Message)
}

// ValidationResult contains the results of configuration validation
type ValidationResult struct {
	Valid    bool              `json:"valid"`
	Errors   []ValidationError `json:"errors"`
	Warnings []ValidationError `json:"warnings"`
}

// IsValid returns true if there are no validation errors
func (r *ValidationResult) IsValid() bool {
	return len(r.Errors) == 0
}

// HasWarnings returns true if there are validation warnings
func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// AddError adds a validation error
func (r *ValidationResult) AddError(field, message, code string, value interface{}) {
	r.Errors = append(r.Errors, ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
		Code:    code,
	})
	r.Valid = false
}

// AddWarning adds a validation warning
func (r *ValidationResult) AddWarning(field, message, code string, value interface{}) {
	r.Warnings = append(r.Warnings, ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
		Code:    code,
	})
}

// ConfigValidator validates configuration objects
type ConfigValidator struct {
	rules []ValidationRule
}

// ValidationRule defines a single validation rule
type ValidationRule interface {
	Validate(cfg *config.Config) []ValidationError
	GetName() string
	GetSeverity() ValidationSeverity
}

// ValidationSeverity represents the severity of a validation issue
type ValidationSeverity int

const (
	SeverityError   ValidationSeverity = iota // Must be fixed
	SeverityWarning                           // Should be reviewed
	SeverityInfo                              // Informational only
)

// NewConfigValidator creates a new configuration validator
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{
		rules: []ValidationRule{},
	}
}

// AddRule adds a validation rule
func (v *ConfigValidator) AddRule(rule ValidationRule) {
	v.rules = append(v.rules, rule)
}

// ValidateConfig validates a complete configuration
func (v *ConfigValidator) ValidateConfig(cfg *config.Config) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationError{},
	}

	// Apply all validation rules
	for _, rule := range v.rules {
		errors := rule.Validate(cfg)
		for _, err := range errors {
			if rule.GetSeverity() == SeverityError {
				result.Errors = append(result.Errors, err)
				result.Valid = false
			} else if rule.GetSeverity() == SeverityWarning {
				result.Warnings = append(result.Warnings, err)
			}
		}
	}

	return result
}

// Standard validation rules

// LLMModelValidationRule validates LLM model configurations
type LLMModelValidationRule struct{}

func (r *LLMModelValidationRule) GetName() string {
	return "llm_model_validation"
}

func (r *LLMModelValidationRule) GetSeverity() ValidationSeverity {
	return SeverityError
}

func (r *LLMModelValidationRule) Validate(cfg *config.Config) []ValidationError {
	var errors []ValidationError

	if cfg.LLM == nil {
		errors = append(errors, ValidationError{
			Field:   "llm",
			Value:   nil,
			Message: "LLM configuration is required",
			Code:    "MISSING_LLM_CONFIG",
		})
		return errors
	}

	// Validate editing model
	if cfg.LLM.EditingModel == "" {
		errors = append(errors, ValidationError{
			Field:   "llm.editing_model",
			Value:   "",
			Message: "Editing model is required",
			Code:    "MISSING_EDITING_MODEL",
		})
	}

	// Validate temperature range
	if cfg.LLM.Temperature < 0.0 || cfg.LLM.Temperature > 2.0 {
		errors = append(errors, ValidationError{
			Field:   "llm.temperature",
			Value:   cfg.LLM.Temperature,
			Message: "Temperature must be between 0.0 and 2.0",
			Code:    "INVALID_TEMPERATURE_RANGE",
		})
	}

	// Validate max tokens
	if cfg.LLM.MaxTokens < 1 || cfg.LLM.MaxTokens > 32768 {
		errors = append(errors, ValidationError{
			Field:   "llm.max_tokens",
			Value:   cfg.LLM.MaxTokens,
			Message: "Max tokens must be between 1 and 32768",
			Code:    "INVALID_MAX_TOKENS_RANGE",
		})
	}

	// Validate timeout
	if cfg.LLM.DefaultTimeoutSecs < 10 || cfg.LLM.DefaultTimeoutSecs > 600 {
		errors = append(errors, ValidationError{
			Field:   "llm.default_timeout_secs",
			Value:   cfg.LLM.DefaultTimeoutSecs,
			Message: "Timeout must be between 10 and 600 seconds",
			Code:    "INVALID_TIMEOUT_RANGE",
		})
	}

	return errors
}

// SecurityValidationRule validates security configurations
type SecurityValidationRule struct{}

func (r *SecurityValidationRule) GetName() string {
	return "security_validation"
}

func (r *SecurityValidationRule) GetSeverity() ValidationSeverity {
	return SeverityWarning
}

func (r *SecurityValidationRule) Validate(cfg *config.Config) []ValidationError {
	var errors []ValidationError

	if cfg.Security == nil {
		return errors // Security config is optional
	}

	// Warn if security checks are disabled
	if !cfg.Security.EnableSecurityChecks {
		errors = append(errors, ValidationError{
			Field:   "security.enable_security_checks",
			Value:   false,
			Message: "Security checks are disabled - this may pose security risks",
			Code:    "SECURITY_CHECKS_DISABLED",
		})
	}

	// Validate blocked commands
	for i, command := range cfg.Security.BlockedCommands {
		if strings.TrimSpace(command) == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("security.blocked_commands[%d]", i),
				Value:   command,
				Message: "Empty blocked command pattern",
				Code:    "EMPTY_BLOCKED_COMMAND",
			})
		}
	}

	return errors
}

// PerformanceValidationRule validates performance configurations
type PerformanceValidationRule struct{}

func (r *PerformanceValidationRule) GetName() string {
	return "performance_validation"
}

func (r *PerformanceValidationRule) GetSeverity() ValidationSeverity {
	return SeverityWarning
}

func (r *PerformanceValidationRule) Validate(cfg *config.Config) []ValidationError {
	var errors []ValidationError

	if cfg.Performance == nil {
		return errors // Performance config is optional
	}

	// Warn about potentially problematic settings
	if cfg.Performance.MaxConcurrentRequests > 50 {
		errors = append(errors, ValidationError{
			Field:   "performance.max_concurrent_requests",
			Value:   cfg.Performance.MaxConcurrentRequests,
			Message: "Very high concurrent request limit may overwhelm LLM providers",
			Code:    "HIGH_CONCURRENT_REQUESTS",
		})
	}

	if cfg.Performance.RequestDelayMs < 100 && cfg.Performance.MaxConcurrentRequests > 10 {
		errors = append(errors, ValidationError{
			Field:   "performance.request_delay_ms",
			Value:   cfg.Performance.RequestDelayMs,
			Message: "Low delay with high concurrency may trigger rate limiting",
			Code:    "LOW_DELAY_HIGH_CONCURRENCY",
		})
	}

	return errors
}

// ModelCompatibilityValidationRule validates model and provider compatibility
type ModelCompatibilityValidationRule struct{}

func (r *ModelCompatibilityValidationRule) GetName() string {
	return "model_compatibility_validation"
}

func (r *ModelCompatibilityValidationRule) GetSeverity() ValidationSeverity {
	return SeverityWarning
}

func (r *ModelCompatibilityValidationRule) Validate(cfg *config.Config) []ValidationError {
	var errors []ValidationError

	if cfg.LLM == nil {
		return errors
	}

	// Check for common model/provider mismatches
	modelProviderMap := map[string][]string{
		"gpt-3.5-turbo": {"openai"},
		"gpt-4":         {"openai"},
		"gemini-pro":    {"gemini", "google"},
		"claude-3":      {"anthropic"},
		"mixtral":       {"groq", "ollama"},
		"llama":         {"ollama", "groq"},
	}

	models := []string{
		cfg.LLM.EditingModel,
		cfg.LLM.SummaryModel,
		cfg.LLM.OrchestrationModel,
		cfg.LLM.WorkspaceModel,
	}

	for i, model := range models {
		if model == "" {
			continue
		}

		fieldNames := []string{
			"llm.editing_model",
			"llm.summary_model",
			"llm.orchestration_model",
			"llm.workspace_model",
		}

		// Check if model seems to be from a specific provider
		for modelPattern, validProviders := range modelProviderMap {
			if strings.Contains(strings.ToLower(model), strings.ToLower(modelPattern)) {
				// This looks like it should be used with specific providers
				if len(validProviders) > 0 {
					// For now, just add an info message
					errors = append(errors, ValidationError{
						Field:   fieldNames[i],
						Value:   model,
						Message: fmt.Sprintf("Model appears to be for providers: %s", strings.Join(validProviders, ", ")),
						Code:    "MODEL_PROVIDER_HINT",
					})
				}
			}
		}
	}

	return errors
}

// CreateStandardValidator creates a validator with standard rules
func CreateStandardValidator() *ConfigValidator {
	validator := NewConfigValidator()

	// Add standard validation rules
	validator.AddRule(&LLMModelValidationRule{})
	validator.AddRule(&SecurityValidationRule{})
	validator.AddRule(&PerformanceValidationRule{})
	validator.AddRule(&ModelCompatibilityValidationRule{})

	return validator
}

// ValidatedLayeredConfigProvider wraps LayeredConfigProvider with validation
type ValidatedLayeredConfigProvider struct {
	*LayeredConfigProvider
	validator *ConfigValidator
}

// NewValidatedLayeredConfigProvider creates a new validated layered config provider
func NewValidatedLayeredConfigProvider(provider *LayeredConfigProvider) *ValidatedLayeredConfigProvider {
	return &ValidatedLayeredConfigProvider{
		LayeredConfigProvider: provider,
		validator:             CreateStandardValidator(),
	}
}

// GetProviderConfig validates configuration before returning
func (v *ValidatedLayeredConfigProvider) GetProviderConfig(providerName string) (*types.ProviderConfig, error) {
	// Validate the merged configuration first
	merged := v.loader.GetMergedConfig()
	result := v.validator.ValidateConfig(merged)

	if !result.IsValid() {
		// Log validation errors but don't fail completely
		fmt.Printf("Configuration validation errors: %d errors, %d warnings\n",
			len(result.Errors), len(result.Warnings))
		for _, err := range result.Errors {
			fmt.Printf("  ERROR: %s\n", err.Error())
		}
		for _, warn := range result.Warnings {
			fmt.Printf("  WARNING: %s\n", warn.Error())
		}
	}

	return v.LayeredConfigProvider.GetProviderConfig(providerName)
}

// ValidateConfiguration validates the current configuration and returns results
func (v *ValidatedLayeredConfigProvider) ValidateConfiguration() *ValidationResult {
	merged := v.loader.GetMergedConfig()
	return v.validator.ValidateConfig(merged)
}
