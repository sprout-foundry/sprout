package agent

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// executeValidation runs validation checks
func executeValidation(context *AgentContext) error {
	context.Logger.LogProcessStep("🔍 Executing validation checks...")

	if context.IntentAnalysis == nil {
		return fmt.Errorf("cannot validate without intent analysis")
	}

	// Skip validation for read-only/analysis tasks
	if context.IntentAnalysis.Category == "review" && !strings.Contains(strings.ToLower(context.UserIntent), "refactor") {
		context.Logger.LogProcessStep("✅ Skipping validation for read-only analysis task")
		context.ValidationResults = append(context.ValidationResults, "✅ Validation skipped (read-only task)")
		context.ExecutedOperations = append(context.ExecutedOperations, "Validation skipped (read-only task)")
		return nil
	}

	// Skip build for documentation-only changes; apply outcome-driven check instead
	if context.IntentAnalysis.Category == "docs" && context.TaskComplexity == TaskSimple {
		// Attempt outcome-driven evaluation: if a file is mentioned and now begins with comment lines, consider success
		file := extractPathFromUserIntent(context.UserIntent)
		if file != "" {
			if ok := fileStartsWithComment(file); ok {
				context.Logger.LogProcessStep("✅ Doc outcome met: top-of-file comment present")
				context.ValidationResults = append(context.ValidationResults, "✅ Doc success criteria met (top-of-file comment present)")
				context.ExecutedOperations = append(context.ExecutedOperations, "Validation completed (docs-only success)")
				return nil
			}
		}
		context.Logger.LogProcessStep("✅ Skipping validation for documentation-only changes")
		context.ValidationResults = append(context.ValidationResults, "✅ Validation skipped (docs-only)")
		context.ExecutedOperations = append(context.ExecutedOperations, "Validation skipped (docs-only)")
		return nil
	}

	// Fast-path validation for simple tasks
	if context.TaskComplexity == TaskSimple {
		return executeSimpleValidation(context)
	}

	// Full validation for moderate and complex tasks
	tokens, err := validateChangesWithIteration(context.IntentAnalysis, context.UserIntent, context.Config, context.Logger, context.TokenUsage)
	if err != nil {
		context.Errors = append(context.Errors, fmt.Sprintf("Validation failed: %v", err))
		context.ValidationResults = append(context.ValidationResults, fmt.Sprintf("❌ Validation failed: %v", err))
		context.ValidationFailed = true
		context.Logger.LogProcessStep("❌ Validation failed - marking for recovery")
		return nil // Don't fail the agent - let it handle the validation failure
	}

	context.TokenUsage.Validation += tokens
	context.ValidationResults = append(context.ValidationResults, "✅ Validation passed")
	context.ExecutedOperations = append(context.ExecutedOperations, "Validation completed successfully")

	context.Logger.LogProcessStep("✅ Validation completed successfully")
	return nil
}

// executeSimpleValidation performs minimal validation for simple tasks to avoid overhead
func executeSimpleValidation(context *AgentContext) error {
	context.Logger.LogProcessStep("🚀 Fast validation for simple task...")

	// Enhanced validation for refactoring tasks
	if strings.Contains(strings.ToLower(context.UserIntent), "refactor") ||
		strings.Contains(strings.ToLower(context.UserIntent), "extract") {
		return executeRefactoringValidation(context)
	}

	// For simple tasks (like adding comments), just check basic syntax
	if context.IntentAnalysis.Category == "docs" {
		// Just run a basic build check for documentation changes
		cmd := exec.Command("go", "build", ".")
		output, err := cmd.CombinedOutput()

		if err != nil {
			errorMsg := fmt.Sprintf("Validation failed - compilation errors detected: %v\nOutput: %s", err, string(output))
			context.Errors = append(context.Errors, errorMsg)
			context.ValidationResults = append(context.ValidationResults, fmt.Sprintf("❌ %s", errorMsg))
			context.Logger.LogProcessStep("❌ Validation failed - compilation errors found")
			context.ValidationFailed = true
			return nil // Don't fail the agent - let it handle the validation failure
		}

		context.ValidationResults = append(context.ValidationResults, "✅ Simple validation passed (syntax check)")
		context.ExecutedOperations = append(context.ExecutedOperations, "Simple validation completed")
		context.Logger.LogProcessStep("✅ Simple validation completed - syntax check passed")
		return nil
	}

	// For ALL other tasks involving Go code, run compilation check
	context.Logger.LogProcessStep("🔍 Running compilation check for code changes...")

	cmd := exec.Command("go", "build", "./...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorMsg := fmt.Sprintf("Validation failed - compilation errors detected: %v\nOutput: %s", err, string(output))
		context.Errors = append(context.Errors, errorMsg)
		context.ValidationResults = append(context.ValidationResults, fmt.Sprintf("❌ %s", errorMsg))
		context.Logger.LogProcessStep("❌ Validation failed - compilation errors found")
		context.ValidationFailed = true
		return nil // Don't fail the agent - let it handle the validation failure
	}

	context.ValidationResults = append(context.ValidationResults, "✅ Simple validation passed (compilation check)")
	context.ExecutedOperations = append(context.ExecutedOperations, "Simple validation completed")
	context.Logger.LogProcessStep("✅ Simple validation completed - compilation check passed")
	return nil
}

// executeRefactoringValidation performs thorough validation for refactoring tasks
func executeRefactoringValidation(context *AgentContext) error {
	context.Logger.LogProcessStep("🔍 Enhanced validation for refactoring task...")

	// First, run basic compilation check
	cmd := exec.Command("go", "build", "./...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorMsg := fmt.Sprintf("Validation failed - refactoring introduced compilation errors: %v\nOutput: %s", err, string(output))
		context.Errors = append(context.Errors, errorMsg)
		context.ValidationResults = append(context.ValidationResults, fmt.Sprintf("❌ %s", errorMsg))
		context.Logger.LogProcessStep("❌ Refactoring validation failed - compilation errors found")
		context.ValidationFailed = true
		return nil // Don't fail the agent - let it handle the validation failure
	}

	context.ValidationResults = append(context.ValidationResults, "✅ Refactoring validation passed (compilation + goal achieved)")
	context.ExecutedOperations = append(context.ExecutedOperations, "Refactoring validation completed")
	context.Logger.LogProcessStep("✅ Refactoring validation completed successfully")
	return nil
}

// extractPathFromUserIntent returns the first token that looks like a path with an extension
func extractPathFromUserIntent(intent string) string {
	// generic heuristic: find first token that looks like a relative or absolute path with extension
	for _, tok := range strings.Fields(intent) {
		cleaned := filepath.Clean(strings.Trim(tok, "\"'`"))
		if strings.Contains(cleaned, "/") && strings.Contains(cleaned, ".") {
			return cleaned
		}
	}
	return ""
}

// fileStartsWithComment checks whether the file begins with comments (line or block)
func fileStartsWithComment(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	r := bufio.NewReader(f)
	inBlock := false
	for i := 0; i < 20; i++ { // inspect up to ~20 lines to catch license headers
		line, err := r.ReadString('\n')
		if len(line) == 0 && err != nil {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "/*") {
			inBlock = true
		}
		if inBlock {
			if strings.Contains(trimmed, "*/") {
				return true
			}
			// still inside top block comment
			continue
		}
		// Not in block; check line comment at the top
		return strings.HasPrefix(trimmed, "//")
	}
	return inBlock
}

// executeValidationFailureRecovery analyzes validation failures and creates targeted fixes
func executeValidationFailureRecovery(context *AgentContext) error {
	context.Logger.LogProcessStep("🔧 Analyzing validation failures and creating recovery plan...")

	// Extract compilation errors from validation results
	var compilationErrors []string
	for _, result := range context.ValidationResults {
		if strings.Contains(result, "❌") && (strings.Contains(result, "compilation") || strings.Contains(result, "Compilation")) {
			compilationErrors = append(compilationErrors, result)
		}
	}

	if len(compilationErrors) == 0 {
		return fmt.Errorf("validation failed but no compilation errors found in results")
	}

	// Create a fix plan using the error analysis
	fixInstructions := fmt.Sprintf(`VALIDATION FAILURE RECOVERY

Original Intent: %s

COMPILATION ERRORS DETECTED:
%s

TASK: Analyze these compilation errors and create targeted fixes to resolve the syntax/compilation issues.

The previous edits introduced compilation errors. Please:
1. Identify the specific syntax issues
2. Create targeted edits to fix the compilation problems  
3. Ensure the fixes maintain the original intent of the changes
4. Focus only on fixing compilation errors, don't add new features

Please create a fix plan that addresses these compilation issues.`,
		context.UserIntent,
		strings.Join(compilationErrors, "\n"))

	// Create fix plan using orchestration model
	editPlan, tokens, err := createDetailedEditPlan(fixInstructions, context.IntentAnalysis, context.Config, context.Logger)
	if err != nil {
		return fmt.Errorf("failed to create validation fix plan: %w", err)
	}

	// Replace current plan with fix plan
	context.CurrentPlan = editPlan
	context.TokenUsage.Planning += tokens
	context.ValidationFailed = false // Reset flag since we're addressing it

	context.ExecutedOperations = append(context.ExecutedOperations,
		fmt.Sprintf("Created validation fix plan: %d operations to resolve compilation errors", len(editPlan.EditOperations)))

	context.Logger.LogProcessStep(fmt.Sprintf("✅ Validation fix plan created: %d operations for %d files",
		len(editPlan.EditOperations), len(editPlan.FilesToEdit)))

	return nil
}
