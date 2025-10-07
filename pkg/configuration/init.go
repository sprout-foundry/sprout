package configuration

import (
	"fmt"
	"os"
	"strings"
)

// Initialize loads or creates configuration with first-run setup
func Initialize() (*Config, *APIKeys, error) {
	// Check if we're in a CI environment
	isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""

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

	// Populate from environment variables FIRST - prioritize env vars over stored keys
	envKeysFound := apiKeys.PopulateFromEnvironment()
	if envKeysFound {
		fmt.Println("✅ Found API keys from environment variables")
	}

	// Check if we have any usable providers from environment variables
	for _, provider := range getSupportedProviders() {
		if provider.RequiresKey && apiKeys.HasAPIKey(provider.Name) {
			// Found at least one usable provider
			break
		}
	}

	// Check if this is first run (no provider selected)
	isFirstRun := config.LastUsedProvider == ""

	// Also check if current provider has no API key (and needs one)
	needsSetup := false
	if !isFirstRun {
		currentProvider := config.LastUsedProvider
		if RequiresAPIKey(currentProvider) && !apiKeys.HasAPIKey(currentProvider) {
			needsSetup = true
			if !isCI {
				fmt.Printf("⚠️  Current provider '%s' requires an API key but none is configured.\n", getProviderDisplayName(currentProvider))
			}
		}
	}

	// In CI environments, skip interactive setup and use defaults
	if isCI && (isFirstRun || needsSetup) {
		if isFirstRun {
			fmt.Printf("🚀 Welcome to ledit! Let's set up your AI provider.\n")
			fmt.Printf("   Config directory: %s\n\n", configDir)
		}

		// Set a default provider that works in CI
		if apiKeys.HasAPIKey("openrouter") {
			config.LastUsedProvider = "openrouter"
			fmt.Printf("✅ Using OpenRouter provider from environment\n")
		} else if apiKeys.HasAPIKey("openai") {
			config.LastUsedProvider = "openai"
			fmt.Printf("✅ Using OpenAI provider from environment\n")
		} else {
			// Default to test provider which is designed for CI/testing
			config.LastUsedProvider = "test"
			fmt.Printf("✅ Using test provider (designed for CI environments)\n")
		}

		if err := config.Save(); err != nil {
			return nil, nil, fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("🎉 Setup complete! You can now use ledit.\n\n")

		return config, apiKeys, nil
	}

	if isFirstRun || needsSetup {
		if isFirstRun {
			ShowWelcomeMessage()
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
		ShowNextSteps(provider, configDir)
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

	// First, check for providers that have environment variables set
	envProviders := []string{}
	for _, provider := range allProviders {
		if provider.RequiresKey && provider.EnvVariableName != "" {
			if envKey := os.Getenv(provider.EnvVariableName); envKey != "" {
				envProviders = append(envProviders, provider.Name)
			}
		}
	}

	// If we have providers with environment variables, prioritize them
	if len(envProviders) > 0 {
		fmt.Println("🚀 Found providers with environment variables set:")
		for i, providerName := range envProviders {
			// Find the provider struct to get the environment variable name
			var envVarName string
			for _, provider := range allProviders {
				if provider.Name == providerName {
					envVarName = provider.EnvVariableName
					break
				}
			}
			fmt.Printf("  %d. %s (from %s)", i+1, getProviderDisplayName(providerName), envVarName)
			fmt.Println()
		}
		fmt.Println()

		// Auto-select the first provider with environment variable
		fmt.Printf("✅ Auto-selecting %s (environment variable detected)\n", getProviderDisplayName(envProviders[0]))
		return envProviders[0], nil
	}

	// Check which providers have API keys already (from file)
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
		description := ""

		if provider.RequiresKey && !apiKeys.HasAPIKey(provider.Name) {
			status = " (needs API key)"
		} else if provider.RequiresKey && apiKeys.HasAPIKey(provider.Name) {
			status = " ✅"
		} else {
			status = " (local, no key needed)"
		}

		// Add helpful descriptions
		switch provider.Name {
		case "openrouter":
			description = " - 100+ models, free options, pay-as-you-go"
		case "openai":
			description = " - GPT models, reliable but pricier"
		case "deepinfra":
			description = " - Open-source models, good performance"
		case "ollama":
			description = " - Run models locally, completely free (requires setup)"
		case "ollama-turbo":
			description = " - Hosted Ollama with API access"
		case "lmstudio":
			description = " - Local AI server, run models on your machine"
		case "jinaai":
			description = " - Specialized in embeddings and search"
		}

		fmt.Printf("  %d. %s%s%s\n", i+1, provider.FormattedName, status, description)
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
			fmt.Println("   • Note: OpenAI models are more expensive (~$0.01-0.06 per request)")
			fmt.Println("   • Consider OpenRouter for more cost-effective options")
		case "openrouter":
			fmt.Println("   • Visit: https://openrouter.ai/keys")
			fmt.Println("   • Access to 100+ AI models through one API")
			fmt.Println("   • Important: Choose models with tool-calling support")
			fmt.Println("   • Recommended: qwen/qwen3-coder-30b-a3b-instruct (great for coding)")
			fmt.Println("   • Also good: anthropic/claude-3.5-haiku, openai/gpt-5-mini")
			fmt.Println("   • Pay-as-you-go pricing, no monthly fees")
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
		"lmstudio",
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
		"openai", "openrouter", "deepinfra", "ollama-turbo", "lmstudio",
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

		// Basic API key format validation - skip in CI/test environments
		isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
		key := apiKeys.GetAPIKey(provider)

		// In CI environments, accept test keys that start with "test"
		if isCI && len(key) >= 4 && (key[:4] == "test" || key[:4] == "fake" || key[:4] == "mock") {
			// Allow test keys in CI
			return nil
		}

		// For real environments, enforce minimum length
		if !isCI && len(key) < 10 {
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

// ShowWelcomeMessage displays a comprehensive welcome message for new users
func ShowWelcomeMessage() {
	fmt.Println("🚀 Welcome to ledit - AI-powered code assistance!")
	fmt.Println()
	fmt.Println("   ledit helps you write code faster using AI language models.")
	fmt.Println("   Requires models with tool-calling support for code editing.")
	fmt.Println("   Get started with low-cost AI models - no lock-in, maximum flexibility.")
	fmt.Println()
	fmt.Println("💡 Recommended for beginners:")
	fmt.Println("   • OpenRouter - Access to 100+ AI models through one API")
	fmt.Println("   • Tool-calling models work best with ledit (required for code editing)")
	fmt.Println("   • Pay-as-you-go pricing starting from $0.0001 per request")
	fmt.Println("   • Great model: qwen/qwen3-coder-30b-a3b-instruct (excellent for coding)")
	fmt.Println()
	fmt.Println("🔗 Get started: https://openrouter.ai/keys")
	fmt.Println()
}

// ShowNextSteps displays helpful next steps after successful setup
func ShowNextSteps(provider, configDir string) {
	fmt.Println("Next steps:")
	fmt.Println("  • Run 'ledit' to start the interactive mode")
	fmt.Println("  • Run 'ledit agent \"your task here\"' for direct commands")

	// Add specific recommendations based on provider
	if provider == "openrouter" {
		fmt.Println()
		fmt.Println("💰 Cost-effective tool-calling models:")
		fmt.Println("  • qwen/qwen3-coder-30b-a3b-instruct - Excellent for coding tasks")
		fmt.Println("  • anthropic/claude-3.5-haiku - Fast and affordable")
		fmt.Println("  • openai/gpt-5-mini - Good performance, low cost")
		fmt.Println("  • Note: Avoid models without tool-calling support - they won't work with ledit")
		fmt.Println()
		fmt.Println("📖 Usage examples:")
		fmt.Println("  ledit agent -m \"qwen/qwen3-coder-30b-a3b-instruct\" \"Add error handling to my function\"")
		fmt.Println("  ledit agent -p openrouter \"Explain this code and suggest improvements\"")
	}

	fmt.Println()
	fmt.Println("  • Use --provider and --model flags to try different options")
	fmt.Printf("  • Config stored in: %s\n", configDir)
	fmt.Println()
}
