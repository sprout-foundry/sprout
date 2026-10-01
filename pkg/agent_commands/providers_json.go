package commands

// providers_json.go — the /provider JSON list output, split out of
// providers.go. ExecuteWithJSONOutput (and its providerInfoJSON /
// providersJSONPayload payload types) emit the provider list as JSON for
// machine consumption, mirroring the interactive /provider list path.
import (
	"github.com/sprout-foundry/sprout/pkg/agent"
)

// providerInfoJSON is a single provider entry in the JSON output.
type providerInfoJSON struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Model  string `json:"model"`
	Ready  bool   `json:"ready"`
	Active bool   `json:"active"`
}

// providersJSONPayload wraps the provider list with current selection context.
type providersJSONPayload struct {
	CurrentProvider string             `json:"current_provider"`
	CurrentModel    string             `json:"current_model"`
	Providers       []providerInfoJSON `json:"providers"`
}

// ExecuteWithJSONOutput emits the provider list as a JSON array. This
// mirrors the /provider list path (the status, select, and setProvider
// subcommands are interactive/stateful and fall through to text Execute).
func (p *ProvidersCommand) ExecuteWithJSONOutput(args []string, chatAgent *agent.Agent, ctx *CommandContext) error {
	if chatAgent == nil {
		return WriteJSONToOutput(providersJSONPayload{})
	}

	configManager := chatAgent.GetConfigManager()
	if configManager == nil {
		return WriteJSONToOutput(providersJSONPayload{})
	}

	currentProvider := chatAgent.GetProviderType()
	available := configManager.GetAvailableProviders()

	providers := make([]providerInfoJSON, 0, len(available))
	for _, provider := range available {
		providers = append(providers, providerInfoJSON{
			ID:     string(provider),
			Name:   getProviderDisplayName(provider),
			Model:  configManager.GetModelForProvider(provider),
			Ready:  p.isProviderReady(configManager, provider),
			Active: provider == currentProvider,
		})
	}

	return WriteJSONToOutput(providersJSONPayload{
		CurrentProvider: getProviderDisplayName(currentProvider),
		CurrentModel:    chatAgent.GetModel(),
		Providers:       providers,
	})
}
