// Agent command for ledit
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	agent_api "github.com/alantheprice/ledit/pkg/agent_api"
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
		// The agent console will set up its own streaming callback
		chatAgent.EnableStreaming(nil)
	}

	return chatAgent, nil
}

func init() {
	agentCmd.Flags().BoolVar(&agentSkipPrompt, "skip-prompt", false, "Skip user prompts (enhanced by automated validation)")
	agentCmd.Flags().StringVarP(&agentModel, "model", "m", "", "Model name for agent system")
	agentCmd.Flags().StringVarP(&agentProvider, "provider", "p", "", "Provider to use (openai, openrouter, deepinfra, ollama, ollama-local, ollama-turbo)")
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "Run tools in simulation mode (enhanced safety)")
	agentCmd.Flags().IntVar(&maxIterations, "max-iterations", 1000, "Maximum iterations before stopping (default: 1000)")
	agentCmd.Flags().BoolVar(&agentNoStreaming, "no-stream", false, "Disable streaming mode (useful for scripts and pipelines)")
	agentCmd.Flags().StringVar(&agentSystemPromptFile, "system-prompt", "", "File path containing custom system prompt")
	agentCmd.Flags().StringVar(&agentSystemPrompt, "system-prompt-str", "", "Direct system prompt string")
}

// runSimpleInteractiveMode provides a simple console-based interactive mode
func runInteractiveMode(chatAgent *agent.Agent) error {
	// Create console app
	app := console.NewConsoleApp()

	// Configure app
	config := &console.Config{
		RawMode:      true,
		MouseEnabled: false,
		AltScreen:    true, // Use alternate screen buffer like vim
		Components: []console.ComponentConfig{
			{
				ID:      "agent-console",
				Type:    "agent",
				Region:  "main",
				Enabled: true,
			},
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

	// Register cleanup to ensure terminal is restored
	console.RegisterCleanup(func() error {
		return app.Stop()
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
	outputHandler.WriteString("🔄 Processing query...\n\n")

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
	chatAgent.EnableStreaming(func(content string) {
		outputHandler.Write([]byte(content))
	})
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

	// Print summary
	outputHandler.PrintSummary()

	// Also print agent's concise summary for backwards compatibility
	// (Skip in CI mode since we already have a summary)
	if !outputHandler.IsCI() {
		chatAgent.PrintConciseSummary()
	}

	return nil
}

// listModels displays all available models for the current provider
func listModels(chatAgent *agent.Agent) error {
	clientType := chatAgent.GetProviderType()
	providerName := agent_api.GetProviderName(clientType)

	fmt.Printf("\n📋 Available Models (%s):\n", providerName)
	fmt.Println("====================")

	models, err := agent_api.GetModelsForProvider(clientType)
	if err != nil {
		return fmt.Errorf("failed to get available models: %w", err)
	}

	if len(models) == 0 {
		fmt.Printf("No models available for %s.\n", providerName)
		fmt.Println()
		fmt.Println("💡 Tip: Use '/providers select' to switch to a different provider")
		return nil
	}

	// Sort models alphabetically by model ID (like original coder)
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	// Identify featured models
	featuredIndices := findFeaturedModels(models, clientType)

	// Display all models with full information like original coder
	for i, model := range models {
		fmt.Printf("%d. %s\n", i+1, model.ID)
		if model.Description != "" {
			fmt.Printf("   Description: %s\n", model.Description)
		}
		if model.Size != "" {
			fmt.Printf("   Size: %s\n", model.Size)
		}
		if model.InputCost > 0 || model.OutputCost > 0 {
			if model.InputCost > 0 && model.OutputCost > 0 {
				fmt.Printf("   Cost: $%.3f/M input, $%.3f/M output tokens\n", model.InputCost, model.OutputCost)
			} else if model.Cost > 0 {
				// Fallback to legacy format
				fmt.Printf("   Cost: ~$%.2f/M tokens\n", model.Cost)
			}
		} else if model.Provider == "Ollama (Local)" {
			fmt.Printf("   Cost: FREE (local)\n")
		} else {
			fmt.Printf("   Cost: N/A\n")
		}
		if model.ContextLength > 0 {
			fmt.Printf("   Context: %d tokens\n", model.ContextLength)
		}
		if len(model.Tags) > 0 {
			// Highlight tool support
			hasTools := false
			for _, tag := range model.Tags {
				if tag == "tools" || tag == "tool_choice" {
					hasTools = true
					break
				}
			}
			if hasTools {
				fmt.Printf("   🛠️  Supports tools: %s\n", strings.Join(model.Tags, ", "))
			} else {
				fmt.Printf("   Features: %s\n", strings.Join(model.Tags, ", "))
			}
		}
		fmt.Println()
	}

	// Display featured models section
	if len(featuredIndices) > 0 {
		fmt.Println("⭐ Featured Models (Popular & High Performance):")
		fmt.Println("================================================")
		for _, idx := range featuredIndices {
			model := models[idx]
			fmt.Printf("%d. %s", idx+1, model.ID)
			if model.InputCost > 0 && model.OutputCost > 0 {
				fmt.Printf(" - $%.3f/$%.3f per M tokens", model.InputCost, model.OutputCost)
			} else if model.Cost > 0 {
				fmt.Printf(" - ~$%.2f/M tokens", model.Cost)
			} else if model.Provider == "Ollama (Local)" {
				fmt.Printf(" - FREE")
			} else {
				fmt.Printf(" - N/A")
			}
			if model.ContextLength > 0 {
				fmt.Printf(" - %dK context", model.ContextLength/1000)
			}
			fmt.Println()
		}
		fmt.Println()
	}

	fmt.Println("Usage:")
	fmt.Println("  /models select          - Interactive model selection (current provider)")
	fmt.Println("  /models <model_id>      - Set model directly")
	fmt.Println("  /models                 - Show this list")
	fmt.Println("  /providers select        - Switch providers first, then select models")

	return nil
}

// findFeaturedModels identifies indices of featured models
// Now that we've removed the featured models concept, this returns an empty list
func findFeaturedModels(models []agent_api.ModelInfo, clientType agent_api.ClientType) []int {
	// Featured models concept has been removed - all models are treated equally
	return []int{}
}

// selectModel allows interactive model selection from the current provider
func selectModel(chatAgent *agent.Agent) error {
	clientType := chatAgent.GetProviderType()
	providerName := agent_api.GetProviderName(clientType)

	models, err := agent_api.GetModelsForProvider(clientType)
	if err != nil {
		return fmt.Errorf("failed to get available models: %w", err)
	}

	if len(models) == 0 {
		fmt.Printf("No models available for %s.\n", providerName)
		fmt.Println()
		fmt.Println("💡 Tip: Use '/providers select' to switch to a different provider with available models")
		return nil
	}

	// Sort models alphabetically by model ID (like original coder)
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	// Identify featured models
	featuredIndices := findFeaturedModels(models, clientType)

	fmt.Printf("\n🎯 Select a Model (%s):\n", providerName)
	fmt.Println("==================")

	fmt.Printf("All %s Models:\n", providerName)
	fmt.Println("===============")
	// Display all models with numbers and pricing info
	for i, model := range models {
		fmt.Printf("%d. \x1b[34m%s\x1b[0m", i+1, model.ID)
		if model.InputCost > 0 && model.OutputCost > 0 {
			fmt.Printf(" - $%.3f/$%.3f per M tokens", model.InputCost, model.OutputCost)
		} else if model.Cost > 0 {
			fmt.Printf(" - ~$%.2f/M tokens", model.Cost)
		} else if providerName == "Ollama (Local)" {
			fmt.Printf(" - FREE")
		} else {
			fmt.Printf(" - N/A")
		}
		fmt.Println()
	}

	// Display featured models at the end if any exist
	if len(featuredIndices) > 0 {
		fmt.Println("\n⭐ Featured Models (Popular & High Performance):")
		fmt.Println("================================================")
		for _, idx := range featuredIndices {
			model := models[idx]
			fmt.Printf("%d. \x1b[34m%s\x1b[0m", idx+1, model.ID)
			if model.InputCost > 0 && model.OutputCost > 0 {
				fmt.Printf(" - $%.3f/$%.3f per M tokens", model.InputCost, model.OutputCost)
			} else if model.Cost > 0 {
				fmt.Printf(" - ~$%.2f/M tokens", model.Cost)
			} else if providerName == "Ollama (Local)" {
				fmt.Printf(" - FREE")
			} else {
				fmt.Printf(" - N/A")
			}
			if model.ContextLength > 0 {
				fmt.Printf(" - %dK context", model.ContextLength/1000)
			}
			fmt.Println()
		}
		fmt.Println()
	}

	// Get user selection
	fmt.Printf("\nEnter model number (1-%d) or 'cancel': ", len(models))

	// Temporarily disable escape monitoring during user input to avoid interference
	chatAgent.DisableEscMonitoring()
	defer chatAgent.DisableEscMonitoring() // Keep it disabled after this function

	// Use bufio.Scanner for better input handling
	scanner := bufio.NewScanner(os.Stdin)
	var input string
	if scanner.Scan() {
		input = strings.TrimSpace(scanner.Text())
	}

	if input == "cancel" || input == "" {
		fmt.Println("Model selection cancelled.")
		return nil
	}

	// Parse selection
	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > len(models) {
		return fmt.Errorf("invalid selection. Please enter a number between 1 and %d", len(models))
	}

	selectedModel := models[selection-1]
	return setModel(selectedModel.ID, chatAgent)
}

// setModel sets the specified model for the agent
func setModel(modelID string, chatAgent *agent.Agent) error {
	// Update the agent's model
	err := chatAgent.SetModel(modelID)
	if err != nil {
		return fmt.Errorf("failed to set model: %w", err)
	}

	fmt.Printf("✅ Model set to: %s\n", modelID)
	return nil
}

// showCurrentProvider displays current provider information
func showCurrentProvider(chatAgent *agent.Agent) error {
	clientType := chatAgent.GetProviderType()
	providerName := agent_api.GetProviderName(clientType)
	modelName := chatAgent.GetModel()

	fmt.Printf("\n📡 Current Provider: %s\n", providerName)
	fmt.Printf("🤖 Current Model: %s\n", modelName)
	fmt.Println()
	fmt.Println("Use '/providers select' to switch providers")
	fmt.Println("Use '/models select' to switch models")

	return nil
}

// listProviders displays all available providers
func listProviders() error {
	fmt.Println("\n📡 Available Providers:")
	fmt.Println("======================")
	fmt.Println("1. DeepInfra")
	fmt.Println("2. OpenRouter")
	fmt.Println("3. Ollama (Local)")
	fmt.Println()
	fmt.Println("Use '/providers select' to switch providers")

	return nil
}

// selectProvider allows interactive provider selection
func selectProvider(chatAgent *agent.Agent) error {
	fmt.Println("\n🎯 Select a Provider:")
	fmt.Println("====================")
	fmt.Println("1. DeepInfra")
	fmt.Println("2. OpenRouter")
	fmt.Println("3. Ollama (Local)")

	fmt.Print("\nEnter provider number (1-3) or 'cancel': ")

	// Temporarily disable escape monitoring during user input to avoid interference
	chatAgent.DisableEscMonitoring()
	defer chatAgent.DisableEscMonitoring() // Keep it disabled after this function

	// Use bufio.Scanner for better input handling
	scanner := bufio.NewScanner(os.Stdin)
	var input string
	if scanner.Scan() {
		input = strings.TrimSpace(scanner.Text())
	}

	if input == "cancel" || input == "" {
		fmt.Println("Provider selection cancelled.")
		return nil
	}

	// Parse selection
	selection, err := strconv.Atoi(input)
	if err != nil || selection < 1 || selection > 3 {
		return fmt.Errorf("invalid selection. Please enter a number between 1 and 3")
	}

	// Define available providers (same order as displayed)
	providers := []agent_api.ClientType{
		agent_api.DeepInfraClientType,
		agent_api.OpenRouterClientType,
		agent_api.OllamaClientType,
	}

	selectedProvider := providers[selection-1]
	selectedName := agent_api.GetProviderName(selectedProvider)

	// Get default model for the selected provider
	configManager := chatAgent.GetConfigManager()
	defaultModel := configManager.GetModelForProvider(selectedProvider)

	fmt.Printf("🔄 Switching to %s with model %s...\n", selectedName, defaultModel)

	// Use the agent's SetModel method which handles provider switching automatically
	err = chatAgent.SetModel(defaultModel)
	if err != nil {
		return fmt.Errorf("failed to switch to provider %s: %w", selectedName, err)
	}

	fmt.Printf("✅ Provider switched to: %s\n", selectedName)
	fmt.Printf("🤖 Using model: %s\n", defaultModel)

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
					fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n", err)
					os.Exit(1)
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
							os.Exit(1)
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
				os.Exit(1)
			}
			fmt.Println("✅ Task completed successfully")
			return nil
		}
	},
}
