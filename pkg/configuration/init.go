package configuration

import (
	"fmt"
	"strings"
)

// Initialize loads or creates configuration with first-run setup
func Initialize() (*Config, *APIKeys, error) {
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

	// Populate API keys from environment on startup
	if apiKeys.PopulateFromEnvironment() {
		// Save if we captured any new keys from environment
		if err := SaveAPIKeys(apiKeys); err != nil {
			return nil, nil, fmt.Errorf("failed to save API keys: %w", err)
		}
	}

	// Check if this is first run (no provider selected)
	if config.LastUsedProvider == "" {
		// First run - select initial provider
		provider, err := selectInitialProvider(apiKeys)
		if err != nil {
			return nil, nil, err
		}

		config.LastUsedProvider = provider
		if err := config.Save(); err != nil {
			return nil, nil, fmt.Errorf("failed to save config: %w", err)
		}
	}

	return config, apiKeys, nil
}

// selectInitialProvider guides user through initial provider selection
func selectInitialProvider(apiKeys *APIKeys) (string, error) {
	fmt.Println("🚀 Welcome to ledit! Let's set up your AI provider.")
	fmt.Println()

	// Check which providers have API keys already
	availableProviders := []string{}
	providersWithKeys := []string{}

	// Check all providers
	providers := []string{
		"openai", "anthropic", "openrouter", "deepinfra",
		"deepseek", "gemini", "groq", "cerebras",
		"ollama", "ollama-local", "ollama-turbo",
	}

	for _, provider := range providers {
		if !RequiresAPIKey(provider) {
			// Ollama variants always available
			availableProviders = append(availableProviders, provider)
		} else if apiKeys.HasAPIKey(provider) {
			availableProviders = append(availableProviders, provider)
			providersWithKeys = append(providersWithKeys, provider)
		}
	}

	// If we have providers with keys, show them first
	if len(providersWithKeys) > 0 {
		fmt.Println("📋 Providers with API keys configured:")
		for i, provider := range providersWithKeys {
			fmt.Printf("  %d. %s\n", i+1, getProviderDisplayName(provider))
		}
		fmt.Println()
	}

	// Show all provider options
	fmt.Println("🤖 Available AI providers:")
	fmt.Println("  1. OpenAI (gpt-4o, gpt-4, etc.)")
	fmt.Println("  2. Anthropic (claude-3.5-sonnet, etc.)")
	fmt.Println("  3. OpenRouter (access to many models)")
	fmt.Println("  4. DeepInfra (open source models)")
	fmt.Println("  5. DeepSeek (deepseek-chat)")
	fmt.Println("  6. Google Gemini (gemini-2.0-flash)")
	fmt.Println("  7. Groq (fast inference)")
	fmt.Println("  8. Cerebras (fast inference)")
	fmt.Println("  9. Ollama (local models)")
	fmt.Println()

	// Get user choice
	var choice int
	fmt.Print("Select a provider (1-9): ")
	_, err := fmt.Scanln(&choice)
	if err != nil || choice < 1 || choice > 9 {
		return "", fmt.Errorf("invalid choice")
	}

	// Map choice to provider
	providerMap := map[int]string{
		1: "openai",
		2: "anthropic",
		3: "openrouter",
		4: "deepinfra",
		5: "deepseek",
		6: "gemini",
		7: "groq",
		8: "cerebras",
		9: "ollama",
	}

	selectedProvider := providerMap[choice]

	// For Ollama, ask which variant
	if selectedProvider == "ollama" {
		selectedProvider = selectOllamaVariant()
	}

	// Check if API key is needed
	if RequiresAPIKey(selectedProvider) && !apiKeys.HasAPIKey(selectedProvider) {
		fmt.Println()
		apiKey, err := PromptForAPIKey(selectedProvider)
		if err != nil {
			return "", fmt.Errorf("failed to get API key: %w", err)
		}

		apiKeys.SetAPIKey(selectedProvider, apiKey)
		if err := SaveAPIKeys(apiKeys); err != nil {
			return "", fmt.Errorf("failed to save API key: %w", err)
		}

		fmt.Printf("✅ API key saved for %s\n", getProviderDisplayName(selectedProvider))
	}

	return selectedProvider, nil
}

// selectOllamaVariant asks user which Ollama variant to use
func selectOllamaVariant() string {
	fmt.Println()
	fmt.Println("🦙 Ollama variants:")
	fmt.Println("  1. Standard Ollama (ollama serve)")
	fmt.Println("  2. Ollama Turbo (requires separate setup)")
	fmt.Println("  3. Local optimized (smaller models)")
	fmt.Println()

	var choice int
	fmt.Print("Select Ollama variant (1-3): ")
	_, err := fmt.Scanln(&choice)
	if err != nil || choice < 1 || choice > 3 {
		// Default to standard
		return "ollama"
	}

	switch choice {
	case 2:
		return "ollama-turbo"
	case 3:
		return "ollama-local"
	default:
		return "ollama"
	}
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
		"openai", "anthropic", "openrouter", "deepinfra",
		"deepseek", "gemini", "groq", "cerebras",
	}
	for _, provider := range providers {
		if apiKeys.HasAPIKey(provider) {
			key := apiKeys.GetAPIKey(provider)
			masked := strings.Repeat("*", len(key)-4) + key[len(key)-4:]
			fmt.Printf("    %s: %s\n", provider, masked)
		}
	}
}
