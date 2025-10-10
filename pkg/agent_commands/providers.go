package commands

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/ui"
)

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
		requiresKey := api.IsProviderAvailable(provider) == false && provider != api.OllamaLocalClientType
		hasKey := configManager.HasAPIKey(provider)
		isReady := !requiresKey || hasKey

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
		requiresKey := api.IsProviderAvailable(provider) == false && provider != api.OllamaLocalClientType
		hasKey := configManager.HasAPIKey(provider)
		isReady := !requiresKey || hasKey

		status := "❌ (API key required)"
		if isReady {
			status = "✅ (ready)"
		}

		fmt.Printf("%d. **%s** %s - %s\n", i+1, name, status, model)
	}

	return nil
}

// selectProvider allows interactive provider selection
func (p *ProvidersCommand) selectProvider(configManager *configuration.Manager, chatAgent *agent.Agent) error {
	// Get all available providers
	providers := configManager.GetAvailableProviders()
	currentProvider := chatAgent.GetProviderType()

	// Convert providers to dropdown items
	items := make([]ui.DropdownItem, 0, len(providers))
	for _, provider := range providers {
		// Check if provider is ready
		requiresKey := !api.IsProviderAvailable(provider) && provider != api.OllamaLocalClientType
		hasKey := configManager.HasAPIKey(provider)
		isReady := !requiresKey || hasKey

		displayName := getProviderDisplayName(provider)

		// Add status to display name
		if !isReady {
			displayName += " (API key required)"
		} else if provider == currentProvider {
			displayName += " ✓"
		}

		item := &providerDropdownItem{
			provider:    provider,
			displayName: displayName,
			available:   isReady,
		}
		items = append(items, item)
	}

	// Try to show dropdown using the agent's UI
	selected, err := chatAgent.ShowDropdown(items, ui.DropdownOptions{
		Prompt:       "🎯 Select a Provider:",
		SearchPrompt: "Search: ",
		ShowCounts:   false,
	})

	if err != nil {
		fmt.Printf("DEBUG: ShowDropdown error: %v\n", err)
		// If dropdown is not available, show list with help
		if err == ui.ErrUINotAvailable {
			fmt.Println("\n📋 Available Providers:")
			fmt.Println("======================")

			for i, provider := range providers {
				// Check if provider is ready
				requiresKey := !api.IsProviderAvailable(provider) && provider != api.OllamaLocalClientType
				hasKey := configManager.HasAPIKey(provider)
				isReady := !requiresKey || hasKey

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

		// Check if it was just cancelled
		if err == ui.ErrCancelled {
			fmt.Printf("Provider selection cancelled.\n")
			return nil
		}

		return fmt.Errorf("failed to show provider selection: %w", err)
	}

	// Get the selected provider
	selectedProvider := selected.Value().(api.ClientType)

	// Check if provider needs API key
	if !api.IsProviderAvailable(selectedProvider) && selectedProvider != api.OllamaLocalClientType {
		// Try to ensure API key
		err = configManager.EnsureAPIKey(selectedProvider)
		if err != nil {
			return fmt.Errorf("failed to configure %s: %w", getProviderDisplayName(selectedProvider), err)
		}
	}

	// Switch to the provider
	fmt.Printf("🔄 Switching to %s...\n", getProviderDisplayName(selectedProvider))

	err = chatAgent.SetProvider(selectedProvider)
	if err != nil {
		return fmt.Errorf("failed to switch to provider: %w", err)
	}

	// Get the model that was set
	model := chatAgent.GetModel()

	fmt.Printf("✅ Provider switched to: %s\n", getProviderDisplayName(selectedProvider))
	fmt.Printf("🤖 Using model: %s\n", model)

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
	// Convert name to provider type
	provider, err := api.ParseProviderName(strings.ToLower(providerName))
    if err != nil {
        return fmt.Errorf("unknown provider '%s'. Available: openai, zai, deepinfra, ollama, ollama-turbo, openrouter, lmstudio, test", providerName)
    }

	// Check if provider needs API key but doesn't have one
	if !api.IsProviderAvailable(provider) && provider != api.OllamaLocalClientType {
		// Try to ensure API key
		err = configManager.EnsureAPIKey(provider)
		if err != nil {
			return fmt.Errorf("failed to configure %s: %w", getProviderDisplayName(provider), err)
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

// getProviderDisplayName returns a user-friendly name for the provider
func getProviderDisplayName(provider api.ClientType) string {
    switch provider {
    case api.OpenAIClientType:
        return "OpenAI"
    case api.ZAIClientType:
        return "Z.AI Coding Plan"
    case api.DeepInfraClientType:
        return "DeepInfra"
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
