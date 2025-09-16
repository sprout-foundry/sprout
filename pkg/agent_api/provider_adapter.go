package api

import (
	"context"
	"fmt"
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

// SendChatRequest adapts the old interface to the new one
func (a *ProviderAdapter) SendChatRequest(ctx context.Context, req *ProviderChatRequest) (*ChatResponse, error) {
	// Convert ProviderChatRequest to old format
	messages := req.Messages
	tools := req.Tools

	// Determine reasoning parameter based on options
	reasoning := ""
	if req.Options != nil && req.Options.ReasoningEffort != "" {
		reasoning = req.Options.ReasoningEffort
	}

	// Call the old interface
	return a.client.SendChatRequest(messages, tools, reasoning)
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
	// Convert from featured models list
	featured := a.client.GetFeaturedModels()

	models := make([]ModelDetails, 0, len(featured))
	for i, modelID := range featured {
		// Get context limit for each model
		oldModel := a.client.GetModel()
		a.client.SetModel(modelID)
		contextLimit, _ := a.client.GetModelContextLimit()
		a.client.SetModel(oldModel) // Restore original

		models = append(models, ModelDetails{
			ID:            modelID,
			Name:          modelID,
			ContextLength: contextLimit,
			IsDefault:     i == 0, // First model is default
			Features:      a.getModelFeatures(modelID),
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
	// This would need to be extracted from the client implementation
	// For now, return a placeholder
	switch a.clientType {
	case OpenAIClientType:
		return "https://api.openai.com/v1/chat/completions"
	case DeepInfraClientType:
		return "https://api.deepinfra.com/v1/openai/chat/completions"
	case GroqClientType:
		return "https://api.groq.com/openai/v1/chat/completions"
	default:
		return ""
	}
}

// SupportsVision returns whether the provider supports vision
func (a *ProviderAdapter) SupportsVision() bool {
	return a.client.SupportsVision()
}

// SupportsTools returns whether the provider supports tools
func (a *ProviderAdapter) SupportsTools() bool {
	// Most providers support tools
	return true
}

// SupportsStreaming returns whether the provider supports streaming
func (a *ProviderAdapter) SupportsStreaming() bool {
	// Most providers support streaming
	return true
}

// SupportsReasoning returns whether the provider supports reasoning
func (a *ProviderAdapter) SupportsReasoning() bool {
	// Check if provider has reasoning models
	model := a.client.GetModel()
	return containsReasoningModel(model) || a.clientType == OpenAIClientType
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

	// Check for vision support
	if a.client.SupportsVision() {
		visionModels := a.client.GetFeaturedVisionModels()
		for _, vm := range visionModels {
			if vm == modelID {
				features = append(features, "vision")
				break
			}
		}
	}

	// Check for reasoning support
	if containsReasoningModel(modelID) {
		features = append(features, "reasoning")
	}

	return features
}

// containsReasoningModel checks if a model supports reasoning
func containsReasoningModel(model string) bool {
	reasoningModels := []string{"o1", "o3", "o4"}
	for _, rm := range reasoningModels {
		if contains(model, rm) {
			return true
		}
	}
	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					findSubstring(s, substr) != -1))
}

// findSubstring finds a substring in a string (simple implementation)
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// CreateProviderFromClient creates a Provider from an existing ClientInterface
func CreateProviderFromClient(clientType ClientType, client ClientInterface) Provider {
	return NewProviderAdapter(clientType, client)
}

// GetProviderFromExisting gets a Provider using the existing system
func GetProviderFromExisting(clientType ClientType, model string) (Provider, error) {
	client, err := NewUnifiedClientWithModel(clientType, model)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return CreateProviderFromClient(clientType, client), nil
}
