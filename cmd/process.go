package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/orchestration"
	"github.com/alantheprice/ledit/pkg/prompts"
	tuiPkg "github.com/alantheprice/ledit/pkg/tui"
	uiPkg "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/spf13/cobra"
)

var createExample bool
var resume bool
var statePath string
var noProgress bool
var dryRun bool
var skipPrompt bool // TODO: Migrate to new BaseCommand framework
var model string    // TODO: Migrate to new BaseCommand framework

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
	  ledit process --create-example process.json
	  ledit process   # interactive authoring if no file is provided`,
	Args: cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		logger := utils.GetLogger(skipPrompt)

		// Handle create-example flag
		if createExample {
			out := "process.json"
			if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
				out = args[0]
			}
			if err := createExampleProcessFile(out, logger); err != nil {
				gracefulExitMsg := prompts.NewGracefulExitWithTokenUsage(
					"Creating example process file",
					err,
					nil,
					"",
				)
				fmt.Fprint(os.Stderr, gracefulExitMsg)
				os.Exit(1)
			}
			return
		}

		// Fallback: if no process file provided, either interactive dry-run or interactive authoring flow
		if len(args) == 0 {
			if dryRun {
				if err := interactiveAuthorProcessDryRun(logger); err != nil {
					logger.LogProcessStep(fmt.Sprintf("Interactive dry-run failed: %v", err))
					gracefulExitMsg := prompts.NewGracefulExitWithTokenUsage(
						"Interactive process authoring dry-run",
						err,
						nil,
						"",
					)
					fmt.Fprint(os.Stderr, gracefulExitMsg)
					os.Exit(1)
				}
				return
			}
			input, err := interactiveAuthorProcessFile(logger)
			if err != nil {
				logger.LogProcessStep(fmt.Sprintf("Interactive authoring failed: %v", err))
				gracefulExitMsg := prompts.NewGracefulExitWithTokenUsage(
					"Interactive process authoring",
					err,
					nil,
					"",
				)
				fmt.Fprint(os.Stderr, gracefulExitMsg)
				os.Exit(1)
			}
			args = []string{input}
		}

		input := args[0]

		// If dry-run with provided file: validate only and exit
		if dryRun {
			if err := validateProcessOnly(input, logger); err != nil {
				logger.LogProcessStep(fmt.Sprintf("Dry-run validation failed: %v", err))
				gracefulExitMsg := prompts.NewGracefulExitWithTokenUsage(
					"Process validation (dry-run)",
					err,
					nil,
					"",
				)
				fmt.Fprint(os.Stderr, gracefulExitMsg)
				os.Exit(1)
			}
			return
		}

		// Multi-agent process mode
		if uiPkg.IsUIActive() {
			uiPkg.SetDefaultSink(uiPkg.TuiSink{})
			go func() { _ = tuiPkg.Run() }()
		}
		if err := runMultiAgentProcess(input, logger); err != nil {
			logger.LogProcessStep(fmt.Sprintf("Multi-agent process failed: %v", err))
			gracefulExitMsg := prompts.NewGracefulExitWithTokenUsage(
				"Multi-agent orchestration process",
				err,
				nil, // Could potentially get token usage from orchestration state
				"",
			)
			fmt.Fprint(os.Stderr, gracefulExitMsg)
			os.Exit(1)
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
	processCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate process file (or interactively author a draft) without executing")
	rootCmd.AddCommand(processCmd)
}

// validateProcessOnly loads and validates a process.json without executing
func validateProcessOnly(processFilePath string, logger *utils.Logger) error {
	loader := orchestration.NewProcessLoader()
	logger.LogProcessStep("🔎 Dry-run: validating process file")
	if _, err := loader.LoadProcessFile(processFilePath); err != nil {
		return err
	}
	logger.LogProcessStep("✅ Process file is valid")
	return nil
}

// interactiveAuthorProcessDryRun runs the interactive authoring but does not write or execute
func interactiveAuthorProcessDryRun(logger *utils.Logger) error {
	if skipPrompt {
		return fmt.Errorf("interactive dry-run requires prompts; pass a file or omit --skip-prompt")
	}
	cfg, err := config.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	cfg.SkipPrompt = false
	sys := "You are a Process Designer for a multi-agent code orchestration tool. Ask the user questions using ask_user and then output ONLY a valid JSON process file."
	msgs := []prompts.Message{{Role: "system", Content: sys}, {Role: "user", Content: "Help me author a process.json for my goal. Ask me questions, then output JSON only when ready."}}
	response, err := llm.CallLLMWithInteractiveContext(cfg.OrchestrationModel, msgs, "process_authoring_dry_run", cfg, 0, func(_ []llm.ContextRequest, _ *config.Config) (string, error) { return "", nil })
	if err != nil {
		return fmt.Errorf("LLM interactive authoring failed: %w", err)
	}
	jsonStr, err := utils.ExtractJSON(response)
	if err != nil {
		jsonStr = ""
	}
	if strings.TrimSpace(jsonStr) == "" {
		return fmt.Errorf("did not receive JSON process definition from the LLM")
	}
	loader := orchestration.NewProcessLoader()
	if _, err := loader.LoadProcessFromBytes([]byte(jsonStr)); err != nil {
		logger.LogProcessStep("❌ Generated process JSON did not validate")
		return err
	}
	logger.LogProcessStep("✅ Dry-run OK: generated process.json is valid (not saved)")
	return nil
}

// interactiveAuthorProcessFile guides the user through creating a process file via an LLM chat
func interactiveAuthorProcessFile(logger *utils.Logger) (string, error) {
	if skipPrompt {
		return "", fmt.Errorf("no process file provided in non-interactive mode; pass a file or use --create-example <path>")
	}
	// Load config to get models and enable tool support
	cfg, err := config.LoadOrInitConfig(false)
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}
	cfg.SkipPrompt = false

	// Choose default output path
	outPath := "process.json"
	reader := bufio.NewReader(os.Stdin)

	// Simple prompt to let the user override filename
	logger.LogUserInteraction("Enter output file path for the process (default: process.json): ")
	if line, _ := reader.ReadString('\n'); strings.TrimSpace(line) != "" {
		outPath = strings.TrimSpace(line)
	}

	// Compose interactive system prompt
	sys := "You are a Process Designer for a multi-agent code orchestration tool.\n" +
		"Your job is to interview the user (use ask_user tool) and then output a complete JSON process file that matches the ProcessFile schema.\n" +
		"Schema keys: version, goal, description, base_model, agents[], steps[], validation, settings.\n" +
		"Agents must include id, name, persona, description, skills, model, priority, depends_on, config, budget.\n" +
		"Steps must include id, name, description, agent_id, input, expected_output, depends_on, timeout, retries.\n" +
		"When you have enough info, output ONLY the final JSON (no prose)."

	// Seed conversation asking the model to collect requirements first, then produce JSON
	msgs := []prompts.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: "Help me author a process.json for my goal. Ask me questions as needed, then output JSON only when ready."},
	}

	// Use the interactive tools loop so the model can ask the user questions via ask_user
	response, err := llm.CallLLMWithInteractiveContext(cfg.OrchestrationModel, msgs, "process_authoring", cfg, 0, func(_ []llm.ContextRequest, _ *config.Config) (string, error) {
		return "", nil
	})
	if err != nil {
		return "", fmt.Errorf("LLM interactive authoring failed: %w", err)
	}

	// Extract JSON from response
	jsonStr, err := utils.ExtractJSON(response)
	if err != nil {
		jsonStr = ""
	}
	if strings.TrimSpace(jsonStr) == "" {
		return "", fmt.Errorf("did not receive JSON process definition from the LLM")
	}

	// Validate using ProcessLoader
	loader := orchestration.NewProcessLoader()
	if _, err := loader.LoadProcessFromBytes([]byte(jsonStr)); err != nil {
		logger.LogProcessStep(fmt.Sprintf("Generated process JSON did not validate: %v", err))
		logger.LogProcessStep("You can copy the JSON above, fix it, and try again.")
		return "", fmt.Errorf("invalid generated process.json: %w", err)
	}

	// Show a brief confirmation and write
	logger.LogProcessStep("A valid process.json was generated. Proceed to save and start the run?")
	if !logger.AskForConfirmation("Save and start now?", true, true) {
		return "", fmt.Errorf("user aborted before saving process file")
	}
	if err := filesystem.SaveFile(outPath, jsonStr); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", outPath, err)
	}
	return outPath, nil
}
