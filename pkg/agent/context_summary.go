package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// summarizeContextIfNeeded summarizes agent context if it gets too large
func summarizeContextIfNeeded(context *AgentContext) error {
	const maxOperations = 20
	const maxErrors = 10
	const maxValidationResults = 10

	needsSummary := len(context.ExecutedOperations) > maxOperations ||
		len(context.Errors) > maxErrors ||
		len(context.ValidationResults) > maxValidationResults
	if !needsSummary {
		return nil
	}

	context.Logger.LogProcessStep("📝 Summarizing agent context to prevent overflow...")

	prompt := fmt.Sprintf(`Summarize this agent execution context to keep only the most important information:

EXECUTED OPERATIONS (%d):
%s

ERRORS (%d):
%s

VALIDATION RESULTS (%d):
%s

TASK: Create a concise summary that preserves:
1. Key milestones and achievements
2. Critical errors and their impact
3. Important validation outcomes
4. Overall progress status

Respond with JSON:
{
  "operations_summary": "concise summary of key operations",
  "errors_summary": "summary of critical errors",
  "validation_summary": "summary of validation outcomes",
  "key_achievements": ["achievement1", "achievement2"],
  "critical_issues": ["issue1", "issue2"]
}`,
		len(context.ExecutedOperations), strings.Join(context.ExecutedOperations, "\n"),
		len(context.Errors), strings.Join(context.Errors, "\n"),
		len(context.ValidationResults), strings.Join(context.ValidationResults, "\n"))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at summarizing complex execution contexts while preserving critical information. Always respond with valid JSON."},
		{Role: "user", Content: prompt},
	}

	response, _, err := llm.GetLLMResponse(context.Config.OrchestrationModel, messages, "", context.Config, 30*time.Second)
	if err != nil {
		context.Logger.Logf("Context summarization failed: %v", err)
		// Fallback: truncate arrays with bounds checking
		if len(context.ExecutedOperations) > maxOperations/2 {
			context.ExecutedOperations = context.ExecutedOperations[len(context.ExecutedOperations)-maxOperations/2:]
		}
		if len(context.Errors) > maxErrors/2 {
			context.Errors = context.Errors[len(context.Errors)-maxErrors/2:]
		}
		if len(context.ValidationResults) > maxValidationResults/2 {
			context.ValidationResults = context.ValidationResults[len(context.ValidationResults)-maxValidationResults/2:]
		}
		return nil
	}

	var summary struct {
		OperationsSummary string   `json:"operations_summary"`
		ErrorsSummary     string   `json:"errors_summary"`
		ValidationSummary string   `json:"validation_summary"`
		KeyAchievements   []string `json:"key_achievements"`
		CriticalIssues    []string `json:"critical_issues"`
	}

	if err := json.Unmarshal([]byte(response), &summary); err != nil {
		context.Logger.Logf("Failed to parse summary JSON: %v", err)
		if len(context.ExecutedOperations) > maxOperations/2 {
			context.ExecutedOperations = context.ExecutedOperations[len(context.ExecutedOperations)-maxOperations/2:]
		}
		if len(context.Errors) > maxErrors/2 {
			context.Errors = context.Errors[len(context.Errors)-maxErrors/2:]
		}
		if len(context.ValidationResults) > maxValidationResults/2 {
			context.ValidationResults = context.ValidationResults[len(context.ValidationResults)-maxValidationResults/2:]
		}
		return nil
	}

	originalOps := make([]string, len(context.ExecutedOperations))
	copy(originalOps, context.ExecutedOperations)

	context.ExecutedOperations = []string{
		"=== SUMMARIZED OPERATIONS ===",
		summary.OperationsSummary,
		"=== KEY ACHIEVEMENTS ===",
	}
	context.ExecutedOperations = append(context.ExecutedOperations, summary.KeyAchievements...)

	hasExecutedEdits := false
	for _, op := range originalOps {
		if strings.Contains(op, "Edit") && strings.Contains(op, "completed successfully") {
			hasExecutedEdits = true
			break
		}
	}
	if hasExecutedEdits {
		context.ExecutedOperations = append(context.ExecutedOperations, "✅ Edits completed successfully (preserved after summarization)")
	}

	if context.CurrentPlan != nil {
		context.ExecutedOperations = append(context.ExecutedOperations,
			fmt.Sprintf("ACTIVE PLAN: %d operations",
				len(context.CurrentPlan.EditOperations)))
	}

	context.Errors = []string{
		"=== SUMMARIZED ERRORS ===",
		summary.ErrorsSummary,
		"=== CRITICAL ISSUES ===",
	}
	context.Errors = append(context.Errors, summary.CriticalIssues...)

	context.ValidationResults = []string{
		"=== SUMMARIZED VALIDATION ===",
		summary.ValidationSummary,
	}

	tokens := utils.EstimateTokens(prompt)
	context.TokenUsage.ProgressEvaluation += tokens

	context.Logger.LogProcessStep("✅ Context summarized successfully")
	return nil
}
