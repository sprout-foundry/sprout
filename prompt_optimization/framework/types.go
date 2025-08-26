package framework

import (
	"time"

	"github.com/alantheprice/ledit/pkg/types"
)

// PromptType represents different categories of prompts used in the system
type PromptType string

const (
	PromptTypeCodeGeneration   PromptType = "code_generation"
	PromptTypeTextReplacement  PromptType = "text_replacement"
	PromptTypeCodeReview       PromptType = "code_review"
	PromptTypeAnalysis         PromptType = "analysis"
	PromptTypeOrchestration    PromptType = "orchestration"
	PromptTypeSummarization    PromptType = "summarization"
	PromptTypeWorkspaceContext PromptType = "workspace_context"
)

// TestCase represents a single test scenario for a prompt
type TestCase struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	PromptType  PromptType             `json:"prompt_type"`
	Inputs      map[string]interface{} `json:"inputs"`
	Expected    ExpectedOutput         `json:"expected"`
	Tags        []string               `json:"tags"`
	Priority    int                    `json:"priority"` // 1-5, 5 being highest
}

// ExpectedOutput defines what we expect from a prompt
type ExpectedOutput struct {
	// For validation
	Contains    []string `json:"contains,omitempty"`     // Must contain these strings
	NotContains []string `json:"not_contains,omitempty"` // Must not contain these
	Patterns    []string `json:"patterns,omitempty"`     // Must match these regex patterns
	Format      string   `json:"format,omitempty"`       // Expected format (json, code, markdown, etc.)

	// For code generation specifically
	ValidCode bool     `json:"valid_code,omitempty"` // Must be valid code
	Language  string   `json:"language,omitempty"`   // Programming language
	Functions []string `json:"functions,omitempty"`  // Expected function names

	// For structured output
	JSONSchema map[string]interface{} `json:"json_schema,omitempty"` // Expected JSON structure

	// Quality metrics
	MinLength int `json:"min_length,omitempty"` // Minimum response length
	MaxLength int `json:"max_length,omitempty"` // Maximum response length

	// Custom validation function name
	Validator string `json:"validator,omitempty"` // Name of custom validator
}

// PromptCandidate represents a prompt version to be tested
type PromptCandidate struct {
	ID          string     `json:"id"`
	Version     string     `json:"version"`
	PromptType  PromptType `json:"prompt_type"`
	Content     string     `json:"content"`
	Description string     `json:"description"`
	Author      string     `json:"author"`
	CreatedAt   time.Time  `json:"created_at"`
	Parent      string     `json:"parent,omitempty"` // ID of parent prompt (for iterations)
}

// TestResult represents the result of testing a prompt against a test case
type TestResult struct {
	TestCaseID        string             `json:"test_case_id"`
	PromptID          string             `json:"prompt_id"`
	Success           bool               `json:"success"`
	Response          string             `json:"response"`
	TokenUsage        *types.TokenUsage  `json:"token_usage,omitempty"`
	Cost              float64            `json:"cost"`
	Duration          time.Duration      `json:"duration"`
	ValidationResults []ValidationResult `json:"validation_results"`
	Model             string             `json:"model"`
	Timestamp         time.Time          `json:"timestamp"`
	Error             string             `json:"error,omitempty"`
}

// ValidationResult represents the result of a single validation check
type ValidationResult struct {
	Check    string  `json:"check"`
	Passed   bool    `json:"passed"`
	Expected string  `json:"expected,omitempty"`
	Actual   string  `json:"actual,omitempty"`
	Score    float64 `json:"score,omitempty"` // 0.0 - 1.0
	Message  string  `json:"message,omitempty"`
}

// OptimizationConfig defines parameters for prompt optimization
type OptimizationConfig struct {
	PromptType        PromptType `json:"prompt_type"`
	MaxIterations     int        `json:"max_iterations"`
	SuccessThreshold  float64    `json:"success_threshold"`  // 0.0 - 1.0
	CostThreshold     float64    `json:"cost_threshold"`     // Max cost per test
	Models            []string   `json:"models"`             // Models to test against
	ParallelTests     int        `json:"parallel_tests"`     // Concurrent test execution
	AutoGenerate      bool       `json:"auto_generate"`      // Auto-generate prompt variants
	OptimizationGoals []string   `json:"optimization_goals"` // accuracy, cost, speed, etc.
}

// OptimizationResult contains the results of an optimization run
type OptimizationResult struct {
	OriginalPrompt     *PromptCandidate `json:"original_prompt"`
	BestPrompt         *PromptCandidate `json:"best_prompt"`
	AllResults         []*TestResult    `json:"all_results"`
	Iterations         int              `json:"iterations"`
	TotalCost          float64          `json:"total_cost"`
	TotalDuration      time.Duration    `json:"total_duration"`
	SuccessRate        float64          `json:"success_rate"`
	CostReduction      float64          `json:"cost_reduction"`
	QualityImprovement float64          `json:"quality_improvement"`
	Summary            string           `json:"summary"`
	CreatedAt          time.Time        `json:"created_at"`
}

// PromptMetrics aggregates performance data for a prompt
type PromptMetrics struct {
	PromptID        string         `json:"prompt_id"`
	TotalTests      int            `json:"total_tests"`
	SuccessRate     float64        `json:"success_rate"`
	AverageCost     float64        `json:"average_cost"`
	AverageDuration time.Duration  `json:"average_duration"`
	QualityScore    float64        `json:"quality_score"`
	LastTested      time.Time      `json:"last_tested"`
	TestCoverge     map[string]int `json:"test_coverage"` // TestCase ID -> pass count
}
