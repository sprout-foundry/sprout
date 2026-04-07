// Package credentials provides unified credential resolution for all providers.
// This package contains the single source of truth for environment variable names
// and credential resolution logic, replacing the hardcoded strings previously
// scattered across multiple files.
package credentials

import (
	"os"
	"strings"
)

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
// It uses ProviderEnvVar to determine the env var name (no hardcoded strings in callers).
// Returns true if the provider is always available (e.g., local providers) or if a
// non-empty credential is found via environment or stored credentials.
func HasProviderCredential(provider string) bool {
	p := strings.ToLower(strings.TrimSpace(provider))

	// Local providers don't require credentials
	switch p {
	case "ollama", "ollama-local", "lmstudio", "test":
		return true
	}

	envVar := ProviderEnvVar(provider)
	if envVar != "" {
		if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
			return true
		}
	}

	resolved, err := Resolve(provider, envVar)
	if err != nil {
		return false
	}
	if strings.TrimSpace(resolved.Value) != "" {
		return true
	}

	// For unknown/custom providers with no standard env var, allow them through.
	// Custom providers may use their own auth mechanisms (e.g., bearer token in
	// the provider config) or may not require credentials at all.
	if envVar == "" && strings.TrimSpace(provider) != "" {
		return true
	}

	return false
}
