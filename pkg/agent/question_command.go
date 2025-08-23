//go:build !agent2refactor

package agent

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
)

// handleQuestion responds directly to user questions
func handleQuestion(ctx *SimplifiedAgentContext) error {
	ctx.Logger.LogProcessStep("❓ Handling question directly...")

	prompt := fmt.Sprintf(`You are an expert software developer. Please answer this question:

Question: "%s"

Provide a clear, helpful answer. If this involves code or technical details, be specific and include examples where appropriate.`, ctx.UserIntent)

	messages := []prompts.Message{
		{Role: "system", Content: "You are a helpful software development assistant."},
		{Role: "user", Content: prompt},
	}

	response, _, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to get answer: %w", err)
	}

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
