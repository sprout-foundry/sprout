package configuration

import (
	"errors"
	"strings"

	providers "github.com/alantheprice/ledit/pkg/agent_providers"
	"github.com/alantheprice/ledit/pkg/credentials"
)

type ProviderAuthMetadata struct {
	Provider       string
	DisplayName    string
	RequiresAPIKey bool
	EnvVar         string
	AuthType       string
}

type ResolvedProviderCredential struct {
	Provider string
	EnvVar   string
	Value    string
	Source   string
}

func GetProviderAuthMetadata(provider string) (ProviderAuthMetadata, error) {
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" {
		return ProviderAuthMetadata{}, errors.New("provider is required")
	}

	switch name {
	case "ollama", "ollama-local", "lmstudio", "test":
		return ProviderAuthMetadata{
			Provider:       name,
			DisplayName:    getProviderDisplayName(name),
			RequiresAPIKey: false,
			AuthType:       "none",
		}, nil
	}

	if cfg, err := Load(); err == nil {
		if custom, exists := cfg.CustomProviders[name]; exists {
			return ProviderAuthMetadata{
				Provider:       name,
				DisplayName:    custom.Name,
				RequiresAPIKey: custom.RequiresAPIKey || strings.TrimSpace(custom.EnvVar) != "" || strings.TrimSpace(custom.APIKey) != "",
				EnvVar:         strings.TrimSpace(custom.EnvVar),
				AuthType:       "bearer",
			}, nil
		}
	}

	providerFactory := providers.NewProviderFactory()
	if err := providerFactory.LoadEmbeddedConfigs(); err == nil {
		if providerConfig, err := providerFactory.GetProviderConfig(name); err == nil {
			requires := providerConfig.Auth.Type != "" && providerConfig.Auth.Type != "none"
			return ProviderAuthMetadata{
				Provider:       name,
				DisplayName:    getProviderDisplayName(name),
				RequiresAPIKey: requires,
				EnvVar:         strings.TrimSpace(providerConfig.Auth.EnvVar),
				AuthType:       providerConfig.Auth.Type,
			}, nil
		}
	}

	for _, supported := range getSupportedProviders() {
		if supported.Name != name {
			continue
		}
		return ProviderAuthMetadata{
			Provider:       name,
			DisplayName:    supported.FormattedName,
			RequiresAPIKey: supported.RequiresKey,
			EnvVar:         strings.TrimSpace(supported.EnvVariableName),
			AuthType:       "bearer",
		}, nil
	}

	return ProviderAuthMetadata{
		Provider:       name,
		DisplayName:    name,
		RequiresAPIKey: true,
		AuthType:       "bearer",
	}, nil
}

func GetProviderEnvVarName(provider string) string {
	metadata, err := GetProviderAuthMetadata(provider)
	if err != nil {
		return ""
	}
	return metadata.EnvVar
}

func ResolveProviderCredential(provider string, apiKeys *APIKeys) (ResolvedProviderCredential, error) {
	metadata, err := GetProviderAuthMetadata(provider)
	if err != nil {
		return ResolvedProviderCredential{}, err
	}
	if !metadata.RequiresAPIKey {
		return ResolvedProviderCredential{
			Provider: metadata.Provider,
			EnvVar:   metadata.EnvVar,
		}, nil
	}

	if apiKeys != nil {
		if value := strings.TrimSpace(apiKeys.GetAPIKey(metadata.Provider)); value != "" {
			if envValue := strings.TrimSpace(metadata.EnvVar); envValue != "" {
				if envResolved, err := credentials.Resolve(metadata.Provider, envValue); err == nil && envResolved.Source == "environment" {
					return ResolvedProviderCredential{
						Provider: metadata.Provider,
						EnvVar:   envResolved.EnvVar,
						Value:    envResolved.Value,
						Source:   envResolved.Source,
					}, nil
				}
			}
			return ResolvedProviderCredential{
				Provider: metadata.Provider,
				EnvVar:   metadata.EnvVar,
				Value:    value,
				Source:   "stored",
			}, nil
		}
	}

	resolved, err := credentials.Resolve(metadata.Provider, metadata.EnvVar)
	if err != nil {
		return ResolvedProviderCredential{}, err
	}
	return ResolvedProviderCredential{
		Provider: metadata.Provider,
		EnvVar:   resolved.EnvVar,
		Value:    resolved.Value,
		Source:   resolved.Source,
	}, nil
}
