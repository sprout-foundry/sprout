package configuration

import (
	"fmt"
	"strings"
	"sync"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
)

type ProviderAuthMetadata struct {
	Provider       string
	DisplayName    string
	RequiresAPIKey bool
	EnvVar         string
	AuthType       string
}

// ProviderConfigLookupFunc returns the env-var and auth-type for a runtime
// provider config — typically backed by the global provider factory, which
// merges embedded, filesystem, and remote (GitHub Pages) sources.
// Returns ok=false when the provider is not present in the runtime view.
type ProviderConfigLookupFunc func(name string) (envVar, authType string, ok bool)

var (
	providerConfigLookupMu sync.RWMutex
	providerConfigLookup   ProviderConfigLookupFunc
)

// SetProviderConfigLookup registers a runtime config lookup. pkg/factory
// wires this to GlobalFactory().GetProviderConfig in its init() so that
// GetProviderAuthMetadata sees providers added by refreshFromRemote — not
// just embedded ones. The pattern mirrors credentials.SetProviderInfoFunc
// and exists to avoid a configuration → factory import cycle.
func SetProviderConfigLookup(fn ProviderConfigLookupFunc) {
	providerConfigLookupMu.Lock()
	defer providerConfigLookupMu.Unlock()
	providerConfigLookup = fn
}

// ProviderNamesLookupFunc returns the full set of provider names known
// to the runtime — typically the global factory's view, which includes
// embedded, filesystem, and remote (GitHub Pages) registrations.
type ProviderNamesLookupFunc func() []string

var (
	providerNamesLookupMu sync.RWMutex
	providerNamesLookup   ProviderNamesLookupFunc
)

// SetProviderNamesLookup registers a runtime provider-names lookup.
// pkg/factory wires this to GlobalFactory().GetAvailableProviders in
// its init() so onboarding / credential-enumeration loops surface
// providers added by refreshFromRemote.
func SetProviderNamesLookup(fn ProviderNamesLookupFunc) {
	providerNamesLookupMu.Lock()
	defer providerNamesLookupMu.Unlock()
	providerNamesLookup = fn
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

	// Ask the runtime factory first — it sees embedded + filesystem +
	// remote (refreshFromRemote upserts via GitHub Pages) providers, so a
	// provider published only to the remote registry still gets its
	// declared env_var and auth.type picked up here.
	providerConfigLookupMu.RLock()
	lookup := providerConfigLookup
	providerConfigLookupMu.RUnlock()
	if lookup != nil {
		if envVar, authType, ok := lookup(name); ok {
			requires := authType != "" && authType != "none"
			return ProviderAuthMetadata{
				Provider:       name,
				DisplayName:    getProviderDisplayName(name),
				RequiresAPIKey: requires,
				EnvVar:         strings.TrimSpace(envVar),
				AuthType:       authType,
			}, nil
		}
	}

	// Fallback for callers that import configuration without factory
	// (e.g., narrow unit tests): consult the embedded configs directly.
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
