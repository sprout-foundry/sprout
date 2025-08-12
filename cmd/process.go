package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/orchestration"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"

	"github.com/spf13/cobra"
)

var createExample bool

// processCmd represents the process command
var processCmd = &cobra.Command{
	Use:   "process [prompt|process-file]",
	Short: "Executes a large feature implementation or multi-agent orchestration process.",
	Long: `This command supports two modes:

1. Single Prompt Mode: Provide a prompt to analyze and implement a feature
2. Multi-Agent Process Mode: Provide a process file to orchestrate multiple agents with different personas

Multi-Agent Process Mode:
- Loads a process file defining agents, steps, and dependencies
- Coordinates multiple agents with specialized personas (e.g., frontend developer, backend architect, QA engineer)
- Executes steps in dependency order
- Tracks progress and agent status

Examples:
  ledit process "Implement user authentication system"
  ledit process process.json
  ledit process --create-example process.json`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		input := args[0]
		logger := utils.GetLogger(skipPrompt)

		// Handle create-example flag
		if createExample {
			if err := createExampleProcessFile(input, logger); err != nil {
				logger.LogProcessStep(fmt.Sprintf("Failed to create example process file: %v", err))
				log.Fatalf("Error creating example process file: %v", err)
			}
			return
		}

		// Check if this is a process file or a prompt
		if strings.HasSuffix(input, ".json") || strings.HasSuffix(input, ".process") {
			// Multi-agent process mode
			if err := runMultiAgentProcess(input, logger); err != nil {
				logger.LogProcessStep(fmt.Sprintf("Multi-agent process failed: %v", err))
				log.Fatalf("Error during multi-agent process: %v", err)
			}
		} else {
			// Single prompt mode (legacy)
			runSinglePromptProcess(input, logger)
		}
	},
}

// runMultiAgentProcess executes a multi-agent orchestration process
func runMultiAgentProcess(processFilePath string, logger *utils.Logger) error {
	logger.LogProcessStep("🚀 Starting multi-agent orchestration process")
	logger.LogProcessStep(fmt.Sprintf("Process file: %s", processFilePath))

	// Load configuration
	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		logger.LogProcessStep(fmt.Sprintf("Error loading config: %v", err))
		return fmt.Errorf("failed to load config: %w", err)
	}

	if model != "" {
		cfg.EditingModel = model
		cfg.OrchestrationModel = model
	}
	cfg.SkipPrompt = skipPrompt

	// Load the process file
	loader := orchestration.NewProcessLoader()
	processFile, err := loader.LoadProcessFile(processFilePath)
	if err != nil {
		logger.LogProcessStep(fmt.Sprintf("Error loading process file: %v", err))
		return fmt.Errorf("failed to load process file: %w", err)
	}

	// Create and execute the multi-agent orchestrator
	orchestrator := orchestration.NewMultiAgentOrchestrator(processFile, cfg, logger)
	if err := orchestrator.Execute(); err != nil {
		logger.LogProcessStep(fmt.Sprintf("Multi-agent orchestration failed: %v", err))
		return fmt.Errorf("multi-agent orchestration failed: %w", err)
	}

	logger.LogProcessStep("✅ Multi-agent orchestration completed successfully")
	return nil
}

// runSinglePromptProcess executes the legacy single-prompt orchestration
func runSinglePromptProcess(prompt string, logger *utils.Logger) {
	logger.LogProcessStep("🚀 Starting single-prompt orchestration process")
	logger.LogProcessStep(fmt.Sprintf("Prompt: %s", prompt))

	// Add the alpha warning for orchestration
	logger.LogProcessStep(prompts.OrchestrationAlphaWarning())

	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		logger.LogProcessStep(prompts.ConfigLoadFailed(err))
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	logger.Logf("Using configuration: %+v and model: %s", cfg, model)

	if model != "" {
		cfg.EditingModel = model
		cfg.OrchestrationModel = model
	}
	cfg.SkipPrompt = skipPrompt

	if err := orchestration.OrchestrateFeature(prompt, cfg); err != nil {
		logger.LogProcessStep(prompts.OrchestrationError(err))
		log.Fatalf("Error during orchestration: %v", err)
	}

	logger.LogProcessStep(prompts.OrchestrationFinishedSuccessfully())
}

// createExampleProcessFile creates an example process file
func createExampleProcessFile(filePath string, logger *utils.Logger) error {
	logger.LogProcessStep("📝 Creating example process file")
	logger.LogProcessStep(fmt.Sprintf("File path: %s", filePath))

	loader := orchestration.NewProcessLoader()
	if err := loader.CreateExampleProcessFile(filePath); err != nil {
		return fmt.Errorf("failed to create example process file: %w", err)
	}

	logger.LogProcessStep("✅ Example process file created successfully")
	logger.LogProcessStep("You can now edit this file and run: ledit process " + filePath)
	return nil
}

func init() {
	processCmd.Flags().StringVarP(&model, "model", "m", "", "Model to use for orchestration and editing.")
	processCmd.Flags().BoolVar(&skipPrompt, "skip-prompt", false, "Skip the confirmation prompt and proceed with the plan")
	processCmd.Flags().BoolVar(&createExample, "create-example", false, "Create an example process file instead of executing")
	rootCmd.AddCommand(processCmd)
}
