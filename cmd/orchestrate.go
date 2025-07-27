package cmd

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"log"
	"os"

	"github.com/spf13/cobra"
)

// orchestrateCmd represents the orchestrate command
var orchestrateCmd = &cobra.Command{
	Use:   "orchestrate [prompt]",
	Short: "Orchestrates a large feature implementation based on a prompt.",
	Long: `Based on a prompt, this command will:
1. Analyze your current workspace.
2. Form a plan to implement the feature.
3. Execute the plan of file changes, asking for confirmation before each step.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		prompt := args[0]
		logger := utils.GetLogger(skipPrompt) // Get the logger instance

		cfg, err := config.LoadOrInitConfig(skipPrompt)
		if err != nil {
			logger.LogProcessStep(prompts.ConfigLoadFailed(err)) // Use prompt
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}
		logger.Logf("Using configuration: %+v and model: %s", cfg, model) // Log config details

		if model != "" {
			cfg.EditingModel = model
			cfg.OrchestrationModel = model // Use the same model for orchestration
		}
		cfg.SkipPrompt = skipPrompt

		if err := editor.OrchestrateFeature(prompt, cfg); err != nil {
			logger.LogProcessStep(prompts.OrchestrationError(err)) // Use prompt
			log.Fatalf("Error during orchestration: %v", err)
		}

		// Log the success and close out the orchestration process
		logger.LogProcessStep(prompts.OrchestrationFinishedSuccessfully()) // Use prompt
	},
}

func init() {
	orchestrateCmd.Flags().StringVarP(&model, "model", "m", "", "Model to use for orchestration and editing.")
	orchestrateCmd.Flags().BoolVar(&skipPrompt, "skip-prompt", false, "Skip the confirmation prompt and proceed with the plan")
	rootCmd.AddCommand(orchestrateCmd)
}
