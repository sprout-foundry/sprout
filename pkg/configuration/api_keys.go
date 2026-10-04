package configuration

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/term"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// staticProviderNames is the compile-time canonical list of built-in
// provider names. CLI/UI ordering anchors on this — built-ins keep
// their generated order; runtime-only additions (e.g. providers
// published to GitHub Pages but not yet shipped in the binary) are
// appended in sorted order by KnownProviderNames().
var staticProviderNames = providers.KnownProviders()

// isCustomProvider reports whether a provider name was added by the user via
// the custom-providers settings (not a built-in or factory-registered one).
// A custom provider that shadows a built-in name is treated as built-in so
// it still gets full ListModels validation.
func isCustomProvider(provider string) bool {
	for _, b := range KnownProviderNames() {
		if b == provider {
			return false
		}
	}
	if cfg, err := Load(); err == nil {
		if _, ok := cfg.CustomProviders[provider]; ok {
			return true
		}
	}
	if customs, err := LoadCustomProviders(); err == nil {
		if _, ok := customs[provider]; ok {
			return true
		}
	}
	return false
}

// knownProviderDisplayNames maps provider names to their display names.
// This is the single source of truth for provider display names in CLI/UI.
// Generated from provider configs - use providers.ProviderDisplayNames() for the full map.
var knownProviderDisplayNames = providers.ProviderDisplayNames()

// KnownProviderNames returns the union of the compile-time provider
// list and whatever the runtime factory has registered (which includes
// embedded + filesystem + remote configs once pkg/factory.init has
// wired SetProviderNamesLookup). Static entries keep their generated
// order; runtime-only additions are appended in sorted order so the
// result is deterministic across calls.
func KnownProviderNames() []string {
	static := staticProviderNames
	providerNamesLookupMu.RLock()
	lookup := providerNamesLookup
	providerNamesLookupMu.RUnlock()
	if lookup == nil {
		return append([]string(nil), static...)
	}

	seen := make(map[string]struct{}, len(static))
	for _, n := range static {
		seen[n] = struct{}{}
	}

	var extras []string
	for _, n := range lookup() {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		extras = append(extras, n)
	}

	if len(extras) == 0 {
		return append([]string(nil), static...)
	}
	sort.Strings(extras)
	out := make([]string, 0, len(static)+len(extras))
	out = append(out, static...)
	out = append(out, extras...)
	return out
}

// GetAPIKey returns the API key for a provider
func (keys *APIKeys) GetAPIKey(provider string) string {
	if keys == nil {
		return ""
	}
	return (*keys)[provider]
}

// SetAPIKey sets the API key for a provider
func (keys *APIKeys) SetAPIKey(provider, key string) {
	if keys == nil || *keys == nil {
		*keys = make(APIKeys)
	}
	(*keys)[provider] = key
}

// HasAPIKey checks if a provider has an API key set.
// Checks the in-memory map first, then falls back to the active backend
// (keyring or file store) for credentials not in the map.
func (keys *APIKeys) HasAPIKey(provider string) bool {
	// First check stored keys
	if keys.GetAPIKey(provider) != "" {
		return true
	}
	// Check active backend (keyring or file store) as fallback
	value, _, err := credentials.GetFromActiveBackend(provider)
	if err == nil && value != "" {
		return true
	}
	return false
}

// PromptForAPIKey prompts the user for an API key with helpful guidance
func PromptForAPIKey(provider string) (string, error) {
	providerName := GetProviderDisplayName(provider)

	// Provide specific guidance for getting API keys
	fmt.Printf("[key] Enter your %s API key\n", providerName)
	fmt.Printf("   (The key will be hidden as you type for security)\n")
	fmt.Printf("   API key: ")

	// Read API key securely (hidden input)
	byteKey, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		// Fall back to regular input if term doesn't work
		fmt.Println() // New line after the prompt
		fmt.Printf("   (Hidden input not available, key will be visible)\n")
		fmt.Printf("   API key: ")
		reader := bufio.NewReader(os.Stdin)
		key, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read API key: %w", err)
		}
		byteKey = []byte(strings.TrimSpace(key))
	} else {
		fmt.Println() // New line after hidden input
	}

	apiKey := strings.TrimSpace(string(byteKey))
	if apiKey == "" {
		return "", fmt.Errorf("no API key provided")
	}

	// Basic validation
	if len(apiKey) < 10 {
		return "", fmt.Errorf("%w (expected at least 10 characters, got %d)", ErrAPIKeyTooShort, len(apiKey))
	}

	// Provider-specific validation patterns
	switch provider {
	case "openai":
		if !strings.HasPrefix(apiKey, "sk-") {
			console.GlyphWarning.Fprintln(os.Stdout, "Warning: OpenAI API keys typically start with 'sk-'")
		}
	case "openrouter":
		if !strings.HasPrefix(apiKey, "sk-or-") {
			console.GlyphWarning.Fprintln(os.Stdout, "Warning: OpenRouter API keys typically start with 'sk-or-'")
		}
	}

	return apiKey, nil
}

// ErrAPIKeyTooShort reports a pasted key too short to be real, usually a
// partial paste; callers can ask again.
var ErrAPIKeyTooShort = errors.New("API key seems too short")

// GetProviderDisplayName returns a user-friendly name for the provider.
// Lookup chain:
//  1. Static display-name map (generated from embedded configs — fastest
//     and the common case for built-ins).
//  2. Runtime factory (covers remote-only providers published to GitHub
//     Pages whose display_name isn't baked into the static map).
//  3. CustomProviders (user-defined local providers in config.json).
//  4. Raw provider ID as a last resort.
func GetProviderDisplayName(provider string) string {
	if displayName, ok := knownProviderDisplayNames[provider]; ok {
		return displayName
	}

	providerDisplayLookupMu.RLock()
	lookup := providerDisplayLookup
	providerDisplayLookupMu.RUnlock()
	if lookup != nil {
		if displayName, ok := lookup(provider); ok && displayName != "" {
			return displayName
		}
	}

	if cfg, err := Load(); err == nil {
		if custom, exists := cfg.CustomProviders[provider]; exists {
			if custom.Name != "" {
				return custom.Name
			}
		}
	}

	return provider
}

// RequiresAPIKey checks if a provider requires an API key.
func RequiresAPIKey(provider string) bool {
	metadata, err := GetProviderAuthMetadata(provider)
	if err != nil {
		return true // default to requiring key for unknown providers
	}
	return metadata.RequiresAPIKey
}
