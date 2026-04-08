package configuration

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent_providers"
	"github.com/alantheprice/ledit/pkg/credentials"
	"golang.org/x/term"
)

// readInput reads a line of input from stdin without conflicting with other input systems
func readInput() string {
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// readIntInput reads an integer from stdin with validation
func readIntInput(prompt string, min int, max int) (int, error) {
	for {
		fmt.Print(prompt)
		input := readInput()

		choice, err := strconv.Atoi(input)
		if err != nil {
			fmt.Printf("Please enter a valid number between %d and %d\n", min, max)
			continue
		}

		if choice < min || choice > max {
			fmt.Printf("Please enter a number between %d and %d\n", min, max)
			continue
		}

		return choice, nil
	}
}

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
	if !apiKeys.PopulateFromEnvironment() {
		log.Printf("[debug] no API keys found in environment variables")
	}

	// Check if this is first run (no provider selected)
	isFirstRun := config.LastUsedProvider == ""

	// Also check if current provider has no API key (and needs one)
	needsSetup := false
	if !isFirstRun {
		currentProvider := config.LastUsedProvider
		if currentProvider != "editor" && RequiresAPIKey(currentProvider) && !HasProviderAuth(currentProvider) {
			needsSetup = true
			if !isCI {
				fmt.Printf("\n[WARN] Current provider '%s' requires an API key but none is configured.\n", getProviderDisplayName(currentProvider))
			}
		}
	}

	// In CI environments, skip interactive setup and use defaults
	if isCI && (isFirstRun || needsSetup) {
		if isFirstRun {
			fmt.Printf("[>>] Welcome to ledit! Let's set up your AI provider.\n")
			fmt.Printf("   Config directory: %s\n\n", configDir)
		}

		// Set a default provider that works in CI
		if HasProviderAuth("openrouter") {
			config.LastUsedProvider = "openrouter"
			fmt.Printf("[OK] Using OpenRouter provider from environment\n")
		} else if HasProviderAuth("openai") {
			config.LastUsedProvider = "openai"
			fmt.Printf("[OK] Using OpenAI provider from environment\n")
		} else {
			// Default to test provider which is designed for CI/testing
			config.LastUsedProvider = "test"
			fmt.Printf("[OK] Using test provider (designed for CI environments)\n")
		}

		if err := config.Save(); err != nil {
			return nil, nil, fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("[done] Setup complete! You can now use ledit.\n\n")

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

		fmt.Printf("[done] Setup complete! You can now use ledit with %s.\n\n", getProviderDisplayName(provider))

		// Show helpful next steps
		ShowNextSteps(provider, configDir)
	}

	// Final validation - ensure selected provider is actually usable
	if err := validateProviderSetup(config.LastUsedProvider); err != nil {
		return nil, nil, fmt.Errorf("provider validation failed: %w", err)
	}

	return config, apiKeys, nil
}

// selectInitialProvider guides user through initial provider selection
func selectInitialProvider(apiKeys *APIKeys) (string, error) {
	// Check which providers have API keys already
	providersWithKeys := []string{}

	// First, check for providers that have environment variables set
	envProviders := []string{}
	for _, name := range knownProviderNames {
		metadata, err := GetProviderAuthMetadata(name)
		if err != nil {
			continue
		}
		if metadata.RequiresAPIKey && metadata.EnvVar != "" {
			if envKey := os.Getenv(metadata.EnvVar); envKey != "" {
				envProviders = append(envProviders, name)
			}
		}
	}

	// If we have providers with environment variables, prioritize them
	if len(envProviders) > 0 {
		fmt.Println("[>>] Found providers with environment variables set:")
		for i, providerName := range envProviders {
			// Get the environment variable name for display
			metadata, _ := GetProviderAuthMetadata(providerName)
			envVarName := metadata.EnvVar
			fmt.Printf("  %d. %s (from %s)", i+1, getProviderDisplayName(providerName), envVarName)
			fmt.Println()
		}
		fmt.Println()

		// Auto-select the first provider with environment variable
		fmt.Printf("[OK] Auto-selecting %s (environment variable detected)\n", getProviderDisplayName(envProviders[0]))
		return envProviders[0], nil
	}

	// Check which providers have API keys already (from file)
	for _, name := range knownProviderNames {
		metadata, err := GetProviderAuthMetadata(name)
		if err != nil {
			continue
		}
		if !metadata.RequiresAPIKey || HasProviderAuth(name) {
			providersWithKeys = append(providersWithKeys, name)
		}
	}

	// If we have providers ready to use, show them first
	if len(providersWithKeys) > 0 {
		fmt.Println("[OK] Ready to use (configured or no API key needed):")
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
	fmt.Println("[bot] All available AI providers:")
	for i, name := range knownProviderNames {
		metadata, err := GetProviderAuthMetadata(name)
		if err != nil {
			continue
		}

		status := ""
		description := ""

		if metadata.RequiresAPIKey && !HasProviderAuth(name) {
			status = " (needs API key)"
		} else if metadata.RequiresAPIKey && HasProviderAuth(name) {
			status = " [OK]"
		} else {
			status = " (local, no key needed)"
		}

		// Add helpful descriptions
		switch name {
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

		fmt.Printf("  %d. %s%s%s\n", i+1, getProviderDisplayName(name), status, description)
	}
	fmt.Println()

	// Get user choice
	choice, err := readIntInput(fmt.Sprintf("Select a provider (1-%d): ", len(knownProviderNames)), 1, len(knownProviderNames))
	if err != nil {
		return "", fmt.Errorf("invalid choice: %w", err)
	}

	selectedProvider := knownProviderNames[choice-1]

	// Check if API key is needed
	metadata, _ := GetProviderAuthMetadata(selectedProvider)
	if metadata.RequiresAPIKey && !HasProviderAuth(selectedProvider) {
		fmt.Println()
		fmt.Printf("[list] Setting up %s:\n", getProviderDisplayName(selectedProvider))

		// Provide helpful information about getting API keys
		switch selectedProvider {
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

		apiKey, err := PromptForAPIKey(selectedProvider)
		if err != nil {
			return "", fmt.Errorf("failed to get API key: %w", err)
		}

		// Validate the API key before saving
		modelCount, err := ValidateAndSaveAPIKey(selectedProvider, apiKey)
		if err != nil {
			return "", fmt.Errorf("failed to validate and save API key: %w", err)
		}

		fmt.Printf("[OK] API key saved for %s (%d models available)\n", getProviderDisplayName(selectedProvider), modelCount)
	} else if metadata.RequiresAPIKey {
		fmt.Printf("[OK] Using existing API key for %s\n", getProviderDisplayName(selectedProvider))
	} else {
		fmt.Printf("[OK] Selected %s (no API key required)\n", getProviderDisplayName(selectedProvider))
	}

	return selectedProvider, nil
}

// EnsureProviderAPIKey ensures the provider has an API key, prompting if needed
func EnsureProviderAPIKey(provider string, apiKeys *APIKeys) error {
	if !RequiresAPIKey(provider) {
		return nil
	}

	if HasProviderAuth(provider) {
		return nil
	}

	// Non-interactive environments cannot prompt for API keys.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("no API key for %s. running in non-interactive mode. Set LEDIT_PROVIDER / configure ~/.ledit/config.json, or run `ledit agent` interactively", getProviderDisplayName(provider))
	}

	fmt.Println()
	fmt.Printf("[WARN] No API key found for %s\n", getProviderDisplayName(provider))
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  1. Enter API key now")
	fmt.Println("  2. Select a different provider")
	fmt.Println()

	choice, err := readIntInput("Choice (1-2): ", 1, 2)
	if err != nil {
		return fmt.Errorf("invalid choice: %w", err)
	}

	if choice == 1 {
		apiKey, err := PromptForAPIKey(provider)
		if err != nil {
			return fmt.Errorf("prompt for API key: %w", err)
		}

		// Validate the API key before saving
		modelCount, err := ValidateAndSaveAPIKey(provider, apiKey)
		if err != nil {
			return fmt.Errorf("failed to validate and save API key: %w", err)
		}

		fmt.Printf("[OK] API key saved for %s (%d models available)\n", getProviderDisplayName(provider), modelCount)
		return nil
	}

	// Choice 2 - select different provider
	return fmt.Errorf("provider requires API key")
}

// GetAvailableProviders returns all supported providers
func GetAvailableProviders() []string {
	// Use the provider factory to dynamically discover all available providers
	providerFactory := providers.NewProviderFactory()
	err := providerFactory.LoadEmbeddedConfigs()
	if err != nil {
		// Fallback to hardcoded list if factory fails to load
		return []string{
			"openai",
			"chutes",
			"zai",
			"openrouter",
			"deepinfra",
			"ollama-local",
			"ollama-turbo",
			"lmstudio",
		}
	}

	// Get all available providers from the factory
	factoryProviders := providerFactory.GetAvailableProviders()

	// Add the hardcoded providers that aren't in the factory (built-in ones)
	hardcodedProviders := []string{
		"openai",
		"ollama-local",
		"ollama-turbo",
	}

	// Combine and deduplicate
	providerSet := make(map[string]bool)

	// Add factory providers
	for _, provider := range factoryProviders {
		providerSet[provider] = true
	}

	// Add hardcoded providers
	for _, provider := range hardcodedProviders {
		providerSet[provider] = true
	}

	// Convert back to slice
	result := make([]string, 0, len(providerSet))
	for provider := range providerSet {
		result = append(result, provider)
	}

	if cfg, err := Load(); err == nil {
		for provider := range cfg.CustomProviders {
			if !providerSet[provider] {
				result = append(result, provider)
			}
		}
	}
	sort.Strings(result)

	return result
}

// SelectProvider allows user to select a provider interactively
func SelectProvider(currentProvider string, apiKeys *APIKeys) (string, error) {
	available := GetAvailableProviders()

	if len(available) == 0 {
		return "", fmt.Errorf("no providers available - please configure API keys")
	}

	fmt.Println("[bot] Available providers:")
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

	choice, err := readIntInput("Select provider: ", 1, len(available)+1)
	if err != nil {
		return "", fmt.Errorf("invalid choice: %w", err)
	}

	if choice <= len(available) {
		selectedProvider := available[choice-1]

		// Check if this provider needs an API key but doesn't have one
		if RequiresAPIKey(selectedProvider) && !HasProviderAuth(selectedProvider) {
			// Prompt for API key
			err := EnsureProviderAPIKey(selectedProvider, apiKeys)
			if err != nil {
				return "", fmt.Errorf("failed to ensure API key for %s: %w", selectedProvider, err)
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
	fmt.Println("[+] Add new provider:")

	// Show providers that need API keys
	needsKey := []string{}
	for _, provider := range []string{
		"openai", "openrouter", "deepinfra", "ollama-turbo", "lmstudio",
	} {
		if !HasProviderAuth(provider) {
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

	choice, err := readIntInput("Select provider to add: ", 1, len(needsKey))
	if err != nil {
		return "", fmt.Errorf("invalid choice: %w", err)
	}

	provider := needsKey[choice-1]

	// Get API key
	apiKey, err := PromptForAPIKey(provider)
	if err != nil {
		return "", fmt.Errorf("failed to prompt for API key: %w", err)
	}

	// Validate the API key before saving
	modelCount, err := ValidateAndSaveAPIKey(provider, apiKey)
	if err != nil {
		return "", fmt.Errorf("failed to validate and save API key: %w", err)
	}

	fmt.Printf("[OK] Added %s (%d models available)\n", getProviderDisplayName(provider), modelCount)
	return provider, nil
}

// validateProviderSetup ensures the provider can actually be used
func validateProviderSetup(provider string) error {
	if provider == "editor" {
		return nil // Editor-only mode — no provider validation needed
	}
	if provider == "" {
		return fmt.Errorf("no provider selected")
	}

	// Check if provider requires API key
	if RequiresAPIKey(provider) {
		if !HasProviderAuth(provider) {
			return fmt.Errorf("provider '%s' requires an API key but none is configured", provider)
		}

		// Basic API key format validation - skip in CI/test environments
		isCI := os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
		resolved, err := credentials.ResolveProvider(provider)
		if err != nil {
			return fmt.Errorf("validate provider setup: %w", err)
		}
		key := resolved.Value
		if key == "" {
			return nil
		}

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
	if err != nil {
		return config, fmt.Errorf("initialize configuration: %w", err)
	}
	return config, nil
}

// DebugPrintConfig prints current configuration for debugging
func DebugPrintConfig(config *Config, apiKeys *APIKeys) {
	fmt.Println("[tool] Current Configuration:")
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
	fmt.Println("[>>] Welcome to ledit - AI-powered code assistance!")
	fmt.Println()
	fmt.Println("   ledit helps you write code faster using AI language models.")
	fmt.Println("   Requires models with tool-calling support for code editing.")
	fmt.Println("   Get started with low-cost AI models - no lock-in, maximum flexibility.")
	fmt.Println()
	fmt.Println("[i] Recommended for beginners:")
	fmt.Println("   • OpenRouter - Access to 100+ AI models through one API")
	fmt.Println("   • Tool-calling models are required for ledit to function properly")
	fmt.Println("   • Pay-as-you-go pricing starting from $0.0001 per request")
	fmt.Println("   • Great model: qwen/qwen3-coder-30b-a3b-instruct (excellent for coding)")
	fmt.Println()
	fmt.Println("[link] Get started: https://openrouter.ai/keys")
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
		fmt.Println("$ Cost-effective tool-calling models:")
		fmt.Println("  • qwen/qwen3-coder-30b-a3b-instruct - Excellent for coding tasks")
		fmt.Println("  • openai/gpt-5-mini - Good performance, low cost")
		fmt.Println("  • Note: Avoid models without tool-calling support - they won't work with ledit")
		fmt.Println()
		fmt.Println("[read] Usage examples:")
		fmt.Println("  ledit agent -m \"qwen/qwen3-coder-30b-a3b-instruct\" \"Add error handling to my function\"")
		fmt.Println("  ledit agent -p openrouter \"Explain this code and suggest improvements\"")
	}

	fmt.Println()
	fmt.Println("  • Use --provider and --model flags to try different options")
	fmt.Printf("  • Config stored in: %s\n", configDir)
	fmt.Println()
}
