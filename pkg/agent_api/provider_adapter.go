package api

import (
	"context"
	"fmt"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	utils "github.com/sprout-foundry/sprout/pkg/utils"
)

// ProviderAdapter adapts the existing ClientInterface to the new Provider interface
type ProviderAdapter struct {
	client     ClientInterface
	clientType ClientType
}

// NewProviderAdapter creates an adapter for existing clients
func NewProviderAdapter(clientType ClientType, client ClientInterface) *ProviderAdapter {
	return &ProviderAdapter{
		client:     client,
		clientType: clientType,
	}
}

// SendChatRequest adapts the old interface to the new one.
//
// Note: This uses the same global per-provider rate limiter as APIClient.sendRequest().
// Both paths share one bucket per provider to coordinate across all agents, preventing
// cascading 429s when multiple subagents run concurrently. Do NOT add additional rate
// limiting at this layer without coordinating with pkg/agent/api_client.go.
func (a *ProviderAdapter) SendChatRequest(ctx context.Context, req *ProviderChatRequest) (*ChatResponse, error) {
	// Proactive per-provider rate limiting to prevent cascading 429s
	// when multiple agents share the same provider.
	limiter := utils.GetProviderRateLimiter(string(a.clientType))
	if err := limiter.Wait(ctx); err != nil {
		return nil, agenterrors.NewTransientError("rate limit wait canceled", err)
	}

	// Convert ProviderChatRequest to old format
	messages := req.Messages
	tools := req.Tools

	// Determine reasoning parameter based on options
	reasoning := ""
	disableThinking := false
	if req.Options != nil {
		if req.Options.ReasoningEffort != "" {
			reasoning = req.Options.ReasoningEffort
		}
		if req.Options.DisableThinking != nil {
			disableThinking = *req.Options.DisableThinking
		}
	}

	// Call the old interface
	return a.client.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}

// CheckConnection verifies connectivity
func (a *ProviderAdapter) CheckConnection(ctx context.Context) error {
	return a.client.CheckConnection()
}

// GetModel returns the current model
func (a *ProviderAdapter) GetModel() string {
	return a.client.GetModel()
}

// SetModel sets the current model
func (a *ProviderAdapter) SetModel(model string) error {
	return a.client.SetModel(model)
}

// GetAvailableModels returns available models for this provider
func (a *ProviderAdapter) GetAvailableModels(ctx context.Context) ([]ModelDetails, error) {
	// Get models using the provider-specific model fetcher
	modelInfos, err := GetModelsForProvider(a.clientType)
	if err != nil {
		return nil, agenterrors.NewProviderError(fmt.Sprintf("failed to get models for provider %s", a.clientType), err, string(a.clientType), "")
	}

	// Convert ModelInfo to ModelDetails
	models := make([]ModelDetails, 0, len(modelInfos))
	for i, modelInfo := range modelInfos {
		models = append(models, ModelDetails{
			ID:            modelInfo.ID,
			Name:          modelInfo.Name,
			ContextLength: modelInfo.ContextLength,
			IsDefault:     i == 0, // First model is default
			Features:      a.getModelFeatures(modelInfo.ID),
		})
	}

	return models, nil
}

// GetModelContextLimit returns the context window size
func (a *ProviderAdapter) GetModelContextLimit() (int, error) {
	return a.client.GetModelContextLimit()
}

// GetName returns the provider name
func (a *ProviderAdapter) GetName() string {
	return a.client.GetProvider()
}

// GetType returns the provider type
func (a *ProviderAdapter) GetType() ClientType {
	return a.clientType
}

// GetEndpoint returns the API endpoint
func (a *ProviderAdapter) GetEndpoint() string {
	// Extract endpoint from the client implementation
	switch a.clientType {
	case OpenAIClientType:
		return "https://api.openai.com/v1/chat/completions"
	case DeepInfraClientType:
		return "https://api.deepinfra.com/v1/openai/chat/completions"
	case DeepSeekClientType:
		return "https://api.deepseek.com/v1/chat/completions"
	case OpenRouterClientType:
		return "https://openrouter.ai/api/v1/chat/completions"
	case ChutesClientType:
		return "https://chutes.ai/v1/chat/completions"
	case ZAIClientType:
		return "https://z.ai/v1/chat/completions"
	case OllamaClientType, OllamaLocalClientType:
		// For local Ollama, use the default local endpoint
		return "http://localhost:11434/v1/chat/completions"
	case OllamaCloudClientType:
		return "https://turbo.ollama.ai/v1/chat/completions"
	case LMStudioClientType:
		// For LM Studio, use the default local endpoint
		return "http://localhost:1234/v1/chat/completions"
	case TestClientType:
		return "https://test.api.example.com/v1/chat/completions"
	default:
		// For unknown client types, try to extract from client if possible
		if clientWithEndpoint, ok := a.client.(interface{ GetEndpoint() string }); ok {
			return clientWithEndpoint.GetEndpoint()
		}
		return ""
	}
}

// SupportsVision returns whether the provider supports vision
func (a *ProviderAdapter) SupportsVision() bool {
	return a.client.SupportsVision()
}

// SupportsConversationalVision returns whether the provider handles inline
// multimodal chat messages. Delegates to the underlying client; falls back
// to SupportsVision() if the client doesn't implement the new method.
func (a *ProviderAdapter) SupportsConversationalVision() bool {
	if typed, ok := a.client.(interface{ SupportsConversationalVision() bool }); ok {
		return typed.SupportsConversationalVision()
	}
	return a.client.SupportsVision()
}

// SupportsTools returns whether the provider supports tools
func (a *ProviderAdapter) SupportsTools() bool {
	// Most providers support tools
	return true
}

// SupportsStreaming returns whether the provider supports streaming
func (a *ProviderAdapter) SupportsStreaming() bool {
	// OpenAI now supports streaming with usage data via stream_options
	return true
}

// SupportsReasoning returns whether the provider supports reasoning
func (a *ProviderAdapter) SupportsReasoning() bool {
	// Check if provider has reasoning models
	model := a.client.GetModel()
	return containsReasoningModel(model) || a.clientType == OpenAIClientType
}

// VisionCapabilities returns the per-provider vision limits by delegating
// to the wrapped client. If the client does not implement
// VisionCapabilities() (e.g. legacy / mock clients), returns the zero
// value; callers should pass that through VisionCapabilitiesOrDefault()
// to get a safe usable configuration. SP-103-D3 / AUDIT-GAP-2.
func (a *ProviderAdapter) VisionCapabilities() VisionCapabilities {
	if typed, ok := a.client.(interface {
		VisionCapabilities() VisionCapabilities
	}); ok {
		return typed.VisionCapabilities()
	}
	return VisionCapabilities{}
}

// SetDebug enables or disables debug mode
func (a *ProviderAdapter) SetDebug(debug bool) {
	a.client.SetDebug(debug)
}

// IsDebug returns whether debug mode is enabled
func (a *ProviderAdapter) IsDebug() bool {
	// The old interface doesn't expose this, so we track it separately
	return false
}

// getModelFeatures returns features for a model
func (a *ProviderAdapter) getModelFeatures(modelID string) []string {
	features := []string{"tools"}

	// Vision support is determined by the client's SupportsVision() method,
	// which resolves through the provider config's supports_vision flag and
	// per-model tag overrides. This is the same path used at runtime by
	// attachPastedImages and processImagesInQuery.
	if a.client.SupportsVision() {
		features = append(features, "vision")
	}

	// Check for reasoning support
	if containsReasoningModel(modelID) {
		features = append(features, "reasoning")
	}

	return features
}

// containsReasoningModel checks if a model supports reasoning
func containsReasoningModel(model string) bool {
	modelLower := strings.ToLower(model)
	for _, rm := range []string{"o1", "o3", "o4"} {
		if strings.Contains(modelLower, rm) {
			return true
		}
	}
	return false
}

// CreateProviderFromClient creates a Provider from an existing ClientInterface
func CreateProviderFromClient(clientType ClientType, client ClientInterface) Provider {
	return NewProviderAdapter(clientType, client)
}

// GetProviderFromExisting is deprecated - use agent.CreateProviderClient directly
