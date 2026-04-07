// Package configuration provides high-level configuration and credential resolution
// for providers, including support for custom providers and stored credentials.
package configuration

import (
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

// ResolveProviderAuth resolves a credential for a provider.
//
// Deprecated: Use credentials.ResolveProvider(provider) directly.
// This function is kept for backward compatibility.
func ResolveProviderAuth(provider string) (ResolvedProviderCredential, error) {
	return credentials.ResolveProvider(provider)
}

// HasProviderAuth checks whether a provider has a configured credential.
// For providers that don't require an API key (local providers), always returns true.
func HasProviderAuth(provider string) bool {
	return credentials.HasProviderCredential(provider)
}
