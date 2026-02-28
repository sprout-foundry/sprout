package configuration

import (
	"fmt"
	"os"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	providers "github.com/alantheprice/ledit/pkg/agent_providers"
)

// MapProviderStringToClientType converts a provider string to ClientType, including
// built-in providers, custom providers from config, and factory-backed dynamic providers.
func MapProviderStringToClientType(cfg *Config, raw string) (api.ClientType, error) {
	name := strings.TrimSpace(strings.ToLower(raw))
	switch name {
	case "openai":
		return api.OpenAIClientType, nil
	case "chutes":
		return api.ChutesClientType, nil
	case "zai":
		return api.ZAIClientType, nil
	case "openrouter":
		return api.OpenRouterClientType, nil
	case "deepinfra":
		return api.DeepInfraClientType, nil
	case "deepseek":
		return api.DeepSeekClientType, nil
	case "ollama":
		return api.OllamaClientType, nil
	case "ollama-local":
		return api.OllamaLocalClientType, nil
	case "ollama-turbo":
		return api.OllamaTurboClientType, nil
	case "lmstudio":
		return api.LMStudioClientType, nil
	case "mistral":
		return api.MistralClientType, nil
	case "minimax":
		return api.MinimaxClientType, nil
	case "test":
		return api.TestClientType, nil
	}

	if cfg != nil && cfg.CustomProviders != nil {
		if _, exists := cfg.CustomProviders[name]; exists {
			return api.ClientType(name), nil
		}
	}

	providerFactory := providers.NewProviderFactory()
	if err := providerFactory.LoadEmbeddedConfigs(); err == nil {
		if _, err := providerFactory.GetProviderConfig(name); err == nil {
			return api.ClientType(name), nil
		}
	}

	return "", fmt.Errorf("unsupported provider: %s", raw)
}

// ResolveProviderModel resolves provider and model using one canonical precedence path:
// 1) Explicit provider flag/arg
// 2) Explicit model in provider:model format (only when prefix is a valid provider)
// 3) LEDIT_PROVIDER env
// 4) LEDIT_MODEL env (provider:model format only when prefix is a valid provider)
// 5) Config last_used_provider
// 6) Auto-detected provider via DetermineProvider
//
// Model precedence:
// 1) Explicit model (trimmed to model segment when provider:model format is recognized)
// 2) LEDIT_MODEL env (same parsing rule)
// 3) Config provider model default
func ResolveProviderModel(cfg *Config, explicitProvider, explicitModel string) (api.ClientType, string, error) {
	providerName := strings.TrimSpace(explicitProvider)
	modelCandidate := strings.TrimSpace(explicitModel)

	if providerName == "" && modelCandidate != "" {
		if parsedProvider, parsedModel, ok := parseProviderModelSpecifier(cfg, modelCandidate); ok {
			providerName = parsedProvider
			modelCandidate = parsedModel
		}
	}

	if providerName == "" {
		providerName = strings.TrimSpace(os.Getenv("LEDIT_PROVIDER"))
	}
	if modelCandidate == "" {
		modelCandidate = strings.TrimSpace(os.Getenv("LEDIT_MODEL"))
	}

	if providerName == "" && modelCandidate != "" {
		if parsedProvider, parsedModel, ok := parseProviderModelSpecifier(cfg, modelCandidate); ok {
			providerName = parsedProvider
			modelCandidate = parsedModel
		}
	}

	if providerName == "" && cfg != nil {
		providerName = strings.TrimSpace(cfg.LastUsedProvider)
	}

	var clientType api.ClientType
	var err error
	if providerName != "" {
		clientType, err = MapProviderStringToClientType(cfg, providerName)
		if err != nil {
			return "", "", err
		}
	} else {
		// Pass providerName (from env or config) as lastUsedProvider to enable
		// proper fallback in DetermineProvider before auto-detection
		clientType, err = api.DetermineProvider("", api.ClientType(providerName))
		if err != nil {
			return "", "", fmt.Errorf("failed to determine provider: %w", err)
		}
	}

	if modelCandidate == "" && cfg != nil {
		modelCandidate = strings.TrimSpace(cfg.GetModelForProvider(string(clientType)))
	}

	return clientType, modelCandidate, nil
}

func parseProviderModelSpecifier(cfg *Config, raw string) (string, string, bool) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	prefix := strings.TrimSpace(parts[0])
	model := strings.TrimSpace(parts[1])
	if prefix == "" || model == "" {
		return "", "", false
	}
	if _, err := MapProviderStringToClientType(cfg, prefix); err != nil {
		return "", "", false
	}
	return prefix, model, true
}
