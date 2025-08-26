package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
)

// PromptTester is the main testing engine for prompts
type PromptTester struct {
	config     *config.Config
	testCases  []*TestCase
	validators map[string]Validator
	resultsDir string
	mu         sync.RWMutex
}

// Validator interface for custom validation logic
type Validator interface {
	Validate(response string, expected ExpectedOutput) []ValidationResult
}

// NewPromptTester creates a new prompt testing instance
func NewPromptTester(cfg *config.Config) *PromptTester {
	return &PromptTester{
		config:     cfg,
		testCases:  make([]*TestCase, 0),
		validators: make(map[string]Validator),
		resultsDir: "results",
	}
}

// RegisterValidator registers a custom validator
func (pt *PromptTester) RegisterValidator(name string, validator Validator) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.validators[name] = validator
}

// AddTestCase adds a single test case to the tester
func (pt *PromptTester) AddTestCase(testCase *TestCase) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.testCases = append(pt.testCases, testCase)
}

// LoadTestCases loads test cases from a directory
func (pt *PromptTester) LoadTestCases(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && filepath.Ext(path) == ".json" {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read test case file %s: %w", path, err)
			}

			var testCase TestCase
			if err := json.Unmarshal(data, &testCase); err != nil {
				return fmt.Errorf("failed to parse test case %s: %w", path, err)
			}

			pt.testCases = append(pt.testCases, &testCase)
		}

		return nil
	})
}

// TestPrompt tests a single prompt against all relevant test cases
func (pt *PromptTester) TestPrompt(ctx context.Context, prompt *PromptCandidate, model string) ([]*TestResult, error) {
	var results []*TestResult
	var wg sync.WaitGroup
	resultsChan := make(chan *TestResult, len(pt.testCases))

	// Filter test cases for this prompt type
	relevantCases := pt.getRelevantTestCases(prompt.PromptType)

	// Execute tests concurrently
	semaphore := make(chan struct{}, 5) // Limit concurrent tests

	for _, testCase := range relevantCases {
		wg.Add(1)
		go func(tc *TestCase) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			result := pt.executeTest(ctx, prompt, tc, model)
			resultsChan <- result
		}(testCase)
	}

	// Wait for all tests to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	for result := range resultsChan {
		results = append(results, result)
	}

	// Save results
	if err := pt.saveResults(prompt.ID, results); err != nil {
		return results, fmt.Errorf("failed to save results: %w", err)
	}

	return results, nil
}

// executeTest runs a single test case against a prompt
func (pt *PromptTester) executeTest(ctx context.Context, prompt *PromptCandidate, testCase *TestCase, model string) *TestResult {
	start := time.Now()

	result := &TestResult{
		TestCaseID: testCase.ID,
		PromptID:   prompt.ID,
		Model:      model,
		Timestamp:  start,
	}

	// Build the actual prompt with inputs
	actualPrompt, err := pt.buildPrompt(prompt.Content, testCase.Inputs)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to build prompt: %v", err)
		return result
	}

	// Execute LLM call
	messages := []prompts.Message{
		{Role: "user", Content: actualPrompt},
	}

	response, tokenUsage, err := llm.GetLLMResponse(
		model,
		messages,
		"",
		pt.config,
		60*time.Second,
	)

	result.Duration = time.Since(start)
	result.TokenUsage = tokenUsage

	if err != nil {
		result.Error = fmt.Sprintf("LLM call failed: %v", err)
		return result
	}

	result.Response = response
	if tokenUsage != nil {
		result.Cost = llm.CalculateCost(llm.TokenUsage{
			PromptTokens:     tokenUsage.PromptTokens,
			CompletionTokens: tokenUsage.CompletionTokens,
			TotalTokens:      tokenUsage.TotalTokens,
		}, model)
	}

	// Validate the response
	result.ValidationResults = pt.validateResponse(response, testCase.Expected)

	// Determine overall success
	result.Success = pt.isTestSuccessful(result.ValidationResults)

	return result
}

// buildPrompt constructs the actual prompt by substituting input variables
func (pt *PromptTester) buildPrompt(template string, inputs map[string]interface{}) (string, error) {
	// Simple template substitution - can be enhanced with proper templating
	result := template
	for key, value := range inputs {
		placeholder := fmt.Sprintf("{{%s}}", key)
		valueStr := fmt.Sprintf("%v", value)
		result = fmt.Sprintf(strings.ReplaceAll(result, placeholder, valueStr))
	}
	return result, nil
}

// validateResponse validates the LLM response against expected criteria
func (pt *PromptTester) validateResponse(response string, expected ExpectedOutput) []ValidationResult {
	var results []ValidationResult

	// Basic string validations
	for _, contains := range expected.Contains {
		results = append(results, ValidationResult{
			Check:    fmt.Sprintf("contains_%s", contains),
			Passed:   strings.Contains(response, contains),
			Expected: contains,
			Actual:   fmt.Sprintf("Response contains: %t", strings.Contains(response, contains)),
		})
	}

	for _, notContains := range expected.NotContains {
		results = append(results, ValidationResult{
			Check:    fmt.Sprintf("not_contains_%s", notContains),
			Passed:   !strings.Contains(response, notContains),
			Expected: fmt.Sprintf("Should not contain: %s", notContains),
			Actual:   fmt.Sprintf("Response contains: %t", strings.Contains(response, notContains)),
		})
	}

	// Pattern validations
	for _, pattern := range expected.Patterns {
		matched, err := regexp.MatchString(pattern, response)
		if err != nil {
			results = append(results, ValidationResult{
				Check:    fmt.Sprintf("pattern_%s", pattern),
				Passed:   false,
				Expected: pattern,
				Actual:   fmt.Sprintf("Pattern error: %v", err),
			})
		} else {
			results = append(results, ValidationResult{
				Check:    fmt.Sprintf("pattern_%s", pattern),
				Passed:   matched,
				Expected: pattern,
				Actual:   fmt.Sprintf("Pattern matched: %t", matched),
			})
		}
	}

	// Length validations
	if expected.MinLength > 0 {
		results = append(results, ValidationResult{
			Check:    "min_length",
			Passed:   len(response) >= expected.MinLength,
			Expected: fmt.Sprintf("Min %d chars", expected.MinLength),
			Actual:   fmt.Sprintf("Actual %d chars", len(response)),
		})
	}

	if expected.MaxLength > 0 {
		results = append(results, ValidationResult{
			Check:    "max_length",
			Passed:   len(response) <= expected.MaxLength,
			Expected: fmt.Sprintf("Max %d chars", expected.MaxLength),
			Actual:   fmt.Sprintf("Actual %d chars", len(response)),
		})
	}

	// Format validation
	if expected.Format != "" {
		formatResult := pt.validateFormat(response, expected.Format)
		results = append(results, formatResult)
	}

	// Code validation
	if expected.ValidCode {
		codeResult := pt.validateCode(response, expected.Language)
		results = append(results, codeResult)
	}

	// Custom validator
	if expected.Validator != "" {
		if validator, exists := pt.validators[expected.Validator]; exists {
			customResults := validator.Validate(response, expected)
			results = append(results, customResults...)
		}
	}

	return results
}

// validateFormat validates response format (JSON, code, etc.)
func (pt *PromptTester) validateFormat(response, format string) ValidationResult {
	switch strings.ToLower(format) {
	case "json":
		var temp interface{}
		err := json.Unmarshal([]byte(response), &temp)
		return ValidationResult{
			Check:    "json_format",
			Passed:   err == nil,
			Expected: "Valid JSON",
			Actual:   fmt.Sprintf("JSON valid: %t", err == nil),
			Message:  fmt.Sprintf("JSON parse error: %v", err),
		}
	case "code":
		// Basic code validation - check for common code structures
		hasCodeStructures := strings.Contains(response, "{") && strings.Contains(response, "}")
		return ValidationResult{
			Check:    "code_format",
			Passed:   hasCodeStructures,
			Expected: "Code with braces",
			Actual:   fmt.Sprintf("Has code structures: %t", hasCodeStructures),
		}
	default:
		return ValidationResult{
			Check:   "format_unknown",
			Passed:  true,
			Message: fmt.Sprintf("Unknown format: %s", format),
		}
	}
}

// validateCode validates code syntax for specific languages
func (pt *PromptTester) validateCode(response, language string) ValidationResult {
	// This is a simplified validation - in practice, you might use language-specific parsers
	switch strings.ToLower(language) {
	case "go":
		hasPackage := strings.Contains(response, "package ")
		hasFunc := strings.Contains(response, "func ")
		passed := hasPackage || hasFunc
		return ValidationResult{
			Check:    "go_code_valid",
			Passed:   passed,
			Expected: "Valid Go code structure",
			Actual:   fmt.Sprintf("Has package: %t, Has func: %t", hasPackage, hasFunc),
		}
	default:
		return ValidationResult{
			Check:   "code_validation_skipped",
			Passed:  true,
			Message: fmt.Sprintf("No validator for language: %s", language),
		}
	}
}

// isTestSuccessful determines if a test passed based on validation results
func (pt *PromptTester) isTestSuccessful(validations []ValidationResult) bool {
	for _, validation := range validations {
		if !validation.Passed {
			return false
		}
	}
	return true
}

// getRelevantTestCases filters test cases by prompt type
func (pt *PromptTester) getRelevantTestCases(promptType PromptType) []*TestCase {
	var relevant []*TestCase
	for _, testCase := range pt.testCases {
		if testCase.PromptType == promptType {
			relevant = append(relevant, testCase)
		}
	}
	return relevant
}

// saveResults saves test results to disk
func (pt *PromptTester) saveResults(promptID string, results []*TestResult) error {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s.json", promptID, timestamp)
	path := filepath.Join(pt.resultsDir, filename)

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// CalculateMetrics calculates aggregated metrics for a prompt
func (pt *PromptTester) CalculateMetrics(results []*TestResult) PromptMetrics {
	if len(results) == 0 {
		return PromptMetrics{}
	}

	var totalCost float64
	var totalDuration time.Duration
	var successCount int
	var qualitySum float64

	testCoverage := make(map[string]int)

	for _, result := range results {
		totalCost += result.Cost
		totalDuration += result.Duration

		if result.Success {
			successCount++
		}

		// Calculate quality score from validation results
		if len(result.ValidationResults) > 0 {
			passedCount := 0
			for _, validation := range result.ValidationResults {
				if validation.Passed {
					passedCount++
				}
			}
			quality := float64(passedCount) / float64(len(result.ValidationResults))
			qualitySum += quality
		}

		testCoverage[result.TestCaseID]++
	}

	return PromptMetrics{
		PromptID:        results[0].PromptID,
		TotalTests:      len(results),
		SuccessRate:     float64(successCount) / float64(len(results)),
		AverageCost:     totalCost / float64(len(results)),
		AverageDuration: totalDuration / time.Duration(len(results)),
		QualityScore:    qualitySum / float64(len(results)),
		LastTested:      time.Now(),
		TestCoverge:     testCoverage,
	}
}
