// Agent command for ledit
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	agent_api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/console/components"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/spf13/cobra"
)

var (
	agentSkipPrompt bool
	agentModel      string // Declare agentModel variable
	agentProvider   string // Declare agentProvider variable
	agentDryRun     bool
	maxIterations   int
)

func init() {
	agentCmd.Flags().BoolVar(&agentSkipPrompt, "skip-prompt", false, "Skip user prompts (enhanced by automated validation)")
	agentCmd.Flags().StringVarP(&agentModel, "model", "m", "", "Model name for agent system")
	agentCmd.Flags().StringVarP(&agentProvider, "provider", "p", "", "Provider to use (openai, openrouter, deepinfra, ollama, cerebras, groq, deepseek)")
	agentCmd.Flags().BoolVar(&agentDryRun, "dry-run", false, "Run tools in simulation mode (enhanced safety)")
	agentCmd.Flags().IntVar(&maxIterations, "max-iterations", 1000, "Maximum iterations before stopping (default: 1000)")
}

// runSimpleInteractiveMode provides a simple console-based interactive mode
func runInteractiveMode() error {
	// Create agent with provider and model if specified
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
		return fmt.Errorf("failed to initialize agent: %w", err)
	}

	// Set max iterations if specified
	chatAgent.SetMaxIterations(maxIterations)

	// Create console app
	app := console.NewConsoleApp()

	// Configure app
	config := &console.Config{
		RawMode:      true,
		MouseEnabled: false,
		AltScreen:    false,
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

	// Run the app
	return app.Run()
}

// executeDirectAgentCommand executes an agent command directly (like coder does)
func executeDirectAgentCommand(userIntent string) error {
	// Create agent with provider and model if specified
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
		return fmt.Errorf("failed to initialize agent: %w", err)
	}

	// Set max iterations if specified
	chatAgent.SetMaxIterations(maxIterations)

	// Process the query directly with the agent using continuity (like coder does)
	response, err := chatAgent.ProcessQueryWithContinuity(userIntent)
	if err != nil {
		return fmt.Errorf("agent processing failed: %w", err)
	}

	fmt.Printf("\n🎯 Agent Response:\n%s\n", response)

	// Print cost and token summary
	chatAgent.PrintConciseSummary()
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
		fmt.Println("💡 Tip: Use '/provider select' to switch to a different provider")
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
	fmt.Println("  /provider select        - Switch providers first, then select models")

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
		fmt.Println("💡 Tip: Use '/provider select' to switch to a different provider with available models")
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
	fmt.Println("Use '/provider select' to switch providers")
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
	fmt.Println("4. Groq")
	fmt.Println("5. Cerebras")
	fmt.Println("6. DeepSeek")
	fmt.Println()
	fmt.Println("Use '/provider select' to switch providers")

	return nil
}

// selectProvider allows interactive provider selection
func selectProvider(chatAgent *agent.Agent) error {
	fmt.Println("\n🎯 Select a Provider:")
	fmt.Println("====================")
	fmt.Println("1. DeepInfra")
	fmt.Println("2. OpenRouter")
	fmt.Println("3. Ollama (Local)")
	fmt.Println("4. Groq")
	fmt.Println("5. Cerebras")
	fmt.Println("6. DeepSeek")

	fmt.Print("\nEnter provider number (1-6) or 'cancel': ")

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
	if err != nil || selection < 1 || selection > 6 {
		return fmt.Errorf("invalid selection. Please enter a number between 1 and 6")
	}

	// Define available providers (same order as displayed)
	providers := []agent_api.ClientType{
		agent_api.DeepInfraClientType,
		agent_api.OpenRouterClientType,
		agent_api.OllamaClientType,
		agent_api.GroqClientType,
		agent_api.CerebrasClientType,
		agent_api.DeepSeekClientType,
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
	Short: "AI agent for code analysis and editing",
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
  ledit agent -p deepinfra -m "deepseek-v3" "Analyze the codebase structure"`,
	Args: cobra.MaximumNArgs(1), // Allow 0 or 1 args for interactive mode
	RunE: func(cmd *cobra.Command, args []string) error {
		// Handle interactive mode
		if len(args) == 0 {
			// Always use our new console architecture for interactive mode
			return runInteractiveMode()
		}

		// Direct mode - execute single command
		userIntent := strings.Join(args, " ")

		// Mark environment flags
		_ = os.Setenv("LEDIT_FROM_AGENT", "1")

		if agentDryRun {
			_ = os.Setenv("LEDIT_DRY_RUN", "1")
		}

		// Execute using direct agent
		err := executeDirectAgentCommand(userIntent)

		if err != nil {
			gracefulExitMsg := prompts.NewGracefulExitWithTokenUsage(
				"AI agent processing your request",
				err,
				nil,
				"ledit-agent",
			)
			fmt.Fprint(os.Stderr, gracefulExitMsg)
			os.Exit(1)
		}

		fmt.Println("✅ Task completed successfully")
		return nil
	},
}
