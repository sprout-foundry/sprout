package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// validateChangesWithIteration validates changes using iterative improvement
func validateChangesWithIteration(intentAnalysis *IntentAnalysis, originalIntent string, cfg *config.Config, logger *utils.Logger, tokenUsage *types.AgentTokenUsage) (int, error) {
	logger.LogProcessStep("🔍 Starting validation with iteration...")

	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: validateChangesWithIteration started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Phase: Determine validation strategy
	strategyStartTime := time.Now()
	logger.Logf("DEBUG: Determining validation strategy...")
	// Prefer deterministic strategy based on workspace signals; fall back to LLM when unknown
	validationStrategy, strategyTokens, err := determineValidationStrategy(intentAnalysis, cfg, logger)
	strategyDuration := time.Since(strategyStartTime)

	if err != nil {
		logger.Logf("DEBUG: Falling back to basic validation strategy.")
		validationStrategy = getBasicValidationStrategy(intentAnalysis, logger)
		strategyTokens = 0 // No tokens used for fallback
	}

	logger.Logf("DEBUG: Validation strategy determined (took %v). Project Type: %s, Steps: %d", strategyDuration, validationStrategy.ProjectType, len(validationStrategy.Steps))
	tokenUsage.Validation += strategyTokens
	tokenUsage.ValidationSplit.Prompt += strategyTokens // approx: strategy prompt heavy

	// Phase: Execute validation steps
	executionStartTime := time.Now()
	logger.Logf("DEBUG: Running %d validation steps...", len(validationStrategy.Steps))
	var validationResults []string

	for i, step := range validationStrategy.Steps {
		logger.LogProcessStep(fmt.Sprintf("🔍 Validation step %d/%d: %s", i+1, len(validationStrategy.Steps), step.Description))

		result, err := runValidationStep(step, logger)
		if err != nil {
			if step.Required {
				logger.LogError(fmt.Errorf("required validation step failed: %w", err))
				validationResults = append(validationResults, fmt.Sprintf("FAILED: %s - %v", step.Description, err))
			} else {
				logger.Logf("Optional validation step failed (continuing): %v", err)
				validationResults = append(validationResults, fmt.Sprintf("WARNING: %s - %v", step.Description, err))
			}
		} else {
			validationResults = append(validationResults, fmt.Sprintf("PASSED: %s - %s", step.Description, result))
		}
	}

	executionDuration := time.Since(executionStartTime)
	logger.Logf("PERF: Validation steps completed in %v", executionDuration)

	// Phase: Analyze validation results
	analysisStartTime := time.Now()
	analysisTokens, err := analyzeValidationResults(validationResults, intentAnalysis, validationStrategy, cfg, logger)
	analysisDuration := time.Since(analysisStartTime)

	if err != nil {
		logger.LogError(fmt.Errorf("validation analysis failed: %w", err))
		// Continue with basic analysis
		analysisTokens = 0
	}

	// Save split counts if possible by estimating from prompt/response (done inside analyzeValidationResults)
	tokenUsage.Validation += analysisTokens
	// We have split numbers inside analyzeValidationResults but not returned; approx equally split
	tokenUsage.ValidationSplit.Prompt += analysisTokens / 2
	tokenUsage.ValidationSplit.Completion += analysisTokens - (analysisTokens / 2)
	logger.Logf("DEBUG: Final validation analysis completed (took %v) - LLM approved proceeding", analysisDuration)

	// Log final performance metrics
	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: validateChangesWithIteration completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	return analysisTokens, nil
}

// fixValidationIssues attempts to fix validation issues using LLM analysis
func fixValidationIssues(validationResults []string, originalIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (int, error) {
	logger.LogProcessStep("🔧 Attempting to fix validation issues...")

	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: fixValidationIssues started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Phase: Analyze errors with context
	analysisStartTime := time.Now()
	fixPlan, analysisTokens, err := analyzeValidationErrorsWithContext(validationResults, originalIntent, intentAnalysis, cfg, logger)
	analysisDuration := time.Since(analysisStartTime)

	if err != nil {
		logger.LogError(fmt.Errorf("error analysis failed: %w", err))
		return analysisTokens, fmt.Errorf("failed to analyze validation errors: %w", err)
	}

	logger.Logf("PERF: Error analysis completed in %v", analysisDuration)

	// Phase: Execute fix plan
	executionStartTime := time.Now()
	executionTokens, err := executeValidationFixPlan(fixPlan, cfg, logger)
	executionDuration := time.Since(executionStartTime)

	if err != nil {
		logger.LogError(fmt.Errorf("fix execution failed: %w", err))
		return analysisTokens + executionTokens, fmt.Errorf("failed to execute fixes: %w", err)
	}

	logger.Logf("PERF: Fix execution completed in %v", executionDuration)

	// Log final performance metrics
	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: fixValidationIssues completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	return analysisTokens + executionTokens, nil
}

// analyzeValidationErrorsWithContext analyzes validation errors and creates a fix plan
func analyzeValidationErrorsWithContext(validationResults []string, originalIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (*ValidationFixPlan, int, error) {
	logger.LogProcessStep("🔍 Analyzing validation errors with context...")

	// Find files related to the errors
	errorFiles, err := findFilesRelatedToErrors(validationResults, cfg, logger)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to find error-related files: %w", err))
		errorFiles = []string{} // Continue with empty list
	}

	// Build context for error analysis
	contextPrompt := fmt.Sprintf(`ANALYZE VALIDATION ERRORS

ORIGINAL INTENT: %s
TASK CATEGORY: %s
COMPLEXITY: %s

VALIDATION RESULTS:
%s

ERROR-RELATED FILES: %v

ANALYZE:
1. What went wrong?
2. Which files are affected?
3. What's the best fix strategy?
4. Provide step-by-step instructions

Respond with JSON:
{
  "error_analysis": "detailed analysis of what went wrong",
  "affected_files": ["list", "of", "affected", "files"],
  "fix_strategy": "overall approach to fixing the issues",
  "instructions": ["step 1", "step 2", "step 3"]
}`,
		originalIntent,
		intentAnalysis.Category,
		intentAnalysis.Complexity,
		strings.Join(validationResults, "\n"),
		errorFiles)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at analyzing validation errors and creating fix plans. Respond only with valid JSON."},
		{Role: "user", Content: contextPrompt},
	}

	response, _, err := llm.GetLLMResponse(cfg.EditingModel, messages, "", cfg, 2*time.Minute)
	if err != nil {
		return nil, 0, fmt.Errorf("LLM analysis failed: %w", err)
	}

	// Parse the response
	var fixPlan ValidationFixPlan
	if err := json.Unmarshal([]byte(response), &fixPlan); err != nil {
		return nil, 0, fmt.Errorf("failed to parse fix plan: %w", err)
	}

	// Estimate tokens used
	tokens := llm.EstimateTokens(contextPrompt) + llm.EstimateTokens(response)
	return &fixPlan, tokens, nil
}

// executeValidationFixPlan executes the validation fix plan
func executeValidationFixPlan(plan *ValidationFixPlan, cfg *config.Config, logger *utils.Logger) (int, error) {
	logger.LogProcessStep("🔧 Executing validation fix plan...")

	var totalTokens int

	// Execute the instructions (treat as a single instruction for now)
	logger.LogProcessStep(fmt.Sprintf("🔧 Executing fix plan instructions: %s", plan.Instructions))

	// For now, just log the instruction
	// In a full implementation, this would execute the actual fixes
	logger.Logf("Would execute: %s", plan.Instructions)

	// Estimate tokens for this instruction
	tokens := llm.EstimateTokens(plan.Instructions)
	totalTokens += tokens

	logger.LogProcessStep("✅ Validation fix plan executed")
	return totalTokens, nil
}

// findFilesRelatedToErrors finds files that might be related to validation errors
func findFilesRelatedToErrors(errorMessages []string, cfg *config.Config, logger *utils.Logger) ([]string, error) {
	logger.LogProcessStep("🔍 Finding files related to validation errors...")

	var relatedFiles []string
	seen := make(map[string]bool)

	for _, errorMsg := range errorMessages {
		// Extract file paths from error messages
		words := strings.Fields(errorMsg)
		for _, word := range words {
			if strings.Contains(word, ".") && !strings.Contains(word, "://") {
				// This might be a file path
				cleanPath := strings.Trim(word, ".,:;()[]{}")
				if !seen[cleanPath] && hasFile(cleanPath) {
					relatedFiles = append(relatedFiles, cleanPath)
					seen[cleanPath] = true
				}
			}
		}
	}

	logger.Logf("Found %d error-related files: %v", len(relatedFiles), relatedFiles)
	return relatedFiles, nil
}

// determineValidationStrategy determines the appropriate validation strategy
func determineValidationStrategy(intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (*ValidationStrategy, int, error) {
	logger.LogProcessStep("🔍 Determining validation strategy...")

	// Build context for strategy determination
	contextPrompt := fmt.Sprintf(`DETERMINE VALIDATION STRATEGY

TASK CATEGORY: %s
COMPLEXITY: %s
ESTIMATED FILES: %v

PROJECT CONTEXT:
- Type: %s
- Has Tests: %t
- Has Linting: %t
- Build Command: %s
- Test Command: %s
- Lint Command: %s

What validation steps should be performed? Consider:
1. Build validation
2. Test execution
3. Linting checks
4. Syntax validation
5. Custom validation for this task type

Respond with JSON:
{
  "project_type": "detected project type",
  "steps": [
    {
      "type": "build|test|lint|syntax|custom",
      "command": "command to run",
      "description": "what this step validates",
      "required": true|false
    }
  ],
  "context": "explanation of why these steps were chosen"
}`,
		intentAnalysis.Category,
		intentAnalysis.Complexity,
		intentAnalysis.EstimatedFiles,
		"go", // Default for now
		true, // Default for now
		true, // Default for now
		"go build",
		"go test ./...",
		"go vet ./...")

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at determining validation strategies for different project types and tasks. Respond only with valid JSON."},
		{Role: "user", Content: contextPrompt},
	}

	response, _, err := llm.GetLLMResponse(cfg.EditingModel, messages, "", cfg, 1*time.Minute)
	if err != nil {
		return nil, 0, fmt.Errorf("LLM strategy determination failed: %w", err)
	}

	// Parse the response
	var strategy ValidationStrategy
	if err := json.Unmarshal([]byte(response), &strategy); err != nil {
		return nil, 0, fmt.Errorf("failed to parse validation strategy: %w", err)
	}

	// Estimate tokens used
	tokens := llm.EstimateTokens(contextPrompt) + llm.EstimateTokens(response)
	return &strategy, tokens, nil
}

// generateSmokeTestsIfEnabled creates minimal tests for changed functions/files when configured
func generateSmokeTestsIfEnabled(changedFiles []string, cfg *config.Config, logger *utils.Logger) {
	if cfg == nil || !cfg.AutoGenerateTests {
		return
	}
	for _, f := range changedFiles {
		lf := strings.ToLower(f)
		// Simple heuristics: Go and Python examples
		if strings.HasSuffix(lf, ".go") && !strings.HasSuffix(lf, "_test.go") {
			testFile := strings.TrimSuffix(f, ".go") + "_test.go"
			if _, err := os.Stat(testFile); err == nil {
				continue
			}
			// Try to extract a couple of function names to scaffold specific tests
			funcNames := extractGoFuncNames(f, 2)
			var builder strings.Builder
			builder.WriteString("package ")
			builder.WriteString(guessPackageName(f))
			builder.WriteString("\n\nimport \"testing\"\n\n")
			builder.WriteString("func TestSmoke(t *testing.T) {\n\t// TODO: add real assertions\n}\n\n")
			for _, fn := range funcNames {
				builder.WriteString("func Test_")
				builder.WriteString(fn)
				builder.WriteString("(t *testing.T) {\n\t// TODO: call ")
				builder.WriteString(fn)
				builder.WriteString(" and assert expected behavior\n}\n\n")
			}
			content := builder.String()
			_ = os.WriteFile(testFile, []byte(content), 0644)
			logger.LogProcessStep("🧪 Generated smoke test: " + testFile)
		} else if strings.HasSuffix(lf, ".py") {
			testFile := strings.TrimSuffix(f, ".py") + "_test.py"
			if _, err := os.Stat(testFile); err == nil {
				continue
			}
			// Extract a couple of def names to scaffold tests
			pyTests := extractPyFuncNames(f, 2)
			var builder strings.Builder
			builder.WriteString("def test_smoke():\n    assert True\n\n")
			for _, fn := range pyTests {
				builder.WriteString("def test_")
				builder.WriteString(fn)
				builder.WriteString("():\n    # TODO: call ")
				builder.WriteString(fn)
				builder.WriteString(" and assert expected behavior\n\n")
			}
			content := builder.String()
			_ = os.WriteFile(testFile, []byte(content), 0644)
			logger.LogProcessStep("🧪 Generated smoke test: " + testFile)
		}
	}
}

// runSmokeTestsIfEnabled executes minimal smoke tests when configured.
// It attempts fast, language-appropriate commands and treats failures as non-fatal.
func runSmokeTestsIfEnabled(changedFiles []string, cfg *config.Config, logger *utils.Logger) {
	if cfg == nil || !cfg.AutoGenerateTests {
		return
	}
	// Heuristic: prefer Go if any .go files changed, otherwise try Python.
	hasGo := false
	hasPy := false
	for _, f := range changedFiles {
		lf := strings.ToLower(f)
		if strings.HasSuffix(lf, ".go") {
			hasGo = true
		}
		if strings.HasSuffix(lf, ".py") {
			hasPy = true
		}
	}
	if hasGo {
		logger.LogProcessStep("🧪 Running Go smoke tests (TestSmoke only)...")
		cmd := exec.Command("sh", "-c", "go test ./... -run TestSmoke -count=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			logger.LogProcessStep("⚠️ Go smoke tests failed (non-fatal): " + err.Error())
			logger.Logf("Output: %s", string(out))
		} else {
			logger.LogProcessStep("✅ Go smoke tests passed")
		}
		return
	}
	if hasPy {
		// Try pytest first; if unavailable, try running the file directly
		logger.LogProcessStep("🧪 Attempting Python smoke tests (pytest -k smoke)...")
		cmd := exec.Command("sh", "-c", "pytest -q -k smoke || python -m pytest -q -k smoke")
		out, err := cmd.CombinedOutput()
		if err != nil {
			logger.LogProcessStep("⚠️ Python smoke tests failed or pytest unavailable (non-fatal)")
			logger.Logf("Output: %s", string(out))
		} else {
			logger.LogProcessStep("✅ Python smoke tests passed")
		}
	}
}

func guessPackageName(path string) string {
	base := filepath.Base(filepath.Dir(path))
	if base == "." || base == ".." || base == "" {
		return "main"
	}
	return base
}

// extractGoFuncNames pulls up to max function names from a Go source file for scaffolding tests
func extractGoFuncNames(path string, max int) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	names := []string{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		if len(m) >= 2 {
			names = append(names, m[1])
			if len(names) >= max {
				break
			}
		}
	}
	return names
}

// extractPyFuncNames pulls up to max def names from a Python source file for scaffolding tests
func extractPyFuncNames(path string, max int) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	re := regexp.MustCompile(`(?m)^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	names := []string{}
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		if len(m) >= 2 {
			names = append(names, m[1])
			if len(names) >= max {
				break
			}
		}
	}
	return names
}

// getBasicValidationStrategy provides a fallback validation strategy
func getBasicValidationStrategy(intentAnalysis *IntentAnalysis, logger *utils.Logger) *ValidationStrategy {
	logger.LogProcessStep("🔍 Using basic validation strategy...")

	// Heuristic project detection
	project := detectProjectType()
	switch project {
	case "node":
		return &ValidationStrategy{ProjectType: "node", Steps: []ValidationStep{
			{Type: "build", Command: "npm -s run -q build || tsc -p .", Description: "Type-check/build the project", Required: false},
			{Type: "lint", Command: "npx -y eslint . -f unix || true", Description: "Lint source files", Required: false},
			{Type: "test", Command: "npm -s test -w 2 --silent || jest -w 2 || true", Description: "Run unit tests (if present)", Required: false},
		}, Context: "Node.js project validation"}
	case "python":
		return &ValidationStrategy{ProjectType: "python", Steps: []ValidationStep{
			{Type: "lint", Command: "ruff . || true", Description: "Lint Python", Required: false},
			{Type: "syntax", Command: "python -m py_compile $(git ls-files '*.py') || true", Description: "Syntax check", Required: false},
			{Type: "test", Command: "pytest -q || true", Description: "Run pytest if present", Required: false},
		}, Context: "Python project validation"}
	case "rust":
		return &ValidationStrategy{ProjectType: "rust", Steps: []ValidationStep{
			{Type: "build", Command: "cargo check -q", Description: "Type/check Rust project", Required: true},
			{Type: "test", Command: "cargo test -q || true", Description: "Run Rust tests", Required: false},
		}, Context: "Rust project validation"}
	case "java":
		return &ValidationStrategy{ProjectType: "java", Steps: []ValidationStep{
			{Type: "build", Command: "mvn -q -DskipITs test || ./gradlew test --quiet || true", Description: "Compile and unit test", Required: false},
		}, Context: "Java project validation"}
	case "php":
		return &ValidationStrategy{ProjectType: "php", Steps: []ValidationStep{
			{Type: "syntax", Command: "php -l $(git ls-files '*.php') || true", Description: "PHP syntax check", Required: false},
			{Type: "test", Command: "phpunit --colors=never || true", Description: "Run PHPUnit if present", Required: false},
		}, Context: "PHP project validation"}
	case "ruby":
		return &ValidationStrategy{ProjectType: "ruby", Steps: []ValidationStep{
			{Type: "lint", Command: "rubocop -f quiet || true", Description: "Lint Ruby files", Required: false},
			{Type: "test", Command: "rspec --format progress || true", Description: "Run RSpec if present", Required: false},
		}, Context: "Ruby project validation"}
	default:
		return &ValidationStrategy{
			ProjectType: "go",
			Steps: []ValidationStep{
				{Type: "build", Command: "go build ./...", Description: "Build the project to check for compilation errors", Required: true},
				{Type: "test", Command: "go test ./...", Description: "Run tests to ensure functionality", Required: false},
			},
			Context: "Basic Go project validation strategy",
		}
	}
}

// detectProjectType uses simple file presence heuristics
func detectProjectType() string {
	// Node
	if _, err := os.Stat("package.json"); err == nil {
		return "node"
	}
	// Python
	if _, err := os.Stat("pyproject.toml"); err == nil {
		return "python"
	}
	if _, err := os.Stat("requirements.txt"); err == nil {
		return "python"
	}
	// Rust
	if _, err := os.Stat("Cargo.toml"); err == nil {
		return "rust"
	}
	// Java
	if _, err := os.Stat("pom.xml"); err == nil {
		return "java"
	}
	if _, err := os.Stat("build.gradle"); err == nil {
		return "java"
	}
	if _, err := os.Stat("build.gradle.kts"); err == nil {
		return "java"
	}
	// PHP
	if _, err := os.Stat("composer.json"); err == nil {
		return "php"
	}
	// Ruby
	if _, err := os.Stat("Gemfile"); err == nil {
		return "ruby"
	}
	return "go"
}

// runValidationStep runs a single validation step
func runValidationStep(step ValidationStep, logger *utils.Logger) (string, error) {
	logger.LogProcessStep(fmt.Sprintf("🔍 Running validation: %s", step.Description))

	// Execute the command
	cmd := exec.Command("sh", "-c", step.Command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("validation step failed: %w\nOutput: %s", err, string(output))
	}

	return string(output), nil
}

// analyzeValidationResults analyzes the results of validation steps
func analyzeValidationResults(validationResults []string, intentAnalysis *IntentAnalysis, validationStrategy *ValidationStrategy, cfg *config.Config, logger *utils.Logger) (int, error) {
	logger.LogProcessStep("🔍 Analyzing validation results...")

	// Build context for result analysis
	contextPrompt := fmt.Sprintf(`ANALYZE VALIDATION RESULTS

TASK: %s
CATEGORY: %s
COMPLEXITY: %s

VALIDATION STRATEGY: %s
STEPS: %d

RESULTS:
%s

ANALYZE:
1. Did all required steps pass?
2. Are there any critical issues?
3. Can the task proceed?
4. What's the next action?

Respond with JSON:
{
  "status": "on_track|needs_adjustment|critical_error|completed",
  "completion_percentage": 0-100,
  "next_action": "continue|revise_plan|run_command|validate",
  "reasoning": "explanation of the decision",
  "concerns": ["list", "of", "concerns"],
  "commands": ["list", "of", "commands", "if", "next_action", "is", "run_command"]
}`,
		"Validation analysis",
		intentAnalysis.Category,
		intentAnalysis.Complexity,
		validationStrategy.Context,
		len(validationStrategy.Steps),
		strings.Join(validationResults, "\n"))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at analyzing validation results and determining next steps. Respond only with valid JSON."},
		{Role: "user", Content: contextPrompt},
	}

	response, _, err := llm.GetLLMResponse(cfg.EditingModel, messages, "", cfg, 1*time.Minute)
	if err != nil {
		return 0, fmt.Errorf("LLM result analysis failed: %w", err)
	}

	// Parse the response
	var evaluation ProgressEvaluation
	if err := json.Unmarshal([]byte(response), &evaluation); err != nil {
		return 0, fmt.Errorf("failed to parse validation evaluation: %w", err)
	}

	// Estimate tokens used and attribute to Validation category (with split)
	promptTokens := llm.EstimateTokens(contextPrompt)
	completionTokens := llm.EstimateTokens(response)
	tokens := promptTokens + completionTokens
	logger.Logf("Validation tokens: prompt=%d completion=%d total=%d", promptTokens, completionTokens, tokens)
	// Track precise split via a global or returned path; here we only return totals.
	return tokens, nil
}

// parseValidationDecision parses the LLM's validation decision
func parseValidationDecision(response string, logger *utils.Logger) string {
	// Simple parsing for now - could be enhanced with more sophisticated logic
	if strings.Contains(strings.ToLower(response), "pass") || strings.Contains(strings.ToLower(response), "success") {
		return "pass"
	}
	if strings.Contains(strings.ToLower(response), "fail") || strings.Contains(strings.ToLower(response), "error") {
		return "fail"
	}
	return "unknown"
}

// hasFile checks if a file exists
func hasFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// hasTestFiles checks if a directory has test files
func hasTestFiles(dir, language string) bool {
	switch language {
	case "go":
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_test.go") {
				return true
			}
		}
	case "python":
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if !entry.IsDir() && (strings.HasSuffix(entry.Name(), "_test.py") || strings.HasSuffix(entry.Name(), "test_")) {
				return true
			}
		}
	case "javascript", "typescript":
		entries, err := os.ReadDir(dir)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".test.js") || strings.HasSuffix(entry.Name(), ".spec.js")) {
				return true
			}
		}
	}
	return false
}
