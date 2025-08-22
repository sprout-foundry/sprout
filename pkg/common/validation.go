package common

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

// Validator provides common validation functionality
type Validator struct {
	config *config.Config
	logger *utils.Logger
}

// NewValidator creates a new validator
func NewValidator(cfg *config.Config, logger *utils.Logger) *Validator {
	return &Validator{
		config: cfg,
		logger: logger,
	}
}

// ValidationResult contains the result of a validation operation
type ValidationResult struct {
	Valid       bool
	Errors      []string
	Warnings    []string
	Suggestions []string
}

// ValidateFile validates a file
func (v *Validator) ValidateFile(filePath string, criteria *FileValidationCriteria) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		Errors:      []string{},
		Warnings:    []string{},
		Suggestions: []string{},
	}

	if criteria == nil {
		return result
	}

	// Check if file exists
	if criteria.MustExist {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("file does not exist: %s", filePath))
			return result
		}
	}

	// Check file size
	if criteria.MaxSize > 0 || criteria.MinSize > 0 {
		if info, err := os.Stat(filePath); err == nil {
			size := info.Size()

			if criteria.MaxSize > 0 && size > criteria.MaxSize {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("file too large: %d bytes (max: %d)", size, criteria.MaxSize))
			}

			if criteria.MinSize > 0 && size < criteria.MinSize {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("file too small: %d bytes (min: %d)", size, criteria.MinSize))
			}
		}
	}

	// Check file extension
	if len(criteria.AllowedExtensions) > 0 {
		ext := strings.ToLower(filepath.Ext(filePath))
		found := false
		for _, allowed := range criteria.AllowedExtensions {
			if ext == strings.ToLower(allowed) {
				found = true
				break
			}
		}
		if !found {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("invalid file extension: %s (allowed: %v)", ext, criteria.AllowedExtensions))
		}
	}

	// Check file name pattern
	if criteria.NamePattern != "" {
		if matched, _ := regexp.MatchString(criteria.NamePattern, filepath.Base(filePath)); !matched {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("file name does not match pattern: %s", criteria.NamePattern))
		}
	}

	// Check modification time
	if !criteria.ModifiedAfter.IsZero() || !criteria.ModifiedBefore.IsZero() {
		if info, err := os.Stat(filePath); err == nil {
			modTime := info.ModTime()

			if !criteria.ModifiedAfter.IsZero() && modTime.Before(criteria.ModifiedAfter) {
				result.Warnings = append(result.Warnings, fmt.Sprintf("file modified before required time: %v", criteria.ModifiedAfter))
			}

			if !criteria.ModifiedBefore.IsZero() && modTime.After(criteria.ModifiedBefore) {
				result.Warnings = append(result.Warnings, fmt.Sprintf("file modified after required time: %v", criteria.ModifiedBefore))
			}
		}
	}

	// Check content if required
	if criteria.CheckContent && len(criteria.ContentPatterns) > 0 {
		if content, err := os.ReadFile(filePath); err == nil {
			contentStr := string(content)

			for _, pattern := range criteria.ContentPatterns {
				if matched, _ := regexp.MatchString(pattern, contentStr); !matched {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf("content does not match required pattern: %s", pattern))
				}
			}
		}
	}

	return result
}

// FileValidationCriteria defines criteria for file validation
type FileValidationCriteria struct {
	MustExist         bool
	MinSize           int64
	MaxSize           int64
	AllowedExtensions []string
	NamePattern       string
	ModifiedAfter     time.Time
	ModifiedBefore    time.Time
	CheckContent      bool
	ContentPatterns   []string
}

// ValidateDirectory validates a directory
func (v *Validator) ValidateDirectory(dirPath string, criteria *DirectoryValidationCriteria) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		Errors:      []string{},
		Warnings:    []string{},
		Suggestions: []string{},
	}

	if criteria == nil {
		return result
	}

	// Check if directory exists
	if criteria.MustExist {
		if info, err := os.Stat(dirPath); os.IsNotExist(err) {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("directory does not exist: %s", dirPath))
			return result
		} else if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("cannot access directory: %v", err))
			return result
		} else if !info.IsDir() {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("path is not a directory: %s", dirPath))
			return result
		}
	}

	// Check file count constraints
	if criteria.MinFileCount > 0 || criteria.MaxFileCount > 0 {
		fileCount := v.countFilesInDirectory(dirPath)

		if criteria.MinFileCount > 0 && fileCount < criteria.MinFileCount {
			result.Warnings = append(result.Warnings, fmt.Sprintf("directory has too few files: %d (min: %d)", fileCount, criteria.MinFileCount))
		}

		if criteria.MaxFileCount > 0 && fileCount > criteria.MaxFileCount {
			result.Warnings = append(result.Warnings, fmt.Sprintf("directory has too many files: %d (max: %d)", fileCount, criteria.MaxFileCount))
		}
	}

	// Check for required files
	for _, requiredFile := range criteria.RequiredFiles {
		requiredPath := filepath.Join(dirPath, requiredFile)
		if _, err := os.Stat(requiredPath); os.IsNotExist(err) {
			if criteria.RequireAllFiles {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("required file missing: %s", requiredFile))
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf("recommended file missing: %s", requiredFile))
			}
		}
	}

	// Check for forbidden files
	for _, forbiddenFile := range criteria.ForbiddenFiles {
		forbiddenPath := filepath.Join(dirPath, forbiddenFile)
		if _, err := os.Stat(forbiddenPath); err == nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("potentially problematic file found: %s", forbiddenFile))
		}
	}

	return result
}

// DirectoryValidationCriteria defines criteria for directory validation
type DirectoryValidationCriteria struct {
	MustExist       bool
	MinFileCount    int
	MaxFileCount    int
	RequiredFiles   []string
	ForbiddenFiles  []string
	RequireAllFiles bool
}

// countFilesInDirectory counts files in a directory
func (v *Validator) countFilesInDirectory(dirPath string) int {
	count := 0

	if entries, err := os.ReadDir(dirPath); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				count++
			}
		}
	}

	return count
}

// ValidateString validates a string
func (v *Validator) ValidateString(value string, criteria *StringValidationCriteria) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		Errors:      []string{},
		Warnings:    []string{},
		Suggestions: []string{},
	}

	if criteria == nil {
		return result
	}

	// Check length constraints
	length := len(value)

	if criteria.MinLength > 0 && length < criteria.MinLength {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("string too short: %d chars (min: %d)", length, criteria.MinLength))
	}

	if criteria.MaxLength > 0 && length > criteria.MaxLength {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("string too long: %d chars (max: %d)", length, criteria.MaxLength))
	}

	// Check pattern
	if criteria.Pattern != "" {
		if matched, _ := regexp.MatchString(criteria.Pattern, value); !matched {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("string does not match pattern: %s", criteria.Pattern))
		}
	}

	// Check forbidden patterns
	for _, forbiddenPattern := range criteria.ForbiddenPatterns {
		if matched, _ := regexp.MatchString(forbiddenPattern, value); matched {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("string contains forbidden pattern: %s", forbiddenPattern))
		}
	}

	// Check required patterns
	for _, requiredPattern := range criteria.RequiredPatterns {
		if matched, _ := regexp.MatchString(requiredPattern, value); !matched {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("string does not contain required pattern: %s", requiredPattern))
		}
	}

	// Check for empty/whitespace-only strings
	if criteria.NoEmpty && strings.TrimSpace(value) == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "string cannot be empty or whitespace-only")
	}

	// Check for leading/trailing whitespace
	if criteria.NoWhitespace && (strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ")) {
		result.Warnings = append(result.Warnings, "string has leading or trailing whitespace")
		result.Suggestions = append(result.Suggestions, "consider trimming whitespace")
	}

	return result
}

// StringValidationCriteria defines criteria for string validation
type StringValidationCriteria struct {
	MinLength         int
	MaxLength         int
	Pattern           string
	ForbiddenPatterns []string
	RequiredPatterns  []string
	NoEmpty           bool
	NoWhitespace      bool
}

// ValidateNumber validates a number
func (v *Validator) ValidateNumber(value int64, criteria *NumberValidationCriteria) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		Errors:      []string{},
		Warnings:    []string{},
		Suggestions: []string{},
	}

	if criteria == nil {
		return result
	}

	// Check range constraints
	if criteria.MinValue != nil && value < *criteria.MinValue {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("value too small: %d (min: %d)", value, *criteria.MinValue))
	}

	if criteria.MaxValue != nil && value > *criteria.MaxValue {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("value too large: %d (max: %d)", value, *criteria.MaxValue))
	}

	// Check for zero
	if criteria.NoZero && value == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "value cannot be zero")
	}

	// Check for negative
	if criteria.NoNegative && value < 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "value cannot be negative")
	}

	return result
}

// NumberValidationCriteria defines criteria for number validation
type NumberValidationCriteria struct {
	MinValue   *int64
	MaxValue   *int64
	NoZero     bool
	NoNegative bool
}

// ValidateConfig validates configuration
func (v *Validator) ValidateConfig(cfg *config.Config) *ValidationResult {
	result := &ValidationResult{
		Valid:       true,
		Errors:      []string{},
		Warnings:    []string{},
		Suggestions: []string{},
	}

	if cfg == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "configuration is nil")
		return result
	}

	// Validate LLM configuration
	llmConfig := cfg.GetLLMConfig()
	if llmConfig != nil {
		if llmConfig.EditingModel == "" {
			result.Valid = false
			result.Errors = append(result.Errors, "editing model cannot be empty")
		}

		if llmConfig.Temperature < 0.0 || llmConfig.Temperature > 2.0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("temperature outside recommended range: %f", llmConfig.Temperature))
		}

		if llmConfig.MaxTokens < 1 || llmConfig.MaxTokens > 32768 {
			result.Errors = append(result.Errors, fmt.Sprintf("max tokens out of valid range: %d", llmConfig.MaxTokens))
		}
	}

	// Validate security configuration
	securityConfig := cfg.GetSecurityConfig()
	if securityConfig != nil {
		if securityConfig.EnableSecurityChecks && len(securityConfig.ShellAllowlist) == 0 {
			result.Warnings = append(result.Warnings, "security checks enabled but shell allowlist is empty")
		}
	}

	// Validate performance configuration
	perfConfig := cfg.GetPerformanceConfig()
	if perfConfig != nil {
		if perfConfig.MaxConcurrentRequests < 1 {
			result.Errors = append(result.Errors, "max concurrent requests must be at least 1")
		}

		if perfConfig.FileBatchSize < 1 {
			result.Errors = append(result.Errors, "file batch size must be at least 1")
		}
	}

	return result
}

// CombineResults combines multiple validation results
func (v *Validator) CombineResults(results ...*ValidationResult) *ValidationResult {
	combined := &ValidationResult{
		Valid:       true,
		Errors:      []string{},
		Warnings:    []string{},
		Suggestions: []string{},
	}

	for _, result := range results {
		if !result.Valid {
			combined.Valid = false
		}
		combined.Errors = append(combined.Errors, result.Errors...)
		combined.Warnings = append(combined.Warnings, result.Warnings...)
		combined.Suggestions = append(combined.Suggestions, result.Suggestions...)
	}

	return combined
}

// LogValidationResult logs a validation result
func (v *Validator) LogValidationResult(result *ValidationResult, context string) {
	if v.logger == nil {
		return
	}

	if !result.Valid {
		v.logger.Logf("VALIDATION FAILED [%s]: %v", context, result.Errors)
	}

	if len(result.Warnings) > 0 {
		v.logger.Logf("VALIDATION WARNINGS [%s]: %v", context, result.Warnings)
	}

	if len(result.Suggestions) > 0 {
		v.logger.Logf("VALIDATION SUGGESTIONS [%s]: %v", context, result.Suggestions)
	}
}

// IsValidResult checks if a validation result is valid
func (r *ValidationResult) IsValidResult() bool {
	return r.Valid
}

// HasErrors checks if there are any errors
func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// HasWarnings checks if there are any warnings
func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

// HasSuggestions checks if there are any suggestions
func (r *ValidationResult) HasSuggestions() bool {
	return len(r.Suggestions) > 0
}

// GetErrorCount returns the number of errors
func (r *ValidationResult) GetErrorCount() int {
	return len(r.Errors)
}

// GetWarningCount returns the number of warnings
func (r *ValidationResult) GetWarningCount() int {
	return len(r.Warnings)
}

// GetSuggestionCount returns the number of suggestions
func (r *ValidationResult) GetSuggestionCount() int {
	return len(r.Suggestions)
}
