//go:build modular

package adapters

import (
	"context"
	"io"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/providers/llm"
	oldTypes "github.com/alantheprice/ledit/pkg/types"
)

// LegacyLLMBridge bridges the old LLM API to the new interface-based system
type LegacyLLMBridge struct {
	factory *llm.Factory
}

// NewLegacyLLMBridge creates a new bridge to the legacy LLM API
func NewLegacyLLMBridge() *LegacyLLMBridge {
	return &LegacyLLMBridge{
		factory: llm.NewGlobalFactory(),
	}
}

// GetLLMResponse provides backward compatibility with the old API
func (b *LegacyLLMBridge) GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, imagePath ...string) (string, *oldTypes.TokenUsage, error) {
	// Convert old messages to new format
	newMessages := b.convertMessages(messages)

	// Convert config to provider config
	providerConfig, err := b.configToProviderConfig(modelName, cfg)
	if err != nil {
		return "", nil, err
	}

	// Create provider
	provider, err := b.factory.CreateProvider(providerConfig)
	if err != nil {
		return "", nil, err
	}

	// Create request options
	options := types.RequestOptions{
		Model:   modelName,
		Timeout: timeout,
	}

	// Generate response
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	}

	response, metadata, err := provider.GenerateResponse(ctx, newMessages, options)
	if err != nil {
		return "", nil, err
	}

	// Convert token usage back to old format
	oldUsage := &oldTypes.TokenUsage{
		PromptTokens:     metadata.TokenUsage.PromptTokens,
		CompletionTokens: metadata.TokenUsage.CompletionTokens,
		TotalTokens:      metadata.TokenUsage.TotalTokens,
		Estimated:        metadata.TokenUsage.Estimated,
	}

	return response, oldUsage, nil
}

// GetLLMResponseStream provides backward compatibility for streaming
func (b *LegacyLLMBridge) GetLLMResponseStream(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, writer io.Writer, imagePath ...string) (*oldTypes.TokenUsage, error) {
	// Convert old messages to new format
	newMessages := b.convertMessages(messages)

	// Convert config to provider config
	providerConfig, err := b.configToProviderConfig(modelName, cfg)
	if err != nil {
		return nil, err
	}

	// Create provider
	provider, err := b.factory.CreateProvider(providerConfig)
	if err != nil {
		return nil, err
	}

	// Create request options
	options := types.RequestOptions{
		Model:   modelName,
		Timeout: timeout,
		Stream:  true,
	}

	// Generate streaming response
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	}

	metadata, err := provider.GenerateResponseStream(ctx, newMessages, options, writer)
	if err != nil {
		return nil, err
	}

	// Convert token usage back to old format
	oldUsage := &oldTypes.TokenUsage{
		PromptTokens:     metadata.TokenUsage.PromptTokens,
		CompletionTokens: metadata.TokenUsage.CompletionTokens,
		TotalTokens:      metadata.TokenUsage.TotalTokens,
		Estimated:        metadata.TokenUsage.Estimated,
	}

	return oldUsage, nil
}

// Helper methods

func (b *LegacyLLMBridge) convertMessages(oldMessages []prompts.Message) []types.Message {
	newMessages := make([]types.Message, len(oldMessages))

	for i, msg := range oldMessages {
		newMessages[i] = types.Message{
			Role:    msg.Role,
			Content: msg.Content,
			// TODO: Convert tool calls and images if needed
		}
	}

	return newMessages
}

func (b *LegacyLLMBridge) configToProviderConfig(modelName string, cfg *config.Config) (*types.ProviderConfig, error) {
	// Determine provider from model name or config
	providerName := b.inferProviderFromModel(modelName)
	if cfg.LLMProvider != "" {
		providerName = cfg.LLMProvider
	}

	// Get API key (this would need to be extracted from your existing config system)
	apiKey := "" // TODO: Get from actual config/apikeys system

	providerConfig := &types.ProviderConfig{
		Name:    providerName,
		Model:   modelName,
		APIKey:  apiKey,
		Enabled: true,
		Timeout: 60, // Default timeout
	}

	// Set provider-specific defaults
	switch providerName {
	case "openai":
		providerConfig.BaseURL = "https://api.openai.com/v1"
	case "gemini":
		providerConfig.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	return providerConfig, nil
}

func (b *LegacyLLMBridge) inferProviderFromModel(modelName string) string {
	if modelName == "" {
		return "openai" // Default
	}

	// Simple inference based on model name patterns
	switch {
	case contains(modelName, "gpt"):
		return "openai"
	case contains(modelName, "gemini"):
		return "gemini"
	case contains(modelName, "llama"), contains(modelName, "codellama"), contains(modelName, "mistral"):
		return "ollama"
	default:
		return "openai" // Default fallback
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			indexOf(s, substr) >= 0)))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
