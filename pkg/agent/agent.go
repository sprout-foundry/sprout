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
	ui.Out().Print("🤖 Simplified Agent Mode\n")
	ui.Out().Printf("🎯 Intent: %s\n", userIntent)

	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if model != "" {
		cfg.EditingModel = model
	}
	cfg.SkipPrompt = skipPrompt
	cfg.FromAgent = true

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

	// Analyze intent type
	intentType := analyzeIntentType(userIntent, logger)

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
			ui.Out().Print("\n📊 Agent Usage Summary\n")
			ui.Out().Printf("├─ Duration: %.2f seconds\n", duration.Seconds())
			ui.Out().Printf("├─ Total tokens used: %s\n", formatTokenCount(ctx.TotalTokensUsed))
			ui.Out().Printf("└─ Total cost: $%s\n", formatCost(ctx.TotalCost))
		}
	}()

	switch intentType {
	case IntentTypeCodeUpdate:
		return handleCodeUpdate(ctx, startTime)
	case IntentTypeQuestion:
		return handleQuestion(ctx)
	case IntentTypeCommand:
		return handleCommand(ctx)
	default:
		return fmt.Errorf("unknown intent type")
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
				ui.Out().Printf("📄 Analysis summary saved to: %s\n", summaryPath)
			}
		}
	}

	// Final summary
	ui.Out().Print("\n✅ Simplified Agent completed successfully\n")
	ui.Out().Printf("├─ Todos completed: %d\n", len(ctx.Todos))
	ui.Out().Printf("└─ Status: All changes validated\n")

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
