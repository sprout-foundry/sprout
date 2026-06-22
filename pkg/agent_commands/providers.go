package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// readInput reads a line of input from stdin without conflicting with other input systems
func readInput() string {
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// ProvidersCommand implements the /provider slash command
type ProvidersCommand struct{}

// Name returns the command name
func (p *ProvidersCommand) Name() string {
	return "provider"
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
	fmt.Println()
	console.GlyphInfo.Print("Provider Status:")

	// Show current active provider
	currentProvider := chatAgent.GetProviderType()
	currentModel := chatAgent.GetModel()
	console.GlyphSuccess.Printf("Active Provider: %s", getProviderDisplayName(currentProvider))
	console.GlyphInfo.Printf("Current Model: %s", currentModel)
	fmt.Println()

	// Show all supported providers
	available := configManager.GetAvailableProviders()
	console.GlyphInfo.Print("Supported Providers:")

	for _, provider := range available {
		displayName := getProviderDisplayName(provider)
		model := configManager.GetModelForProvider(provider)

		// Check if provider is ready to use
		isReady := p.isProviderReady(configManager, provider)

		statusGlyph := console.GlyphError
		statusText := "(API key required)"
		if isReady {
			statusGlyph = console.GlyphSuccess
			statusText = "(configured)"
			if provider == currentProvider {
				statusGlyph = console.GlyphAction
				statusText = "(active)"
			}
		}

		fmt.Printf("%s**%s** %s\n", statusGlyph.Prefix(), displayName, statusText)
		fmt.Printf("   Model: %s\n", model)
		fmt.Println()
	}

	fmt.Println("Usage:")
	fmt.Println("  /provider                    - Show this status")
	fmt.Println("  /provider list              - List available providers only")
	fmt.Println("  /provider select            - Interactive provider selection")
	fmt.Println("  /provider <provider_name>   - Switch to specific provider")

	return nil
}

// listProviders shows all supported providers
func (p *ProvidersCommand) listProviders(configManager *configuration.Manager) error {
	available := configManager.GetAvailableProviders()

	fmt.Println()
	console.GlyphInfo.Print("All Providers:")

	for i, provider := range available {
		name := getProviderDisplayName(provider)
		model := configManager.GetModelForProvider(provider)

		// Check if provider is ready
		isReady := p.isProviderReady(configManager, provider)

		status := console.GlyphError.Prefix() + "(API key required)"
		if isReady {
			status = console.GlyphSuccess.Prefix() + "(ready)"
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

// selectProvider drives the interactive provider picker. SP-057
// Phase 5 — replaces the prior "Interactive provider selection not
// available" stub with a SelectList over the configured providers.
// The Detail column shows credential readiness so the picker doubles
// as a configuration overview. Picking a provider that needs an API
// key chains into setProvider, which already handles the EnsureAPIKey
// prompt flow.
func (p *ProvidersCommand) selectProvider(configManager *configuration.Manager, chatAgent *agent.Agent) error {
	providers := configManager.GetAvailableProviders()
	if len(providers) == 0 {
		console.GlyphInfo.Print("No providers configured.")
		return nil
	}

	current := chatAgent.GetProviderType()
	items := make([]console.SelectItem, 0, len(providers))
	for _, provider := range providers {
		isReady := p.isProviderReady(configManager, provider)
		var detail string
		switch {
		case provider == current:
			detail = "active"
		case !isReady:
			detail = "needs API key"
		default:
			detail = "ready"
		}
		items = append(items, console.SelectItem{
			Label:  getProviderDisplayName(provider),
			Detail: detail,
			Value:  string(provider),
		})
	}

	picker := console.NewSelectList(console.SelectListOptions{
		Title:    "Select provider",
		Items:    items,
		PageSize: 12,
	})
	chosen, ok, err := picker.Run(context.Background())
	if err != nil {
		return fmt.Errorf("provider picker: %w", err)
	}
	if !ok || chosen == "" {
		fmt.Println("Provider selection cancelled.")
		return nil
	}
	return p.setProvider(chosen, configManager, chatAgent)
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
	if !p.isProviderReady(configManager, provider) && provider != api.OllamaLocalClientType {
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
	console.GlyphDim.Printf("Switching to %s...", getProviderDisplayName(provider))

	// Switch the agent to the new provider (persisted for CLI use)
	err = chatAgent.SetProviderPersisted(provider)
	if err != nil {
		return fmt.Errorf("failed to switch to provider %s: %w", getProviderDisplayName(provider), err)
	}

	// Get the model that was set
	model := chatAgent.GetModel()

	console.GlyphSuccess.Printf("Provider switched to: %s", getProviderDisplayName(provider))
	console.GlyphInfo.Printf("Using model: %s", model)
	if note := chatAgent.ConsumePendingStrictSwitchNotice(); note != "" {
		fmt.Println()
		console.GlyphInfo.Print(note)
	}

	return nil
}

// selectModelFromList allows users to interactively select from available models
func selectModelFromList(models []api.ModelInfo, preferredModel string) (string, error) {
	if len(models) == 0 {
		return "", errors.New("no models available")
	}

	// If preferred model is available, use it
	for _, model := range models {
		if model.ID == preferredModel {
			return preferredModel, nil
		}
	}

	fmt.Println()
	console.GlyphWarning.Printf("Preferred model '%s' not found.", preferredModel)
	fmt.Println("Available models:")
	for i, model := range models {
		fmt.Printf("  %d) %s\n", i+1, model.ID)
	}

	fmt.Print("Select a model (or press Enter for first available): ")
	input := readInput()

	if input == "" {
		// Default to first model
		selectedModel := models[0].ID
		console.GlyphDim.Printf("Selected first available model: %s", selectedModel)
		return selectedModel, nil
	}

	// Try to parse as number
	if selection, err := strconv.Atoi(input); err == nil && selection >= 1 && selection <= len(models) {
		selectedModel := models[selection-1].ID
		console.GlyphSuccess.Printf("Selected model: %s", selectedModel)
		return selectedModel, nil
	}

	// Try to find exact match
	for _, model := range models {
		if strings.EqualFold(model.ID, input) {
			console.GlyphSuccess.Printf("Selected model: %s", model.ID)
			return model.ID, nil
		}
	}

	console.GlyphError.Printf("Invalid selection. Using first available model: %s", models[0].ID)
	return models[0].ID, nil
}

// getProviderDisplayName returns a user-friendly name for the provider
func getProviderDisplayName(provider api.ClientType) string {
	switch provider {
	case api.OpenAIClientType:
		return "OpenAI"
	case api.ZAIClientType:
		return "Z.AI"
	case api.ZAICodingClientType:
		return "GLM Coding Plan"
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
	case api.OllamaCloudClientType:
		return "Ollama Cloud"
	case api.LMStudioClientType:
		return "LM Studio"
	case api.TestClientType:
		return "Test (CI/Mock)"
	default:
		return string(provider)
	}
}
