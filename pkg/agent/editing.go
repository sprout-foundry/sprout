package agent

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/utils"
)

// executeEditOperations executes the planned edit operations
func executeEditOperations(context *AgentContext) error {
	context.Logger.LogProcessStep("⚡ Executing planned edit operations...")

	if context.CurrentPlan == nil {
		return fmt.Errorf("cannot execute edits without a plan")
	}
	if len(context.CurrentPlan.EditOperations) == 0 {
		return fmt.Errorf("cannot execute edits: plan contains 0 operations")
	}

	tokens, err := executeEditPlanWithErrorHandling(context.CurrentPlan, context)
	if err != nil {
		// Check if this is a non-critical error that shouldn't fail the entire task
		if strings.Contains(err.Error(), "no changes detected") ||
			strings.Contains(err.Error(), "file already") ||
			strings.Contains(err.Error(), "minimal change") {
			context.Logger.LogProcessStep("⚠️ Edit operation had minor issues but task may be complete")
			context.ExecutedOperations = append(context.ExecutedOperations, "Edit operation completed with minor issues")
			context.TokenUsage.CodeGeneration += tokens
			return nil
		}
		return fmt.Errorf("edit execution failed: %w", err)
	}

	context.TokenUsage.CodeGeneration += tokens
	context.ExecutedOperations = append(context.ExecutedOperations,
		fmt.Sprintf("Executed %d edit operations", len(context.CurrentPlan.EditOperations)))

	context.Logger.LogProcessStep(fmt.Sprintf("✅ Completed %d edit operations", len(context.CurrentPlan.EditOperations)))
	return nil
}

// executeEditPlanWithErrorHandling executes edit plan with proper error handling for agent context
func executeEditPlanWithErrorHandling(editPlan *EditPlan, context *AgentContext) (int, error) {
	totalTokens := 0

	// Track changes for context
	var operationResults []string

	for i, operation := range editPlan.EditOperations {
		context.Logger.LogProcessStep(fmt.Sprintf("🔧 Edit %d/%d: %s (%s)", i+1, len(editPlan.EditOperations), operation.Description, operation.FilePath))

		// Create focused instructions for this specific edit
		editInstructions := buildFocusedEditInstructions(operation, context.Logger)
		// Count prompt/input tokens for this edit
		promptTokens := utils.EstimateTokens(editInstructions)
		totalTokens += promptTokens
		context.TokenUsage.CodeGeneration += promptTokens
		context.TokenUsage.CodegenSplit.Prompt += promptTokens

		// Retry logic: attempt the operation up to 3 times (1 initial + 2 retries)
		const maxRetries = 2
		var err error
		var success bool

		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				context.Logger.LogProcessStep(fmt.Sprintf("🔄 Retry attempt %d/%d for edit %d", attempt, maxRetries, i+1))
			}

			// Execute the edit with error handling
			if shouldUsePartialEdit(operation, context.Logger) {
				context.Logger.Logf("Attempting partial edit for %s (attempt %d)", operation.FilePath, attempt+1)
				diff, perr := editor.ProcessPartialEdit(operation.FilePath, operation.Instructions, context.Config, context.Logger)
				if perr == nil {
					// Approximate completion tokens by estimating diff size
					completionTokens := utils.EstimateTokens(diff)
					totalTokens += completionTokens
					context.TokenUsage.CodeGeneration += completionTokens
					context.TokenUsage.CodegenSplit.Completion += completionTokens
				}
				err = perr
				if err != nil {
					context.Logger.Logf("Partial edit failed, falling back to full file edit: %v", err)
					fdiff, ferr := editor.ProcessCodeGeneration(operation.FilePath, editInstructions, context.Config, "")
					if ferr == nil {
						completionTokens := utils.EstimateTokens(fdiff)
						totalTokens += completionTokens
						context.TokenUsage.CodeGeneration += completionTokens
						context.TokenUsage.CodegenSplit.Completion += completionTokens
					}
					err = ferr
				}
			} else {
				context.Logger.Logf("Using full file edit for %s (attempt %d)", operation.FilePath, attempt+1)
				fdiff, ferr := editor.ProcessCodeGeneration(operation.FilePath, editInstructions, context.Config, "")
				if ferr == nil {
					completionTokens := utils.EstimateTokens(fdiff)
					totalTokens += completionTokens
					context.TokenUsage.CodeGeneration += completionTokens
					context.TokenUsage.CodegenSplit.Completion += completionTokens
				}
				err = ferr
			}

			if err != nil {
				// Check if this is a "revisions applied" signal from the editor's review process
				if strings.Contains(err.Error(), "revisions applied, re-validating") {
					success = true
					operationResult := "✅ Edit operation completed (with review cycle)"
					operationResults = append(operationResults, operationResult)
					context.Logger.LogProcessStep(operationResult)
					break
				} else {
					// Log the attempt failure
					context.Logger.LogProcessStep(fmt.Sprintf("❌ Edit %d attempt %d failed: %v", i+1, attempt+1, err))

					// If this was the last attempt, record the failure
					if attempt == maxRetries {
						errorMsg := fmt.Sprintf("Edit operation %d failed after %d attempts: %v", i+1, maxRetries+1, err)
						context.Errors = append(context.Errors, errorMsg)
						context.Logger.LogProcessStep(fmt.Sprintf("💥 Edit %d failed permanently after %d attempts", i+1, maxRetries+1))

						// Track the failed operation
						operationResult := fmt.Sprintf("❌ Edit operation %d failed after %d attempts: %v", i+1, maxRetries+1, err)
						operationResults = append(operationResults, operationResult)
					}
					// Continue to next retry attempt or next operation
				}
			} else {
				success = true
				operationResult := "✅ Edit operation completed successfully"
				operationResults = append(operationResults, operationResult)
				context.Logger.LogProcessStep(operationResult)
				break
			}
		}

		// If the operation failed all attempts, log final failure
		if !success && err != nil {
			context.Logger.LogProcessStep(fmt.Sprintf("🚫 Edit %d exhausted all retry attempts", i+1))
		}
	}
	// Update agent context with results
	context.ExecutedOperations = append(context.ExecutedOperations, operationResults...)

	// Check if any operations failed
	hasFailures := false
	successCount := 0
	failureCount := 0

	for _, result := range operationResults {
		if strings.HasPrefix(result, "❌") {
			hasFailures = true
			failureCount++
		} else if strings.HasPrefix(result, "✅") {
			successCount++
		}
	}

	// Summarize results to the user
	context.Logger.LogProcessStep(fmt.Sprintf("📊 Edit execution summary: %d successful, %d failed out of %d total operations", successCount, failureCount, len(editPlan.EditOperations)))

	if hasFailures && successCount == 0 {
		return totalTokens, fmt.Errorf("all edit operations failed")
	}
	return totalTokens, nil
}
