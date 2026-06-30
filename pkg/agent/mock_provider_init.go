//go:build !js

package agent

import (
	"context"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// tryMockLLMAgent handles the --mock-llm override. When UseMockLLM is set,
// returns (true, agent, nil) on success or (true, nil, err) on failure.
// When UseMockLLM is false, returns (false, nil, nil) so the caller
// falls through to the regular provider-resolution path.
//
// MockLLMProvider is a test-only deterministic stand-in; it is excluded
// from the WASM build (this file's `!js` tag), where the no-op stub in
// mock_provider_init_js.go applies.
func tryMockLLMAgent(model string, configManager *configuration.Manager, workspaceRoot string) (bool, *Agent, error) {
	if !UseMockLLM {
		return false, nil, nil
	}
	client := NewMockLLMProvider()
	if model != "" {
		client.SetModel(model)
	}
	client.SetDebug(isDebugEnvEnabled())

	providerName := client.GetProvider()
	systemPrompt, err := GetEmbeddedSystemPromptWithProvider(providerName)
	if err != nil {
		return true, nil, agenterrors.NewPermanentError("failed to load system prompt", err)
	}
	systemPrompt = resolveConfiguredSystemPrompt(configManager.GetConfig(), systemPrompt)

	agent, err := initAgentFromResolvedProvider(agentInitParams{
		client:          client,
		clientType:      api.TestClientType,
		systemPrompt:    systemPrompt,
		configManager:   configManager,
		workspaceRoot:   workspaceRoot,
		debug:           isDebugEnvEnabled(),
		interruptCtx:    context.Background(),
		interruptCancel: func() { /* no-op */ },
		isProduction:    true,
	})
	return true, agent, err
}