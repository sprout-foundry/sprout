//go:build !agent2refactor

package agent

import (
	"fmt"
	"os"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
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

	// Analyze intent type
	intentType := analyzeIntentType(userIntent, logger)

	ctx := &SimplifiedAgentContext{
		UserIntent:      userIntent,
		Config:          cfg,
		Logger:          logger,
		Todos:           []TodoItem{},
		AnalysisResults: make(map[string]string),
	}

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

	// Execute todos sequentially
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

		// Validate build after each todo
		err = validateBuild(ctx)
		if err != nil {
			ctx.Logger.LogError(fmt.Errorf("build validation failed after todo %d: %w", i+1, err))
			return fmt.Errorf("build validation failed: %w", err)
		}

		ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Todo %d completed and validated", i+1))
	}

	// Final summary
	duration := time.Since(startTime)
	ui.Out().Print("\n✅ Simplified Agent completed successfully\n")
	ui.Out().Printf("├─ Duration: %.2f seconds\n", duration.Seconds())
	ui.Out().Printf("├─ Todos completed: %d\n", len(ctx.Todos))
	ui.Out().Printf("└─ Status: All changes validated\n")

	return nil
}
