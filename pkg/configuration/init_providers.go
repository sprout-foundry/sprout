package configuration

// init_providers.go — the interactive provider-selection layer: the
// onboarding provider pick (selectInitialProvider), the runtime provider
// switcher (SelectProvider), and the add-a-new-provider flow (addNewProvider).
// Split out of init.go.

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
)

// selectInitialProvider guides user through initial provider selection
func selectInitialProvider(apiKeys *APIKeys, cfg *Config) (string, error) {
	// Non-interactive environments cannot prompt for provider selection.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no provider configured. Running in non-interactive mode. " + noninteractive.HelpHint)
	}

	// Show skip option first
	fmt.Println("[bot] Skip provider setup:")
	fmt.Println("  0. Skip provider setup — use as editor only (no AI features needed)")
	fmt.Println("     Select this if you just want to try the editor or use a local model")
	fmt.Println("     like LM Studio or the built-in local model without configuring")
	fmt.Println("     an API key now. You can always set up an AI provider later.")
	fmt.Println()

	// Check which providers have API keys already
	providersWithKeys := []string{}

	// First, check for providers that have environment variables set
	envProviders := []string{}
	for _, name := range KnownProviderNames() {
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
	// Reorder envProviders by user history (used-before first, then any
	// creds, then the rest). The display menu and selection index both
	// reference this slice, so the order is what the user sees and what
	// the default pick points at. Without this reordering the first
	// entry would be whatever KnownProviderNames() happens to return
	// first alphabetically (often openrouter), regardless of which
	// provider the user actually runs day-to-day.
	envProviders = orderProvidersByUsage(envProviders, cfg)

	// If we have providers with environment variables, offer them with skip option
	if len(envProviders) > 0 {
		fmt.Println("[>>] Detected providers from environment variables:")
		fmt.Println("  0. Skip — use as editor only (no AI features needed)")
		for i, providerName := range envProviders {
			metadata, _ := GetProviderAuthMetadata(providerName)
			envVarName := metadata.EnvVar
			fmt.Printf("  %d. %s (from %s)\n", i+1, GetProviderDisplayName(providerName), envVarName)
		}
		fmt.Println()

		choice, err := readIntInput(fmt.Sprintf("Select a provider (0-%d, 0 to skip, default 1): ", len(envProviders)), 0, len(envProviders))
		if err != nil {
			return "", fmt.Errorf("invalid choice: %w", err)
		}

		if choice == 0 {
			return "editor", nil
		}

		selected := envProviders[0]
		if choice > 0 && choice <= len(envProviders) {
			selected = envProviders[choice-1]
		}
		console.GlyphSuccess.Fprintln(os.Stdout, fmt.Sprintf("Using %s (environment variable detected)", GetProviderDisplayName(selected)))
		return selected, nil
	}

	// Snapshot the merged provider list so the displayed menu, the
	// readIntInput bounds, and the index used for selection are all
	// guaranteed consistent — even if the runtime factory happens to
	// upsert a new remote provider mid-onboarding.
	providerNames := KnownProviderNames()

	// Check which providers have API keys already (from file)
	for _, name := range providerNames {
		metadata, err := GetProviderAuthMetadata(name)
		if err != nil {
			continue
		}
		if !metadata.RequiresAPIKey || HasProviderAuth(name) {
			providersWithKeys = append(providersWithKeys, name)
		}
	}
	// Same tiered reordering as envProviders: prefer providers the user
	// has actually used before, then any provider with credentials, then
	// the rest. Without this, the "Ready to use" list defaults to
	// openrouter-first purely by alphabetical chance.
	providersWithKeys = orderProvidersByUsage(providersWithKeys, cfg)

	// If we have providers ready to use, show them first
	if len(providersWithKeys) > 0 {
		console.GlyphSuccess.Fprintln(os.Stdout, "Ready to use (configured or no API key needed):")
		for i, providerName := range providersWithKeys {
			fmt.Printf("  %d. %s", i+1, GetProviderDisplayName(providerName))
			if !RequiresAPIKey(providerName) {
				fmt.Print(" (no API key needed)")
			}
			fmt.Println()
		}
		fmt.Println()
	}

	// Show all provider options with clear descriptions
	fmt.Println("[bot] All available AI providers:")
	for i, name := range providerNames {
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

		if desc := providerDescription(name); desc != "" {
			description = " - " + desc
		}

		fmt.Printf("  %d. %s%s%s\n", i+1, GetProviderDisplayName(name), status, description)
	}
	fmt.Println()

	// Get user choice
	choice, err := readIntInput(fmt.Sprintf("Select a provider (0-%d, 0 to skip): ", len(providerNames)), 0, len(providerNames))
	if err != nil {
		return "", fmt.Errorf("invalid choice: %w", err)
	}

	// Handle skip option (choice 0)
	if choice == 0 {
		return "editor", nil
	}

	selectedProvider := providerNames[choice-1]

	// Check if API key is needed
	metadata, _ := GetProviderAuthMetadata(selectedProvider)
	if metadata.RequiresAPIKey && !HasProviderAuth(selectedProvider) {
		fmt.Println()
		fmt.Printf("[list] Setting up %s:\n", GetProviderDisplayName(selectedProvider))

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

		console.GlyphSuccess.Fprintln(os.Stdout, fmt.Sprintf("API key saved for %s (%d models available)", GetProviderDisplayName(selectedProvider), modelCount))
	} else if metadata.RequiresAPIKey {
		console.GlyphSuccess.Fprintln(os.Stdout, fmt.Sprintf("Using existing API key for %s", GetProviderDisplayName(selectedProvider)))
	} else {
		console.GlyphSuccess.Fprintln(os.Stdout, fmt.Sprintf("Selected %s (no API key required)", GetProviderDisplayName(selectedProvider)))
	}

	return selectedProvider, nil
}

// SelectProvider allows user to select a provider interactively
func SelectProvider(currentProvider string, apiKeys *APIKeys) (string, error) {
	// Non-interactive environments cannot prompt for provider selection.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no provider configured. Running in non-interactive mode. " + noninteractive.HelpHint)
	}

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
		fmt.Printf("%s%d. %s\n", indicator, i+1, GetProviderDisplayName(provider))
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
		"openai", "openrouter", "deepinfra", "ollama-cloud", "lmstudio",
	} {
		if !HasProviderAuth(provider) {
			needsKey = append(needsKey, provider)
		}
	}

	if len(needsKey) == 0 {
		return "", fmt.Errorf("all providers already configured")
	}

	for i, provider := range needsKey {
		fmt.Printf("  %d. %s\n", i+1, GetProviderDisplayName(provider))
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

	console.GlyphSuccess.Fprintln(os.Stdout, fmt.Sprintf("Added %s (%d models available)", GetProviderDisplayName(provider), modelCount))
	return provider, nil
}
