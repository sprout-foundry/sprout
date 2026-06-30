//go:build !js

package agent

import (
	"context"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// tryMockLLMAgent returns a fully-initialized Agent backed by the mock LLM
// provider, or (nil, nil) when the --mock-llm flag is not set.
//
// Split out of agent_creation.go because the mock provider itself is
// native-only (see mock_provider.go's //go:build !js constraint). Keeping
// the wiring here means agent_creation.go compiles cleanly for both
// GOOS=js and native targets — the wasm side has its own stub.
func tryMockLLMAgent(configManager *configuration.Manager, workspaceRoot string, model string) (*Agent, error) {
	if !UseMockLLM {
		return nil, nil
	}

	client := NewMockLLMProvider()
	if model != "" {
		client.SetModel(model)
	}
	client.SetDebug(isDebugEnvEnabled())

	providerName := client.GetProvider()
	systemPrompt, err := GetEmbeddedSystemPromptWithProvider(providerName)
	if err != nil {
		return nil, agenterrors.NewPermanentError("failed to load system prompt", err)
	}
	systemPrompt = resolveConfiguredSystemPrompt(configManager.GetConfig(), systemPrompt)

	return initAgentFromResolvedProvider(agentInitParams{
		client:          client,
		clientType:      api.TestClientType,
		systemPrompt:    systemPrompt,
		configManager:   configManager,
		workspaceRoot:   workspaceRoot,
		debug:           isDebugEnvEnabled(),
		interruptCtx:    context.Background(),
		interruptCancel: func() {},
		isProduction:    true,
	})
}