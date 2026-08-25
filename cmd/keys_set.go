//go:build !js

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// keysSetCmd adds or replaces an API key for a built-in provider.
var keysSetCmd = &cobra.Command{
	Use:   "set <provider>",
	Short: "Add or replace an API key for a built-in provider",
	Long: `Add or replace an API key for a built-in provider.

This is the recommended way to configure credentials on first run. The key
is validated against the provider's live API (so a typo is caught now, not
later) and stored in the active credential backend (OS keyring or encrypted
file).

Usage:
  sprout keys set <provider>   # prompt for the key interactively

If no provider is given and stdin is a terminal, a selectable list of
providers that require an API key is shown.

The new key replaces any existing key for that provider (after confirmation).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runKeysSet(args)
	},
}

func init() {
	keysCmd.AddCommand(keysSetCmd)
}

// runKeysSet implements `sprout keys set [provider]`.
func runKeysSet(args []string) error {
	// 1. Resolve the provider argument.
	var provider string
	if len(args) >= 1 {
		provider = strings.ToLower(strings.TrimSpace(args[0]))
	} else {
		// No argument — offer an interactive menu when on a TTY.
		if !StdinIsTerminal() {
			return fmt.Errorf("provider is required (run 'sprout keys set <provider>')")
		}
		selected, ok, err := promptProviderSelection()
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println()
			console.GlyphInfo.Printf("Run 'sprout keys set <provider>' to configure later.")
			return nil
		}
		provider = selected
	}

	// 2. Validate the provider against known auth metadata.
	if !isKnownProvider(provider, nil) {
		printValidProviders()
		return fmt.Errorf("unknown provider %q — not a built-in provider", provider)
	}
	meta, err := configuration.GetProviderAuthMetadata(provider)
	if err != nil {
		printValidProviders()
		return fmt.Errorf("unknown provider %q: %w", provider, err)
	}
	if !meta.RequiresAPIKey {
		fmt.Printf("Provider %s does not require an API key (it's local or uses no auth).\n", meta.DisplayName)
		return nil
	}

	displayName := meta.DisplayName
	if displayName == "" {
		displayName = configuration.GetProviderDisplayName(provider)
	}

	// 3. Warn if a key already exists and confirm replacement.
	// Only check the stored backend (keyring/file) — env-var credentials are
	// not "replaceable" from here, and the default HasProviderAuth returns true
	// for unknown providers, so we gate it behind the known-provider check above.
	if hasStoredKey(provider) {
		console.GlyphWarning.Printf("An API key for %s is already configured. This will replace it.\n", displayName)
		if !ConfirmPrompt("Continue?") {
			return nil // user declined — exit silently
		}
	}

	// 4. Read the key (hidden input, provider-specific prefix hints).
	key, err := configuration.PromptForAPIKey(provider)
	if err != nil {
		return fmt.Errorf("failed to read API key: %w", err)
	}

	// 5. Validate against the live API and persist (rolls back on failure).
	modelCount, err := configuration.ValidateAndSaveAPIKey(provider, key)
	if err != nil {
		return fmt.Errorf("failed to validate and save API key for %s: %w", displayName, err)
	}

	// 6. Success.
	console.GlyphSuccess.Fprintf(os.Stdout, "API key for %s validated and saved (%d models available).", displayName, modelCount)
	fmt.Println("Run 'sprout agent' to start using it, or 'sprout keys encrypt' to encrypt it at rest.")
	return nil
}

// promptProviderSelection lists providers that require an API key using an
// interactive SelectList. Returns the chosen provider name plus ok=true on
// confirm, or ("", false, nil) on Esc/Ctrl+C cancellation. Returns an error
// only on transport / IO failures.
func promptProviderSelection() (string, bool, error) {
	var keyProviders []string
	for _, name := range configuration.KnownProviderNames() {
		meta, err := configuration.GetProviderAuthMetadata(name)
		if err != nil || !meta.RequiresAPIKey {
			continue
		}
		keyProviders = append(keyProviders, name)
	}
	if len(keyProviders) == 0 {
		return "", false, fmt.Errorf("no providers require an API key")
	}

	items := make([]console.SelectItem, 0, len(keyProviders))
	for _, name := range keyProviders {
		items = append(items, console.SelectItem{
			Label: configuration.GetProviderDisplayName(name),
			Value: name,
		})
	}

	sl := console.NewSelectList(console.SelectListOptions{
		Title:      "Pick a provider to configure",
		Items:      items,
		Searchable: true,
		PageSize:   10,
	})

	ctx := context.Background()
	value, ok, err := sl.Run(ctx)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	return value, true, nil
}

// printValidProviders lists every built-in provider that requires an API key,
// shown after an "unknown provider" error so the user knows what to type.
func printValidProviders() {
	var keyProviders []string
	for _, name := range configuration.KnownProviderNames() {
		meta, err := configuration.GetProviderAuthMetadata(name)
		if err != nil || !meta.RequiresAPIKey {
			continue
		}
		keyProviders = append(keyProviders, name)
	}
	if len(keyProviders) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "Valid providers that require an API key:")
	for _, name := range keyProviders {
		fmt.Fprintf(os.Stderr, "  - %s (%s)\n", name, configuration.GetProviderDisplayName(name))
	}
}

// isKnownProvider reports whether name matches a built-in provider or a
// user-defined custom provider. This gates the "set" flow so a typo (e.g.
// "bogus") is rejected before we attempt to read or validate a key.
//
// If cfg is non-nil, the caller-supplied config is consulted for custom
// providers (avoids a second disk load). When cfg is nil, isKnownProvider
// falls back to configuration.Load() — used by callers that don't already
// have a config in hand.
//
// The name argument is normalized to lowercase + trim so callers don't have
// to remember the contract; KnownProviderNames always returns lowercase.
func isKnownProvider(name string, cfg *configuration.Config) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	for _, p := range configuration.KnownProviderNames() {
		if p == name {
			return true
		}
	}
	if cfg == nil {
		var err error
		cfg, err = configuration.Load()
		if err != nil {
			return false
		}
	}
	if cfg != nil {
		if _, exists := cfg.CustomProviders[name]; exists {
			return true
		}
	}
	return false
}

// hasStoredKey reports whether a credential for provider exists in the active
// backend (keyring or encrypted file) — NOT from an environment variable.
// We intentionally exclude env vars here because they can't be replaced from
// this command and would produce a misleading "already configured" warning.
func hasStoredKey(provider string) bool {
	value, _, err := credentials.GetFromActiveBackend(provider)
	return err == nil && strings.TrimSpace(value) != ""
}
