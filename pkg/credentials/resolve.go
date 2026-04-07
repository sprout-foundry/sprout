// Package credentials provides unified credential resolution for all providers.
// This package contains the single source of truth for environment variable names
// and credential resolution logic, replacing the hardcoded strings previously
// scattered across multiple files.
package credentials

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// ProviderInfo holds metadata about a provider needed for credential resolution.
// Higher-level packages (e.g., configuration) register a ProviderInfoFunc
// callback to supply provider-specific env var names and auth requirements.
type ProviderInfo struct {
	EnvVar         string // Environment variable name for the provider's API key
	RequiresAPIKey bool   // Whether the provider requires an API key
}

// ProviderInfoFunc looks up provider metadata by provider name.
// Returns zero-value ProviderInfo if the provider is unknown.
type ProviderInfoFunc func(provider string) ProviderInfo

var (
	providerInfoMu   sync.RWMutex
	providerInfoFunc ProviderInfoFunc
)

// SetProviderInfoFunc registers a callback for looking up provider metadata.
// This is called by higher-level packages to provide env var names
// and auth requirements for custom/built-in providers.
//
// The callback is invoked lazily at resolution time (not at registration time),
// so it can rely on runtime state (e.g., loaded config files).
//
// IMPORTANT: This must be called before any credential resolution function
// (ResolveProvider, HasProviderCredential, etc.) is invoked. Typically this
// happens automatically via configuration.init() when the configuration package
// is imported. In test code that doesn't import configuration, no callback is
// registered and the built-in fallback metadata is used.
//
// Use ResetProviderInfoFunc() in tests to clear the callback.
func SetProviderInfoFunc(fn ProviderInfoFunc) {
	providerInfoMu.Lock()
	defer providerInfoMu.Unlock()
	providerInfoFunc = fn
}

// ResetProviderInfoFunc clears the registered provider info callback.
// This is intended for use in tests to restore the default (callback-less) behavior.
func ResetProviderInfoFunc() {
	providerInfoMu.Lock()
	defer providerInfoMu.Unlock()
	providerInfoFunc = nil
}

// getProviderInfo returns the ProviderInfo for a provider.
// If a ProviderInfoFunc is registered, it is called first.
// If the callback returns zero values or no callback is registered,
// falls back to built-in provider metadata.
func getProviderInfo(provider string) ProviderInfo {
	providerInfoMu.RLock()
	fn := providerInfoFunc
	providerInfoMu.RUnlock()

	if fn != nil {
		info := fn(provider)
		if info.EnvVar != "" || info.RequiresAPIKey {
			return info
		}
	}

	return ProviderInfo{
		EnvVar:         ProviderEnvVar(provider),
		RequiresAPIKey: providerRequiresAPIKey(provider),
	}
}

// providerRequiresAPIKey returns whether a built-in provider requires an API key.
func providerRequiresAPIKey(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "chutes":
		return true
	case "ollama", "ollama-local", "lmstudio", "test":
		return false
	default:
		return true
	}
}

// ResolveProvider resolves a credential for a provider using the unified resolution chain.
// This is the single authoritative function for credential resolution.
//
// IMPORTANT: Requires SetProviderInfoFunc to have been called (typically by
// configuration.init()) to properly resolve custom provider env vars. If no
// callback is registered, falls back to built-in provider metadata.
//
// Resolution precedence:
//  1. Environment variable (looked up via ProviderInfoFunc or built-in metadata)
//  2. Keyring backend (if active)
//  3. Encrypted file store
//
// Returns credentials.Resolved with the value and source.
func ResolveProvider(provider string) (Resolved, error) {
	info := getProviderInfo(provider)
	return Resolve(provider, info.EnvVar)
}

// ResolveProviderAPIKey resolves a provider's API key and validates it's non-empty.
// This is a convenience wrapper around ResolveProvider that returns just the key value
// with a clear error message when no credential is available.
//
// This eliminates the duplicated "resolve → check empty → format error" pattern
// previously scattered across multiple files.
func ResolveProviderAPIKey(provider, displayName string) (string, error) {
	resolved, err := ResolveProvider(provider)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s API key: %w", displayName, err)
	}
	apiKey := strings.TrimSpace(resolved.Value)
	if apiKey == "" {
		if resolved.EnvVar != "" {
			return "", fmt.Errorf("failed to resolve %s API key: %s not set and no stored API key configured", displayName, resolved.EnvVar)
		}
		return "", fmt.Errorf("failed to resolve %s API key: no stored API key configured for %s", displayName, provider)
	}
	return apiKey, nil
}

// ProviderEnvVar returns the standard environment variable name for a provider's API key.
// This provides a single source of truth for env var name mapping, replacing
// the hardcoded strings previously scattered across multiple files.
func ProviderEnvVar(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "openai":
		return "OPENAI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "deepinfra":
		return "DEEPINFRA_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "zai", "z.ai":
		return "ZAI_API_KEY"
	case "ollama", "ollama-local", "ollama-turbo":
		return "OLLAMA_API_KEY"
	case "minimax":
		return "MINIMAX_API_KEY"
	case "chutes":
		return "CHUTES_API_KEY"
	case "mistral":
		return "MISTRAL_API_KEY"
	case "lmstudio", "test":
		// Local providers don't require API keys
		return ""
	default:
		// For custom providers, caller should provide the env var
		return ""
	}
}

// HasProviderCredential checks if a provider has a configured API key.
// Uses ProviderInfoFunc callback (if registered) for env var and auth requirement lookup,
// falling back to built-in provider metadata.
// Returns true if the provider is always available (e.g., local providers) or if a
// non-empty credential is found via environment or stored credentials.
func HasProviderCredential(provider string) bool {
	info := getProviderInfo(provider)

	if !info.RequiresAPIKey {
		return true
	}

	// Early exit to avoid hitting disk/keyring for the common case
	if info.EnvVar != "" {
		if value := strings.TrimSpace(os.Getenv(info.EnvVar)); value != "" {
			return true
		}
	}

	// Check stored credentials (keyring/file)
	resolved, err := Resolve(provider, info.EnvVar)
	if err != nil {
		return false
	}
	if strings.TrimSpace(resolved.Value) != "" {
		return true
	}

	// For custom providers with no standard env var, allow them through.
	// Custom providers may use their own auth mechanisms.
	if info.EnvVar == "" && strings.TrimSpace(provider) != "" {
		return true
	}

	return false
}
