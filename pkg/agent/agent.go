package agent

import (
	"fmt"
	"os"
	"strings"
	"time"

	pb "github.com/alantheprice/ledit/pkg/agent/playbooks"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/index"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

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

	// If embeddings failed, try symbol index
	root, _ := os.Getwd()
	if idx, err := index.BuildSymbols(root); err == nil && idx != nil {
		tokens := strings.Fields(userIntent)
		if sym := index.SearchSymbols(idx, tokens); len(sym) > 0 {
			logger.Logf("Symbol index found %d candidate files", len(sym))
			return sym
		}
	}

	// If embeddings and symbols failed, try content-based search
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

	// Playbook fast-path: if a registered playbook matches, let it build the plan
	if sel := pb.Select(context.UserIntent, context.IntentAnalysis.Category); sel != nil {
		context.Logger.LogProcessStep("📘 Using playbook: " + sel.Name())
		spec := sel.BuildPlan(context.UserIntent, context.IntentAnalysis.EstimatedFiles)
		if spec != nil && (len(spec.Ops) > 0 || len(spec.Files) > 0) {
			// Convert PlanSpec → EditPlan
			ep := &EditPlan{FilesToEdit: append([]string{}, spec.Files...), ScopeStatement: spec.Scope}
			for _, op := range spec.Ops {
				ep.EditOperations = append(ep.EditOperations, EditOperation{
					FilePath:           op.FilePath,
					Description:        op.Description,
					Instructions:       op.Instructions,
					ScopeJustification: op.ScopeJustification,
				})
			}
			context.CurrentPlan = ep
			context.ExecutedOperations = append(context.ExecutedOperations, "Plan created via playbook: "+sel.Name())
			context.Logger.LogProcessStep(fmt.Sprintf("✅ Plan created via playbook: %d files, %d operations", len(ep.FilesToEdit), len(ep.EditOperations)))
			return nil
		}
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
