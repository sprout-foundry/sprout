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

// evaluateProgress evaluates the current state and decides what to do next
func evaluateProgress(context *AgentContext) (*ProgressEvaluation, int, error) {
	// Fast-path for simple tasks - avoid expensive LLM evaluations
	if context.TaskComplexity == TaskSimple {
		return evaluateProgressFastPath(context)
	}

	// Standard LLM-based evaluation for moderate and complex tasks
	return evaluateProgressWithLLM(context)
}

// evaluateProgressFastPath provides deterministic progress evaluation for simple tasks
func evaluateProgressFastPath(context *AgentContext) (*ProgressEvaluation, int, error) {
	// Simple rule-based evaluation to avoid LLM calls

	// Check if task was completed via immediate execution during intent analysis
	for _, op := range context.ExecutedOperations {
		if strings.Contains(op, "Task completed via immediate command execution") {
			return &ProgressEvaluation{
				Status:               "completed",
				CompletionPercentage: 100,
				NextAction:           "completed",
				Reasoning:            "Task completed via immediate command execution during intent analysis",
				Concerns:             []string{},
			}, 0, nil
		}
	}

	// If no intent analysis, analyze first
	if context.IntentAnalysis == nil {
		return &ProgressEvaluation{
			Status:               "on_track",
			CompletionPercentage: 10,
			NextAction:           "analyze_intent",
			Reasoning:            "Simple task: need to analyze intent first",
			Concerns:             []string{},
		}, 0, nil // 0 tokens used
	}

	// If no plan, create one
	if context.CurrentPlan == nil {
		return &ProgressEvaluation{
			Status:               "on_track",
			CompletionPercentage: 30,
			NextAction:           "create_plan",
			Reasoning:            "Simple task: intent analyzed, now need to create execution plan",
			Concerns:             []string{},
		}, 0, nil
	}

	// If plan exists but no edits executed, execute them; if plan is empty, fail fast
	if len(context.CurrentPlan.EditOperations) == 0 {
		return &ProgressEvaluation{
			Status:               "critical_error",
			CompletionPercentage: 0,
			NextAction:           "completed",
			Reasoning:            "Plan contains 0 operations; aborting to avoid no-op loops",
			Concerns:             []string{"Empty plan produced by planner"},
		}, 0, nil
	}

	// If plan exists but no edits executed, execute them
	hasExecutedEdits := false
	for _, op := range context.ExecutedOperations {
		if strings.Contains(op, "Edit") && strings.Contains(op, "completed successfully") {
			hasExecutedEdits = true
			break
		}
	}

	if !hasExecutedEdits {
		return &ProgressEvaluation{
			Status:               "on_track",
			CompletionPercentage: 70,
			NextAction:           "execute_edits",
			Reasoning:            "Simple task: plan ready, executing edits now",
			Concerns:             []string{},
		}, 0, nil
	}

	// If edits executed, validate (simplified validation for simple tasks)
	hasValidation := false
	for _, result := range context.ValidationResults {
		if len(result) > 0 {
			hasValidation = true
			break
		}
	}

	if !hasValidation {
		return &ProgressEvaluation{
			Status:               "on_track",
			CompletionPercentage: 90,
			NextAction:           "validate",
			Reasoning:            "Simple task: edits complete, running basic validation",
			Concerns:             []string{},
		}, 0, nil
	}

	// If validation done, check if the actual goal was achieved
	if hasValidation {
		// TODO: Validate that the agent also thinks that we have completed successfully

		// Standard completion for other tasks
		return &ProgressEvaluation{
			Status:               "completed",
			CompletionPercentage: 100,
			NextAction:           "completed",
			Reasoning:            "Simple task: all steps completed successfully",
			Concerns:             []string{},
		}, 0, nil
	}

	// Fallback if no validation was done yet
	return &ProgressEvaluation{
		Status:               "on_track",
		CompletionPercentage: 90,
		NextAction:           "validate",
		Reasoning:            "Simple task: edits complete, running basic validation",
		Concerns:             []string{},
	}, 0, nil
}

// evaluateProgressWithLLM performs full LLM-based progress evaluation for complex tasks
func evaluateProgressWithLLM(context *AgentContext) (*ProgressEvaluation, int, error) {
	// CRITICAL: Deterministic check - if plan exists but no edits executed, ALWAYS execute first
	if context.CurrentPlan != nil {
		hasExecutedEdits := false
		hasRunValidation := false

		for _, op := range context.ExecutedOperations {
			if strings.Contains(op, "Edit") && strings.Contains(op, "completed successfully") {
				hasExecutedEdits = true
			}
			if strings.Contains(op, "validation") || strings.Contains(op, "Validation") {
				hasRunValidation = true
			}
		}

		if !hasExecutedEdits {
			return &ProgressEvaluation{
				Status:               "on_track",
				CompletionPercentage: 50,
				NextAction:           "execute_edits",
				Reasoning:            "Plan exists but no edits executed - executing plan immediately to avoid analysis loops",
				Concerns:             []string{},
			}, 0, nil // 0 tokens - deterministic decision
		}

		if hasExecutedEdits && !hasRunValidation {
			return &ProgressEvaluation{
				Status:               "on_track",
				CompletionPercentage: 90,
				NextAction:           "validate",
				Reasoning:            "Edits completed but validation not run - mandatory validation required before completion",
				Concerns:             []string{},
			}, 0, nil // 0 tokens - deterministic decision
		}
	}

	// CRITICAL: Handle validation failures - create fix plans for compilation errors
	if context.ValidationFailed {
		// Check if we have compilation errors that can be fixed
		hasCompilationErrors := false
		for _, result := range context.ValidationResults {
			if strings.Contains(result, "compilation errors") || strings.Contains(result, "Compilation check failed") {
				hasCompilationErrors = true
				break
			}
		}

		if hasCompilationErrors {
			return &ProgressEvaluation{
				Status:               "needs_adjustment",
				CompletionPercentage: 70,
				NextAction:           "revise_plan",
				Reasoning:            "Validation failed due to compilation errors - need to analyze and fix the syntax/compilation issues",
				Concerns:             []string{"Compilation errors detected after edits", "Code changes introduced syntax errors"},
				NewPlan:              nil, // Will be generated by LLM in revise_plan action
			}, 0, nil // 0 tokens - deterministic decision
		}
	}

	// Build a comprehensive context summary for the LLM
	var contextSummary strings.Builder

	contextSummary.WriteString("AGENT EXECUTION CONTEXT:\n")
	contextSummary.WriteString(fmt.Sprintf("User Intent: %s\n", context.UserIntent))
	contextSummary.WriteString(fmt.Sprintf("Iteration: %d/%d\n", context.IterationCount, context.MaxIterations))
	contextSummary.WriteString(fmt.Sprintf("Elapsed Time: %v\n", time.Since(context.StartTime)))

	if context.IntentAnalysis != nil {
		contextSummary.WriteString(fmt.Sprintf("Intent Analysis: Category=%s, Complexity=%s\n",
			context.IntentAnalysis.Category, context.IntentAnalysis.Complexity))
	}

	if context.CurrentPlan != nil {
		contextSummary.WriteString(fmt.Sprintf("Current Plan: %d files to edit, %d operations\n",
			len(context.CurrentPlan.FilesToEdit), len(context.CurrentPlan.EditOperations)))
		contextSummary.WriteString(fmt.Sprintf("Plan Context: %s\n", context.CurrentPlan.Context))
	}

	contextSummary.WriteString(fmt.Sprintf("Executed Operations (%d):\n", len(context.ExecutedOperations)))
	for i, op := range context.ExecutedOperations {
		contextSummary.WriteString(fmt.Sprintf("  %d. %s\n", i+1, op))
	}

	contextSummary.WriteString(fmt.Sprintf("Errors Encountered (%d):\n", len(context.Errors)))
	for i, err := range context.Errors {
		contextSummary.WriteString(fmt.Sprintf("  %d. %s\n", i+1, err))
	}

	contextSummary.WriteString(fmt.Sprintf("Validation Results (%d):\n", len(context.ValidationResults)))
	for i, result := range context.ValidationResults {
		contextSummary.WriteString(fmt.Sprintf("  %d. %s\n", i+1, result))
	}
	prompt := BuildProgressEvaluationPrompt(contextSummary.String())
	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert software development agent that excels at evaluating progress and making smart decisions. Always respond with valid JSON."},
		{Role: "user", Content: prompt},
	}
	response, _, err := llm.GetLLMResponse(context.Config.OrchestrationModel, messages, "", context.Config, 60*time.Second)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get progress evaluation: %w", err)
	}
	cleanedResponse, cleanErr := utils.CleanAndValidateJSONResponse(response, []string{"status", "completion_percentage", "next_action", "reasoning"})
	if cleanErr != nil {
		context.Logger.LogError(fmt.Errorf("CRITICAL: LLM returned invalid JSON for progress evaluation: %w\nRaw response: %s", cleanErr, response))
		return nil, 0, fmt.Errorf("unrecoverable JSON validation error in progress evaluation: %w\nLLM Response: %s", cleanErr, response)
	}
	var evaluation ProgressEvaluation
	err = json.Unmarshal([]byte(cleanedResponse), &evaluation)
	if err != nil {
		context.Logger.LogError(fmt.Errorf("CRITICAL: Failed to parse progress evaluation JSON from LLM: %w\nCleaned response: %s", err, cleanedResponse))
		return nil, 0, fmt.Errorf("unrecoverable JSON parsing error in progress evaluation: %w\nCleaned Response: %s", err, cleanedResponse)
	}
	// JSON schema guards: required fields & allowed next actions
	if evaluation.Status == "" || evaluation.NextAction == "" || evaluation.CompletionPercentage < 0 || evaluation.CompletionPercentage > 100 {
		return nil, 0, fmt.Errorf("invalid evaluator JSON: missing/invalid required fields")
	}
	allowed := map[string]bool{"analyze_intent": true, "create_plan": true, "execute_edits": true, "run_command": true, "validate": true, "revise_plan": true, "workspace_info": true, "grep_search": true, "list_files": true, "micro_edit": true, "completed": true, "continue": true}
	if !allowed[strings.ToLower(evaluation.NextAction)] {
		return nil, 0, fmt.Errorf("invalid evaluator next_action: %s", evaluation.NextAction)
	}

	promptTokens := utils.EstimateTokens(prompt)
	completionTokens := utils.EstimateTokens(response)
	tokens := promptTokens + completionTokens
	// Save split for precise costing later
	context.TokenUsage.ProgressSplit.Prompt += promptTokens
	context.TokenUsage.ProgressSplit.Completion += completionTokens
	return &evaluation, tokens, nil
}
