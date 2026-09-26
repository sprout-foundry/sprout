package configuration

// init.go — configuration lifecycle + onboarding entry points: Initialize,
// the API-key / provider validation layer, LoadOrInitConfig, the provider
// catalog listing (GetAvailableProviders), and the welcome / next-step /
// debug output. The interactive provider-selection layer (selectInitialProvider,
// SelectProvider, addNewProvider) lives in init_providers.go.

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/term"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/noninteractive"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// specialProviderDescriptions covers the few non-config providers
// (ollama, ollama-local, jinaai) that aren't in pkg/providercatalog
// because they ship no provider JSON. Built-ins and remote providers
// pull from the catalog instead; this map is the small fallback for
// the rest. Format matches the catalog: no leading separator.
var specialProviderDescriptions = map[string]string{
	"ollama":       "Run models locally, completely free (requires setup)",
	"ollama-local": "Run models locally, completely free (requires setup)",
	"jinaai":       "Specialized in embeddings and search",
}

// providerDescription returns the user-facing description for a
// provider. Lookup chain: pkg/providercatalog (the curated catalog,
// refreshed by .github/workflows/provider-catalog-refresh.yml) →
// specialProviderDescriptions (for non-config providers like ollama)
// → empty string. Replaces an earlier hardcoded switch in init.go
// that only covered 7 of the 16 known providers.
func providerDescription(name string) string {
	if p, ok := providercatalog.FindProvider(name); ok && strings.TrimSpace(p.Description) != "" {
		return p.Description
	}
	return specialProviderDescriptions[name]
}

// readInput reads a line of input from stdin without conflicting with other input systems
func readInput() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return strings.TrimSpace(input), nil
}

// readIntInput reads an integer from stdin with validation
func readIntInput(prompt string, min int, max int) (int, error) {
	input, err := readInput()
	if err != nil {
		return 0, fmt.Errorf("failed to read input: %w", err)
	}

	choice, err := strconv.Atoi(input)
	if err != nil {
		fmt.Printf("Please enter a valid number between %d and %d\n", min, max)
		// Recursion is bounded - each call prompts the user once.
		// We only recurse on invalid input, and user must eventually
		// enter valid input or exhaust retries.
		choice, err = readIntInput(prompt, min, max)
		if err != nil {
			return 0, err
		}
		return choice, nil
	}

	if choice < min || choice > max {
		fmt.Printf("Please enter a number between %d and %d\n", min, max)
		// Recursion is bounded - each call prompts the user once.
		// We only recurse on invalid input, and user must eventually
		// enter valid input or exhaust retries.
		choice, err = readIntInput(prompt, min, max)
		if err != nil {
			return 0, err
		}
		return choice, nil
	}

	return choice, nil
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

	// Populate from SPROUT_API_KEYS_JSON env var (bulk injection for SaaS/container environments)
	apiKeys.PopulateFromJSONEnv()

	// Populate from individual environment variables — these take priority over JSON blob
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
				fmt.Println()
				console.GlyphWarning.Fprintln(os.Stdout, fmt.Sprintf("Current provider '%s' requires an API key but none is configured.", GetProviderDisplayName(currentProvider)))
			}
		}
	}

	// In CI environments, skip interactive setup and use defaults
	if isCI && (isFirstRun || needsSetup) {
		if isFirstRun {
			fmt.Printf("[>>] Welcome to sprout! Let's set up your AI provider.\n")
			fmt.Printf("   Config directory: %s\n\n", configDir)
		}

		// Set a default provider that works in CI. The historical chain
		// (openrouter → openai → fall through) was alphabetical and ignored
		// the user's actual usage history; instead, prefer a provider the
		// user has already used (ProviderModels entry), then any provider
		// with credentials configured. See orderProvidersByUsage.
		chosen := ""
		for _, name := range orderProvidersByUsage(KnownProviderNames(), config) {
			if HasProviderAuth(name) {
				chosen = name
				break
			}
		}
		if chosen != "" {
			config.LastUsedProvider = chosen
			console.GlyphSuccess.Fprintln(os.Stdout, fmt.Sprintf("Using %s provider from environment", GetProviderDisplayName(chosen)))
		} else {
			// Don't save test provider as default - it's for testing only
			// Leave LastUsedProvider empty and let callers handle the test provider
			console.GlyphSuccess.Fprintln(os.Stdout, "No real provider available; using test provider for CI")
			console.GlyphWarning.Fprintln(os.Stdout, "Please configure a real provider (OPENROUTER_API_KEY or OPENAI_API_KEY)")
		}

		if err := config.Save(); err != nil {
			return nil, nil, fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("[done] Setup complete! You can now use sprout.\n\n")

		return config, apiKeys, nil
	}

	if isFirstRun || needsSetup {
		if isFirstRun {
			ShowWelcomeMessage()
			fmt.Printf("   Config directory: %s\n\n", configDir)
		}

		// First run or setup needed - select initial provider
		provider, err := selectInitialProvider(apiKeys, config)
		if err != nil {
			return nil, nil, fmt.Errorf("provider setup failed: %w", err)
		}

		config.LastUsedProvider = provider
		if err := config.Save(); err != nil {
			return nil, nil, fmt.Errorf("failed to save config: %w", err)
		}

		if provider == "editor" {
			fmt.Println("[done] Setup complete! Editor mode selected — AI features are not configured.")
			fmt.Println("   You can set up an AI provider later:")
			fmt.Println("   • Run 'sprout agent --provider openrouter' to use AI features")
			fmt.Println("   • Or set up a provider via the webui: sprout agent -d")
			fmt.Println()
		} else {
			fmt.Printf("[done] Setup complete! You can now use sprout with %s.\n\n", GetProviderDisplayName(provider))
		}

		// Show helpful next steps
		ShowNextSteps(provider, configDir)
	}

	// Final validation - ensure selected provider is actually usable
	if err := validateProviderSetup(config.LastUsedProvider); err != nil {
		return nil, nil, fmt.Errorf("provider validation failed: %w", err)
	}

	return config, apiKeys, nil
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
		return fmt.Errorf("no API key for %s. Running in non-interactive mode. "+noninteractive.HelpHint, GetProviderDisplayName(provider))
	}

	fmt.Println()
	console.GlyphWarning.Fprintln(os.Stdout, fmt.Sprintf("No API key found for %s", GetProviderDisplayName(provider)))
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

		console.GlyphSuccess.Fprintln(os.Stdout, fmt.Sprintf("API key saved for %s (%d models available)", GetProviderDisplayName(provider), modelCount))
		return nil
	}

	// Choice 2 - select different provider
	return fmt.Errorf("provider requires API key")
}

// GetAvailableProviders returns all supported providers
func GetAvailableProviders() []string {
	// Use the global factory (which has embedded + remote configs)
	providerFactory := providers.GlobalFactory()
	factoryProviders := providerFactory.GetAvailableProviders()

	// Add the special providers that aren't in the factory (built-in ones).
	// `test` (api.TestClientType) is intentionally excluded — it's an in-process
	// mock sentinel for unit tests; if a user selected it from a settings
	// dropdown and it landed on disk as LastUsedProvider, the next session
	// would silently route to a no-op mock client. Tests that need the mock
	// construct api.TestClientType directly.
	specialProviders := []string{
		"ollama-local",
		"ollama-cloud",
		"editor",
	}

	// Combine and deduplicate
	providerSet := make(map[string]bool)

	// Add factory providers
	for _, provider := range factoryProviders {
		providerSet[provider] = true
	}

	// Add special providers
	for _, provider := range specialProviders {
		providerSet[provider] = true
	}

	// Convert back to slice
	result := make([]string, 0, len(providerSet))
	for provider := range providerSet {
		result = append(result, provider)
	}

	// Add custom providers to the result set before sorting.
	var cfg *Config
	if c, err := Load(); err == nil {
		for provider := range c.CustomProviders {
			if !providerSet[provider] {
				result = append(result, provider)
			}
		}
		cfg = c
	}
	sort.Strings(result)

	// Reorder by user history: previously-used credentialed providers
	// first, then credentialed-but-unused, then the rest. The default
	// alphabetical sort put openrouter first almost every time, which
	// doesn't reflect what the user actually runs day-to-day. Tier 1
	// (ProviderModels history) needs cfg; if Load failed above, cfg is
	// nil and the helper degrades to a 2-tier sort by credentials
	// alone.
	result = orderProvidersByUsage(result, cfg)

	return result
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
		"openai", "openrouter", "deepinfra", "ollama-local", "ollama-cloud",
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
	fmt.Println("[>>] Welcome to sprout - AI-powered code assistance!")
	fmt.Println()
	fmt.Println("[i] Get started with the web-based code editor:")
	fmt.Println("   • Run 'sprout agent -d' to launch at http://localhost:56000")
	fmt.Println("   • Full code editor with AI integration built in")
	fmt.Println("   • Friendly setup experience for providers and models")
	fmt.Println("   • The recommended way to explore sprout's capabilities")
	fmt.Println()
	fmt.Println("   sprout helps you write code faster using AI language models.")
	fmt.Println("   Requires models with tool-calling support for code editing.")
	fmt.Println("   Get started with low-cost AI models - no lock-in, maximum flexibility.")
	fmt.Println()
	fmt.Println("[i] Recommended for beginners:")
	fmt.Println("   • OpenRouter - Access to 100+ AI models through one API")
	fmt.Println("   • Tool-calling models are required for sprout to function properly")
	fmt.Println("   • Pay-as-you-go pricing starting from $0.0001 per request")
	fmt.Println("   • Great model: qwen/qwen3-coder-30b-a3b-instruct (excellent for coding)")
	fmt.Println()
	fmt.Println("[link] Get started: https://openrouter.ai/keys")
	fmt.Println()
}

// ShowNextSteps displays helpful next steps after successful setup
func ShowNextSteps(provider, configDir string) {
	if provider == "editor" {
		fmt.Println("You're in editor-only mode. AI-powered features are not available.")
		fmt.Println()
		fmt.Println("To enable AI features:")
		fmt.Println("  • Run 'sprout agent -d' to launch the webui and configure providers")
		fmt.Println("  • Or set the SPROUT_PROVIDER environment variable (SPROUT_PROVIDER also supported)")
		fmt.Println()
		return
	}

	fmt.Println("Next steps:")
	fmt.Println("  • Run 'sprout agent -d' to start the web-based code editor (recommended)")
	fmt.Println("  • Run 'sprout' to start the interactive CLI mode")
	fmt.Println("  • Run 'sprout agent \"your task here\"' for direct commands")

	// Add specific recommendations based on provider
	if provider == "openrouter" {
		fmt.Println()
		fmt.Println("$ Cost-effective tool-calling models:")
		fmt.Println("  • qwen/qwen3-coder-30b-a3b-instruct - Excellent for coding tasks")
		fmt.Println("  • openai/gpt-5-mini - Good performance, low cost")
		fmt.Println("  • Note: Avoid models without tool-calling support - they won't work with sprout")
		fmt.Println()
		fmt.Println("[read] Usage examples:")
		fmt.Println("  sprout agent -m \"qwen/qwen3-coder-30b-a3b-instruct\" \"Add error handling\"")
		fmt.Println("  sprout agent -p openrouter \"Explain this code\"")
	}

	fmt.Println()
	fmt.Println("  • Use --provider and --model flags to try different options")
	fmt.Printf("  • Config stored in: %s\n", configDir)
	fmt.Println()
}
