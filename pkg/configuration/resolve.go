// Package configuration provides high-level configuration and credential resolution
// for providers, including support for custom providers and stored credentials.
package configuration

import (
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/credentials"
)

// ResolveProviderAuth resolves a credential for a provider using a single precedence chain:
//   1. Environment variable (from ProviderAuthMetadata.EnvVar)
//   2. API keys map (if apiKeys is non-nil)
//   3. credentials.Resolve (keyring → encrypted file store)
//
// This is the authoritative resolution function for high-level credential resolution
// that needs to know about custom providers and provider metadata.
//
// Returns ResolvedProviderCredential with the resolved value and source.
// If the provider does not require an API key, returns with Value="" and Source="none".
func ResolveProviderAuth(provider string, apiKeys *APIKeys) (ResolvedProviderCredential, error) {
	metadata, err := GetProviderAuthMetadata(provider)
	if err != nil {
		return ResolvedProviderCredential{}, fmt.Errorf("get auth metadata for %q: %w", provider, err)
	}
	if !metadata.RequiresAPIKey {
		return ResolvedProviderCredential{
			Provider: metadata.Provider,
			Source:   "none",
		}, nil
	}

	// 1. Check environment variable (from metadata, not hardcoded)
	if metadata.EnvVar != "" {
		if value := strings.TrimSpace(os.Getenv(metadata.EnvVar)); value != "" {
			return ResolvedProviderCredential{
				Provider: metadata.Provider,
				EnvVar:   metadata.EnvVar,
				Value:    value,
				Source:   "environment",
			}, nil
		}
	}

	// 2. Check explicit API keys map (if provided)
	if apiKeys != nil {
		if value := strings.TrimSpace(apiKeys.GetAPIKey(metadata.Provider)); value != "" {
			return ResolvedProviderCredential{
				Provider: metadata.Provider,
				EnvVar:   metadata.EnvVar,
				Value:    value,
				Source:   "stored",
			}, nil
		}
	}

	// 3. Check keyring / encrypted file store via credentials.Resolve
	resolved, err := credentials.Resolve(metadata.Provider, metadata.EnvVar)
	if err != nil {
		return ResolvedProviderCredential{}, fmt.Errorf("resolve credential for %q: %w", metadata.Provider, err)
	}
	return ResolvedProviderCredential{
		Provider: metadata.Provider,
		EnvVar:   resolved.EnvVar,
		Value:    resolved.Value,
		Source:   resolved.Source,
	}, nil
}

// HasProviderAuth checks whether a provider has a configured credential.
// For providers that don't require an API key (local providers), always returns true.
func HasProviderAuth(provider string, apiKeys *APIKeys) bool {
	metadata, err := GetProviderAuthMetadata(provider)
	if err != nil {
		return false
	}
	if !metadata.RequiresAPIKey {
		return true
	}
	resolved, err := ResolveProviderAuth(provider, apiKeys)
	if err != nil {
		return false
	}
	return strings.TrimSpace(resolved.Value) != ""
}
