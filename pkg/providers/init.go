package providers

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
	// Removed duplicate LLM providers - using agent_api system instead
	// "github.com/alantheprice/ledit/pkg/providers/llm"
	// "github.com/alantheprice/ledit/pkg/providers/llm/gemini"
	// "github.com/alantheprice/ledit/pkg/providers/llm/ollama"
	// "github.com/alantheprice/ledit/pkg/providers/llm/openai"
)

// RegisterDefaultProviders registers all default providers with the global registry
func RegisterDefaultProviders() error {
	// Legacy provider registration removed - using agent_api system instead
	// Providers are now registered in pkg/agent_api
	return nil
}

// MustRegisterDefaultProviders registers default providers and panics on error
func MustRegisterDefaultProviders() {
	if err := RegisterDefaultProviders(); err != nil {
		panic("failed to register default providers: " + err.Error())
	}
}

// Convenience types for external use
type TokenUsage = types.TokenUsage

// GetProvider creates a provider instance based on model name
func GetProvider(modelName string) (interfaces.LLMProvider, error) {
	if modelName == "" {
		return nil, fmt.Errorf("model name is required")
	}

	// Legacy factory removed - providers now handled by agent_api
	return nil, fmt.Errorf("legacy provider system removed - use agent_api instead")
}
