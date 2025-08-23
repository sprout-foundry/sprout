//go:build !agent2refactor

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/editor"
)

// executeEditStep performs a single granular edit
func executeEditStep(ctx *SimplifiedAgentContext, todo *TodoItem, step EditStep, stepIndex int) error {
	stepPrompt := fmt.Sprintf(`Execute this specific edit step:

Task: %s
Step %d: %s

Files to modify: %s

Changes to make: %s

Please implement ONLY this specific step. Keep the changes minimal and focused. Do not implement additional features or other steps.`, todo.Content, stepIndex+1, step.Description, strings.Join(step.Files, ", "), step.Changes)

	// Use ProcessCodeGeneration for proper, targeted edits instead of the broken direct edit
	agentConfig := *ctx.Config
	agentConfig.SkipPrompt = true
	agentConfig.FromAgent = true

	// Set environment variables to ensure non-interactive mode
	os.Setenv("LEDIT_FROM_AGENT", "1")
	os.Setenv("LEDIT_SKIP_PROMPT", "1")

	// Clear any previous token usage
	agentConfig.LastTokenUsage = nil

	_, err := editor.ProcessCodeGeneration("", stepPrompt, &agentConfig, "")

	// Track token usage from the editor's LLM calls
	if agentConfig.LastTokenUsage != nil {
		trackTokenUsage(ctx, agentConfig.LastTokenUsage, agentConfig.EditingModel)
		ctx.Logger.LogProcessStep(fmt.Sprintf("📊 Tracked %d tokens from editor LLM calls", agentConfig.LastTokenUsage.TotalTokens))
	}

	return err
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
