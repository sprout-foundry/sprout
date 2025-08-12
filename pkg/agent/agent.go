// Core agent functionality package
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime" // Import runtime for memory stats
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// isSimpleShellCommand returns true for trivial, safe commands we allow for fast-path execution
func isSimpleShellCommand(s string) bool {
	t := strings.TrimSpace(strings.ToLower(s))
	if t == "" {
		return false
	}
	// Very conservative allowlist patterns
	if strings.HasPrefix(t, "echo ") {
		return true
	}
	if t == "ls" || strings.HasPrefix(t, "ls ") {
		return true
	}
	if strings.HasPrefix(t, "pwd") {
		return true
	}
	if strings.HasPrefix(t, "whoami") {
		return true
	}
	// Basic grep/find read-only searches
	if strings.HasPrefix(t, "grep ") {
		return true
	}
	if strings.HasPrefix(t, "find ") {
		return true
	}
	return false
}

// RunAgentMode is the main public interface for command line usage
func RunAgentMode(userIntent string, skipPrompt bool, model string) error {
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
}

// Execute is the main public interface for running the agent
func Execute(userIntent string, cfg *config.Config, logger *utils.Logger) (*AgentTokenUsage, error) {
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
}

// PrintTokenUsageSummary prints a summary of token usage
func PrintTokenUsageSummary(tokenUsage *AgentTokenUsage, duration time.Duration) {
	printTokenUsageSummary(tokenUsage, duration)
}

// runOptimizedAgent runs the agent with adaptive decision-making and progress evaluation
func runOptimizedAgent(userIntent string, cfg *config.Config, logger *utils.Logger, tokenUsage *AgentTokenUsage) error {
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
}

// Types moved to types.go

// AgentContext maintains state and context throughout agent execution

// ProgressEvaluation represents the agent's assessment of current progress

// IntentAnalysis represents the analysis of user intent

// TaskComplexityLevel represents the complexity level of a task for optimization

// EditOperation represents a single file edit operation

// determineTaskComplexity determines the complexity level for optimization routing
func determineTaskComplexity(intent string, analysis *IntentAnalysis) TaskComplexityLevel {
	intentLower := strings.ToLower(intent)

	// Investigative/search tasks - require tools and should use moderate/complex path
	investigativeKeywords := []string{
		"find", "search", "grep", "list", "show", "check", "analyze", "investigate",
		"look for", "locate", "identify", "discover", "scan", "examine",
		"use grep", "use find", "run command", "execute", "shell",
	}

	for _, keyword := range investigativeKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskModerate // Use moderate to enable tool calling
		}
	}

	// Simple task indicators - use fast path
	simpleKeywords := []string{
		"comment", "add comment", "add a comment", "simple comment",
		"documentation", "docs", "readme", "add doc", "update doc",
		"typo", "fix typo", "spelling", "whitespace", "formatting",
		"rename variable", "rename function", "simple rename",
	}

	for _, keyword := range simpleKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskSimple
		}
	}

	// Check analysis category for simple tasks
	if analysis != nil {
		if analysis.Category == "docs" && analysis.Complexity == "simple" {
			return TaskSimple
		}

		// Complex task indicators - use full orchestration
		if analysis.Complexity == "complex" ||
			analysis.Category == "refactor" ||
			len(analysis.EstimatedFiles) > 3 {
			return TaskComplex
		}
	}

	// Force complex classification for refactoring tasks regardless of LLM analysis
	refactorKeywords := []string{
		"refactor", "restructure", "redesign", "architecture",
		"migrate", "convert", "rewrite", "overhaul",
		"extract", "move code", "split file", "organize code",
	}

	for _, keyword := range refactorKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskComplex // Force complex for refactoring
		}
	}

	// Complex task keywords
	complexKeywords := []string{
		"implement feature", "add feature", "new feature",
		"remove feature", "delete module",
	}

	for _, keyword := range complexKeywords {
		if strings.Contains(intentLower, keyword) {
			return TaskComplex
		}
	}

	// Default to moderate for everything else
	return TaskModerate
} // analyzeIntentWithMinimalContext analyzes user intent with proper workspace context
func analyzeIntentWithMinimalContext(userIntent string, cfg *config.Config, logger *utils.Logger) (*IntentAnalysis, int, error) {
	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: analyzeIntentWithMinimalContext started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// STEP 1: Load workspace file for embeddings (create if it doesn't exist)
	logger.Logf("STEP 1: Loading workspace file...")
	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		if os.IsNotExist(err) {
			logger.Logf("STEP 1: No workspace file found, creating and populating workspace...")
			// Ensure .ledit directory exists
			if err := os.MkdirAll(".ledit", os.ModePerm); err != nil {
				logger.LogError(fmt.Errorf("failed to create .ledit directory: %w", err))
				return nil, 0, fmt.Errorf("failed to create workspace directory: %w", err)
			}
			// Use GetWorkspaceContext to trigger workspace creation and population
			_ = workspace.GetWorkspaceContext("", cfg)
			// Now try to load the workspace file again
			workspaceFile, err = workspace.LoadWorkspaceFile()
			if err != nil {
				logger.LogError(fmt.Errorf("failed to load workspace after creation: %w", err))
				return nil, 0, fmt.Errorf("failed to load workspace after creation: %w", err)
			}
		} else {
			logger.LogError(fmt.Errorf("failed to load workspace file: %w", err))
			return nil, 0, fmt.Errorf("failed to load workspace: %w", err)
		}
	}
	logger.Logf("STEP 1: Successfully loaded workspace with %d files", len(workspaceFile.Files))

	// STEP 1.5: Build basic workspace analysis BEFORE any LLM decisions
	logger.Logf("STEP 1.5: Analyzing workspace structure...")
	workspaceAnalysis, err := buildWorkspaceStructure(logger)
	if err != nil {
		logger.Logf("Warning: Could not build workspace analysis: %v", err)
		workspaceAnalysis = &WorkspaceInfo{
			ProjectType: "other", // Default when detection fails
			AllFiles:    []string{},
		}
	}
	logger.Logf("STEP 1.5: Detected project type: %s with %d files", workspaceAnalysis.ProjectType, len(workspaceAnalysis.AllFiles))

	// STEP 2: Use embeddings to find relevant files
	logger.Logf("STEP 2: Starting embedding search for intent: %s", userIntent)
	logger.Logf("STEP 2: About to call GetFilesForContextUsingEmbeddings...")

	fullContextFiles, summaryContextFiles, err := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFile, cfg, logger)
	logger.Logf("STEP 2: GetFilesForContextUsingEmbeddings returned: full=%d, summary=%d", len(fullContextFiles), len(summaryContextFiles))

	if err != nil {
		logger.LogError(fmt.Errorf("embedding search failed: %w", err))
		// Fallback to basic file listing
		logger.Logf("STEP 2: Falling back to basic file discovery...")
		fullContextFiles, summaryContextFiles = []string{}, []string{}
	}

	// Combine full context and summary files
	relevantFiles := append(fullContextFiles, summaryContextFiles...)
	logger.Logf("Found %d full context files: %v", len(fullContextFiles), fullContextFiles)
	logger.Logf("Found %d summary context files: %v", len(summaryContextFiles), summaryContextFiles)
	logger.Logf("Total relevant files (%d): %v", len(relevantFiles), relevantFiles)

	// STEP 2.5: If embeddings found few/no files, try workspace model rewording
	rewordTokensUsed := 0
	if len(relevantFiles) < 3 {
		logger.Logf("STEP 2.5: Few files found (%d), trying workspace model to reword prompt...", len(relevantFiles))
		rewordedIntent, rewordTokens, rewordErr := rewordPromptForBetterSearch(userIntent, workspaceAnalysis, cfg, logger)
		if rewordErr == nil && rewordedIntent != userIntent {
			logger.Logf("STEP 2.5: Reworded intent: '%s' -> '%s'", userIntent, rewordedIntent)
			// Defer attribution of rewording tokens until after main token calc
			rewordTokensUsed = rewordTokens

			// Try embeddings again with reworded intent
			fullContextFiles2, summaryContextFiles2, err2 := workspace.GetFilesForContextUsingEmbeddings(rewordedIntent, workspaceFile, cfg, logger)
			if err2 == nil && len(fullContextFiles2)+len(summaryContextFiles2) > len(relevantFiles) {
				logger.Logf("STEP 2.5: Reworded search found more files! Using new results.")
				fullContextFiles = fullContextFiles2
				summaryContextFiles = summaryContextFiles2
				relevantFiles = append(fullContextFiles, summaryContextFiles...)
			}
		}
	}

	// STEP 2.7: If still few files, try shell commands to find files
	if len(relevantFiles) < 2 {
		logger.Logf("STEP 2.7: Still few files (%d), trying shell commands to find relevant files...", len(relevantFiles))
		shellFoundFiles := findFilesUsingShellCommands(userIntent, workspaceAnalysis, logger)
		if len(shellFoundFiles) > 0 {
			logger.Logf("STEP 2.7: Shell commands found %d additional files: %v", len(shellFoundFiles), shellFoundFiles)
			relevantFiles = append(relevantFiles, shellFoundFiles...)
		}
	}

	// If still no files found, try content-based search as final fallback
	if len(relevantFiles) == 0 {
		logger.Logf("No files found by embeddings or basic listing, trying content search...")
		relevantFiles = findRelevantFilesByContent(userIntent, logger)
		logger.Logf("Content search found %d files: %v", len(relevantFiles), relevantFiles)
	}

	// Final safety net - ensure we always have some files to analyze
	if len(relevantFiles) == 0 {
		logger.Logf("WARNING: No relevant files found by any method! Using fallback files...")
		// Get recently modified files as fallback candidates
		candidateFiles := getRecentlyModifiedSourceFiles(workspaceAnalysis, logger)
		if len(candidateFiles) == 0 {
			// Final fallback: get common entry points based on project type
			candidateFiles = getCommonEntryPointFiles(workspaceAnalysis.ProjectType, logger)
		}

		for _, file := range candidateFiles {
			if _, err := os.Stat(file); err == nil {
				relevantFiles = append(relevantFiles, file)
			}
		}
		logger.Logf("Fallback selected %d files: %v", len(relevantFiles), relevantFiles)
	}

	prompt := BuildIntentAnalysisPrompt(userIntent, workspaceAnalysis.ProjectType, relevantFiles)
	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at analyzing programming tasks. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}
	response, _, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 60*time.Second)
	if err != nil {
		logger.LogError(fmt.Errorf("orchestration model failed to analyze intent: %w", err))
		// Use fallback analysis since LLM failed
		logger.Logf("Using fallback heuristic analysis due to orchestration model failure")
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: analyzeIntentWithMinimalContext completed (LLM error, fallback). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  []string{}, // Don't try to infer files in fallback
			RequiresContext: true,
		}, 0, nil // No tokens used if LLM failed
	}

	// Estimate tokens used for intent analysis
	promptTokens := utils.EstimateTokens(messages[0].Content.(string) + " " + messages[1].Content.(string))
	responseTokens := utils.EstimateTokens(response)
	totalTokens := promptTokens + responseTokens
	// Note: we computed splits locally (promptTokens+rewordTokensUsed, responseTokens) but only return totals here.
	totalTokens += rewordTokensUsed

	// Clean response and parse JSON using centralized utility
	cleanedResponse, err := utils.ExtractJSONFromLLMResponse(response)
	if err != nil {
		logger.LogError(fmt.Errorf("CRITICAL: Failed to extract JSON from intent analysis response: %w\nRaw response: %s", err, response))
		// Use fallback analysis since JSON extraction failed
		logger.Logf("Using fallback heuristic analysis due to JSON extraction failure")
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: analyzeIntentWithMinimalContext completed (JSON extraction error, fallback). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  []string{}, // Don't try to infer files in fallback
			RequiresContext: true,
		}, totalTokens, nil
	}

	var analysis IntentAnalysis
	if err := json.Unmarshal([]byte(cleanedResponse), &analysis); err != nil {
		// JSON parsing failure is an unrecoverable error - the LLM should always return valid JSON
		logger.LogError(fmt.Errorf("CRITICAL: Failed to parse intent analysis JSON from LLM: %w\nCleaned JSON: %s\nRaw response: %s", err, cleanedResponse, response))
		return nil, 0, fmt.Errorf("unrecoverable JSON parsing error in intent analysis: %w\nCleaned JSON: %s\nRaw Response: %s", err, cleanedResponse, response)
	}

	// Debug: log the parsed analysis
	logger.Logf("Parsed analysis - Category: %s, Complexity: %s, Files: %v", analysis.Category, analysis.Complexity, analysis.EstimatedFiles)

	// If LLM didn't provide files, fall back to embedding-based search
	if len(analysis.EstimatedFiles) == 0 {
		// Try embeddings first, fall back to content search if embeddings fail
		workspaceFileData, embErr := workspace.LoadWorkspaceFile()
		if embErr == nil {
			fullContextFiles, summaryContextFiles, embErr := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFileData, cfg, logger)
			embeddingFiles := append(fullContextFiles, summaryContextFiles...)

			if embErr != nil || len(embeddingFiles) == 0 {
				logger.Logf("Embedding search failed or returned no results, falling back to content search: %v", embErr)
				analysis.EstimatedFiles = findRelevantFilesByContent(userIntent, logger)
				logger.Logf("LLM provided no files, using content-based search: %v", analysis.EstimatedFiles)
			} else {
				analysis.EstimatedFiles = embeddingFiles
				logger.Logf("LLM provided no files, using embedding-based search: %v", analysis.EstimatedFiles)
			}
		} else {
			logger.Logf("Could not load workspace file, falling back to content search: %v", embErr)
			analysis.EstimatedFiles = findRelevantFilesByContent(userIntent, logger)
			logger.Logf("LLM provided no files, using content-based search: %v", analysis.EstimatedFiles)
		}
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: analyzeIntentWithMinimalContext completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
	return &analysis, totalTokens, nil
}

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
func findRelevantFilesByContent(userIntent string, logger *utils.Logger) []string {
	intentLower := strings.ToLower(userIntent)

	// Extract key terms from the intent
	searchTerms := extractSearchTerms(intentLower)
	if len(searchTerms) == 0 {
		logger.Logf("No search terms extracted from intent, returning empty list")
		return []string{} // Return empty instead of using project-specific inference
	}

	logger.Logf("Searching for files containing terms: %v", searchTerms)

	// Search for files containing these terms
	relevantFiles := make(map[string]int) // file -> relevance score

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue walking despite errors
		}

		// Skip non-source files and directories
		if info.IsDir() || !isSourceFile(path) {
			return nil
		}

		// Skip hidden files and common ignore patterns
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return nil // Continue despite read errors
		}

		contentLower := strings.ToLower(string(content))
		score := 0

		// Score based on search terms found in content
		for _, term := range searchTerms {
			if strings.Contains(contentLower, term) {
				score += 10
				// Bonus for terms in function names, types, etc.
				if strings.Contains(contentLower, "func "+term) ||
					strings.Contains(contentLower, "type "+term) ||
					strings.Contains(contentLower, term+"(") {
					score += 20
				}
			}
		}

		// Bonus for file path relevance
		pathLower := strings.ToLower(path)
		for _, term := range searchTerms {
			if strings.Contains(pathLower, term) {
				score += 15
			}
		}

		if score > 0 {
			relevantFiles[path] = score
			logger.Logf("Found relevant file: %s (score: %d)", path, score)
		}

		return nil
	})

	if err != nil {
		logger.LogError(fmt.Errorf("error walking directory for content search: %w", err))
		return []string{} // Return empty instead of using project-specific inference
	}

	// Sort files by relevance score
	type fileScore struct {
		path  string
		score int
	}

	var scored []fileScore
	for file, score := range relevantFiles {
		scored = append(scored, fileScore{file, score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Return top 5 most relevant files
	var result []string
	maxFiles := 5
	for i, fs := range scored {
		if i >= maxFiles {
			break
		}
		result = append(result, fs.path)
	}

	if len(result) == 0 {
		logger.Logf("No files found by content search")
		return []string{} // Return empty instead of using project-specific inference
	}

	logger.Logf("Content search found %d relevant files: %v", len(result), result)
	return result
}

// WorkspaceInfo represents comprehensive workspace structure

// buildWorkspaceStructure creates comprehensive workspace analysis
func buildWorkspaceStructure(logger *utils.Logger) (*WorkspaceInfo, error) {
	logger.Logf("Building comprehensive workspace structure...")

	info := &WorkspaceInfo{
		FilesByDir:    make(map[string][]string),
		RelevantFiles: make(map[string]string),
	}

	// Detect project type based on files present
	if _, err := os.Stat("go.mod"); err == nil {
		info.ProjectType = "go"
	} else if _, err := os.Stat("package.json"); err == nil {
		info.ProjectType = "javascript"
	} else if _, err := os.Stat("requirements.txt"); err == nil || hasFile("setup.py") {
		info.ProjectType = "python"
	} else if _, err := os.Stat("Cargo.toml"); err == nil {
		info.ProjectType = "rust"
	} else if _, err := os.Stat("pom.xml"); err == nil {
		info.ProjectType = "java"
	} else {
		info.ProjectType = "other"
	}

	logger.Logf("Detected project type: %s", info.ProjectType)

	// Walk directory structure
	err := filepath.Walk(".", func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue despite errors
		}

		// Skip hidden and common ignore directories
		if strings.HasPrefix(filepath.Base(path), ".") ||
			strings.Contains(path, "node_modules") ||
			strings.Contains(path, "vendor") ||
			strings.Contains(path, "__pycache__") {
			if fileInfo.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !fileInfo.IsDir() && isSourceFile(path) {
			dir := filepath.Dir(path)
			info.AllFiles = append(info.AllFiles, path)
			info.FilesByDir[dir] = append(info.FilesByDir[dir], path)

			// Add root files
			if dir == "." {
				info.RootFiles = append(info.RootFiles, path)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	logger.Logf("Found %d source files across %d directories", len(info.AllFiles), len(info.FilesByDir))
	return info, nil
}

// extractSearchTerms extracts key search terms from user intent
func extractSearchTerms(intentLower string) []string {
	var terms []string

	// Direct keyword extraction from intent
	words := strings.Fields(intentLower)
	for _, word := range words {
		// Clean punctuation
		word = strings.Trim(word, ".,!?;:")

		// Include relevant technical terms
		if len(word) > 3 && (strings.Contains(word, "orchestration") ||
			strings.Contains(word, "editing") ||
			strings.Contains(word, "model") ||
			strings.Contains(word, "review") ||
			strings.Contains(word, "code") ||
			strings.Contains(word, "llm") ||
			strings.Contains(word, "api") ||
			strings.Contains(word, "config") ||
			strings.Contains(word, "prompt") ||
			strings.Contains(word, "editor") ||
			strings.Contains(word, "embedding")) {
			terms = append(terms, word)
		}
	}

	// Add compound terms that might be written as one word
	if strings.Contains(intentLower, "codereviews") || strings.Contains(intentLower, "codereview") {
		terms = append(terms, "review", "code", "getcodereview")
	}

	if strings.Contains(intentLower, "orchestration model") {
		terms = append(terms, "orchestration", "model")
	}

	if strings.Contains(intentLower, "editing model") {
		terms = append(terms, "editing", "model", "editor")
	}

	// Remove duplicates
	uniqueTerms := make(map[string]bool)
	var result []string
	for _, term := range terms {
		if !uniqueTerms[term] && len(term) > 2 {
			uniqueTerms[term] = true
			result = append(result, term)
		}
	}

	return result
} // getBasicFileListing returns a simple list of files without full analysis
func getBasicFileListing(logger *utils.Logger) ([]string, error) {
	// This is a simplified version that just lists files without full workspace analysis
	var files []string

	// Walk the current directory to get file paths
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Logf("Error walking path %s: %v", path, err)
			return err
		}

		// Skip hidden directories and files
		if strings.HasPrefix(filepath.Base(path), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common directories to ignore
		skipDirs := []string{"node_modules", "vendor", "target", "build", "dist", "__pycache__"}
		for _, skip := range skipDirs {
			if strings.Contains(path, skip) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Only include source files
		if !info.IsDir() && isSourceFile(path) {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// getRecentlyModifiedSourceFiles returns a list of recently modified source files as fallback candidates
// moved to workspace_helpers.go
/*func getRecentlyModifiedSourceFiles(workspaceInfo *WorkspaceInfo, logger *utils.Logger) []string {
	if len(workspaceInfo.AllFiles) == 0 {
		return []string{}
	}

	// Sort files by modification time and return the 5 most recent
	type fileInfo struct {
		path    string
		modTime time.Time
	}

	var files []fileInfo
	for _, file := range workspaceInfo.AllFiles {
		if stat, err := os.Stat(file); err == nil {
			files = append(files, fileInfo{path: file, modTime: stat.ModTime()})
		}
	}

	// Sort by modification time (most recent first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	// Return up to 5 most recent files
	var result []string
	for i, file := range files {
		if i >= 5 {
			break
		}
		result = append(result, file.path)
	}

	return result
}*/

// getCommonEntryPointFiles returns common entry point files based on project type
// moved to workspace_helpers.go
/*func getCommonEntryPointFiles(projectType string, logger *utils.Logger) []string {
	switch projectType {
	case "go":
		return []string{"main.go", "cmd/main.go", "app/main.go"}
	case "javascript":
		return []string{"index.js", "app.js", "server.js", "src/index.js"}
	case "python":
		return []string{"main.py", "app.py", "__init__.py", "src/main.py"}
	case "java":
		return []string{"Main.java", "App.java", "src/main/java/Main.java"}
	case "rust":
		return []string{"main.rs", "lib.rs", "src/main.rs", "src/lib.rs"}
	default:
		// Generic fallback for unknown project types
		return []string{"README.md", "index.*", "main.*", "app.*"}
	}
}*/

// isSourceFile checks if a file is likely a source code file
// moved to workspace_helpers.go
/*func isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	sourceExts := []string{".go", ".py", ".js", ".ts", ".java", ".c", ".cpp", ".rs", ".rb", ".php", ".scala", ".kt"}

	for _, sourceExt := range sourceExts {
		if ext == sourceExt {
			return true
		}
	}
	return false
}*/

// shouldUsePartialEdit determines whether to use partial editing or full file editing
// based on the operation characteristics and file size
// shouldUsePartialEdit determines whether to use partial editing or full file editing
// based on the operation characteristics and file size
// THIS IS NOT READY TO USE, NEEDS IMPROVEMENTS
func shouldUsePartialEdit(operation EditOperation, logger *utils.Logger) bool {
	// Check if file exists and get its size
	fileInfo, err := os.Stat(operation.FilePath)
	if err != nil {
		logger.Logf("Cannot stat file %s, using full file edit: %v", operation.FilePath, err)
		return false
	}

	// For very small files (< 1KB), partial editing overhead isn't worth it
	if fileInfo.Size() < 1024 {
		logger.Logf("File %s is small (%d bytes), using full file edit", operation.FilePath, fileInfo.Size())
		return false
	}

	// For very large files (> 50KB), partial editing is more efficient
	if fileInfo.Size() > 50*1024 {
		logger.Logf("File %s is large (%d bytes), using partial edit", operation.FilePath, fileInfo.Size())
		return true
	}

	// For medium files, check if the operation seems focused/targeted
	instructionsLower := strings.ToLower(operation.Instructions)
	description := strings.ToLower(operation.Description)

	// Keywords that suggest focused changes suitable for partial editing
	focusedKeywords := []string{
		"function", "method", "struct", "type", "variable",
		"add", "modify", "update", "change", "fix",
		"import", "constant", "field",
	}

	// Keywords that suggest broad changes requiring full file context
	broadKeywords := []string{
		"refactor", "restructure", "rewrite", "reorganize",
		"architecture", "design pattern", "interface",
		"multiple", "throughout", "entire",
	}

	focusedScore := 0
	broadScore := 0

	for _, keyword := range focusedKeywords {
		if strings.Contains(instructionsLower, keyword) || strings.Contains(description, keyword) {
			focusedScore++
		}
	}

	for _, keyword := range broadKeywords {
		if strings.Contains(instructionsLower, keyword) || strings.Contains(description, keyword) {
			broadScore++
		}
	}

	// If it seems focused, use partial editing
	if focusedScore > broadScore {
		logger.Logf("Operation seems focused (score: %d vs %d), using partial edit", focusedScore, broadScore)
		return true
	}

	// Default to full file editing for ambiguous cases
	logger.Logf("Operation seems broad or ambiguous (score: %d vs %d), using full file edit", focusedScore, broadScore)
	return false
}

// buildFocusedEditInstructions creates targeted instructions for a single file edit
// The orchestration model should provide self-contained instructions with hashtag file references
func buildFocusedEditInstructions(operation EditOperation, logger *utils.Logger) string {
	// Log inputs for debugging
	logger.LogProcessStep("🔧 BUILDING EDIT INSTRUCTIONS:")
	logger.LogProcessStep(fmt.Sprintf("Operation: %s", operation.Description))
	logger.LogProcessStep(fmt.Sprintf("Target File: %s", operation.FilePath))
	logger.LogProcessStep(fmt.Sprintf("Scope Justification: %s", operation.ScopeJustification))

	var instructions strings.Builder

	// Start with the specific operation instructions
	instructions.WriteString(fmt.Sprintf("Task: %s\n\n", operation.Instructions))

	// Add file-specific context
	instructions.WriteString(fmt.Sprintf("Target File: %s\n\n", operation.FilePath))

	// Add scope constraint
	instructions.WriteString(fmt.Sprintf("SCOPE REQUIREMENT: %s\n\n", operation.ScopeJustification))

	// Add focused guidance for fast editing model
	instructions.WriteString(`CRITICAL EDITING CONSTRAINTS:
- Make ONLY the changes specified in the task - NO ADDITIONAL IMPROVEMENTS
- Do NOT add features, optimizations, or enhancements not explicitly requested  
- Do NOT refactor code unless that was the specific request
- Do NOT fix unrelated issues or add "nice to have" changes
- STAY STRICTLY within the scope defined above
- Make TARGETED, PRECISE edits to achieve the specified goal
- Follow existing code patterns and conventions in the file
- Preserve all existing functionality unless explicitly changing it
- Focus only on the requested change, don't make unrelated improvements
- Ensure the change integrates naturally with the existing code

`)

	// The orchestration model should have provided self-contained instructions
	instructions.WriteString("Please implement the requested change efficiently and precisely.\n")

	// Log the full context being sent to the LLM for debugging
	fullInstructions := instructions.String()
	logger.LogProcessStep("📋 FULL INSTRUCTIONS SENT TO EDITING MODEL:")
	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.LogProcessStep(fmt.Sprintf("Target File: %s", operation.FilePath))
	logger.LogProcessStep(fmt.Sprintf("Instructions Size: %d characters", len(fullInstructions)))
	logger.LogProcessStep("Self-contained: Using hashtag file references for context")

	// Check if instructions contain hashtag references
	if strings.Contains(operation.Instructions, "#") {
		logger.LogProcessStep("✅ Instructions contain hashtag file references - context will be loaded automatically")
	} else {
		logger.LogProcessStep("ℹ️  No hashtag file references found - instructions should be self-contained")
	}

	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return fullInstructions
}

// buildBasicFileContext is a fallback when workspace.json is not available
// moved to workspace_helpers.go
/*func buildBasicFileContext(contextFiles []string, logger *utils.Logger) string {
	var context strings.Builder
	context.WriteString("Relevant Files (Basic Context):\n")

	for _, file := range contextFiles {
		if content, err := os.ReadFile(file); err == nil {
			// Limit content size to reduce token usage
			contentStr := string(content)
			if len(contentStr) > 5000 { // Limit to ~5KB per file
				contentStr = contentStr[:5000] + "\n... (content truncated)"
			}

			context.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", file, contentStr))
		} else {
			logger.Logf("Could not read file %s for context: %v", file, err)
		}
	}

	return context.String()
}*/

// executeValidationFixPlan executes the fix plan using the editing model

// findFilesRelatedToErrors uses embeddings to find files that might be related to the validation errors

// getProjectFileTree returns a representation of the project file structure
func getProjectFileTree() (string, error) {
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
}

// analyzeBuildErrorsAndCreateFix uses LLM to understand build errors and create targeted fixes
func analyzeBuildErrorsAndCreateFix(validationResults []string, originalIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (string, int, error) {
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
}

// ProjectContext represents the detected project characteristics

// ValidationStep represents a single validation action

// ValidationStrategy represents the complete validation approach for a project

// ProjectInfo represents detected project characteristics

// detectProjectInfo gathers basic project information for LLM analysis
func detectProjectInfo(logger *utils.Logger) ProjectInfo {
	info := ProjectInfo{}

	// Check for common project files
	commonFiles := []string{"go.mod", "package.json", "requirements.txt", "pyproject.toml", "Makefile", "Dockerfile", "README.md"}

	for _, file := range commonFiles {
		if hasFile(file) {
			info.AvailableFiles = append(info.AvailableFiles, file)
			switch file {
			case "go.mod":
				info.HasGoMod = true
			case "package.json":
				info.HasPackageJSON = true
			case "requirements.txt":
				info.HasRequirements = true
			case "Makefile":
				info.HasMakefile = true
			}
		}
	}

	// Add some source files to give context
	if files, err := getBasicFileListing(logger); err == nil && len(files) > 0 {
		// Add up to 5 source files as examples
		count := 0
		for _, file := range files {
			if count >= 5 {
				break
			}
			info.AvailableFiles = append(info.AvailableFiles, file)
			count++
		}
	}

	return info
}

// getBasicValidationStrategy provides fallback validation when LLM fails

// runValidationStep executes a single validation step

// analyzeValidationResults uses LLM to analyze validation results and decide whether to proceed

// parseValidationDecision extracts the decision from the LLM response

// printTokenUsageSummary prints a summary of token usage for the agent execution
func printTokenUsageSummary(tokenUsage *AgentTokenUsage, duration time.Duration) {
	fmt.Printf("\n💰 Token Usage Summary:\n")
	fmt.Printf("├─ Intent Analysis: %d tokens\n", tokenUsage.IntentAnalysis)
	fmt.Printf("├─ Planning (Orchestration): %d tokens\n", tokenUsage.Planning)
	fmt.Printf("├─ Code Generation (Editing): %d tokens\n", tokenUsage.CodeGeneration)
	fmt.Printf("├─ Validation: %d tokens\n", tokenUsage.Validation)
	fmt.Printf("├─ Progress Evaluation: %d tokens\n", tokenUsage.ProgressEvaluation)

	// Calculate total if not already set
	if tokenUsage.Total == 0 {
		tokenUsage.Total = tokenUsage.IntentAnalysis + tokenUsage.Planning + tokenUsage.CodeGeneration +
			tokenUsage.Validation + tokenUsage.ProgressEvaluation
	}

	fmt.Printf("└─ Total Usage: %d tokens\n", tokenUsage.Total)

	// Cost is printed at the end of Execute where we know the models used; avoid rough estimate here

	// Performance metrics
	tokensPerSecond := float64(tokenUsage.Total) / duration.Seconds()
	fmt.Printf("⚡ Performance: %.1f tokens/second\n", tokensPerSecond)
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

// findFilesUsingShellCommands uses shell commands to find relevant files when other methods fail
func findFilesUsingShellCommands(userIntent string, workspaceInfo *WorkspaceInfo, logger *utils.Logger) []string {
	logger.Logf("Using shell commands to find files for: %s", userIntent)

	var foundFiles []string
	intentLower := strings.ToLower(userIntent)

	// Extract search terms from intent
	searchTerms := extractSearchTerms(intentLower)
	logger.Logf("Shell search terms: %v", searchTerms)

	for _, term := range searchTerms {
		if len(term) < 3 {
			continue // Skip very short terms
		}

		// Use grep to find files containing the term
		logger.Logf("Searching for files containing: %s", term)
		cmd := exec.Command("grep", "-r", "-l", "-i", term, "--include=*.go", ".")
		output, err := cmd.Output()

		if err != nil {
			logger.Logf("Grep search for '%s' failed: %v", term, err)
			continue
		}

		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line != "" && strings.HasSuffix(line, ".go") {
				cleanPath := strings.TrimPrefix(line, "./")
				foundFiles = append(foundFiles, cleanPath)
				logger.Logf("Shell search found: %s (contains '%s')", cleanPath, term)
			}
		}
	}

	// Remove duplicates and limit results
	seen := make(map[string]bool)
	var unique []string
	for _, file := range foundFiles {
		if !seen[file] && len(unique) < 5 { // Limit to 5 files
			seen[file] = true
			unique = append(unique, file)
		}
	}

	// If no files found with content search, try filename search
	if len(unique) == 0 {
		logger.Logf("No content matches, trying filename search...")
		for _, term := range searchTerms {
			cmd := exec.Command("find", ".", "-name", "*.go", "-path", fmt.Sprintf("*%s*", term))
			output, err := cmd.Output()

			if err != nil {
				continue
			}

			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			for _, line := range lines {
				if line != "" && strings.HasSuffix(line, ".go") {
					cleanPath := strings.TrimPrefix(line, "./")
					if !seen[cleanPath] && len(unique) < 5 {
						seen[cleanPath] = true
						unique = append(unique, cleanPath)
						logger.Logf("Filename search found: %s", cleanPath)
					}
				}
			}
		}
	}

	logger.Logf("Shell commands found %d unique files: %v", len(unique), unique)
	return unique
}

// WorkspacePatterns holds analysis of workspace organization patterns

// analyzeWorkspacePatterns analyzes the codebase to understand organizational preferences
/*func analyzeWorkspacePatterns(logger *utils.Logger) *WorkspacePatterns {
	patterns := &WorkspacePatterns{
		AverageFileSize:    0,
		ModularityLevel:    "medium",
		GoSpecificPatterns: make(map[string]string),
	}

	// Analyze Go files in the workspace
	goFiles, err := findGoFiles(".")
	if err != nil {
		logger.Logf("Warning: Could not analyze workspace patterns: %v", err)
		// Set sensible defaults based on detected project type
		patterns.AverageFileSize = 200
		patterns.PreferredPackageSize = 500
		patterns.ModularityLevel = "high"
		patterns.GoSpecificPatterns["file_organization"] = "prefer_small_focused_files"
		patterns.GoSpecificPatterns["package_structure"] = "pkg_separation"
		return patterns
	}

	totalLines := 0
	largeFiles := 0

	for _, file := range goFiles {
		lines := countLines(file)
		totalLines += lines

		if lines > 500 {
			largeFiles++
		}
	}

	if len(goFiles) > 0 {
		patterns.AverageFileSize = totalLines / len(goFiles)
	}

	// Determine modularity preference based on file sizes
	if largeFiles > len(goFiles)/3 {
		patterns.ModularityLevel = "low"
		patterns.GoSpecificPatterns["refactoring_preference"] = "break_large_files"
	} else {
		patterns.ModularityLevel = "high"
		patterns.GoSpecificPatterns["refactoring_preference"] = "maintain_separation"
	}

	// Analyze package structure
	pkgDirs := findPackageDirectories(".")
	if len(pkgDirs) > 3 {
		patterns.GoSpecificPatterns["package_structure"] = "highly_modular"
	} else {
		patterns.GoSpecificPatterns["package_structure"] = "simple_structure"
	}

	patterns.PreferredPackageSize = patterns.AverageFileSize * 3 // Prefer packages with 3 average-sized files

	logger.Logf("Workspace Analysis: Avg file size: %d, Modularity: %s, Large files: %d/%d",
		patterns.AverageFileSize, patterns.ModularityLevel, largeFiles, len(goFiles))

	return patterns
}*/

// isLargeFileRefactoringTask determines if the task involves refactoring large files
/*func isLargeFileRefactoringTask(userIntent string, contextFiles []string, logger *utils.Logger) bool {
	intentLower := strings.ToLower(userIntent)

	// Check for refactoring keywords
	refactoringKeywords := []string{"refactor", "split", "break down", "reorganize", "move", "extract"}
	hasRefactoringIntent := false
	for _, keyword := range refactoringKeywords {
		if strings.Contains(intentLower, keyword) {
			hasRefactoringIntent = true
			break
		}
	}

	if !hasRefactoringIntent {
		return false
	}

	// Check if any of the context files are large
	for _, file := range contextFiles {
		if lines := countLines(file); lines > 1000 {
			logger.Logf("Detected large file refactoring task: %s has %d lines", file, lines)
			return true
		}
	}

	return false
}*/

// extractSourceFileFromIntent extracts the main source file from user intent
/*func extractSourceFileFromIntent(userIntent string, contextFiles []string) string {
	intentLower := strings.ToLower(userIntent)

	// Look for explicit file mentions
	if strings.Contains(intentLower, "cmd/agent.go") {
		return "cmd/agent.go"
	}

	// Look for large files in context
	for _, file := range contextFiles {
		if countLines(file) > 1000 {
			return file
		}
	}

	return ""
}*/

// analyzeFunctionsInFile analyzes a Go file to extract function and type information
/*func analyzeFunctionsInFile(filePath string, logger *utils.Logger) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		logger.Logf("Could not read file %s for function analysis: %v", filePath, err)
		return "Could not analyze functions in source file"
	}

	lines := strings.Split(string(content), "\n")
	var functions []string
	var types []string
	var structs []string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Find function definitions
		if strings.HasPrefix(trimmed, "func ") && !strings.Contains(trimmed, "//") {
			// Extract function name
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				funcName := parts[1]
				if idx := strings.Index(funcName, "("); idx > 0 {
					funcName = funcName[:idx]
				}
				functions = append(functions, fmt.Sprintf("Line %d: func %s", i+1, funcName))
			}
		}

		// Find type definitions
		if strings.HasPrefix(trimmed, "type ") && !strings.Contains(trimmed, "//") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 {
				typeName := parts[1]
				typeKind := parts[2]
				if typeKind == "struct" {
					structs = append(structs, fmt.Sprintf("Line %d: type %s struct", i+1, typeName))
				} else {
					types = append(types, fmt.Sprintf("Line %d: type %s %s", i+1, typeName, typeKind))
				}
			}
		}
	}

	var result []string
	if len(functions) > 0 {
		result = append(result, fmt.Sprintf("FUNCTIONS (%d found):", len(functions)))
		// Limit to first 20 functions to avoid overwhelming the prompt
		limit := len(functions)
		if limit > 20 {
			limit = 20
		}
		for i := 0; i < limit; i++ {
			result = append(result, "- "+functions[i])
		}
		if len(functions) > 20 {
			result = append(result, fmt.Sprintf("... and %d more functions", len(functions)-20))
		}
	}

	if len(structs) > 0 {
		result = append(result, fmt.Sprintf("\nSTRUCTS (%d found):", len(structs)))
		for _, s := range structs {
			result = append(result, "- "+s)
		}
	}

	if len(types) > 0 {
		result = append(result, fmt.Sprintf("\nTYPES (%d found):", len(types)))
		for _, t := range types {
			result = append(result, "- "+t)
		}
	}

	if len(result) == 0 {
		return "No functions, types, or structs found for extraction"
	}

	return strings.Join(result, "\n")
}*/

// generateRefactoringStrategy creates a strategy for complex refactoring tasks
/*func generateRefactoringStrategy(userIntent string, contextFiles []string, patterns *WorkspacePatterns, logger *utils.Logger) string {
	strategy := []string{
		"INTELLIGENT REFACTORING STRATEGY:",
		fmt.Sprintf("- Workspace prefers files with ~%d lines (current average)", patterns.AverageFileSize),
		fmt.Sprintf("- Modularity level: %s", patterns.ModularityLevel),
	}

	// Analyze the target files
	for _, file := range contextFiles {
		lines := countLines(file)
		if lines > 1000 {
			strategy = append(strategy, fmt.Sprintf("- File %s (%d lines) should be broken into ~%d smaller files",
				file, lines, (lines/patterns.PreferredPackageSize)+1))
		}
	}

	// Add Go-specific guidance
	strategy = append(strategy, []string{
		"",
		"GO BEST PRACTICES FOR REFACTORING:",
		"1. Group related types and functions into logical packages",
		"2. Separate interfaces from implementations",
		"3. Create focused files: types.go, handlers.go, utils.go, etc.",
		"4. Maintain clear import dependencies",
		"5. Use meaningful package and file names",
		"",
		"EXECUTION APPROACH:",
		"- Create step-by-step plan with dependency order",
		"- Move types first, then interfaces, then implementations",
		"- Update imports in dependent files",
		"- Verify compilation after each major step",
	}...)

	return strings.Join(strategy, "\n")
}*/

// Helper functions for workspace analysis
// moved to workspace_helpers.go
/*func findGoFiles(dir string) ([]string, error) {
	var goFiles []string

	cmd := exec.Command("find", dir, "-name", "*.go", "-type", "f")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line != "" && !strings.Contains(line, "vendor/") && !strings.Contains(line, ".git/") {
			goFiles = append(goFiles, strings.TrimPrefix(line, "./"))
		}
	}

	return goFiles, nil
}*/

// moved to workspace_helpers.go
/*func countLines(filePath string) int {
	cmd := exec.Command("wc", "-l", filePath)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	parts := strings.Fields(string(output))
	if len(parts) > 0 {
		if lines, err := strconv.Atoi(parts[0]); err == nil {
			return lines
		}
	}

	return 0
}*/

// moved to workspace_helpers.go
/*func findPackageDirectories(dir string) []string {
	var pkgDirs []string

	cmd := exec.Command("find", dir, "-name", "*.go", "-type", "f", "-exec", "dirname", "{}", ";")
	output, err := cmd.Output()
	if err != nil {
		return pkgDirs
	}

	seen := make(map[string]bool)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		dir := strings.TrimPrefix(line, "./")
		if dir != "" && !seen[dir] && !strings.Contains(dir, "vendor/") && !strings.Contains(dir, ".git/") {
			seen[dir] = true
			pkgDirs = append(pkgDirs, dir)
		}
	}

	return pkgDirs
}*/

// min helper function
// moved to workspace_helpers.go
/*func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}*/

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
/*func executeEditPlanWithErrorHandling(editPlan *EditPlan, context *AgentContext) (int, error) {
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
	} // Update agent context with results
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

	// Log execution summary
	context.Logger.LogProcessStep(fmt.Sprintf("📊 Edit execution summary: %d successful, %d failed out of %d total operations",
		successCount, failureCount, len(editPlan.EditOperations)))

	// If all operations failed, return an error
	if failureCount > 0 && successCount == 0 {
		return totalTokens, fmt.Errorf("all %d edit operations failed", failureCount)
	}

	// If some operations failed but some succeeded, don't fail the entire execution
	// The agent will evaluate this in the next iteration and decide what to do
	if hasFailures {
		context.Logger.LogProcessStep("⚠️ Some edit operations failed, but continuing with partial success")
	}

	return totalTokens, nil
}*/

// handleErrorEscalation handles errors by using the agent's context to make intelligent decisions
// summarizeContextIfNeeded summarizes agent context if it gets too large
func summarizeContextIfNeeded(context *AgentContext) error {
	const maxOperations = 20
	const maxErrors = 10
	const maxValidationResults = 10

	// Check if summarization is needed
	needsSummary := len(context.ExecutedOperations) > maxOperations ||
		len(context.Errors) > maxErrors ||
		len(context.ValidationResults) > maxValidationResults

	if !needsSummary {
		return nil
	}

	context.Logger.LogProcessStep("📝 Summarizing agent context to prevent overflow...")

	// Build summarization prompt
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
		// Fallback: just truncate the arrays with bounds checking
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

	// Parse the summary and replace the context
	var summary struct {
		OperationsSummary string   `json:"operations_summary"`
		ErrorsSummary     string   `json:"errors_summary"`
		ValidationSummary string   `json:"validation_summary"`
		KeyAchievements   []string `json:"key_achievements"`
		CriticalIssues    []string `json:"critical_issues"`
	}

	err = json.Unmarshal([]byte(response), &summary)
	if err != nil {
		context.Logger.Logf("Failed to parse summary JSON: %v", err)
		// Fallback: simple truncation with bounds checking
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

	// Replace context arrays with summarized versions
	// First, preserve critical state indicators before replacement
	originalOps := make([]string, len(context.ExecutedOperations))
	copy(originalOps, context.ExecutedOperations)

	context.ExecutedOperations = []string{
		"=== SUMMARIZED OPERATIONS ===",
		summary.OperationsSummary,
		"=== KEY ACHIEVEMENTS ===",
	}
	context.ExecutedOperations = append(context.ExecutedOperations, summary.KeyAchievements...)

	// Preserve critical execution state indicators for evaluation logic
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

	// Preserve important context: add plan status to operations if plan exists
	if context.CurrentPlan != nil {
		context.ExecutedOperations = append(context.ExecutedOperations,
			fmt.Sprintf("ACTIVE PLAN: %d files to edit, %d operations (%s)",
				len(context.CurrentPlan.FilesToEdit), len(context.CurrentPlan.EditOperations), context.CurrentPlan.Context))
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
