//go:build !agent2refactor

package agent

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
)

// executeExplorationPhase reads relevant files and gathers context
func executeExplorationPhase(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("🔍 Phase 1: Exploration & Context Gathering")

	// Generate exploration instructions
	explorationPrompt := fmt.Sprintf(`Explore and understand the codebase context for this task:

Task: %s
Description: %s

Please:
1. Identify the most relevant files that need to be examined
2. Read and understand the current code structure
3. Note any dependencies or relationships between components
4. Identify potential challenges or constraints

Focus on understanding the existing patterns and architecture before making any changes.`, todo.Content, todo.Description)

	// Use analysis workflow to gather context
	analysisTodo := &TodoItem{
		Content:     "Gather comprehensive context for: " + todo.Content,
		Description: explorationPrompt,
		ID:          todo.ID + "_explore",
	}

	return executeAnalysisTodo(ctx, analysisTodo)
}

// executePlanningPhase creates a detailed implementation strategy
func executePlanningPhase(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("📋 Phase 2: Detailed Planning & Strategy")

	// Get exploration context
	explorationContext := ctx.AnalysisResults[todo.ID+"_explore"]

	planningPrompt := fmt.Sprintf(`Based on the exploration context, create a detailed implementation plan using the "think hard" approach:

Task: %s
Description: %s

Exploration Context: %s

Please think hard about this implementation and create a detailed plan that includes:

1. **Analysis & Understanding**
   - What files are involved and their current state?
   - What are the key dependencies and relationships?
   - What patterns should be preserved?

2. **Implementation Strategy**
   - Break the work into 3-5 small, focused edit steps
   - What is the safest order to make changes?
   - What are the minimal viable changes for each step?

3. **Risk Assessment**
   - What could go wrong with each approach?
   - What are the rollback strategies?
   - How can we verify each step incrementally?

4. **Granular Edit Plan**
   - Step 1: [Specific file + minimal change description]
   - Step 2: [Specific file + minimal change description]
   - Step 3: [Specific file + minimal change description]
   - etc.

Focus on creating a plan that minimizes risk and allows for incremental verification.`, todo.Content, todo.Description, explorationContext)

	// Use a planning-focused todo to generate the actual plan
	planningTodo := &TodoItem{
		Content:     "Create detailed implementation strategy for: " + todo.Content,
		Description: planningPrompt,
		ID:          todo.ID + "_plan",
	}

	// Execute the planning analysis
	if err := executeAnalysisTodo(ctx, planningTodo); err != nil {
		return fmt.Errorf("planning analysis failed: %w", err)
	}

	// The plan is now stored in ctx.AnalysisResults[todo.ID+"_plan"]
	ctx.Logger.LogProcessStep("📋 Detailed implementation plan created")
	return nil
}

// executeGranularEditingPhase performs focused edits based on the plan
func executeGranularEditingPhase(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("✏️ Phase 3: Granular Code Editing")

	// Get configuration for granular editing
	config := getGranularEditingConfig(ctx)

	if !config.Enabled {
		ctx.Logger.LogProcessStep("⚠️ Granular editing disabled, using single edit approach")
		plan := ctx.AnalysisResults[todo.ID+"_plan"]
		return executeSingleEdit(ctx, todo, plan)
	}

	// Get the planning context
	plan := ctx.AnalysisResults[todo.ID+"_plan"]

	// Parse the plan to identify individual steps
	steps := parsePlanIntoSteps(plan)

	// Limit the number of steps
	if len(steps) > config.MaxStepsPerPlan {
		ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Limiting steps from %d to %d (max configured)", len(steps), config.MaxStepsPerPlan))
		steps = steps[:config.MaxStepsPerPlan]
	}

	if len(steps) == 0 {
		if config.EnableFallback {
			ctx.Logger.LogProcessStep("⚠️ No clear steps found in plan, falling back to single edit")
			return executeSingleEdit(ctx, todo, plan)
		} else {
			return fmt.Errorf("no clear edit steps found in plan and fallback disabled")
		}
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("📝 Executing %d edit steps", len(steps)))

	// Execute each step individually with verification
	for i, step := range steps {
		ctx.Logger.LogProcessStep(fmt.Sprintf("🔧 Step %d/%d: %s", i+1, len(steps), step.Description))

		if err := executeEditStep(ctx, todo, step, i); err != nil {
			if config.EnableFallback && i == 0 {
				// If first step fails and fallback is enabled, try single edit
				ctx.Logger.LogProcessStep("⚠️ First step failed, attempting fallback to single edit")
				return executeSingleEdit(ctx, todo, plan)
			}
			return fmt.Errorf("step %d failed: %w", i+1, err)
		}

		// Verify the build after each step if enabled
		if config.VerifyBuildAfterStep {
			if err := verifyBuildAfterStep(ctx); err != nil {
				return fmt.Errorf("build verification failed after step %d: %w", i+1, err)
			}
		} else {
			ctx.Logger.LogProcessStep("⏭️ Skipping build verification (disabled in config)")
		}
	}

	ctx.Logger.LogProcessStep("✅ All edit steps completed successfully")
	return nil
}

// executeVerificationPhase reviews and validates the changes
func executeVerificationPhase(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("✅ Phase 4: Verification & Review")

	// Build verification prompt
	verificationPrompt := fmt.Sprintf(`Verify the implemented changes:

Task: %s
Plan: %s

Please verify:
1. Code builds successfully
2. Changes align with the original requirements
3. No regressions introduced
4. Code follows established patterns
5. Documentation/comments are updated if needed

If issues are found, suggest specific fixes.`, todo.Content, ctx.AnalysisResults[todo.ID+"_plan"])

	// Use analysis workflow for verification
	verificationTodo := &TodoItem{
		Content:     "Verify implementation of: " + todo.Content,
		Description: verificationPrompt,
		ID:          todo.ID + "_verify",
	}

	return executeAnalysisTodo(ctx, verificationTodo)
}

// executeSingleEdit falls back to the original approach when no steps are found
func executeSingleEdit(ctx *SimplifiedAgentContext, todo *TodoItem, plan string) error {
	editingPrompt := fmt.Sprintf(`Implement the planned changes:

Task: %s
Plan: %s

Please implement this carefully, focusing on one logical unit at a time.`, todo.Content, plan)

	// Use CallLLMWithUnifiedInteractive for proper tool execution during editing
	editMessages := []prompts.Message{
		{Role: "system", Content: llm.GetSystemMessageForEditing()},
		{Role: "user", Content: editingPrompt},
	}

	// Use the same config as analysis todos for tool execution
	editConfig := *ctx.Config
	editConfig.SkipPrompt = true

	// Clear any previous token usage
	editConfig.LastTokenUsage = nil

	_, response, tokenUsage, err := llm.CallLLMWithUnifiedInteractive(&llm.UnifiedInteractiveConfig{
		ModelName:       ctx.Config.EditingModel, // Use editing model for tool execution
		Messages:        editMessages,
		Filename:        "",
		WorkflowContext: llm.GetAgentWorkflowContext(),
		Config:          &editConfig,
		Timeout:         llm.GetSmartTimeout(ctx.Config, ctx.Config.EditingModel, "analysis"),
	})

	// Track token usage from tool execution
	if tokenUsage != nil {
		trackTokenUsage(ctx, tokenUsage, ctx.Config.EditingModel)
		ctx.Logger.LogProcessStep(fmt.Sprintf("📊 Tracked %d tokens from tool execution", tokenUsage.TotalTokens))
	}

	// Store the response for potential use in verification
	ctx.AnalysisResults[todo.ID+"_edit"] = response
	return err
}
