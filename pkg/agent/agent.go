// Core agent functionality package
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// retryEditPlanWithShorterPrompt attempts to retry the edit plan creation with progressively shorter prompts
func retryEditPlanWithShorterPrompt(userIntent string, intentAnalysis *IntentAnalysis, contextFiles []string, cfg *config.Config, logger *utils.Logger, attempt int) (*EditPlan, int, error) {
	maxRetries := 3
	if attempt > maxRetries {
		logger.LogError(fmt.Errorf("failed to create edit plan after %d attempts due to response truncation", maxRetries))
		return nil, 0, fmt.Errorf("edit plan creation failed after %d truncation retries", maxRetries)
	}

	logger.LogProcessStep(fmt.Sprintf("🔄 Retry attempt %d/%d with optimized prompt length...", attempt, maxRetries))

	// Build context with progressively less detail on retries
	var contextContent strings.Builder
	for i, filePath := range contextFiles {
		if attempt >= 2 && i >= 2 { // Limit files on retry 2+
			break
		}
		if attempt >= 3 && i >= 1 { // Only first file on retry 3
			break
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			logger.LogError(fmt.Errorf("could not read file %s for context: %w", filePath, err))
			continue
		}

		// Truncate file content on retries to reduce prompt size
		maxContentLength := 2000 // Full content on first retry
		if attempt >= 2 {
			maxContentLength = 1000 // Shorter on retry 2
		}
		if attempt >= 3 {
			maxContentLength = 500 // Very short on retry 3
		}

		fileContent := string(content)
		if len(fileContent) > maxContentLength {
			fileContent = fileContent[:maxContentLength] + "\n... [truncated for retry]"
		}

		contextContent.WriteString(fmt.Sprintf("## File: %s\n```\n%s\n```\n\n", filePath, fileContent))
	}

	// Create a more concise prompt on retries
	promptTemplate := `You are a code editing specialist. Create a detailed edit plan for the following request.

USER REQUEST: %s

CONTEXT FILES:
%s

Return a JSON plan with this EXACT structure (be concise on retries):
{
  "files_to_edit": ["file1.go"],
  "edit_operations": [
    {
      "file_path": "file1.go",
      "description": "Brief description of the change",
      "instructions": "Specific editing instructions"
    }
  ],
  "context": "Brief context",
  "scope_statement": "Brief scope description"
}

IMPORTANT: 
- Keep response under 1500 characters on retry %d
- Focus only on essential changes
- Use minimal but complete JSON`

	prompt := fmt.Sprintf(promptTemplate, userIntent, contextContent.String(), attempt)

	// Use shorter timeout on retries
	timeout := time.Duration(120-attempt*20) * time.Second

	messages := []prompts.Message{
		{Role: "user", Content: prompt},
	}

	response, _, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, timeout)
	if err != nil {
		logger.LogError(fmt.Errorf("retry %d orchestration model failed: %w", attempt, err))
		// Try next retry or fail
		return retryEditPlanWithShorterPrompt(userIntent, intentAnalysis, contextFiles, cfg, logger, attempt+1)
	}

	// Try to parse the response
	cleanedResponse, err := utils.ExtractJSONFromLLMResponse(response)
	if err != nil {
		// Check if still truncated
		if strings.Contains(err.Error(), "no matching closing brace") || strings.Contains(err.Error(), "unexpected end of JSON input") {
			logger.LogProcessStep(fmt.Sprintf("⚠️ Retry %d still truncated (length: %d), trying again...", attempt, len(response)))
			return retryEditPlanWithShorterPrompt(userIntent, intentAnalysis, contextFiles, cfg, logger, attempt+1)
		}

		logger.LogError(fmt.Errorf("retry %d JSON extraction failed: %w", attempt, err))
		return retryEditPlanWithShorterPrompt(userIntent, intentAnalysis, contextFiles, cfg, logger, attempt+1)
	}

	// Parse the JSON structure
	var planData struct {
		FilesToEdit    []string `json:"files_to_edit"`
		EditOperations []struct {
			FilePath     string `json:"file_path"`
			Description  string `json:"description"`
			Instructions string `json:"instructions"`
		} `json:"edit_operations"`
		Context        string `json:"context"`
		ScopeStatement string `json:"scope_statement"`
	}

	if err := json.Unmarshal([]byte(cleanedResponse), &planData); err != nil {
		logger.LogError(fmt.Errorf("retry %d JSON parsing failed: %w", attempt, err))
		return retryEditPlanWithShorterPrompt(userIntent, intentAnalysis, contextFiles, cfg, logger, attempt+1)
	}

	// Convert to EditPlan structure
	var operations []EditOperation
	for _, op := range planData.EditOperations {
		operations = append(operations, EditOperation{
			FilePath:     op.FilePath,
			Description:  op.Description,
			Instructions: op.Instructions,
		})
	}

	editPlan := &EditPlan{
		FilesToEdit:    planData.FilesToEdit,
		EditOperations: operations,
		Context:        planData.Context,
		ScopeStatement: planData.ScopeStatement,
	}

	logger.LogProcessStep(fmt.Sprintf("✅ Retry %d successful! Edit plan created with %d operations", attempt, len(operations)))

	return editPlan, utils.EstimateTokens(response), nil
}

// Fallback functions for when LLM analysis fails
func inferCategory(userIntent string) string {
	intentLower := strings.ToLower(userIntent)
	if strings.Contains(intentLower, "test") {
		return "test"
	}
	if strings.Contains(intentLower, "fix") || strings.Contains(intentLower, "bug") {
		return "fix"
	}
	if strings.Contains(intentLower, "comment") || strings.Contains(intentLower, "doc") {
		return "docs"
	}
	if strings.Contains(intentLower, "review") {
		return "review"
	}
	return "code"
}

func inferComplexity(userIntent string) string {
	intentLower := strings.ToLower(userIntent)
	complexWords := []string{"refactor", "architect", "multiple", "design"} // Removed "system"
	simpleWords := []string{"add", "comment", "fix typo", "single"}

	for _, word := range complexWords {
		if strings.Contains(intentLower, word) {
			return "complex"
		}
	}

	for _, word := range simpleWords {
		if strings.Contains(intentLower, word) {
			return "simple"
		}
	}

	return "moderate"
}

// findRelevantFilesRobust uses embeddings and fallback strategies to find relevant files
func findRelevantFilesRobust(userIntent string, cfg *config.Config, logger *utils.Logger) []string {
	// Try embeddings first
	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		if os.IsNotExist(err) {
			logger.Logf("No workspace file found for embeddings, will use fallback methods")
		} else {
			logger.LogError(fmt.Errorf("failed to load workspace for file discovery: %w", err))
		}
	} else {
		fullFiles, _, embErr := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFile, cfg, logger)
		if embErr != nil {
			logger.LogError(fmt.Errorf("embedding search failed: %w", embErr))
		} else if len(fullFiles) > 0 {
			logger.Logf("Embeddings found %d relevant files", len(fullFiles))
			return fullFiles
		}
	}

	// If embeddings failed, try content-based search
	logger.Logf("Embeddings found no files, trying content search...")
	contentFiles := findRelevantFilesByContent(userIntent, logger)
	if len(contentFiles) > 0 {
		return contentFiles
	}

	// If all else fails, use shell commands to find files
	logger.Logf("Content search found no files, trying shell-based discovery...")

	// We need workspace info for shell commands, but keep it lightweight
	workspaceInfo := &WorkspaceInfo{
		ProjectType:   "other", // Default fallback
		RootFiles:     []string{},
		AllFiles:      []string{},
		FilesByDir:    map[string][]string{},
		RelevantFiles: map[string]string{},
	}

	shellFiles := findFilesUsingShellCommands(userIntent, workspaceInfo, logger)
	if len(shellFiles) > 0 {
		return shellFiles
	}

	// Absolute fallback - return empty slice, let caller handle
	logger.Logf("All file discovery methods failed")
	return []string{}
}

// rewordPromptForBetterSearch uses workspace model to reword the user prompt for better file discovery
func rewordPromptForBetterSearch(userIntent string, workspaceInfo *WorkspaceInfo, cfg *config.Config, logger *utils.Logger) (string, int, error) {
	logger.Logf("Using workspace model to reword prompt for better file discovery...")

	prompt := fmt.Sprintf(`You are a %s codebase expert. The user wants to: "%s"

WORKSPACE CONTEXT:
Project Type: %s
Available files include: %v

The initial file search found very few relevant files. Rewrite the user's intent using technical terms and patterns that would be found in a %s codebase to help find the right files.

Focus on:
- Function names that might exist
- File naming patterns in %s projects  
- Technical terms specific to this domain
- Package/module names that might be relevant

Respond with ONLY the reworded search query, no explanation:`,
		workspaceInfo.ProjectType, userIntent, workspaceInfo.ProjectType,
		workspaceInfo.AllFiles[:min(10, len(workspaceInfo.AllFiles))], // Show sample files
		workspaceInfo.ProjectType, workspaceInfo.ProjectType)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at understanding codebases and creating effective search queries."},
		{Role: "user", Content: prompt},
	}

	response, usage, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		logger.LogError(fmt.Errorf("workspace model failed to reword prompt: %w", err))
		return userIntent, 0, err // Return original on failure
	}

	reworded := strings.TrimSpace(response)
	if reworded == "" {
		return userIntent, 0, fmt.Errorf("empty reworded response")
	}

	// Compute tokens used (prefer actual usage if available)
	tokensUsed := 0
	if usage != nil {
		tokensUsed = usage.TotalTokens
	} else {
		// Fallback estimate
		tokensUsed = utils.EstimateTokens(prompt) + utils.EstimateTokens(response)
	}
	logger.Logf("Intent rewording tokens used: total=%d", tokensUsed)

	return reworded, tokensUsed, nil
}

func executeCreatePlan(context *AgentContext) error {
	context.Logger.LogProcessStep("🎯 Creating detailed edit plan...")

	if context.IntentAnalysis == nil {
		return fmt.Errorf("cannot create plan without intent analysis")
	}

	// Try planning up to 3 attempts total (initial + 2 retries). If still empty/fails, abort.
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			context.Logger.LogProcessStep(fmt.Sprintf("🔁 Plan attempt %d/%d", attempt, maxAttempts))
		}
		editPlan, tokens, err := createDetailedEditPlan(context.UserIntent, context.IntentAnalysis, context.Config, context.Logger)
		if err != nil {
			lastErr = err
			if attempt == maxAttempts {
				return fmt.Errorf("plan creation failed after %d attempts: %w", attempt, err)
			}
			continue
		}

		// Validate plan has actionable operations
		if editPlan == nil || len(editPlan.EditOperations) == 0 {
			lastErr = fmt.Errorf("empty plan: no edit operations produced")
			if attempt == maxAttempts {
				return fmt.Errorf("plan creation produced no operations after %d attempts", attempt)
			}
			continue
		}

		// Success: record plan and token usage
		context.CurrentPlan = editPlan
		context.TokenUsage.Planning += tokens
		context.TokenUsage.PlanningSplit.Prompt += tokens
		context.ExecutedOperations = append(context.ExecutedOperations,
			fmt.Sprintf("Created plan with %d operations for %d files", len(editPlan.EditOperations), len(editPlan.FilesToEdit)))

		context.Logger.LogProcessStep(fmt.Sprintf("✅ Plan created: %d files, %d operations",
			len(editPlan.FilesToEdit), len(editPlan.EditOperations)))
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("plan creation failed for unknown reasons")
}

// executeRevisePlan creates a new plan based on current learnings
func executeRevisePlan(context *AgentContext, evaluation *ProgressEvaluation) error {
	context.Logger.LogProcessStep("🔄 Revising plan based on current state...")

	if context.IntentAnalysis == nil {
		return fmt.Errorf("cannot revise plan without intent analysis")
	}

	// Special handling for validation failures - create compilation error fix plan
	if context.ValidationFailed {
		context.Logger.LogProcessStep("🔧 Creating fix plan for validation failures...")
		return executeValidationFailureRecovery(context)
	}

	// If evaluation provided a new plan, use it; otherwise create a fresh one
	if evaluation.NewPlan != nil {
		context.Logger.LogProcessStep("Using revised plan from evaluation")
		// Parse the new plan if it's JSON, otherwise treat as context
		// For now, just create a new plan since parsing arbitrary plan format is complex
	}

	// Create a fresh plan incorporating lessons learned
	editPlan, tokens, err := createDetailedEditPlan(context.UserIntent, context.IntentAnalysis, context.Config, context.Logger)
	if err != nil {
		return fmt.Errorf("plan revision failed: %w", err)
	}

	context.CurrentPlan = editPlan
	context.TokenUsage.Planning += tokens
	context.ExecutedOperations = append(context.ExecutedOperations,
		fmt.Sprintf("Revised plan: %d operations for %d files", len(editPlan.EditOperations), len(editPlan.FilesToEdit)))

	context.Logger.LogProcessStep(fmt.Sprintf("✅ Plan revised: %d files, %d operations",
		len(editPlan.FilesToEdit), len(editPlan.EditOperations)))
	return nil
}
