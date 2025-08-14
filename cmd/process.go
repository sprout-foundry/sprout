package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/orchestration"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/spf13/cobra"
)

var createExample bool
var resume bool
var statePath string
var noProgress bool

// processCmd represents the process command
var processCmd = &cobra.Command{
	Use:   "process [process-file]",
	Short: "Executes a multi-agent orchestration process.",
	Long: `Multi-Agent Process Mode:
- Loads a process file defining agents, steps, and dependencies
- Coordinates multiple agents with specialized personas (e.g., frontend developer, backend architect, QA engineer)
- Executes steps in dependency order
- Tracks progress and agent status
- Supports budget controls and cost management per agent

Examples:
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

		// Multi-agent process mode
		if err := runMultiAgentProcess(input, logger); err != nil {
			logger.LogProcessStep(fmt.Sprintf("Multi-agent process failed: %v", err))
			log.Fatalf("Error during multi-agent process: %v", err)
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
	if noProgress {
		_ = os.Setenv("LEDIT_NO_PROGRESS", "1")
	}

	// Load the process file
	loader := orchestration.NewProcessLoader()
	processFile, err := loader.LoadProcessFile(processFilePath)
	if err != nil {
		logger.LogProcessStep(fmt.Sprintf("Error loading process file: %v", err))
		return fmt.Errorf("failed to load process file: %w", err)
	}

	// Create and execute the multi-agent orchestrator
	orchestrator := orchestration.NewMultiAgentOrchestrator(processFile, cfg, logger, resume, statePath)
	if err := orchestrator.Execute(); err != nil {
		logger.LogProcessStep(fmt.Sprintf("Multi-agent orchestration failed: %v", err))
		return fmt.Errorf("multi-agent orchestration failed: %w", err)
	}

	logger.LogProcessStep("✅ Multi-agent orchestration completed successfully")
	return nil
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
	processCmd.Flags().BoolVar(&resume, "resume", false, "Resume from a previous orchestration state if compatible")
	processCmd.Flags().StringVar(&statePath, "state", "", "Path to orchestration state file (default .ledit/orchestration_state.json)")
	processCmd.Flags().BoolVar(&noProgress, "no-progress", false, "Suppress progress table output during orchestration")
	rootCmd.AddCommand(processCmd)
}
