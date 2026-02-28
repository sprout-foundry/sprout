package spec

import (
	"fmt"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/utils"
)

func resolveSpecAgentClient(cfg *configuration.Config, logger *utils.Logger, purpose string) (api.ClientInterface, error) {
	clientType, model, err := configuration.ResolveProviderModel(cfg, "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve provider/model: %w", err)
	}
	resolvedProvider := string(clientType)

	agentClient, err := factory.CreateProviderClient(clientType, model)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent client: %w", err)
	}

	resolvedModel := strings.TrimSpace(agentClient.GetModel())
	if resolvedModel == "" {
		resolvedModel = "<provider default>"
	}

	if logger != nil {
		logger.LogProcessStep(fmt.Sprintf("ℹ️ %s using provider/model: %s | %s", purpose, resolvedProvider, resolvedModel))
	}

	return agentClient, nil
}
