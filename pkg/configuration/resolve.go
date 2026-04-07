// Package configuration provides high-level configuration and credential resolution
// for providers, including support for custom providers and stored credentials.
package configuration

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/credentials"
)

func init() {
	// Register the unified provider info callback so credentials.ResolveProvider
	// and credentials.HasProviderCredential can look up custom provider env vars
	// and auth requirements through GetProviderAuthMetadata.
	credentials.SetProviderInfoFunc(func(provider string) credentials.ProviderInfo {
		metadata, err := GetProviderAuthMetadata(provider)
		if err != nil {
			return credentials.ProviderInfo{}
		}
		return credentials.ProviderInfo{
			EnvVar:         metadata.EnvVar,
			RequiresAPIKey: metadata.RequiresAPIKey,
		}
	})
}

// ResolveProviderAuth resolves a credential for a provider using the unified resolution chain:
//   1. Environment variable (from ProviderInfoFunc or built-in metadata)
//   2. Keyring backend (if active)
//   3. Encrypted file store
//
// Returns ResolvedProviderCredential with the resolved value and source.
// If the provider does not require an API key, returns with Value="" and Source="none".
func ResolveProviderAuth(provider string) (ResolvedProviderCredential, error) {
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

	// Delegate to the unified credential resolution path
	resolved, err := credentials.ResolveProvider(provider)
	if err != nil {
		return ResolvedProviderCredential{}, fmt.Errorf("resolve credential for %q: %w", provider, err)
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
func HasProviderAuth(provider string) bool {
	return credentials.HasProviderCredential(provider)
}
