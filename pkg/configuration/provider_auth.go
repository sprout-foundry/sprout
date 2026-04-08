package configuration

import (
	"fmt"
	"strings"

	providers "github.com/alantheprice/ledit/pkg/agent_providers"
)

type ProviderAuthMetadata struct {
	Provider       string
	DisplayName    string
	RequiresAPIKey bool
	EnvVar         string
	AuthType       string
}

func GetProviderAuthMetadata(provider string) (ProviderAuthMetadata, error) {
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" {
		return ProviderAuthMetadata{}, fmt.Errorf("provider is required")
	}

	switch name {
	case "ollama", "ollama-local", "lmstudio", "test", "editor":
		return ProviderAuthMetadata{
			Provider:       name,
			DisplayName:    getProviderDisplayName(name),
			RequiresAPIKey: false,
			AuthType:       "none",
		}, nil
	case "jinaai":
		// jinaai is used only for web content search/embeddings (not as an LLM provider).
		// It has no provider config JSON file, so it's handled explicitly here.
		return ProviderAuthMetadata{
			Provider:       name,
			DisplayName:    getProviderDisplayName(name),
			RequiresAPIKey: true,
			EnvVar:         "JINA_API_KEY",
			AuthType:       "bearer",
		}, nil
	}

	if cfg, err := Load(); err == nil {
		if custom, exists := cfg.CustomProviders[name]; exists {
			return ProviderAuthMetadata{
				Provider:       name,
				DisplayName:    custom.Name,
				RequiresAPIKey: custom.RequiresAPIKey || strings.TrimSpace(custom.EnvVar) != "",
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
