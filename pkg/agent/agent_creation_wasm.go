//go:build js

package agent

import (
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// tryMockLLMAgent is a no-op on the wasm build. The mock LLM provider
// is native-only (see mock_provider.go's //go:build !js constraint);
// the browser path always falls through to the real provider path.
func tryMockLLMAgent(configManager *configuration.Manager, workspaceRoot string, model string) (*Agent, error) {
	return nil, nil
}