// Agent command for ledit
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/console/components"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// isTerminal checks if stdout is a terminal (not piped)
func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

var (
	agentSkipPrompt       bool
	agentModel            string // Declare agentModel variable
	agentProvider         string // Declare agentProvider variable
	agentDryRun           bool
	maxIterations         int
	agentNoStreaming      bool   // Disable streaming mode (streaming is default)
	agentSystemPromptFile string // File path for custom system prompt
	agentSystemPrompt     string // Direct system prompt string
)

func createChatAgent() (*agent.Agent, error) {
	var chatAgent *agent.Agent
	var err error

	if agentProvider != "" && agentModel != "" {
		// Both provider and model specified - use them directly
		modelWithProvider := fmt.Sprintf("%s:%s", agentProvider, agentModel)
		chatAgent, err = agent.NewAgentWithModel(modelWithProvider)
	} else if agentModel != "" {
		// Only model specified - use existing behavior
		chatAgent, err = agent.NewAgentWithModel(agentModel)
	} else {
		// Neither specified - use defaults
		chatAgent, err = agent.NewAgent()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to initialize agent: %w", err)
	}

	// Set custom system prompt if specified
	if agentSystemPrompt != "" {
		chatAgent.SetSystemPrompt(agentSystemPrompt)
	} else if agentSystemPromptFile != "" {
		if err := chatAgent.SetSystemPromptFromFile(agentSystemPromptFile); err != nil {
			return nil, fmt.Errorf("failed to load system prompt from file: %w", err)
		}
	}

	// Set max iterations if specified
	chatAgent.SetMaxIterations(maxIterations)

	// Enable streaming by default unless disabled or output is piped
	// Note: OpenAI streaming doesn't include token usage data, but we'll enable it anyway
	// for better UX in interactive mode
	if !agentNoStreaming && isTerminal() {
		// Enable streaming for all providers in interactive mode
		chatAgent.EnableStreaming(nil)
	}

	// Streaming behavior is uniform across providers unless --no-stream is set.

	return chatAgent, nil
}

func init() {
	agentCmd.Flags().BoolVar(&agentSkipPrompt, "skip-prompt", false, "Skip user prompts (enhanced by automated validation)")
	agentCmd.Flags().StringVarP(&agentModel, "model", "m", "", "Model name for agent system")
	agentCmd.Flags().StringVarP(&agentProvider, "provider", "p", "", "Provider to use (openai, openrouter, deepinfra, zai, ollama, ollama-local, ollama-turbo, lmstudio)")
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "Run tools in simulation mode (enhanced safety)")
	agentCmd.Flags().IntVar(&maxIterations, "max-iterations", 1000, "Maximum iterations before stopping (default: 1000)")
	agentCmd.Flags().BoolVar(&agentNoStreaming, "no-stream", false, "Disable streaming mode (useful for scripts and pipelines) (or set LEDIT_NO_STREAM=1)")
	agentCmd.Flags().StringVar(&agentSystemPromptFile, "system-prompt", "", "File path containing custom system prompt")
	agentCmd.Flags().StringVar(&agentSystemPrompt, "system-prompt-str", "", "Direct system prompt string")
	
	// Initialize environment-based defaults
	cobra.OnInitialize(func() {
		// Check for LEDIT_NO_STREAM environment variable
		if os.Getenv("LEDIT_NO_STREAM") == "1" || os.Getenv("LEDIT_NO_STREAM") == "true" {
			agentNoStreaming = true
		}
	})
}

// runSimpleInteractiveMode provides a simple console-based interactive mode
func runInteractiveMode(chatAgent *agent.Agent) error {
	// Create console app with interactive mode configuration
	app := console.NewConsoleAppWithMode(console.OutputModeInteractive)

	// Configure components for interactive mode
	config := app.GetConfig()
	config.Components = []console.ComponentConfig{
		{
			ID:      "agent-console",
			Type:    "agent",
			Region:  "main",
			Enabled: true,
		},
	}

	// Initialize app
	if err := app.Init(config); err != nil {
		return fmt.Errorf("failed to initialize console app: %w", err)
	}

	// Create and add agent console component
	agentConsole := components.NewAgentConsole(chatAgent, nil)
	if err := app.AddComponent(agentConsole); err != nil {
		return fmt.Errorf("failed to add agent console: %w", err)
	}

	// Register cleanup to ensure terminal is fully restored (controller + terminal)
	console.RegisterCleanup(func() error {
		return app.Cleanup()
	})

	// Ensure cleanup happens on panic
	defer console.RunCleanup()

	// Run the app
	err := app.Run()

	// Check if user requested quit
	if errors.Is(err, components.ErrUserQuit) {
		return nil // Normal exit
	}

	return err
}

// executeDirectAgentCommand executes an agent command directly (like coder does)
func executeDirectAgentCommand(chatAgent *agent.Agent, userIntent string) error {
	// Create CI output handler - it handles both CI and direct execution
	outputHandler := console.NewCIOutputHandler(os.Stdout)

	// Ensure shutdown on exit
	defer chatAgent.Shutdown()

	// Set up stats callback to update CI handler
	chatAgent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
		outputHandler.UpdateMetrics(
			totalTokens,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetMaxContextTokens(),
			chatAgent.GetCurrentIteration(),
			totalCost,
		)
	})

	// Mark as CI mode if detected to adjust formatting
	if outputHandler.IsCI() {
		os.Setenv("LEDIT_CI_MODE", "1")
		defer os.Unsetenv("LEDIT_CI_MODE")
	}

	// Show initial message
	outputHandler.Printf("Processing query with model: %s from: %s\n\n", chatAgent.GetModel(), chatAgent.GetProvider())

	// Start progress ticker for CI mode
	var progressTicker *time.Ticker
	var progressDone chan bool
	if outputHandler.IsCI() {
		progressTicker = time.NewTicker(2 * time.Second)
		progressDone = make(chan bool)

		go func() {
			for {
				select {
				case <-progressTicker.C:
					outputHandler.PrintProgress()
				case <-progressDone:
					return
				}
			}
		}()
	}

	// Set up streaming callback that uses the output handler's filtering
	// Only enable streaming if not explicitly disabled
	if !agentNoStreaming {
		chatAgent.EnableStreaming(func(content string) {
			outputHandler.Write([]byte(content))
		})
	}
	defer chatAgent.DisableStreaming()

	// Also ensure we flush any remaining content at the end
	defer func() {
		// Force a final newline if needed
		outputHandler.Write([]byte("\n"))
	}()

	// Process the query
	response, err := chatAgent.ProcessQueryWithContinuity(userIntent)

	// Stop progress ticker if running
	if progressTicker != nil {
		progressTicker.Stop()
		close(progressDone)
	}

	if err != nil {
		return fmt.Errorf("agent processing failed: %w", err)
	}

	// In streaming mode, response is usually empty since content was streamed
	// Only print if we have additional content (and it's not just the completion signal)
	if response != "" && !strings.Contains(response, "[[TASK_COMPLETE]]") {
		outputHandler.WriteString("\n" + response + "\n")
	}

	outputHandler.PrintSummary()

	return nil
}

// agentCmd represents the agent command
var agentCmd = &cobra.Command{
	Use:   "agent [intent]",
	Short: "AI agent for code analysis and editing (default when running 'ledit' alone)",
	Long: `AI agent mode for intelligent code analysis and editing.

Features:
• Error recovery and malformed tool call detection
• Vision analysis capabilities for UI components
• Esc key interrupt handling for interactive control
• Conversation optimization
• Context management and optimization
• Intelligent fallback and retry mechanisms

The agent runs in two modes:

1. **Interactive Mode**:
   - Real-time progress tracking
   - Conversation optimization
   - Error handling and recovery
   - Dynamic reasoning effort adjustment

2. **Direct Mode**:
   - Systematic exploration and structured approach
   - Tool execution with atomic operations
   - Context management and optimization
   - Intelligent fallback and retry mechanisms

Workflow (Phase-based approach):
PHASE 1: UNDERSTAND & PLAN - Break task into specific steps
PHASE 2: EXPLORE - Systematic codebase exploration
PHASE 3: IMPLEMENT - Careful changes with file operations
PHASE 4: VERIFY & COMPLETE - Testing and quality assurance

Examples:
  # Interactive mode (automatic when no arguments provided)
  ledit agent
  
  # Direct mode
  ledit agent "Add better error handling to the main function"
  ledit agent "How does the authentication system work?"
  
  # With specific provider and model
  ledit agent --provider openrouter --model "qwen/qwen3-coder-30b" "Fix the login bug"
  ledit agent -p deepinfra -m "deepseek-v3" "Analyze the codebase structure"

  # UI mode for follow-up interaction
  ledit agent --ui "Fix the login bug"`,
	Args: cobra.MaximumNArgs(1), // Allow 0 or 1 args for interactive mode
	RunE: func(cmd *cobra.Command, args []string) error {
		chatAgent, err := createChatAgent()
		if err != nil {
			return err
		}

		// Mark environment flags
		_ = os.Setenv("LEDIT_FROM_AGENT", "1")

		if agentDryRun {
			_ = os.Setenv("LEDIT_DRY_RUN", "1")
		}

		// Check if we're in a CI environment or non-interactive mode
		isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""

		// Check if stdin is a terminal (not piped)
		stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))

		// We're interactive only if we have a terminal, no args (or UI flag), and not in CI
		isInteractive := (len(args) == 0 || enableUI) && !isCI && stdinIsTerminal
		var userIntent string

		if isInteractive {
			if len(args) > 0 {
				userIntent = strings.Join(args, " ")

				// Set up a simple stats callback for direct command execution
				// This ensures token/cost tracking works even before console setup
				chatAgent.SetStatsUpdateCallback(func(tokens int, cost float64) {
					// Simple callback that just ensures the mechanism works
					// The console will set its own callback later
				})

				err := executeDirectAgentCommand(chatAgent, userIntent)
				if err != nil {
					return fmt.Errorf("direct agent command failed: %w", err)
				}
				fmt.Println("\n✅ Initial task completed. Entering interactive mode for follow-up questions...")
			}
			return runInteractiveMode(chatAgent)
		} else {
			// Direct mode - execute single command or handle CI/piped input without args
			if len(args) == 0 {
				// Check if we have piped input
				if !stdinIsTerminal {
					// Read from stdin
					scanner := bufio.NewScanner(os.Stdin)
					if scanner.Scan() {
						userIntent = scanner.Text()
						err := executeDirectAgentCommand(chatAgent, userIntent)
						if err != nil {
							fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n", err)
							return fmt.Errorf("direct agent command failed: %w", err)
						}
						fmt.Println("✅ Task completed successfully")
						return nil
					}
				}

				// In CI with no args and no piped input, show welcome message and exit gracefully
				fmt.Println("Welcome to ledit! 🤖")
				fmt.Println("Agent initialized successfully in CI environment.")
				fmt.Println("Use 'ledit agent \"your query\"' to execute commands.")
				return nil
			}

			userIntent = strings.Join(args, " ")
			err := executeDirectAgentCommand(chatAgent, userIntent)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n", err)
				return fmt.Errorf("direct agent command failed: %w", err)
			}
			fmt.Println("✅ Task completed successfully")
			return nil
		}
	},
}
