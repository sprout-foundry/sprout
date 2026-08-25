//go:build js

package agent

import "github.com/sprout-foundry/sprout/pkg/configuration"

// tryMockLLMAgent is a no-op stub for the WASM (js) build. The mock
// provider and its symbols (UseMockLLM, NewMockLLMProvider) are excluded
// from this build, so this path always falls through.
func tryMockLLMAgent(model string, configManager *configuration.Manager, workspaceRoot string) (bool, *Agent, error) {
	return false, nil, nil
}
