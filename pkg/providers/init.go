package providers

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
	"github.com/alantheprice/ledit/pkg/providers/llm"
	"github.com/alantheprice/ledit/pkg/providers/llm/gemini"
	"github.com/alantheprice/ledit/pkg/providers/llm/ollama"
	"github.com/alantheprice/ledit/pkg/providers/llm/openai"
)

// RegisterDefaultProviders registers all default providers with the global registry
func RegisterDefaultProviders() error {
	// Register OpenAI provider
	if err := llm.RegisterProvider(&openai.Factory{}); err != nil {
		return err
	}

	// Register Gemini provider
	if err := llm.RegisterProvider(&gemini.Factory{}); err != nil {
		return err
	}

	// Register Ollama provider
	if err := llm.RegisterProvider(&ollama.Factory{}); err != nil {
		return err
	}

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

	// Extract provider name from model name (e.g., "openai:gpt-4" -> "openai")
	providerName := strings.SplitN(modelName, ":", 2)[0]

	// Create a basic configuration
	config := &types.ProviderConfig{
		Name:    providerName,
		Model:   modelName,
		Enabled: true,
	}

	// Use the global factory to create the provider
	factory := llm.NewGlobalFactory()
	return factory.CreateProvider(config)
}
