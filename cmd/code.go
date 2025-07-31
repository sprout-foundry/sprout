package cmd

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/prompts" // Import the new prompts package
	"github.com/alantheprice/ledit/pkg/utils" // Import the utils package
	"log"
	"time"

	"github.com/spf13/cobra"
)

var (
	filename                 string
	model                    string
	skipPrompt               bool
	nonInteractive           bool
	useGeminiSearchGrounding bool // New flag for Gemini Search Grounding
)

var codeCmd = &cobra.Command{
	Use:   "code [instructions]",
	Short: "Generate updated code based on instructions",
	Long:  `Processes a file or generates new files based on natural language instructions using an LLM.`,
	Run: func(cmd *cobra.Command, args []string) {
		instructions := ""
		if len(args) > 0 {
			instructions = args[0]
		}

		// Log the original user prompt
		utils.LogUserPrompt(instructions)

		// Check if instructions are provided
		if instructions == "" {
			fmt.Println(prompts.InstructionsRequired()) // Use new prompt
			cmd.Help()                                  // Print help for the code command
			return                                      // Exit the command execution
		}

		cfg, err := config.LoadOrInitConfig(skipPrompt)
		if err != nil {
			log.Fatalf(prompts.ConfigLoadFailed(err)) // Use prompt
		}

		if model != "" {
			cfg.EditingModel = model
		}
		cfg.SkipPrompt = skipPrompt
		cfg.Interactive = !nonInteractive

		// Set the config value from the command line flag if it was provided
		if cmd.Flags().Changed("gemini-search-grounding") {
			cfg.UseGeminiSearchGrounding = useGeminiSearchGrounding
		}

		fmt.Println(prompts.ProcessingCodeGeneration()) // Use prompt
		startTime := time.Now()

		_, err = editor.ProcessCodeGeneration(filename, instructions, cfg)
		if err != nil {
			log.Fatalf(prompts.CodeGenerationError(err)) // Use prompt
		}
		duration := time.Since(startTime)
		fmt.Printf(prompts.CodeGenerationFinished(duration)) // Use prompt
	},
}

func init() {
	codeCmd.Flags().StringVarP(&filename, "filename", "f", "", "The filename to process (optional)")
	codeCmd.Flags().StringVarP(&model, "model", "m", "", "Model name to use with the LLM")
	codeCmd.Flags().BoolVar(&skipPrompt, "skip-prompt", false, "Skip user prompt for applying changes")
	codeCmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Disable interactive context requests from the LLM")
	codeCmd.Flags().BoolVar(&useGeminiSearchGrounding, "gemini-search-grounding", false, "Enable Gemini Search Grounding (experimental)") // New flag
}
