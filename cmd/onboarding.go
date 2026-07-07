//go:build !js

package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// maxAPIKeyRetries is the number of times we'll re-prompt for an API key
// after a validation failure before giving up and falling through to the
// normal agent-creation path.
const maxAPIKeyRetries = 3

// needsOnboarding reports whether the user has a working provider configured.
// Returns true when LastUsedProvider is empty, a non-AI sentinel ("test" /
// "editor"), or points at a provider whose credentials are missing.
func needsOnboarding() bool {
	cfg, err := configuration.Load()
	if err != nil || cfg == nil {
		return true // unreadable config: surface onboarding so user can reconfigure
	}
	provider := strings.TrimSpace(cfg.LastUsedProvider)
	switch provider {
	case "", "test", "editor":
		return true
	}
	// If the provider is a known built-in OR a configured custom provider,
	// don't second-guess the credential check — let agent creation surface
	// the real auth error. HasProviderAuth can false-negative on transient
	// keyring failures or empty env-var metadata for custom providers.
	if isKnownProvider(provider, cfg) {
		return false
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

// selectProviderInteractive presents a searchable provider menu and a skip
// option. Returns (providerID, true) on selection or ("", false) when the
// user skips. Uses console.SelectList for arrow-key navigation and
// type-to-filter; it has a built-in non-TTY fallback so no extra bufio is
// needed.
//
// Empty-catalog fallback: if the catalog has no providers, we still show a
// SelectList with a "Type provider ID" item and the skip option. The typed
// value is treated as the provider ID so the user can still manually pick
// (e.g. "openrouter") when the embedded catalog hasn't been loaded yet.
func selectProviderInteractive() (string, bool) {
	catalog := providercatalog.Current()

	var items []console.SelectItem

	if len(catalog.Providers) == 0 {
		// Catalog load failed or is empty — give the user a manual entry
		// option plus skip. The manual entry item's value is a sentinel we
		// detect below.
		items = []console.SelectItem{
			{Label: "Type provider ID manually", Detail: "e.g. openrouter, zai", Value: "__type__"},
			{Label: "Skip (editor-only mode)", Detail: "set up AI later", Value: "skip"},
		}
	} else {
		// Recommended first, then the rest.
		for _, p := range catalog.Providers {
			if p.Recommended {
				models := ""
				if p.DefaultModel != "" {
					models = p.DefaultModel
				} else if len(p.Models) > 0 {
					models = p.Models[0].ID
				}
				items = append(items, console.SelectItem{
					Label:  p.Name,
					Detail: "recommended · " + models,
					Value:  p.ID,
				})
			}
		}
		for _, p := range catalog.Providers {
			if p.Recommended {
				continue
			}
			models := ""
			if p.DefaultModel != "" {
				models = p.DefaultModel
			} else if len(p.Models) > 0 {
				models = p.Models[0].ID
			}
			items = append(items, console.SelectItem{
				Label:  p.Name,
				Detail: models,
				Value:  p.ID,
			})
		}
		// Skip option at the end.
		items = append(items, console.SelectItem{
			Label:  "Skip (editor-only mode)",
			Detail: "set up AI later",
			Value:  "skip",
		})
	}

	sl := console.NewSelectList(console.SelectListOptions{
		Title:      "Pick a provider",
		Items:      items,
		Searchable: true,
		PageSize:   10,
	})

	ctx := context.Background()
	value, ok, err := sl.Run(ctx)
	if err != nil || !ok {
		return "", false
	}
	if value == "skip" {
		return "", false
	}
	// Manual type-in sentinel: fall back to a simple prompt.
	if value == "__type__" {
		fmt.Print("Enter provider ID (e.g. openrouter, zai): ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", false
		}
		input = strings.ToLower(strings.TrimSpace(input))
		if input == "" || strings.EqualFold(input, "skip") {
			return "", false
		}
		return input, true
	}
	return value, true
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
