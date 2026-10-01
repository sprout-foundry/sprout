package agent

// models_default.go — default-model selection, split out of models.go.
// selectDefaultModel picks a model from a provider's available list (probe
// recommendations first, then configured patterns, then LM Studio embedding
// filtering); getDefaultModelPatterns reads the embedded provider config;
// matchPattern does '*'-separated substring matching; selectProbeRecommended
// surfaces probe-backed primary/subagent recommendations.
import (
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// selectDefaultModel chooses an appropriate default model from available models. Prefers probe-recommended candidates first.
func (a *Agent) selectDefaultModel(models []api.ModelInfo, provider api.ClientType) string {
	if len(models) == 0 {
		return ""
	}

	if probe := selectProbeRecommended(models); probe != "" {
		return probe
	}

	for _, pattern := range a.getDefaultModelPatterns(provider) {
		for _, model := range models {
			if matchPattern(model.ID, pattern) {
				return model.ID
			}
		}
	}

	// LM Studio may expose embedding models alongside chat models. An empty
	// configured pattern deliberately reaches this filter rather than matching all.
	if provider == api.LMStudioClientType {
		for _, model := range models {
			id := strings.ToLower(model.ID)
			if !strings.Contains(id, "embedding") && !strings.Contains(id, "embed") {
				return model.ID
			}
		}
	}

	return models[0].ID
}

// getDefaultModelPatterns returns auto-selection preferences from the embedded provider config.
func (a *Agent) getDefaultModelPatterns(provider api.ClientType) []string {
	providerFactory := providers.NewProviderFactory()
	if err := providerFactory.LoadEmbeddedConfigs(); err != nil {
		return nil
	}
	config, err := providerFactory.GetProviderConfig(string(provider))
	if err != nil {
		return nil
	}
	return config.Models.DefaultModelPatterns
}

// matchPattern performs a case-insensitive substring match for every non-empty component separated by '*'.
func matchPattern(modelID, pattern string) bool {
	if pattern == "" {
		return false
	}
	id := strings.ToLower(modelID)
	for _, part := range strings.Split(strings.ToLower(pattern), "*") {
		if part != "" && !strings.Contains(id, part) {
			return false
		}
	}
	return true
}

// selectProbeRecommended scans for probe-backed recommendations: primary first, then subagent. Returns "" if none.
func selectProbeRecommended(models []api.ModelInfo) string {
	var firstSubagent string
	for _, m := range models {
		if modelcontract.RoleHas(m.RecommendedRoles, modelcontract.RolePrimary) {
			return m.ID
		}
		if firstSubagent == "" && modelcontract.RoleHas(m.RecommendedRoles, modelcontract.RoleSubagent) {
			firstSubagent = m.ID
		}
	}
	return firstSubagent
}
