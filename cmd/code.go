package cmd

import (
	"log"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"

	"github.com/spf13/cobra"
)

var (
	filename           string
	model              string
	skipPrompt         bool
	imagePath          string
	useSearchGrounding bool
	enableCodeTools    bool
	skipWorkspace      bool
)

var codeCmd = &cobra.Command{
	Use:   "code [instructions]",
	Short: "Generate updated code based on instructions",
	Long: `Processes a file or generates new files based on natural language instructions using an LLM.

When using the --image flag, ensure your model supports vision input. Vision-capable models include:
  - openai:gpt-4o, openai:gpt-4-turbo, openai:gpt-4-vision-preview
  - deepinfra:google/gemini-2.5-flash, deepinfra:google/gemini-2.5-pro`,
	Run: func(cmd *cobra.Command, args []string) {
		instructions := ""
		if len(args) > 0 {
			instructions = args[0]
		}

		// Log the original user prompt Tangent
		utils.LogUserPrompt(instructions)

		// Check if instructions are provided
		if instructions == "" {
			ui.Out().Print(prompts.InstructionsRequired() + "\n")
			cmd.Help() // Print help for the code command
			return     // Exit the command execution
		}

		cfg, err := config.LoadOrInitConfig(skipPrompt)
		if err != nil {
			log.Fatal(prompts.ConfigLoadFailed(err))
		}

		if model != "" {
			cfg.EditingModel = model
		}
		cfg.SkipPrompt = skipPrompt
		// Always use interactive flow; tool usage is controlled by CodeToolsEnabled
		cfg.Interactive = true
		cfg.UseSearchGrounding = useSearchGrounding
		cfg.CodeToolsEnabled = enableCodeTools
		ui.Out().Print(prompts.ProcessingCodeGeneration() + "\n")
		startTime := time.Now()

		_, err = editor.ProcessCodeGeneration(filename, instructions, cfg, imagePath)
		if err != nil {
			log.Fatal(prompts.CodeGenerationError(err))
		}
		duration := time.Since(startTime)

		// Display completion message with timing
		ui.Out().Print(prompts.CodeGenerationFinished(duration))

		// If we have token usage information, display it
		if cfg.LastTokenUsage != nil {
			cost := llm.CalculateCost(llm.TokenUsage(*cfg.LastTokenUsage), cfg.EditingModel)
			ui.Out().Printf("Token Usage: %d prompt + %d completion = %d total (Cost: $%.4f)\n",
				cfg.LastTokenUsage.PromptTokens,
				cfg.LastTokenUsage.CompletionTokens,
				cfg.LastTokenUsage.TotalTokens,
				cost)
		}
	},
}

func init() {
	codeCmd.Flags().StringVarP(&filename, "filename", "f", "", "The filename to process (optional)")
	codeCmd.Flags().StringVarP(&model, "model", "m", "", "Model name to use with the LLM")
	codeCmd.Flags().BoolVar(&skipPrompt, "skip-prompt", false, "Skip user prompt for applying changes")
	codeCmd.Flags().StringVarP(&imagePath, "image", "i", "", "Path to an image file to use as UI reference")
	codeCmd.Flags().BoolVar(&useSearchGrounding, "use-search-grounding", false, "Enable web content search grounding when instructions contain #SG [optional query]")
	codeCmd.Flags().BoolVar(&enableCodeTools, "enable-code-tools", true, "Allow tool-calls during code generation (e.g., search, file reads). Default: enabled.")
	codeCmd.Flags().BoolVar(&skipWorkspace, "skip-workspace", false, "Do not include workspace context by default")
}
