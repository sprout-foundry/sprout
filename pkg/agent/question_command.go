//go:build !agent2refactor

package agent

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
)

// handleQuestion responds directly to user questions
func handleQuestion(ctx *SimplifiedAgentContext) error {
	ctx.Logger.LogProcessStep("❓ Handling question with tool support...")

	prompt := fmt.Sprintf(`Please answer this question using the available tools to gather evidence:

Question: "%s"

Use tools to gather information and provide a clear, helpful answer based on actual evidence from the codebase.`, ctx.UserIntent)

	messages := []prompts.Message{
		{Role: "system", Content: llm.GetSystemMessageForInformational()},
		{Role: "user", Content: prompt},
	}

	// Use the unified agent workflow pattern that works reliably with tools
	response, tokenUsage, err := executeAgentWorkflowWithTools(ctx, messages, "question")

	// If primary model fails, try with fallback approach
	if err != nil {
		ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Question answering failed (%v), trying fallback", err))

		// Try with a simpler prompt but still use tools
		fallbackPrompt := fmt.Sprintf(`Please answer this question briefly using tools if needed:

Question: "%s"

Keep the answer clear and concise.`, ctx.UserIntent)

		fallbackMessages := []prompts.Message{
			{Role: "system", Content: llm.GetSystemMessageForInformational()},
			{Role: "user", Content: fallbackPrompt},
		}

		// Use the unified agent workflow for fallback as well
		response, tokenUsage, err = executeAgentWorkflowWithTools(ctx, fallbackMessages, "question_fallback")

		if err != nil {
			return fmt.Errorf("both primary and fallback question answering failed: %w", err)
		}
	}

	// Track token usage and cost
	trackTokenUsage(ctx, tokenUsage, ctx.Config.OrchestrationModel)

	ui.Out().Print("\n🤖 Answer:\n")
	ui.Out().Print(response + "\n")
	return nil
}

// handleCommand executes user commands directly
func handleCommand(ctx *SimplifiedAgentContext) error {
	ctx.Logger.LogProcessStep("⚡ Handling command directly...")

	// Extract command from intent
	command := extractCommandFromIntent(ctx.UserIntent)
	if command == "" {
		return fmt.Errorf("could not extract command from intent")
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("🚀 Executing: %s", command))

	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		ctx.Logger.LogError(fmt.Errorf("command failed: %s", string(output)))
		return fmt.Errorf("command execution failed: %s", string(output))
	}

	ui.Out().Print("\n📋 Command Output:\n")
	ui.Out().Print(string(output) + "\n")
	return nil
}

// extractCommandFromIntent extracts a command from user intent
func extractCommandFromIntent(intent string) string {
	// Simple extraction - look for commands after "run", "execute", etc.
	intentLower := strings.ToLower(intent)

	commands := []string{"run ", "execute ", "start ", "stop ", "build ", "test ", "deploy ", "install ", "uninstall "}
	for _, prefix := range commands {
		if idx := strings.Index(intentLower, prefix); idx != -1 {
			return strings.TrimSpace(intent[idx+len(prefix):])
		}
	}

	// If no prefix found, return the whole intent as a command
	return strings.TrimSpace(intent)
}
