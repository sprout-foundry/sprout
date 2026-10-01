//go:build !js

package cmd

import "github.com/sprout-foundry/sprout/pkg/configuration"

// providerModelSpec combines --provider and --model into the "provider:model"
// (or bare model) form the agent constructors accept. With only --provider,
// the provider's configured default model fills in: the constructors read a
// bare string as a model name, so "--provider openai" alone would otherwise
// ask the last-used provider for a model called "openai".
func providerModelSpec(provider, model string) string {
	switch {
	case provider != "" && model != "":
		return provider + ":" + model
	case provider != "":
		if resolved := defaultModelForProvider(provider); resolved != "" {
			return provider + ":" + resolved
		}
		return provider
	default:
		return model
	}
}

func defaultModelForProvider(provider string) string {
	mgr, err := configuration.NewManagerSilent()
	if err != nil {
		return ""
	}
	_, model, err := mgr.ResolveProviderModel(provider, "")
	if err != nil {
		return ""
	}
	return model
}
