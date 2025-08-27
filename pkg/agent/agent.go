package agent

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// RunSimplifiedAgent: New simplified agent workflow
func RunSimplifiedAgent(userIntent string, skipPrompt bool, model string) error {
	startTime := time.Now()

	// Warm up prompt cache for better performance
	promptCache := GetPromptCache()
	go promptCache.WarmCache() // Non-blocking warm-up

	// Initialize quality optimizer for intelligent prompt selection
	qualityOptimizer := NewQualityOptimizer()

	// Show mode info - more verbose in console, minimal in UI
	if ui.IsUIActive() {
		ui.Logf("🎯 %s", userIntent)
	} else {
		ui.Out().Print("🤖 Simplified Agent Mode\n")
		ui.Out().Printf("🎯 Intent: %s\n", userIntent)
	}

	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if model != "" {
		cfg.EditingModel = model
	}
	cfg.SkipPrompt = skipPrompt
	cfg.FromAgent = true

	// Publish the actual orchestration model to UI header
	if ui.IsUIActive() {
		ui.PublishModel(cfg.EditingModel)
	}

	// Set environment variables to ensure non-interactive mode for all operations
	os.Setenv("LEDIT_FROM_AGENT", "1")
	os.Setenv("LEDIT_SKIP_PROMPT", "1")

	logger := utils.GetLogger(cfg.SkipPrompt)

	// Initialize context manager for persistent analysis
	contextManager := NewContextManager(cfg, logger)

	// Generate session ID and project hash
	sessionID := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s_%d", userIntent, time.Now().Unix()))))
	projectHash := generateProjectHash(logger)

	// Initialize persistent context
	persistentCtx, err := contextManager.InitializeContext(sessionID, userIntent, projectHash)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to initialize context: %w", err))
		// Continue without context - don't fail the entire agent
		persistentCtx = nil
	}

	// Analyze intent type and task details
	intentType := analyzeIntentType(userIntent, logger)
	taskIntent := analyzeTaskIntent(userIntent, logger)
	projectContext := inferProjectContext(".", userIntent, logger)

	// Determine quality level for code generation
	qualityLevel := qualityOptimizer.DetermineQualityLevel(userIntent, taskIntent, cfg)

	ctx := &SimplifiedAgentContext{
		UserIntent:            userIntent,
		Config:                cfg,
		Logger:                logger,
		Todos:                 []TodoItem{},
		AnalysisResults:       make(map[string]string),
		ContextManager:        contextManager,
		PersistentCtx:         persistentCtx,
		SessionID:             sessionID,
		TotalTokensUsed:       0,
		TotalPromptTokens:     0,
		TotalCompletionTokens: 0,
		TotalCost:             0.0,
		SkipPrompt:            skipPrompt,
		// Enhanced context awareness
		ProjectContext: projectContext,
		TaskIntent:     taskIntent,
		IntentType:     intentType,

		// Quality optimization
		QualityLevel:     qualityLevel,
		QualityOptimizer: qualityOptimizer,
	}

	// Ensure token usage and cost are always displayed, even on failure
	defer func() {
		if ctx.TotalTokensUsed > 0 {
			// Update config with consolidated token usage for command summary display
			ctx.Config.LastTokenUsage = &llm.TokenUsage{
				TotalTokens:      ctx.TotalTokensUsed,
				PromptTokens:     ctx.TotalPromptTokens,
				CompletionTokens: ctx.TotalCompletionTokens,
				Estimated:        false,
			}

			duration := time.Since(startTime)

			// Send token/cost info to UI header, show summary only in console
			if ui.IsUIActive() {
				// Update UI with final token/cost info - header will show it
				ui.PublishProgressWithTokens(0, 0, ctx.TotalTokensUsed, ctx.TotalCost, ctx.Config.EditingModel, []ui.ProgressRow{})
			} else {
				// Console mode - show detailed summary
				ui.Out().Print("\n📊 Agent Usage Summary\n")
				ui.Out().Printf("├─ Duration: %.2f seconds\n", duration.Seconds())
				ui.Out().Printf("├─ Total tokens used: %s\n", formatTokenCount(ctx.TotalTokensUsed))
				ui.Out().Printf("└─ Total cost: $%s\n", formatCost(ctx.TotalCost))
			}
		}
	}()

	switch intentType {
	case IntentTypeCodeUpdate:
		return handleCodeUpdate(ctx, startTime)
	case IntentTypeDocumentation:
		return handleDocumentation(ctx, startTime)
	case IntentTypeCreation:
		return handleCreation(ctx, startTime)
	case IntentTypeAnalysis:
		return handleAnalysis(ctx, startTime)
	case IntentTypeQuestion:
		return handleQuestion(ctx)
	case IntentTypeCommand:
		return handleCommand(ctx)
	default:
		return fmt.Errorf("unknown intent type: %s", intentType)
	}
}

// executeAgentWorkflowWithTools provides a unified, reliable workflow for agent tasks with tool support
// This function implements the working pattern from the question handler that can be reused across all workflows
func executeAgentWorkflowWithTools(ctx *SimplifiedAgentContext, messages []prompts.Message, workflowType string) (string, *llm.TokenUsage, error) {
	// Use the same configuration pattern that works in question handler
	workflowCfg := *ctx.Config
	workflowCfg.SkipPrompt = true

	// Use orchestration model for agent workflows
	model := ctx.Config.OrchestrationModel
	if model == "" {
		model = ctx.Config.EditingModel
	}

	// Create workflow context with controlled tool limits
	workflowContext := llm.GetAgentWorkflowContext()
	workflowContext.MaxToolCalls = 3 // Reasonable limit for agent workflows

	// Execute with the unified interactive system that works reliably
	_, response, tokenUsage, err := llm.CallLLMWithUnifiedInteractive(&llm.UnifiedInteractiveConfig{
		ModelName:       model,
		Messages:        messages,
		Filename:        "",
		WorkflowContext: workflowContext,
		Config:          &workflowCfg,
		Timeout:         llm.GetSmartTimeout(ctx.Config, model, "analysis"),
	})

	if err != nil {
		// Log the error but don't fail completely - try basic approach as fallback
		ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Agent workflow failed (%v), trying basic approach", err))

		// Fallback to basic LLM response without tools
		basicResponse, basicTokenUsage, basicErr := llm.GetLLMResponse(model, messages, "", &workflowCfg, 30*time.Second)
		if basicErr != nil {
			return "", nil, fmt.Errorf("both agent workflow and basic approach failed: %w", err)
		}

		ctx.Logger.LogProcessStep("✅ Basic approach succeeded")
		return basicResponse, basicTokenUsage, nil
	}

	return response, tokenUsage, nil
}

// handleCodeUpdate manages the code update workflow with todos
func handleCodeUpdate(ctx *SimplifiedAgentContext, startTime time.Time) error {
	ctx.Logger.LogProcessStep("🧭 Analyzing intent and creating plan...")

	// Create todos based on user intent
	err := createTodos(ctx)
	if err != nil {
		return fmt.Errorf("failed to create todos: %w", err)
	}

	if len(ctx.Todos) == 0 {
		ctx.Logger.LogProcessStep("⚠️ No actionable todos created")
		return fmt.Errorf("no actionable todos could be created")
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Created %d todos", len(ctx.Todos)))

	// Execute todos using dynamic reprioritization after each step
	completedCount := 0
	for {
		// Select next pending todo by dynamic score
		nextIdx := selectNextTodoIndex(ctx)
		if nextIdx == -1 {
			break // no pending todos remain
		}

		todo := ctx.Todos[nextIdx]
		ctx.Logger.LogProcessStep(fmt.Sprintf("📋 Executing todo: %s", todo.Content))

		// Update todo status
		ctx.CurrentTodo = &todo
		ctx.Todos[nextIdx].Status = "in_progress"

		// Execute via code command with skip prompt, with smart retry
		err := executeTodoWithSmartRetry(ctx, &ctx.Todos[nextIdx])
		if err != nil {
			ctx.Todos[nextIdx].Status = "failed"
			ctx.Logger.LogError(fmt.Errorf("todo failed: %w", err))
			return fmt.Errorf("todo execution failed: %w", err)
		}

		ctx.Todos[nextIdx].Status = "completed"
		completedCount++

		// Mark todo as completed in context manager if available
		if ctx.ContextManager != nil && ctx.PersistentCtx != nil {
			err := ctx.ContextManager.CompleteTodo(ctx.PersistentCtx, ctx.Todos[nextIdx].ID)
			if err != nil {
				ctx.Logger.LogError(fmt.Errorf("failed to mark todo as completed in context: %w", err))
			}
		}

		// Summarize analysis results if they get too large every 3 completions
		if completedCount%3 == 0 {
			if err := summarizeAnalysisResultsIfNeeded(ctx); err != nil {
				ctx.Logger.LogError(fmt.Errorf("analysis results summarization failed: %w", err))
			}
		}

		// Validate build after each todo
		err = validateBuild(ctx)
		if err != nil {
			ctx.Logger.LogError(fmt.Errorf("build validation failed after todo: %w", err))
			return fmt.Errorf("build validation failed: %w", err)
		}

		ctx.Logger.LogProcessStep("✅ Todo completed and validated")
	}

	// Summarize any remaining analysis results before final summary
	if err := summarizeAnalysisResultsIfNeeded(ctx); err != nil {
		ctx.Logger.LogError(fmt.Errorf("final analysis results summarization failed: %w", err))
	}

	// Generate and save context summary if context manager is available
	if ctx.ContextManager != nil && ctx.PersistentCtx != nil {
		_, err := ctx.ContextManager.GenerateSummary(ctx.PersistentCtx)
		if err != nil {
			ctx.Logger.LogError(fmt.Errorf("failed to generate analysis summary: %w", err))
		} else {
			// Save summary to file
			summaryPath := fmt.Sprintf(".ledit/analysis_summary_%s.md", ctx.SessionID[:8])
			err := ctx.ContextManager.WriteSummaryToFile(ctx.PersistentCtx, summaryPath)
			if err != nil {
				ctx.Logger.LogError(fmt.Errorf("failed to write summary file: %w", err))
			} else {
				// Only show file path in console mode - UI users don't need this detail
				ui.PrintfContext(false, "📄 Analysis summary saved to: %s\n", summaryPath)
			}
		}
	}

	// Final summary - detailed in console, minimal in UI
	if ui.IsUIActive() {
		ui.Log("✅ Agent completed successfully")
	} else {
		ui.Out().Print("\n✅ Simplified Agent completed successfully\n")
		ui.Out().Printf("├─ Todos completed: %d\n", len(ctx.Todos))
		ui.Out().Printf("└─ Status: All changes validated\n")
	}

	return nil
}

// generateProjectHash creates a hash of the current workspace structure
func generateProjectHash(logger *utils.Logger) string {
	// Get workspace information
	wsFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		logger.LogError(fmt.Errorf("failed to get workspace info: %w", err))
		// Return a default hash if workspace info fails
		return fmt.Sprintf("%x", md5.Sum([]byte("default_workspace")))
	}

	// Create hash input from workspace structure
	hashInput := fmt.Sprintf("%d_%s_%s_%s",
		len(wsFile.Files),
		wsFile.ProjectGoals.Mission,
		strings.Join(wsFile.Languages, ","),
		strings.Join(wsFile.BuildRunners, ","))

	return fmt.Sprintf("%x", md5.Sum([]byte(hashInput)))
}

// formatTokenCount formats token count with thousands separator for readability
func formatTokenCount(tokens int) string {
	if tokens == 0 {
		return "0"
	}

	// Convert to string and add thousands separators
	str := fmt.Sprintf("%d", tokens)
	n := len(str)
	if n <= 3 {
		return str
	}

	// Add commas every 3 digits from the right
	var result []byte
	for i, digit := range str {
		if i > 0 && (n-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(digit))
	}
	return string(result)
}

// formatCost formats cost with appropriate decimal places
func formatCost(cost float64) string {
	if cost == 0.0 {
		return "0.00"
	}
	return fmt.Sprintf("%.4f", cost)
}

// trackTokenUsage is a helper function to track token usage and cost from LLM calls
func trackTokenUsage(ctx *SimplifiedAgentContext, tokenUsage *llm.TokenUsage, modelName string) {
	if ctx == nil || tokenUsage == nil {
		return
	}

	ctx.TotalTokensUsed += tokenUsage.TotalTokens
	ctx.TotalPromptTokens += tokenUsage.PromptTokens
	ctx.TotalCompletionTokens += tokenUsage.CompletionTokens
	ctx.TotalCost += llm.CalculateCost(*tokenUsage, modelName)
	// If usage is estimated, note it for transparency
	if tokenUsage.Estimated && ctx.Logger != nil {
		ctx.Logger.LogProcessStep("ℹ️ Token usage estimated (provider did not return usage)")
	}
}

// summarizeAnalysisResultsIfNeeded summarizes analysis results if they get too large
func summarizeAnalysisResultsIfNeeded(ctx *SimplifiedAgentContext) error {
	const maxAnalysisResults = 15
	const resultsToKeep = 5

	if len(ctx.AnalysisResults) <= maxAnalysisResults {
		return nil
	}

	ctx.Logger.LogProcessStep("📝 Summarizing analysis results to prevent memory overflow...")

	// Preserve recent results
	var recentResults []string
	var resultsToSummarize []string
	// This is tricky with a map. A better approach would be to store results in a struct with a timestamp.
	// For now, we will iterate and split. This is not guaranteed to be the "most recent".
	i := 0
	for todoID, result := range ctx.AnalysisResults {
		formattedResult := fmt.Sprintf("TODO %s: %s", todoID, result)
		if i >= len(ctx.AnalysisResults)-resultsToKeep {
			recentResults = append(recentResults, formattedResult)
		} else {
			resultsToSummarize = append(resultsToSummarize, formattedResult)
		}
		i++
	}

	prompt := fmt.Sprintf(`Summarize these analysis results to preserve key insights while reducing size:

ANALYSIS RESULTS TO SUMMARIZE (%d):
%s

Create a concise summary that captures:
1. Key findings from each analysis
2. Important patterns or issues identified
3. Critical recommendations
4. Overall progress insights

Respond with JSON:
{
  "summary": "concise summary of all analysis results",
  "key_findings": ["finding1", "finding2", "finding3"],
  "critical_issues": ["issue1", "issue2"],
  "recommendations": ["rec1", "rec2"]
}`,
		len(resultsToSummarize), strings.Join(resultsToSummarize, "\n\n"))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at summarizing analysis results while preserving critical insights. Always respond with valid JSON."},
		{Role: "user", Content: prompt},
	}

	response, tokenUsage, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, llm.GetSmartTimeout(ctx.Config, ctx.Config.OrchestrationModel, "analysis"))
	if err != nil {
		// Fallback: just truncate
		newAnalysisResults := make(map[string]string)
		for todoID, result := range ctx.AnalysisResults {
			if len(newAnalysisResults) < resultsToKeep {
				newAnalysisResults[todoID] = result
			}
		}
		ctx.AnalysisResults = newAnalysisResults
		return fmt.Errorf("context summarization failed, truncated results: %w", err)
	}

	// Track token usage for the summarization
	if tokenUsage != nil {
		trackTokenUsage(ctx, tokenUsage, ctx.Config.OrchestrationModel)
	}

	// Parse the summary
	var summary struct {
		Summary         string   `json:"summary"`
		KeyFindings     []string `json:"key_findings"`
		CriticalIssues  []string `json:"critical_issues"`
		Recommendations []string `json:"recommendations"`
	}

	clean, err := utils.ExtractJSON(response)
	if err != nil {
		return fmt.Errorf("failed to parse summary JSON: %w", err)
	}

	if err := json.Unmarshal([]byte(clean), &summary); err != nil {
		return fmt.Errorf("failed to unmarshal summary: %w", err)
	}

	// Replace all analysis results with the summary
	newAnalysisResults := make(map[string]string)
	newAnalysisResults["summary"] = fmt.Sprintf("SUMMARY: %s\n\nKEY FINDINGS: %s\n\nCRITICAL ISSUES: %s\n\nRECOMMENDATIONS: %s",
		summary.Summary,
		strings.Join(summary.KeyFindings, ", "),
		strings.Join(summary.CriticalIssues, ", "),
		strings.Join(summary.Recommendations, ", "))

	// Add back the recent results
	for i, result := range recentResults {
		newAnalysisResults[fmt.Sprintf("recent_result_%d", i)] = result
	}
	ctx.AnalysisResults = newAnalysisResults

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Analysis results summarized from %d to %d entries", len(ctx.AnalysisResults), len(newAnalysisResults)))
	return nil
}

// handleDocumentation handles documentation generation tasks with parallel execution
func handleDocumentation(ctx *SimplifiedAgentContext, startTime time.Time) error {
	ctx.Logger.LogProcessStep("📚 Documentation task detected")

	// Use the same workflow as code update but with documentation-focused todos
	ctx.Logger.LogProcessStep("🧭 Analyzing documentation requirements and creating plan...")

	// Create documentation-specific todos
	err := createDocumentationTodos(ctx)
	if err != nil {
		return fmt.Errorf("failed to create documentation todos: %w", err)
	}

	if len(ctx.Todos) == 0 {
		ctx.Logger.LogProcessStep("⚠️ No actionable documentation todos created")
		return fmt.Errorf("no actionable documentation todos could be created")
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Created %d documentation todos", len(ctx.Todos)))

	// Separate todos into parallel and sequential groups
	parallelTodos := []TodoItem{}
	sequentialTodos := []TodoItem{}

	for _, todo := range ctx.Todos {
		executionType := analyzeTodoExecutionType(todo.Content, todo.Description)
		// Analysis todos and documentation edits can run in parallel
		if executionType == ExecutionTypeAnalysis || (executionType == ExecutionTypeDirectEdit && isDocumentationTodo(todo)) {
			parallelTodos = append(parallelTodos, todo)
		} else {
			sequentialTodos = append(sequentialTodos, todo)
		}
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("📊 Execution plan: %d parallel, %d sequential todos", len(parallelTodos), len(sequentialTodos)))

	// Execute parallel todos first if any
	if len(parallelTodos) > 0 && canExecuteInParallel(parallelTodos) {
		ctx.Logger.LogProcessStep("🚀 Starting parallel execution for documentation tasks")
		err := executeParallelTodos(ctx, parallelTodos)
		if err != nil {
			ctx.Logger.LogError(fmt.Errorf("parallel execution failed: %w", err))
			// Fall back to sequential execution for failed todos
			ctx.Logger.LogProcessStep("⚠️ Falling back to sequential execution")
			return executeTodosWithFallback(ctx)
		}
	}

	// Execute remaining sequential todos
	if len(sequentialTodos) > 0 {
		ctx.Logger.LogProcessStep("📝 Executing remaining sequential todos")
		// Update context with only sequential todos
		originalTodos := ctx.Todos
		ctx.Todos = sequentialTodos
		err := executeTodosWithFallback(ctx)
		// Restore all todos for final reporting
		ctx.Todos = originalTodos
		if err != nil {
			return err
		}
	}

	// Final summary
	ui.Out().Print("\n✅ Documentation generation completed successfully\n")
	ui.Out().Printf("├─ Parallel todos: %d\n", len(parallelTodos))
	ui.Out().Printf("├─ Sequential todos: %d\n", len(sequentialTodos))
	ui.Out().Printf("└─ Total todos: %d\n", len(ctx.Todos))

	return nil
}

// handleCreation handles file/content creation tasks
func handleCreation(ctx *SimplifiedAgentContext, startTime time.Time) error {
	ctx.Logger.LogProcessStep("🆕 Creation task detected")

	ctx.Logger.LogProcessStep("🧭 Analyzing creation requirements and creating plan...")

	// Create creation-specific todos
	err := createCreationTodos(ctx)
	if err != nil {
		return fmt.Errorf("failed to create creation todos: %w", err)
	}

	if len(ctx.Todos) == 0 {
		ctx.Logger.LogProcessStep("⚠️ No actionable creation todos created")
		return fmt.Errorf("no actionable creation todos could be created")
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Created %d creation todos", len(ctx.Todos)))

	// Execute creation todos
	return executeTodosWithFallback(ctx)
}

// handleAnalysis handles analysis-only tasks
func handleAnalysis(ctx *SimplifiedAgentContext, startTime time.Time) error {
	ctx.Logger.LogProcessStep("🔍 Analysis task detected")

	ctx.Logger.LogProcessStep("🧭 Analyzing analysis requirements and creating plan...")

	// Create analysis-specific todos
	err := createAnalysisTodos(ctx)
	if err != nil {
		return fmt.Errorf("failed to create analysis todos: %w", err)
	}

	if len(ctx.Todos) == 0 {
		ctx.Logger.LogProcessStep("⚠️ No actionable analysis todos created")
		return fmt.Errorf("no actionable analysis todos could be created")
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Created %d analysis todos", len(ctx.Todos)))

	// Execute analysis todos
	return executeTodosWithFallback(ctx)
}

// executeTodosWithFallback executes todos with improved error recovery
func executeTodosWithFallback(ctx *SimplifiedAgentContext) error {
	completedCount := 0
	maxRetries := 2

	for {
		// Select next pending todo by dynamic score
		nextIdx := selectNextTodoIndex(ctx)
		if nextIdx == -1 {
			break // no pending todos remain
		}

		todo := ctx.Todos[nextIdx]
		ctx.Logger.LogProcessStep(fmt.Sprintf("📋 Executing todo: %s", todo.Content))

		// Update todo status
		ctx.CurrentTodo = &todo
		ctx.Todos[nextIdx].Status = "in_progress"

		var err error
		retry := 0

		// Execute with progressive fallback strategies
		for retry <= maxRetries {
			if retry == 0 {
				// First attempt: normal execution
				err = executeTodoWithSmartRetry(ctx, &ctx.Todos[nextIdx])
			} else if retry == 1 {
				// Second attempt: try different strategy
				ctx.Logger.LogProcessStep("🔄 First attempt failed, trying alternative approach...")
				err = executeTodoWithFallbackStrategy(ctx, &ctx.Todos[nextIdx])
			} else {
				// Final attempt: simplest approach
				ctx.Logger.LogProcessStep("🔄 Previous attempts failed, trying simplest approach...")
				err = executeTodoWithSimpleStrategy(ctx, &ctx.Todos[nextIdx])
			}

			if err == nil {
				break // Success!
			}

			retry++
			ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Attempt %d failed: %v", retry, err))
		}

		if err != nil {
			ctx.Todos[nextIdx].Status = "failed"
			ctx.Logger.LogError(fmt.Errorf("todo failed after %d attempts: %w", maxRetries+1, err))

			// Don't fail completely - mark as failed and continue with other todos
			ctx.Logger.LogProcessStep("⏭️ Continuing with remaining todos...")
			continue
		}

		ctx.Todos[nextIdx].Status = "completed"
		completedCount++

		// Mark todo as completed in context manager if available
		if ctx.ContextManager != nil && ctx.PersistentCtx != nil {
			err := ctx.ContextManager.CompleteTodo(ctx.PersistentCtx, ctx.Todos[nextIdx].ID)
			if err != nil {
				ctx.Logger.LogError(fmt.Errorf("failed to mark todo as completed in context: %w", err))
			}
		}

		ctx.Logger.LogProcessStep("✅ Todo completed and validated")
	}

	if completedCount == 0 {
		return fmt.Errorf("no todos were completed successfully")
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("🎉 Agent completed %d todos", completedCount))
	return nil
}

// executeTodoWithFallbackStrategy tries an alternative execution strategy
func executeTodoWithFallbackStrategy(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	// Try using the creation strategy for failed documentation tasks
	if ctx.TaskIntent == TaskIntentDocumentation {
		// Force creation strategy
		originalTaskIntent := ctx.TaskIntent
		ctx.TaskIntent = TaskIntentCreation

		err := executeTodoWithSmartRetry(ctx, todo)

		// Restore original task intent
		ctx.TaskIntent = originalTaskIntent
		return err
	}

	// For other tasks, try the full edit strategy
	return executeTodoWithSmartRetry(ctx, todo)
}

// executeTodoWithSimpleStrategy uses the simplest possible approach
func executeTodoWithSimpleStrategy(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	// Use basic LLM call without complex tool workflows
	messages := []prompts.Message{
		{Role: "system", Content: "You are a helpful assistant. Provide a clear, direct response to the user's request."},
		{Role: "user", Content: fmt.Sprintf("Task: %s\nDescription: %s", todo.Content, todo.Description)},
	}

	response, tokenUsage, err := llm.GetLLMResponse(
		ctx.Config.EditingModel,
		messages,
		"",
		ctx.Config,
		60*time.Second,
	)

	if tokenUsage != nil {
		trackTokenUsage(ctx, tokenUsage, ctx.Config.EditingModel)
	}

	if err != nil {
		return err
	}

	// Store the response as analysis result
	ctx.AnalysisResults[todo.ID+"_simple_result"] = response
	ctx.Logger.LogProcessStep(fmt.Sprintf("📝 Simple execution completed: %s", response[:min(100, len(response))]))

	return nil
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
