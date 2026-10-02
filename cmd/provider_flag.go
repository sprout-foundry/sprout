//go:build !js

package cmd

import (
	"slices"

	"github.com/spf13/cobra"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// providerFlagUsage stays free of a provider list: one hand-maintained here
// drifted from the generated catalog. Tab completion supplies the real set.
const providerFlagUsage = "Provider to use: a built-in (openai, openrouter, ollama-local, …) or a 'sprout custom' provider; Tab completes"

func providerFlagNames() []string {
	names := append(providers.AllProviderNames(), string(api.OllamaClientType), string(api.OllamaLocalClientType))
	if cfg, err := configuration.Load(); err == nil {
		for name := range cfg.CustomProviders {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func completeProviderFlag(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return providerFlagNames(), cobra.ShellCompDirectiveNoFileComp
}

func registerProviderFlagCompletion(cmds ...*cobra.Command) {
	for _, c := range cmds {
		_ = c.RegisterFlagCompletionFunc("provider", completeProviderFlag)
	}
}
