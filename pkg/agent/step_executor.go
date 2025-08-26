//go:build !agent2refactor

package agent

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
)

// executeEditStep performs a single granular edit
func executeEditStep(ctx *SimplifiedAgentContext, todo *TodoItem, step EditStep, stepIndex int) error {
	stepPrompt := fmt.Sprintf(`Execute this specific edit step:

Task: %s
Step %d: %s

Files to modify: %s

Changes to make: %s

Please implement ONLY this specific step. Keep the changes minimal and focused. Do not implement additional features or other steps.`, todo.Content, stepIndex+1, step.Description, strings.Join(step.Files, ", "), step.Changes)

	// Use CallLLMWithUnifiedInteractive for proper tool execution (like read_file, run_shell_command)
	stepMessages := []prompts.Message{
		{Role: "system", Content: llm.GetSystemMessageForStepExecution()},
		{Role: "user", Content: stepPrompt},
	}

	// Use the same config as analysis todos for tool execution
	stepConfig := *ctx.Config
	stepConfig.SkipPrompt = true

	// Clear any previous token usage
	stepConfig.LastTokenUsage = nil

	_, response, tokenUsage, err := llm.CallLLMWithUnifiedInteractive(&llm.UnifiedInteractiveConfig{
		ModelName:       ctx.Config.EditingModel, // Use editing model for tool execution
		Messages:        stepMessages,
		Filename:        "",
		WorkflowContext: llm.GetAgentWorkflowContext(),
		Config:          &stepConfig,
		Timeout:         llm.GetSmartTimeout(ctx.Config, ctx.Config.EditingModel, "analysis"),
	})

	if err != nil {
		return fmt.Errorf("step execution failed: %w", err)
	}

	// Track token usage from tool execution
	if tokenUsage != nil {
		trackTokenUsage(ctx, tokenUsage, ctx.Config.EditingModel)
		ctx.Logger.LogProcessStep(fmt.Sprintf("📊 Tracked %d tokens from tool execution", tokenUsage.TotalTokens))
	}

	// Store the response for potential use in next steps
	ctx.AnalysisResults[fmt.Sprintf("%s_step_%d", todo.ID, stepIndex)] = response

	return nil
}

// verifyBuildAfterStep ensures code still builds after each edit step
func verifyBuildAfterStep(ctx *SimplifiedAgentContext) error {
	// Run go build to verify compilation
	cmd := exec.Command("go", "build")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	ctx.Logger.LogProcessStep("✅ Build verification passed")
	return nil
}
