//go:build !js

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// maxAPIKeyRetries is the number of times we'll re-prompt for an API key
// after a validation failure before giving up and falling through to the
// normal agent-creation path.
const maxAPIKeyRetries = 3

// descTruncateLen is the maximum width for a provider description line in
// the selection menu. Keeps the list scannable on narrow terminals.
const descTruncateLen = 80

// needsOnboarding reports whether the user has a working provider configured.
// Returns true when LastUsedProvider is empty, a non-AI sentinel ("test" /
// "editor"), or points at a provider whose credentials are missing.
func needsOnboarding() bool {
	cfg, err := configuration.Load()
	if err != nil || cfg == nil {
		return false // let the normal error path surface the failure
	}
	provider := strings.TrimSpace(cfg.LastUsedProvider)
	switch provider {
	case "", "test", "editor":
		return true
	}
	return !configuration.HasProviderAuth(provider)
}

// maybeRunOnboarding is the entry-point gate. Returns true when the guided
// onboarding flow ran to completion (a provider was configured), false when
// it was skipped or not needed. The caller (createChatAgent) invokes this
// before agent creation so a freshly-configured provider is picked up by the
// subsequent NewAgent() call.
//
// Guards (all must pass):
//   - stdin is a terminal (no onboarding in piped/scripted contexts)
//   - not running under CI
//   - not in daemon mode (the WebUI has its own onboarding dialog)
//   - onboarding is actually needed
func maybeRunOnboarding() bool {
	if !StdinIsTerminal() {
		return false
	}
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return false
	}
	if os.Getenv("SPROUT_DAEMON") != "" {
		return false
	}
	if !needsOnboarding() {
		return false
	}
	return runGuidedOnboarding()
}

// runGuidedOnboarding walks the user through provider → model → API key →
// persist. Returns true on successful completion, false on skip or failure.
// All errors are handled gracefully: a failure at any step falls through to
// the normal agent-creation path rather than blocking the user.
func runGuidedOnboarding() bool {
	fmt.Println()
	fmt.Println("Welcome to sprout!")
	fmt.Println()
	fmt.Println("Sprout needs an AI provider to work. Let's pick one — it takes about a minute.")
	fmt.Println("You'll need an API key from the provider you choose.")
	fmt.Println()
	fmt.Println("Type 'skip' at any prompt to explore in editor-only mode (set up AI later).")
	fmt.Println()

	providerID, ok := selectProviderInteractive()
	if !ok {
		fmt.Println()
		console.GlyphInfo.Printf("You can set up a provider later with 'sprout keys set <provider>'")
		return false
	}

	// Look up the catalog entry for model defaults + signup URLs.
	catProvider, _ := providercatalog.FindProvider(providerID)
	meta, _ := configuration.GetProviderAuthMetadata(providerID)

	// API key entry (only for providers that need one).
	if meta.RequiresAPIKey {
		if !collectAndValidateAPIKey(providerID, catProvider) {
			return false
		}
	}

	// Persist provider + model to config.
	modelName := pickModelForProvider(catProvider, providerID)
	if err := persistProviderAndModel(providerID, modelName); err != nil {
		fmt.Println()
		console.GlyphWarning.Printf("Could not save provider config: %v", err)
		console.GlyphInfo.Printf("Run 'sprout keys set %s' to finish setup.", providerID)
		return false
	}

	// Success.
	fmt.Println()
	displayName := configuration.GetProviderDisplayName(providerID)
	if modelName != "" {
		console.GlyphSuccess.Printf("All set! Provider: %s, Model: %s", displayName, modelName)
	} else {
		console.GlyphSuccess.Printf("All set! Provider: %s", displayName)
	}
	fmt.Println()
	fmt.Println("You're ready to go. Try asking sprout to explain your codebase,")
	fmt.Println("or type /help for commands.")
	return true
}

// selectProviderInteractive presents the recommended providers, an
// "Other providers" expander, and a skip option. Returns (providerID, true)
// on selection or ("", false) when the user skips.
func selectProviderInteractive() (string, bool) {
	reader := bufio.NewReader(os.Stdin)

	for {
		catalog := providercatalog.Current()

		var recommended, others []providercatalog.Provider
		for _, p := range catalog.Providers {
			if p.Recommended {
				recommended = append(recommended, p)
			} else {
				others = append(others, p)
			}
		}

		// No catalog data — fall back to the known-providers list so the
		// user can still type a name manually.
		if len(catalog.Providers) == 0 {
			fmt.Println("Enter a provider ID (e.g. openrouter, zai, openai):")
			input, err := promptLine(reader, "Provider: ")
			if err != nil {
				return "", false
			}
			input = strings.ToLower(strings.TrimSpace(input))
			if input == "skip" {
				return "", false
			}
			return input, true
		}

		fmt.Println("Recommended providers:")
		printProviderList(recommended, 1)

		otherOptionNum := len(recommended) + 1
		skipOptionNum := otherOptionNum + 1
		if len(others) > 0 {
			fmt.Printf("  %d. Other providers\n", otherOptionNum)
		} else {
			skipOptionNum = otherOptionNum // collapse when there are no others
		}
		fmt.Printf("  %d. Skip (editor-only mode)\n", skipOptionNum)
		fmt.Println()

		prompt := fmt.Sprintf("Select a provider (1-%d, or type a name): ", skipOptionNum)
		input, err := promptLine(reader, prompt)
		if err != nil {
			return "", false
		}

		input = strings.TrimSpace(input)
		if strings.EqualFold(input, "skip") {
			return "", false
		}

		// Numeric selection?
		if n, convErr := strconv.Atoi(input); convErr == nil {
			if n >= 1 && n <= len(recommended) {
				return recommended[n-1].ID, true
			}
			if n == otherOptionNum && len(others) > 0 {
				// Expand the full list and re-prompt.
				fmt.Println()
				fmt.Println("All providers:")
				printProviderList(others, 1)
				fmt.Printf("  %d. Back to recommended\n", len(others)+1)
				fmt.Println()
				subInput, subErr := promptLine(reader, "Select a provider: ")
				if subErr != nil {
					return "", false
				}
				subInput = strings.TrimSpace(subInput)
				if strings.EqualFold(subInput, "skip") {
					return "", false
				}
				if sn, e := strconv.Atoi(subInput); e == nil {
					if sn >= 1 && sn <= len(others) {
						return others[sn-1].ID, true
					}
				}
				// Non-numeric or out of range — treat as a typed provider name.
				if isKnownProvider(strings.ToLower(subInput)) {
					return strings.ToLower(subInput), true
				}
				fmt.Println()
				console.GlyphWarning.Printf("Unknown provider %q — please choose from the list.", subInput)
				continue
			}
			if n == skipOptionNum {
				return "", false
			}
			fmt.Println()
			console.GlyphWarning.Printf("Selection %d is out of range.", n)
			continue
		}

		// Typed provider name.
		normalizedName := strings.ToLower(input)
		if isKnownProvider(normalizedName) {
			return normalizedName, true
		}
		fmt.Println()
		console.GlyphWarning.Printf("Unknown provider %q — please choose from the list or type 'skip'.", input)
	}
}

// collectAndValidateAPIKey handles the API key prompt → validate → retry
// loop for a provider that requires a key. Returns true on success, false
// if the user gives up (types "skip" or exhausts retries).
func collectAndValidateAPIKey(providerID string, catProvider providercatalog.Provider) bool {
	displayName := configuration.GetProviderDisplayName(providerID)

	if catProvider.SignupURL != "" {
		fmt.Println()
		fmt.Printf("Get a %s API key: %s\n", displayName, catProvider.SignupURL)
	}
	if catProvider.APIKeyHelp != "" {
		fmt.Printf("%s\n", catProvider.APIKeyHelp)
	}
	fmt.Println()

	for attempt := 1; attempt <= maxAPIKeyRetries; attempt++ {
		key, err := configuration.PromptForAPIKey(providerID)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "no api key provided") {
				return false // user typed nothing / aborted
			}
			fmt.Println()
			console.GlyphWarning.Printf("Could not read input: %v", err)
			return false
		}

		if strings.EqualFold(strings.TrimSpace(key), "skip") {
			return false
		}

		modelCount, valErr := configuration.ValidateAndSaveAPIKey(providerID, key)
		if valErr != nil {
			fmt.Println()
			console.GlyphWarning.Printf("Validation failed: %v", valErr)
			if attempt < maxAPIKeyRetries {
				fmt.Printf("Let's try again (%d/%d attempts).\n", attempt, maxAPIKeyRetries)
				continue
			}
			console.GlyphWarning.Printf("Max retries reached. You can retry later with 'sprout keys set %s'.", providerID)
			return false
		}

		console.GlyphSuccess.Printf("API key validated (%d models available)", modelCount)
		return true
	}
	return false
}

// pickModelForProvider resolves the best default model for the selected
// provider from catalog metadata. Returns "" if no model can be determined
// (the agent-creation path will resolve one at runtime).
func pickModelForProvider(catProvider providercatalog.Provider, providerID string) string {
	if catProvider.DefaultModel != "" {
		return catProvider.DefaultModel
	}
	if catProvider.RecommendedModel != "" {
		return catProvider.RecommendedModel
	}
	if len(catProvider.Models) > 0 {
		return catProvider.Models[0].ID
	}
	// Fall back to the user's previously-stored model for this provider, if any.
	if cfg, err := configuration.Load(); err == nil && cfg != nil {
		if m := cfg.GetModelForProvider(providerID); m != "" {
			return m
		}
	}
	return ""
}

// persistProviderAndModel writes the provider and model selection to config
// via the global config manager so the subsequent agent.NewAgent() picks
// them up.
func persistProviderAndModel(providerID, modelName string) error {
	mgr, err := configuration.NewManagerSilent()
	if err != nil {
		return fmt.Errorf("load config manager: %w", err)
	}

	clientType, err := mgr.MapStringToClientType(providerID)
	if err != nil {
		return fmt.Errorf("map provider %q: %w", providerID, err)
	}

	if err := mgr.SetProvider(clientType); err != nil {
		return fmt.Errorf("set provider: %w", err)
	}
	if modelName != "" {
		if err := mgr.SetModelForProvider(clientType, modelName); err != nil {
			return fmt.Errorf("set model: %w", err)
		}
	}
	return mgr.SaveConfig()
}

// printProviderList prints a numbered list of providers with name and a
// truncated description. The starting index controls the first number
// displayed (1-based).
func printProviderList(providers []providercatalog.Provider, startIndex int) {
	for i, p := range providers {
		desc := truncate(strings.TrimSpace(p.Description), descTruncateLen)
		if desc != "" {
			fmt.Printf("  %d. %s — %s\n", startIndex+i, p.Name, desc)
		} else {
			fmt.Printf("  %d. %s\n", startIndex+i, p.Name)
		}
	}
}

// truncate clips s to at most maxLen characters, appending an ellipsis when
// truncation occurs.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return s[:maxLen-1] + "…"
}
