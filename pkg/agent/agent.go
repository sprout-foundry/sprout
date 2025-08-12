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

// moved to shell.go

// RunAgentMode is the main public interface for command line usage
/*func RunAgentMode(userIntent string, skipPrompt bool, model string) error {
	fmt.Printf("🤖 Agent mode: Analyzing your intent...\n")

	// Log the original user prompt
	utils.LogUserPrompt(userIntent)

	// Load configuration
	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		logger := utils.GetLogger(false) // Get a logger even if config fails
		logger.LogError(fmt.Errorf("failed to load config: %w", err))
		return fmt.Errorf("failed to load config: %w", err)
	}
	// If a model is provided via command-line flag, it takes precedence
	if model != "" {
		cfg.OrchestrationModel = model
	}
	cfg.SkipPrompt = skipPrompt
	// Initialize pricing table (for accurate cost calculations)
	_ = llm.InitPricingTable()

	fmt.Printf("🎯 Intent: %s\n", userIntent)

	logger := utils.GetLogger(cfg.SkipPrompt)

	// Execute the agent, tracking total wall time for accurate performance stats
	overallStart := time.Now()
	tokenUsage, err := Execute(userIntent, cfg, logger)
	if err != nil {
		return err
	}

	// Print token usage summary using the actual overall duration
	overallDuration := time.Since(overallStart)
	PrintTokenUsageSummary(tokenUsage, overallDuration)

	fmt.Printf("✅ Agent execution completed\n")
	return nil
}*/

// Execute is the main public interface for running the agent
/*func Execute(userIntent string, cfg *config.Config, logger *utils.Logger) (*AgentTokenUsage, error) {
	logger.LogProcessStep("🚀 Starting optimized agent execution...")

	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: agent.Execute started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Initialize token usage tracking
	tokenUsage := &AgentTokenUsage{}

	// Run optimized agent
	logger.LogProcessStep("🔄 Executing optimized agent...")
	err := runOptimizedAgent(userIntent, cfg, logger, tokenUsage)

	if err != nil {
		logger.LogError(fmt.Errorf("agent execution failed: %w", err))
		return tokenUsage, fmt.Errorf("agent execution failed: %w", err)
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: agent.Execute completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Print cost breakdown using pricing table and split usage when available
	orchestratorModel := cfg.OrchestrationModel
	if orchestratorModel == "" {
		orchestratorModel = cfg.EditingModel
	}
	editingModel := cfg.EditingModel

	buildUsage := func(prompt, completion int) llm.TokenUsage {
		return llm.TokenUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}
	}

	intentUsage := buildUsage(tokenUsage.IntentSplit.Prompt, tokenUsage.IntentSplit.Completion)
	if intentUsage.TotalTokens == 0 && tokenUsage.IntentAnalysis > 0 {
		intentUsage = buildUsage(tokenUsage.IntentAnalysis, 0)
	}
	planningUsage := buildUsage(tokenUsage.PlanningSplit.Prompt, tokenUsage.PlanningSplit.Completion)
	if planningUsage.TotalTokens == 0 && tokenUsage.Planning > 0 {
		planningUsage = buildUsage(tokenUsage.Planning, 0)
	}
	progressUsage := buildUsage(tokenUsage.ProgressSplit.Prompt, tokenUsage.ProgressSplit.Completion)
	if progressUsage.TotalTokens == 0 && tokenUsage.ProgressEvaluation > 0 {
		progressUsage = buildUsage(tokenUsage.ProgressEvaluation, 0)
	}
	codegenUsage := buildUsage(tokenUsage.CodegenSplit.Prompt, tokenUsage.CodegenSplit.Completion)
	if codegenUsage.TotalTokens == 0 && tokenUsage.CodeGeneration > 0 {
		codegenUsage = buildUsage(tokenUsage.CodeGeneration, 0)
	}
	validationUsage := buildUsage(tokenUsage.ValidationSplit.Prompt, tokenUsage.ValidationSplit.Completion)
	if validationUsage.TotalTokens == 0 && tokenUsage.Validation > 0 {
		validationUsage = buildUsage(tokenUsage.Validation, 0)
	}

	intentCost := llm.CalculateCost(intentUsage, orchestratorModel)
	planningCost := llm.CalculateCost(planningUsage, orchestratorModel)
	progressCost := llm.CalculateCost(progressUsage, orchestratorModel)
	codegenCost := llm.CalculateCost(codegenUsage, editingModel)
	validationCost := llm.CalculateCost(validationUsage, editingModel)
	totalCost := intentCost + planningCost + progressCost + codegenCost + validationCost

	fmt.Printf("\n💵 Cost by category (using pricing table):\n")
	fmt.Printf("├─ Intent Analysis: $%.4f (%s)\n", intentCost, orchestratorModel)
	fmt.Printf("├─ Planning:        $%.4f (%s)\n", planningCost, orchestratorModel)
	fmt.Printf("├─ Progress Eval:   $%.4f (%s)\n", progressCost, orchestratorModel)
	fmt.Printf("├─ Code Generation: $%.4f (%s)\n", codegenCost, editingModel)
	fmt.Printf("├─ Validation:      $%.4f (%s)\n", validationCost, editingModel)
	fmt.Printf("└─ Total:           $%.4f\n", totalCost)
	fmt.Printf("⏱️  Total time:     %v\n", duration)

	return tokenUsage, nil
}*/

// moved out: PrintTokenUsageSummary

// runOptimizedAgent runs the agent with adaptive decision-making and progress evaluation
/*func runOptimizedAgent(userIntent string, cfg *config.Config, logger *utils.Logger, tokenUsage *AgentTokenUsage) error {
	logger.LogProcessStep("CHECKPOINT: Starting adaptive agent execution")

	// Initialize agent context
	context := &AgentContext{
		UserIntent:         userIntent,
		ExecutedOperations: []string{},
		Errors:             []string{},
		ValidationResults:  []string{},
		IterationCount:     0,
		MaxIterations:      25, // Prevent infinite loops
		StartTime:          time.Now(),
		TokenUsage:         tokenUsage,
		Config:             cfg,
		Logger:             logger,
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runOptimizedAgent started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Debug logging removed for cleaner output

	// Main adaptive execution loop
	for context.IterationCount < context.MaxIterations {
		context.IterationCount++
		logger.LogProcessStep(fmt.Sprintf(" Agent Iteration %d/%d", context.IterationCount, context.MaxIterations))

		// Step 1: Evaluate current progress and decide next action
		evaluation, evalTokens, err := evaluateProgress(context)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to evaluate progress: %w", err))
			context.Errors = append(context.Errors, fmt.Sprintf("Progress evaluation failed: %v", err))
			// Continue with fallback behavior rather than failing completely
			evaluation = &ProgressEvaluation{
				Status:     "needs_adjustment",
				NextAction: "continue",
				Reasoning:  "Fallback due to evaluation failure",
			}
		}
		context.TokenUsage.ProgressEvaluation += evalTokens

		logger.LogProcessStep(fmt.Sprintf("📊 Progress Status: %s (%d%% complete)", evaluation.Status, evaluation.CompletionPercentage))
		logger.LogProcessStep(fmt.Sprintf("🎯 Next Action: %s", evaluation.NextAction))
		logger.LogProcessStep(fmt.Sprintf("🤔 Reasoning: %s", evaluation.Reasoning))

		// Only log concerns if they're actionable (not just observations)
		if len(evaluation.Concerns) > 0 && evaluation.Status == "critical_error" {
			logger.LogProcessStep("⚠️ Critical concerns:")
			for _, concern := range evaluation.Concerns {
				logger.LogProcessStep(fmt.Sprintf("   • %s", concern))
			}
		}

		// Step 2: Execute the decided action
		// Inline executeAdaptiveAction (function moved during refactor)
		switch evaluation.NextAction {
		case "analyze_intent":
			// executeIntentAnalysis was removed; fall back to analyzing intent inline via existing call path
			intentAnalysis, tokens, e := analyzeIntentWithMinimalContext(context.UserIntent, context.Config, context.Logger)
			if e != nil {
				err = fmt.Errorf("intent analysis failed: %w", e)
			} else {
				context.IntentAnalysis = intentAnalysis
				context.TokenUsage.IntentAnalysis += tokens
				// Approximate split: attribute to prompt for now (no exact split available here)
				context.TokenUsage.IntentSplit.Prompt += tokens
				context.ExecutedOperations = append(context.ExecutedOperations, "Intent analysis completed")

				// Fast path: execute immediate shell command when safe
				cmdToRun := ""
				if intentAnalysis != nil && intentAnalysis.CanExecuteNow && strings.TrimSpace(intentAnalysis.ImmediateCommand) != "" {
					cmdToRun = strings.TrimSpace(intentAnalysis.ImmediateCommand)
				} else if isSimpleShellCommand(context.UserIntent) {
					cmdToRun = strings.TrimSpace(context.UserIntent)
				}
				if cmdToRun != "" {
					context.Logger.LogProcessStep("🚀 Fast path: executing immediate shell command")
					if err := executeShellCommands(context, []string{cmdToRun}); err != nil {
						context.Logger.LogError(fmt.Errorf("immediate command failed: %w", err))
					} else {
						context.ExecutedOperations = append(context.ExecutedOperations, "Task completed via immediate command execution: "+cmdToRun)
						context.IsCompleted = true
					}
				}
				err = nil
			}
		case "create_plan":
			err = executeCreatePlan(context)
		case "execute_edits":
			err = executeEditOperations(context)
		case "run_command":
			err = executeShellCommands(context, evaluation.Commands)
		case "validate":
			err = executeValidation(context)
		case "revise_plan":
			err = executeRevisePlan(context, evaluation)
		case "completed":
			context.Logger.LogProcessStep("✅ Task marked as completed by agent evaluation")
			context.IsCompleted = true
			err = nil
		case "continue":
			context.Logger.LogProcessStep("▶️ Continuing with current plan")
			err = nil
		default:
			err = fmt.Errorf("unknown action: %s", evaluation.NextAction)
		}
		if err != nil {
			context.Errors = append(context.Errors, fmt.Sprintf("Action execution failed: %v", err))
			logger.LogError(fmt.Errorf("action execution failed: %w", err))
			return fmt.Errorf("agent execution failed: %w", err)
		}

		// Check for immediate completion (e.g., from immediate execution)
		if context.IsCompleted {
			logger.LogProcessStep("✅ Agent determined task is completed successfully")
			break
		}

		// Step 3: Check for completion
		if evaluation.Status == "completed" || evaluation.NextAction == "completed" {
			logger.LogProcessStep("✅ Agent determined task is completed successfully")
			context.IsCompleted = true
			break
		}

		// Step 4: Summarize context if it's getting too large
		err = summarizeContextIfNeeded(context)
		if err != nil {
			logger.LogProcessStep(fmt.Sprintf("⚠️ Context summarization failed: %v", err))
		}

		// Prevent infinite loops
		if context.IterationCount == context.MaxIterations {
			logger.LogProcessStep("⚠️ Maximum iterations reached, completing execution")
			break
		}
	}

	duration := time.Since(context.StartTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runOptimizedAgent completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	logger.LogProcessStep(fmt.Sprintf("🎉 Adaptive agent execution completed in %d iterations", context.IterationCount))
	return nil
}*/

// Types moved to types.go

// AgentContext maintains state and context throughout agent execution

// ProgressEvaluation represents the agent's assessment of current progress

// IntentAnalysis represents the analysis of user intent

// TaskComplexityLevel represents the complexity level of a task for optimization

// EditOperation represents a single file edit operation

// analyzeIntentWithMinimalContext analyzes user intent with proper workspace context
// moved to intent.go

// Planning function moved to planning.go

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

// findRelevantFilesByContent searches for files containing relevant content based on the user intent
// moved to workspace_discovery.go

// WorkspaceInfo represents comprehensive workspace structure

// moved out: buildWorkspaceStructure

// moved to workspace_discovery.go
// moved out: getBasicFileListing

// getRecentlyModifiedSourceFiles returns a list of recently modified source files as fallback candidates
// moved to workspace_helpers.go

// getCommonEntryPointFiles returns common entry point files based on project type
// moved to workspace_helpers.go

// isSourceFile checks if a file is likely a source code file
// moved to workspace_helpers.go

// moved to editing.go

// buildBasicFileContext is a fallback when workspace.json is not available
// moved to workspace_helpers.go

// executeValidationFixPlan executes the fix plan using the editing model

// findFilesRelatedToErrors uses embeddings to find files that might be related to the validation errors

// getProjectFileTree returns a representation of the project file structure
/*func getProjectFileTree() (string, error) {
	var tree strings.Builder

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue despite errors
		}

		// Skip hidden files and common ignore patterns
		if strings.HasPrefix(filepath.Base(path), ".") && path != "." {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common build/temp directories
		skipDirs := []string{"vendor", "node_modules", "target", "build", "dist"}
		for _, skipDir := range skipDirs {
			if strings.Contains(path, skipDir) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Build tree representation
		depth := strings.Count(path, string(os.PathSeparator))
		indent := strings.Repeat("  ", depth)

		if info.IsDir() {
			tree.WriteString(fmt.Sprintf("%s%s/\n", indent, filepath.Base(path)))
		} else {
			tree.WriteString(fmt.Sprintf("%s%s\n", indent, filepath.Base(path)))
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return tree.String(), nil
}*/

// analyzeBuildErrorsAndCreateFix uses LLM to understand build errors and create targeted fixes
/*func analyzeBuildErrorsAndCreateFix(validationResults []string, originalIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (string, int, error) {
	// Extract the actual error messages from validation results
	var errorMessages []string
	for _, result := range validationResults {
		if strings.HasPrefix(result, "❌") {
			// Remove the ❌ prefix and add to error messages
			errorMsg := strings.TrimPrefix(result, "❌ ")
			errorMessages = append(errorMessages, errorMsg)
		}
	}

	if len(errorMessages) == 0 {
		return "", 0, fmt.Errorf("no error messages found in validation results")
	}

	prompt := fmt.Sprintf(`You are an expert Go developer helping to fix build errors and improve code quality.

ORIGINAL TASK: %s
TASK CATEGORY: %s

BUILD/VALIDATION ERRORS:
%s

PROJECT CONTEXT:
- This project has detected dependencies and module structure
- All import paths must use proper module paths
- Key APIs available:
  * Logger: Available logging functionality for debugging
  * Filesystem: Use appropriate file operations for this project type
  * Follow existing patterns and conventions in the codebase

ANALYSIS INSTRUCTIONS:
1. **Primary Fix**: Analyze the build/validation errors and determine minimal fixes needed
2. **Error Classification**: Are these errors related to the recent changes or pre-existing issues?
3. **Test Assessment**: Based on the original task, determine if tests are needed:
   - For new utility functions (like CreateBackup): suggest unit tests
   - For new features/commands: suggest integration tests
   - For bug fixes: suggest regression tests
4. **Code Quality**: Identify any obvious quality improvements that align with the original task

RESPONSE FORMAT:
Provide a comprehensive fix prompt that addresses:
- Immediate build errors (highest priority)
- Missing tests if appropriate for the task
- Any quality improvements that directly support the original task

Focus on making the code production-ready while maintaining minimal scope.

Create a detailed fix prompt:`,
		originalIntent,
		intentAnalysis.Category,
		strings.Join(errorMessages, "\n"))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert Go developer who excels at diagnosing and fixing build errors. Respond with a clear, actionable fix prompt."},
		{Role: "user", Content: prompt},
	}

	response, _, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get orchestration model analysis of build errors: %w", err)
	}

	// Estimate tokens used
	tokens := utils.EstimateTokens(prompt + response)

	logger.Logf("LLM build error analysis: %s", response)

	return response, tokens, nil
}*/

// ProjectContext represents the detected project characteristics

// ValidationStep represents a single validation action

// ValidationStrategy represents the complete validation approach for a project

// ProjectInfo represents detected project characteristics

// moved out: detectProjectInfo

// getBasicValidationStrategy provides fallback validation when LLM fails

// runValidationStep executes a single validation step

// analyzeValidationResults uses LLM to analyze validation results and decide whether to proceed

// parseValidationDecision extracts the decision from the LLM response

// moved out: printTokenUsageSummary

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

// moved to workspace_discovery.go

// WorkspacePatterns holds analysis of workspace organization patterns

// analyzeWorkspacePatterns analyzes the codebase to understand organizational preferences

// isLargeFileRefactoringTask determines if the task involves refactoring large files

// extractSourceFileFromIntent extracts the main source file from user intent

// analyzeFunctionsInFile analyzes a Go file to extract function and type information

// generateRefactoringStrategy creates a strategy for complex refactoring tasks

// Helper functions for workspace analysis
// moved to workspace_helpers.go

// moved to workspace_helpers.go

// moved to workspace_helpers.go

// min helper function
// moved to workspace_helpers.go

// progress-related functions moved to progress.go

// executeCreatePlan creates a detailed edit plan
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

// moved to editing.go

// moved to shell.go

// moved to validation_exec.go

// executeSimpleValidation performs minimal validation for simple tasks to avoid overhead
// moved to validation_exec.go

// executeRefactoringValidation performs thorough validation for refactoring tasks
// moved to validation_exec.go

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

// executeValidationFailureRecovery analyzes validation failures and creates targeted fixes
// moved to validation_exec.go

// executeEditPlanWithErrorHandling executes edit plan with proper error handling for agent context

// handleErrorEscalation handles errors by using the agent's context to make intelligent decisions
// summarizeContextIfNeeded summarizes agent context if it gets too large
// moved to context_summary.go
