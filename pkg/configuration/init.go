package configuration

import (
	"fmt"
	"strings"
)

// Initialize loads or creates configuration with first-run setup
func Initialize() (*Config, *APIKeys, error) {
	// Ensure config directory exists
	configDir, err := GetConfigDir()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to access config directory: %w", err)
	}

	// Load or create config
	config, err := Load()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Load API keys
	apiKeys, err := LoadAPIKeys()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load API keys: %w", err)
	}

	// Populate from environment variables
	envKeysFound := apiKeys.PopulateFromEnvironment()
	if envKeysFound {
		fmt.Println("✅ Found API keys from environment variables")
	}

	// Check if this is first run (no provider selected)
	isFirstRun := config.LastUsedProvider == ""

	// Also check if current provider has no API key (and needs one)
	needsSetup := false
	if !isFirstRun {
		currentProvider := config.LastUsedProvider
		if RequiresAPIKey(currentProvider) && !apiKeys.HasAPIKey(currentProvider) {
			needsSetup = true
			fmt.Printf("⚠️  Current provider '%s' requires an API key but none is configured.\n", getProviderDisplayName(currentProvider))
		}
	}

	if isFirstRun || needsSetup {
		if isFirstRun {
			fmt.Printf("🚀 Welcome to ledit! Let's set up your AI provider.\n")
			fmt.Printf("   Config directory: %s\n\n", configDir)
		}

		// First run or setup needed - select initial provider
		provider, err := selectInitialProvider(apiKeys)
		if err != nil {
			return nil, nil, fmt.Errorf("provider setup failed: %w", err)
		}

		config.LastUsedProvider = provider
		if err := config.Save(); err != nil {
			return nil, nil, fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("🎉 Setup complete! You can now use ledit with %s.\n\n", getProviderDisplayName(provider))

		// Show helpful next steps
		fmt.Println("Next steps:")
		fmt.Println("  • Run 'ledit' to start the interactive mode")
		fmt.Println("  • Run 'ledit agent \"your task here\"' for direct commands")
		fmt.Println("  • Use --provider flag to switch providers temporarily")
		fmt.Printf("  • Config stored in: %s\n\n", configDir)
	}

	// Final validation - ensure selected provider is actually usable
	if err := validateProviderSetup(config.LastUsedProvider, apiKeys); err != nil {
		return nil, nil, fmt.Errorf("provider validation failed: %w", err)
	}

	return config, apiKeys, nil
}

// selectInitialProvider guides user through initial provider selection
func selectInitialProvider(apiKeys *APIKeys) (string, error) {
	// Check which providers have API keys already
	providersWithKeys := []string{}
	allProviders := getSupportedProviders()

	for _, provider := range allProviders {
		if !provider.RequiresKey || apiKeys.HasAPIKey(provider.Name) {
			providersWithKeys = append(providersWithKeys, provider.Name)
		}
	}

	// If we have providers ready to use, show them first
	if len(providersWithKeys) > 0 {
		fmt.Println("✅ Ready to use (configured or no API key needed):")
		for i, providerName := range providersWithKeys {
			fmt.Printf("  %d. %s", i+1, getProviderDisplayName(providerName))
			if !RequiresAPIKey(providerName) {
				fmt.Print(" (no API key needed)")
			}
			fmt.Println()
		}
		fmt.Println()
	}

	// Show all provider options with clear descriptions
	fmt.Println("🤖 All available AI providers:")
	for i, provider := range allProviders {
		status := ""
		if provider.RequiresKey && !apiKeys.HasAPIKey(provider.Name) {
			status = " (needs API key)"
		} else if provider.RequiresKey && apiKeys.HasAPIKey(provider.Name) {
			status = " ✅"
		} else {
			status = " (local, no key needed)"
		}

		fmt.Printf("  %d. %s%s\n", i+1, provider.FormattedName, status)
	}
	fmt.Println()

	// Get user choice
	var choice int
	fmt.Printf("Select a provider (1-%d): ", len(allProviders))
	_, err := fmt.Scanln(&choice)
	if err != nil || choice < 1 || choice > len(allProviders) {
		return "", fmt.Errorf("invalid choice, please enter a number between 1 and %d", len(allProviders))
	}

	selectedProvider := allProviders[choice-1]

	// Check if API key is needed
	if selectedProvider.RequiresKey && !apiKeys.HasAPIKey(selectedProvider.Name) {
		fmt.Println()
		fmt.Printf("📋 Setting up %s:\n", selectedProvider.FormattedName)

		// Provide helpful information about getting API keys
		switch selectedProvider.Name {
		case "openai":
			fmt.Println("   • Visit: https://platform.openai.com/api-keys")
			fmt.Println("   • Create an account and generate an API key")
		case "openrouter":
			fmt.Println("   • Visit: https://openrouter.ai/keys")
			fmt.Println("   • Access to many different AI models through one API")
		case "deepinfra":
			fmt.Println("   • Visit: https://deepinfra.com/dash/api_keys")
			fmt.Println("   • Focus on open-source models")
		}
		fmt.Println()

		apiKey, err := PromptForAPIKey(selectedProvider.Name)
		if err != nil {
			return "", fmt.Errorf("failed to get API key: %w", err)
		}

		apiKeys.SetAPIKey(selectedProvider.Name, apiKey)
		if err := SaveAPIKeys(apiKeys); err != nil {
			return "", fmt.Errorf("failed to save API key: %w", err)
		}

		fmt.Printf("✅ API key saved for %s\n", selectedProvider.FormattedName)
	} else if selectedProvider.RequiresKey {
		fmt.Printf("✅ Using existing API key for %s\n", selectedProvider.FormattedName)
	} else {
		fmt.Printf("✅ Selected %s (no API key required)\n", selectedProvider.FormattedName)
	}

	return selectedProvider.Name, nil
}

// EnsureProviderAPIKey ensures the provider has an API key, prompting if needed
func EnsureProviderAPIKey(provider string, apiKeys *APIKeys) error {
	if !RequiresAPIKey(provider) {
		return nil
	}

	if apiKeys.HasAPIKey(provider) {
		return nil
	}

	fmt.Println()
	fmt.Printf("⚠️  No API key found for %s\n", getProviderDisplayName(provider))
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  1. Enter API key now")
	fmt.Println("  2. Select a different provider")
	fmt.Println()

	var choice int
	fmt.Print("Choice (1-2): ")
	_, err := fmt.Scanln(&choice)
	if err != nil || choice < 1 || choice > 2 {
		return fmt.Errorf("invalid choice")
	}

	if choice == 1 {
		apiKey, err := PromptForAPIKey(provider)
		if err != nil {
			return err
		}

		apiKeys.SetAPIKey(provider, apiKey)
		if err := SaveAPIKeys(apiKeys); err != nil {
			return fmt.Errorf("failed to save API key: %w", err)
		}

		fmt.Printf("✅ API key saved for %s\n", getProviderDisplayName(provider))
		return nil
	}

	// Choice 2 - select different provider
	return fmt.Errorf("provider requires API key")
}

// GetAvailableProviders returns all supported providers
func GetAvailableProviders(apiKeys *APIKeys) []string {
	// Return all supported providers, regardless of API key status
	// The selection process will handle prompting for API keys
	return []string{
		"openai",
		"openrouter",
		"deepinfra",
		"ollama-local",
		"ollama-turbo",
	}
}

// SelectProvider allows user to select a provider interactively
func SelectProvider(currentProvider string, apiKeys *APIKeys) (string, error) {
	available := GetAvailableProviders(apiKeys)

	if len(available) == 0 {
		return "", fmt.Errorf("no providers available - please configure API keys")
	}

	fmt.Println("🤖 Available providers:")
	for i, provider := range available {
		indicator := "  "
		if provider == currentProvider {
			indicator = "→ "
		}
		fmt.Printf("%s%d. %s\n", indicator, i+1, getProviderDisplayName(provider))
	}

	// Also show option to add new provider
	fmt.Printf("  %d. Add new provider with API key\n", len(available)+1)
	fmt.Println()

	var choice int
	fmt.Print("Select provider: ")
	_, err := fmt.Scanln(&choice)
	if err != nil || choice < 1 {
		return "", fmt.Errorf("invalid choice")
	}

	if choice <= len(available) {
		selectedProvider := available[choice-1]

		// Check if this provider needs an API key but doesn't have one
		if RequiresAPIKey(selectedProvider) && !apiKeys.HasAPIKey(selectedProvider) {
			// Prompt for API key
			err := EnsureProviderAPIKey(selectedProvider, apiKeys)
			if err != nil {
				return "", err
			}
		}

		return selectedProvider, nil
	}

	// Add new provider
	if choice == len(available)+1 {
		return addNewProvider(apiKeys)
	}

	return "", fmt.Errorf("invalid choice")
}

// addNewProvider guides user through adding a new provider
func addNewProvider(apiKeys *APIKeys) (string, error) {
	fmt.Println()
	fmt.Println("➕ Add new provider:")

	// Show providers that need API keys
	needsKey := []string{}
	for _, provider := range []string{
		"openai", "openrouter", "deepinfra", "ollama-turbo",
	} {
		if !apiKeys.HasAPIKey(provider) {
			needsKey = append(needsKey, provider)
		}
	}

	if len(needsKey) == 0 {
		return "", fmt.Errorf("all providers already configured")
	}

	for i, provider := range needsKey {
		fmt.Printf("  %d. %s\n", i+1, getProviderDisplayName(provider))
	}
	fmt.Println()

	var choice int
	fmt.Print("Select provider to add: ")
	_, err := fmt.Scanln(&choice)
	if err != nil || choice < 1 || choice > len(needsKey) {
		return "", fmt.Errorf("invalid choice")
	}

	provider := needsKey[choice-1]

	// Get API key
	apiKey, err := PromptForAPIKey(provider)
	if err != nil {
		return "", err
	}

	apiKeys.SetAPIKey(provider, apiKey)
	if err := SaveAPIKeys(apiKeys); err != nil {
		return "", fmt.Errorf("failed to save API key: %w", err)
	}

	fmt.Printf("✅ Added %s\n", getProviderDisplayName(provider))
	return provider, nil
}

// validateProviderSetup ensures the provider can actually be used
func validateProviderSetup(provider string, apiKeys *APIKeys) error {
	if provider == "" {
		return fmt.Errorf("no provider selected")
	}

	// Check if provider requires API key
	if RequiresAPIKey(provider) {
		if !apiKeys.HasAPIKey(provider) {
			return fmt.Errorf("provider '%s' requires an API key but none is configured", provider)
		}

		// Basic API key format validation
		key := apiKeys.GetAPIKey(provider)
		if len(key) < 10 {
			return fmt.Errorf("API key for '%s' appears to be too short (expected at least 10 characters)", provider)
		}
	}

	return nil
}

// LoadOrInitConfig loads existing configuration or initializes a new one
func LoadOrInitConfig(skipPrompt bool) (*Config, error) {
	// Try to load existing configuration
	config, err := Load()
	if err == nil {
		// Config loaded successfully
		return config, nil
	}

	// If loading failed and skipPrompt is true, return default config
	if skipPrompt {
		return NewConfig(), nil
	}

	// Otherwise, initialize with prompts
	config, _, err = Initialize()
	return config, err
}

// DebugPrintConfig prints current configuration for debugging
func DebugPrintConfig(config *Config, apiKeys *APIKeys) {
	fmt.Println("🔧 Current Configuration:")
	fmt.Printf("  Config Version: %s\n", config.Version)
	fmt.Printf("  Last Provider: %s\n", config.LastUsedProvider)
	fmt.Println()

	fmt.Println("  Provider Models:")
	for provider, model := range config.ProviderModels {
		fmt.Printf("    %s: %s\n", provider, model)
	}
	fmt.Println()

	fmt.Println("  MCP Config:")
	fmt.Printf("    Enabled: %v\n", config.MCP.Enabled)
	fmt.Printf("    AutoStart: %v\n", config.MCP.AutoStart)
	fmt.Printf("    Servers: %d configured\n", len(config.MCP.Servers))
	fmt.Println()

	fmt.Println("  API Keys:")
	providers := []string{
		"openai", "openrouter", "deepinfra", "ollama-local", "ollama-turbo",
	}
	for _, provider := range providers {
		if apiKeys.HasAPIKey(provider) {
			key := apiKeys.GetAPIKey(provider)
			masked := strings.Repeat("*", len(key)-4) + key[len(key)-4:]
			fmt.Printf("    %s: %s\n", provider, masked)
		}
	}
}
