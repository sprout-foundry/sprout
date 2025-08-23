//go:build !agent2refactor

package agent

import (
	"crypto/md5"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
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
		UserIntent:      userIntent,
		Config:          cfg,
		Logger:          logger,
		Todos:           []TodoItem{},
		AnalysisResults: make(map[string]string),
		ContextManager:  contextManager,
		PersistentCtx:   persistentCtx,
		SessionID:       sessionID,
		TotalTokensUsed: 0,
		TotalCost:       0.0,
	}

	// Ensure token usage and cost are always displayed, even on failure
	defer func() {
		if ctx.TotalTokensUsed > 0 {
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

	// Execute todos sequentially with context management
	for i, todo := range ctx.Todos {
		ctx.Logger.LogProcessStep(fmt.Sprintf("📋 Executing todo %d/%d: %s", i+1, len(ctx.Todos), todo.Content))

		// Update todo status
		ctx.CurrentTodo = &todo
		ctx.Todos[i].Status = "in_progress"

		// Execute via code command with skip prompt
		err := executeTodo(ctx, &ctx.Todos[i])
		if err != nil {
			ctx.Todos[i].Status = "failed"
			ctx.Logger.LogError(fmt.Errorf("todo failed: %w", err))
			return fmt.Errorf("todo execution failed: %w", err)
		}

		ctx.Todos[i].Status = "completed"

		// Mark todo as completed in context manager if available
		if ctx.ContextManager != nil && ctx.PersistentCtx != nil {
			err := ctx.ContextManager.CompleteTodo(ctx.PersistentCtx, ctx.Todos[i].ID)
			if err != nil {
				ctx.Logger.LogError(fmt.Errorf("failed to mark todo as completed in context: %w", err))
			}
		}

		// Validate build after each todo
		err = validateBuild(ctx)
		if err != nil {
			ctx.Logger.LogError(fmt.Errorf("build validation failed after todo %d: %w", i+1, err))
			return fmt.Errorf("build validation failed: %w", err)
		}

		ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Todo %d completed and validated", i+1))
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
		wsFile.ProjectGoals.OverallGoal,
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
