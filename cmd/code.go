package cmd

import (
	"log"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"

	"github.com/spf13/cobra"
)

var (
	filename           string
	model              string
	skipPrompt         bool
	nonInteractive     bool
	imagePath          string
	useSearchGrounding bool
	enableCodeTools    bool
)

var codeCmd = &cobra.Command{
	Use:   "code [instructions]",
	Short: "Generate updated code based on instructions",
	Long: `Processes a file or generates new files based on natural language instructions using an LLM.

When using the --image flag, ensure your model supports vision input. Vision-capable models include:
  - openai:gpt-4o, openai:gpt-4-turbo, openai:gpt-4-vision-preview
  - gemini:gemini-1.5-flash, gemini:gemini-1.5-pro
  - anthropic:claude-3-sonnet, anthropic:claude-3-haiku, anthropic:claude-3-opus`,
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
		cfg.Interactive = !nonInteractive
		cfg.UseSearchGrounding = useSearchGrounding
		cfg.CodeToolsEnabled = enableCodeTools

		ui.Out().Print(prompts.ProcessingCodeGeneration() + "\n")
		startTime := time.Now()

		_, err = editor.ProcessCodeGeneration(filename, instructions, cfg, imagePath)
		if err != nil {
			log.Fatal(prompts.CodeGenerationError(err))
		}
		duration := time.Since(startTime)
		ui.Out().Print(prompts.CodeGenerationFinished(duration))
	},
}

func init() {
	codeCmd.Flags().StringVarP(&filename, "filename", "f", "", "The filename to process (optional)")
	codeCmd.Flags().StringVarP(&model, "model", "m", "", "Model name to use with the LLM")
	codeCmd.Flags().BoolVar(&skipPrompt, "skip-prompt", false, "Skip user prompt for applying changes")
	codeCmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Disable interactive context requests from the LLM")
	codeCmd.Flags().StringVarP(&imagePath, "image", "i", "", "Path to an image file to use as UI reference")
	codeCmd.Flags().BoolVar(&useSearchGrounding, "use-search-grounding", false, "Enable web content search grounding when instructions contain #SG [optional query]")
	codeCmd.Flags().BoolVar(&enableCodeTools, "enable-code-tools", false, "Allow tool-calls during code generation (e.g., search, file reads). Default: disabled.")
}
