package cmd

import (
	"fmt"
	"time"

	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/providers"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

// codeCmd represents the code command using the new unified framework
var codeCmd = createCodeCommand()

func createCodeCommand() *BaseCommand {
	cmd := NewBaseCommand(
		"code [instructions]",
		"Generate updated code based on instructions",
		`Processes a file or generates new files based on natural language instructions using an LLM.

When using the --image flag, ensure your model supports vision input. Vision-capable models include:
  - openai:gpt-4o, openai:gpt-4-turbo, openai:gpt-4-vision-preview
  - deepinfra:google/gemini-2.5-flash, deepinfra:google/gemini-2.5-pro

Examples:
  ledit code "Add error handling to the main function"
  ledit code --filename main.go "Refactor this function to be more efficient"
  ledit code --model gpt-4 --skip-prompt "Generate a REST API endpoint"
  ledit code --image screenshot.png "Create a UI component based on this design"`,
	)

	// Add custom flags specific to code command
	cmd.AddCustomFlag("filename", "f", "", "The filename to process (optional)")
	cmd.AddCustomFlag("image", "i", "", "Path to an image file to use as UI reference")

	// Set the command execution function
	cmd.SetRunFunc(executeCodeCommand)

	return cmd
}

func executeCodeCommand(cfg *CommandConfig, args []string) error {
	// Extract instructions from arguments
	instructions := ""
	if len(args) > 0 {
		instructions = args[0]
	}

	// Log the original user prompt
	utils.LogUserPrompt(instructions)

	// Validate input
	if len(args) == 0 {
		ui.Out().Print(prompts.InstructionsRequired() + "\n")
		return fmt.Errorf("instructions are required")
	}

	// Get custom flag values
	filename := ""
	imagePath := ""

	// Note: In a real implementation, we would access the flag values through the BaseCommand
	// For now, we'll use placeholder values
	if cfg.Config != nil {
		cfg.Config.SkipPrompt = cfg.SkipPrompt
	}

	// Show processing message only in console mode - UI shows progress differently
	ui.PrintContext(prompts.ProcessingCodeGeneration()+"\n", false)
	startTime := time.Now()

	// Execute code generation
	_, err := editor.ProcessCodeGeneration(filename, instructions, cfg.Config, imagePath)
	if err != nil {
		return fmt.Errorf("code generation failed: %w", err)
	}

	duration := time.Since(startTime)

	// Display completion message with timing
	// Show completion message only in console mode
	ui.PrintContext(prompts.CodeGenerationFinished(duration), false)

	// Display token usage if available
	if cfg.Config.LastTokenUsage != nil {
		// Use provider interface for cost calculation
		provider, err := providers.GetProvider(cfg.Config.EditingModel)
		if err == nil {
			cost := provider.CalculateCost(providers.TokenUsage{
				PromptTokens:     cfg.Config.LastTokenUsage.PromptTokens,
				CompletionTokens: cfg.Config.LastTokenUsage.CompletionTokens,
				TotalTokens:      cfg.Config.LastTokenUsage.TotalTokens,
			})

			// Only show token summary in console mode - UI shows this in header
			ui.PrintfContext(false, "Token Usage: %d prompt + %d completion = %d total (Cost: $%.4f)\n",
				cfg.Config.LastTokenUsage.PromptTokens,
				cfg.Config.LastTokenUsage.CompletionTokens,
				cfg.Config.LastTokenUsage.TotalTokens,
				cost)

			// Also log for debugging purposes
			if cfg.Logger != nil {
				cfg.Logger.LogProcessStep(fmt.Sprintf("Token Usage: %d prompt + %d completion = %d total (Cost: $%.4f)",
					cfg.Config.LastTokenUsage.PromptTokens,
					cfg.Config.LastTokenUsage.CompletionTokens,
					cfg.Config.LastTokenUsage.TotalTokens,
					cost))
			}
		}
	}

	return nil
}
