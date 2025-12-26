package commands

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// readInput reads a line of input from stdin without conflicting with other input systems
func readInput() string {
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// ProvidersCommand implements the /providers slash command
type ProvidersCommand struct{}

// Name returns the command name
func (p *ProvidersCommand) Name() string {
	return "providers"
}

// Description returns the command description
func (p *ProvidersCommand) Description() string {
	return "Show current provider status and switch providers"
}

// Execute runs the providers command
func (p *ProvidersCommand) Execute(args []string, chatAgent *agent.Agent) error {
	configManager := chatAgent.GetConfigManager()

	// If no arguments, show current status
	if len(args) == 0 {
		return p.showProviderStatus(configManager, chatAgent)
	}

	// Handle subcommands
	switch args[0] {
	case "list":
		return p.listProviders(configManager)
	case "select":
		return p.selectProvider(configManager, chatAgent)
	case "status":
		return p.showProviderStatus(configManager, chatAgent)
	default:
		// Try to set provider directly by name
		return p.setProvider(args[0], configManager, chatAgent)
	}
}

// showProviderStatus displays current provider information
func (p *ProvidersCommand) showProviderStatus(configManager *configuration.Manager, chatAgent *agent.Agent) error {
	fmt.Println("\n🔧 Provider Status:")
	fmt.Println("===================")

	// Show current active provider
	currentProvider := chatAgent.GetProviderType()
	currentModel := chatAgent.GetModel()
	fmt.Printf("✅ **Active Provider**: %s\n", getProviderDisplayName(currentProvider))
	fmt.Printf("🤖 **Current Model**: %s\n", currentModel)
	fmt.Println()

	// Show all supported providers
	available := configManager.GetAvailableProviders()
	fmt.Println("📋 Supported Providers:")
	fmt.Println("------------------")

	for _, provider := range available {
		displayName := getProviderDisplayName(provider)
		model := configManager.GetModelForProvider(provider)

		// Check if provider is ready to use
		isReady := p.isProviderReady(configManager, provider)

		icon := "❌"
		statusText := "(API key required)"
		if isReady {
			icon = "✅"
			statusText = "(configured)"
			if provider == currentProvider {
				icon = "🌟"
				statusText = "(active)"
			}
		}

		fmt.Printf("%s **%s** %s\n", icon, displayName, statusText)
		fmt.Printf("   Model: %s\n", model)
		fmt.Println()
	}

	fmt.Println("Usage:")
	fmt.Println("  /providers                    - Show this status")
	fmt.Println("  /providers list              - List available providers only")
	fmt.Println("  /providers select            - Interactive provider selection")
	fmt.Println("  /providers <provider_name>   - Switch to specific provider")

	return nil
}

// listProviders shows all supported providers
func (p *ProvidersCommand) listProviders(configManager *configuration.Manager) error {
	available := configManager.GetAvailableProviders()

	fmt.Println("\n📋 All Providers:")
	fmt.Println("=================")

	for i, provider := range available {
		name := getProviderDisplayName(provider)
		model := configManager.GetModelForProvider(provider)

		// Check if provider is ready
		isReady := p.isProviderReady(configManager, provider)

		status := "❌ (API key required)"
		if isReady {
			status = "✅ (ready)"
		}

		fmt.Printf("%d. **%s** %s - %s\n", i+1, name, status, model)
	}

	return nil
}

// isProviderReady checks if a provider is ready to use (has API key if needed)
func (p *ProvidersCommand) isProviderReady(configManager *configuration.Manager, provider api.ClientType) bool {
	// Built-in providers that don't need API keys
	if provider == api.OllamaLocalClientType {
		return true
	}

	// Built-in providers that are available without API keys
	if api.IsProviderAvailable(provider) {
		return true
	}

	// Check if this is a custom provider that doesn't require an API key
	config := configManager.GetConfig()
	if config.CustomProviders != nil {
		if customProvider, exists := config.CustomProviders[string(provider)]; exists && !customProvider.RequiresAPIKey {
			return true
		}
	}

	// For all other providers, check if they have an API key
	return configManager.HasAPIKey(provider)
}

// selectProvider allows interactive provider selection
func (p *ProvidersCommand) selectProvider(configManager *configuration.Manager, chatAgent *agent.Agent) error {
	// Get all available providers
	providers := configManager.GetAvailableProviders()

	// UI not available - show provider list with help
	fmt.Println("Interactive provider selection not available.")
	fmt.Println("\n📋 Available Providers:")
	fmt.Println("======================")

	for i, provider := range providers {
		// Check if provider is ready
		isReady := p.isProviderReady(configManager, provider)

		status := ""
		if !isReady {
			status = " (API key required)"
		} else if provider == chatAgent.GetProviderType() {
			status = " ✓"
		}

		fmt.Printf("%d. %s%s\n", i+1, getProviderDisplayName(provider), status)
	}

	fmt.Println("\n💡 To select a provider, use: /providers <provider_name>")
	fmt.Println("   Example: /providers openai")
	return nil
}

// providerDropdownItem implements agent.DropdownItem for providers
type providerDropdownItem struct {
	provider    api.ClientType
	displayName string
	available   bool
}

func (p *providerDropdownItem) Display() string    { return p.displayName }
func (p *providerDropdownItem) SearchText() string { return p.displayName }
func (p *providerDropdownItem) Value() interface{} { return p.provider }

// setProvider sets a specific provider by name
func (p *ProvidersCommand) setProvider(providerName string, configManager *configuration.Manager, chatAgent *agent.Agent) error {
	// Convert name to provider type using the config manager to handle custom providers
	provider, err := configManager.MapStringToClientType(strings.ToLower(providerName))
	if err != nil {
		// Get list of available providers for better error message
		available := configManager.GetAvailableProviders()
		var names []string
		for _, p := range available {
			names = append(names, string(p))
		}
		return fmt.Errorf("unknown provider '%s'. Available: %s", providerName, strings.Join(names, ", "))
	}

	// Check if provider needs API key but doesn't have one
	if !api.IsProviderAvailable(provider) && provider != api.OllamaLocalClientType {
		// Check if this is a custom provider that doesn't require an API key
		config := configManager.GetConfig()
		if config.CustomProviders != nil {
			if customProvider, exists := config.CustomProviders[string(provider)]; exists && !customProvider.RequiresAPIKey {
				// Custom provider doesn't require API key, skip the prompt
			} else {
				// Try to ensure API key
				err = configManager.EnsureAPIKey(provider)
				if err != nil {
					return fmt.Errorf("failed to configure %s: %w", getProviderDisplayName(provider), err)
				}
			}
		} else {
			// Try to ensure API key for non-custom providers
			err = configManager.EnsureAPIKey(provider)
			if err != nil {
				return fmt.Errorf("failed to configure %s: %w", getProviderDisplayName(provider), err)
			}
		}
	}

	// Switch to the provider
	fmt.Printf("🔄 Switching to %s...\n", getProviderDisplayName(provider))

	// Switch the agent to the new provider
	err = chatAgent.SetProvider(provider)
	if err != nil {
		return fmt.Errorf("failed to switch to provider %s: %w", getProviderDisplayName(provider), err)
	}

	// Get the model that was set
	model := chatAgent.GetModel()

	fmt.Printf("✅ Provider switched to: %s\n", getProviderDisplayName(provider))
	fmt.Printf("🤖 Using model: %s\n", model)

	return nil
}

// selectModelFromList allows users to interactively select from available models
func selectModelFromList(models []api.ModelInfo, preferredModel string) (string, error) {
	if len(models) == 0 {
		return "", fmt.Errorf("no models available")
	}

	// If preferred model is available, use it
	for _, model := range models {
		if model.ID == preferredModel {
			return preferredModel, nil
		}
	}

	fmt.Printf("⚠️  Preferred model '%s' not found.\n", preferredModel)
	fmt.Println("Available models:")
	for i, model := range models {
		fmt.Printf("  %d) %s\n", i+1, model.ID)
	}

	fmt.Print("Select a model (or press Enter for first available): ")
	input := readInput()

	if input == "" {
		// Default to first model
		selectedModel := models[0].ID
		fmt.Printf("🔄 Selected first available model: %s\n", selectedModel)
		return selectedModel, nil
	}

	// Try to parse as number
	if selection, err := strconv.Atoi(input); err == nil && selection >= 1 && selection <= len(models) {
		selectedModel := models[selection-1].ID
		fmt.Printf("✅ Selected model: %s\n", selectedModel)
		return selectedModel, nil
	}

	// Try to find exact match
	for _, model := range models {
		if strings.EqualFold(model.ID, input) {
			fmt.Printf("✅ Selected model: %s\n", model.ID)
			return model.ID, nil
		}
	}

	fmt.Printf("❌ Invalid selection. Using first available model: %s\n", models[0].ID)
	return models[0].ID, nil
}

// getProviderDisplayName returns a user-friendly name for the provider
func getProviderDisplayName(provider api.ClientType) string {
	switch provider {
	case api.OpenAIClientType:
		return "OpenAI"
	case api.ZAIClientType:
		return "Z.AI Coding Plan"
	case api.DeepInfraClientType:
		return "DeepInfra"
	case api.DeepSeekClientType:
		return "DeepSeek"
	case api.OpenRouterClientType:
		return "OpenRouter"
	case api.OllamaClientType:
		return "Ollama"
	case api.OllamaLocalClientType:
		return "Ollama Local"
	case api.OllamaTurboClientType:
		return "Ollama Turbo"
	case api.LMStudioClientType:
		return "LM Studio"
	case api.TestClientType:
		return "Test (CI/Mock)"
	default:
		return string(provider)
	}
}
