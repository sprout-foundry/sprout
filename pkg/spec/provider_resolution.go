package spec

import (
	"fmt"
	"os"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/utils"
)

func resolveSpecAgentClient(cfg *configuration.Config, logger *utils.Logger, purpose string) (api.ClientInterface, error) {
	intendedProvider := strings.TrimSpace(os.Getenv("LEDIT_PROVIDER"))
	providerSource := "environment"
	if intendedProvider == "" && cfg != nil {
		intendedProvider = strings.TrimSpace(cfg.LastUsedProvider)
		providerSource = "config"
	}

	clientType, err := api.DetermineProvider("", api.ClientType(intendedProvider))
	if err != nil {
		return nil, fmt.Errorf("failed to determine provider: %w", err)
	}

	resolvedProvider := string(clientType)
	model := strings.TrimSpace(os.Getenv("LEDIT_MODEL"))
	if model == "" && cfg != nil {
		model = strings.TrimSpace(cfg.GetModelForProvider(resolvedProvider))
	}

	agentClient, err := factory.CreateProviderClient(clientType, model)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent client: %w", err)
	}

	resolvedModel := strings.TrimSpace(agentClient.GetModel())
	if resolvedModel == "" {
		resolvedModel = "<provider default>"
	}

	switch {
	case intendedProvider == "":
		logger.LogProcessStep(fmt.Sprintf("⚠️ %s auto-selected provider/model: %s | %s", purpose, resolvedProvider, resolvedModel))
	case !strings.EqualFold(intendedProvider, resolvedProvider):
		logger.LogProcessStep(fmt.Sprintf("⚠️ %s provider fallback: requested=%s (%s), using=%s | %s", purpose, intendedProvider, providerSource, resolvedProvider, resolvedModel))
	}

	return agentClient, nil
}
