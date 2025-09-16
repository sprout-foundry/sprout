package api

import (
	"fmt"
	"os"
	"strings"
)

// ClientInterface defines the common interface for all API clients
type ClientInterface interface {
	SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error)
	CheckConnection() error
	SetDebug(debug bool)
	SetModel(model string) error
	GetModel() string
	GetProvider() string
	GetModelContextLimit() (int, error)
	SupportsVision() bool
	GetVisionModel() string
	SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error)
}

// ClientType represents the type of client to use
type ClientType string

const (
	DeepInfraClientType  ClientType = "deepinfra"
	OllamaClientType     ClientType = "ollama"
	CerebrasClientType   ClientType = "cerebras"
	OpenRouterClientType ClientType = "openrouter"
	OpenAIClientType     ClientType = "openai"
	GroqClientType       ClientType = "groq"
	DeepSeekClientType   ClientType = "deepseek"
)

// NewUnifiedClient creates a client with default model for the provider
func NewUnifiedClient(clientType ClientType) (ClientInterface, error) {
	registry := GetProviderRegistry()
	defaultModel := registry.GetDefaultModel(clientType)
	return NewUnifiedClientWithModel(clientType, defaultModel)
}

// NewUnifiedClientWithModel creates a client with a specific model
func NewUnifiedClientWithModel(clientType ClientType, model string) (ClientInterface, error) {
	// Use default model if none specified
	registry := GetProviderRegistry()
	if model == "" {
		model = registry.GetDefaultModel(clientType)
	}

	// Use the provider registry for data-driven client creation
	return registry.CreateClient(clientType, model)
}

// NewDeepInfraClientWrapper creates a DeepInfra client wrapper
func NewDeepInfraClientWrapper(model string) (ClientInterface, error) {
	client, err := NewClientWithModel(model)
	if err != nil {
		return nil, err
	}
	return &DeepInfraClientWrapper{client: client}, nil
}

// NewCerebrasClientWrapper creates a Cerebras client wrapper
func NewCerebrasClientWrapper(model string) (ClientInterface, error) {
	return NewCerebrasProvider(model)
}

// NewOpenRouterClientWrapper creates an OpenRouter client wrapper
func NewOpenRouterClientWrapper(model string) (ClientInterface, error) {
	return NewOpenRouterProvider(model)
}

// NewGroqClientWrapper creates a Groq client wrapper
func NewGroqClientWrapper(model string) (ClientInterface, error) {
	// For now, return an error since Groq provider is not fully implemented
	return nil, fmt.Errorf("Groq provider not yet implemented")
}

// NewOpenAIClientWrapper creates an OpenAI client wrapper
func NewOpenAIClientWrapper(model string) (ClientInterface, error) {
	client, err := NewOpenAIClient()
	if err != nil {
		return nil, err
	}
	if model != "" {
		if err := client.SetModel(model); err != nil {
			return nil, err
		}
	}
	return client, nil
}

// NewDeepSeekClientWrapper creates a DeepSeek client wrapper
func NewDeepSeekClientWrapper(model string) (ClientInterface, error) {
	// For now, return an error since DeepSeek provider is not fully implemented
	return nil, fmt.Errorf("DeepSeek provider not yet implemented")
}

// DetermineProvider provides unified provider detection with clear precedence:
// 1. Command-line flag (if provided)
// 2. Environment variable (LEDIT_PROVIDER)
// 3. Config file (last_used_provider)
// 4. First available provider based on API keys
// 5. Fallback to Ollama
func DetermineProvider(explicitProvider string, lastUsedProvider ClientType) (ClientType, error) {
	// 1. Command-line flag has highest priority
	if explicitProvider != "" {
		provider, err := parseProviderName(explicitProvider)
		if err != nil {
			return "", fmt.Errorf("invalid provider '%s': %w", explicitProvider, err)
		}
		if !IsProviderAvailable(provider) {
			return "", fmt.Errorf("provider '%s' is not available (check API key)", explicitProvider)
		}
		return provider, nil
	}

	// 2. Environment variable
	if providerEnv := os.Getenv("LEDIT_PROVIDER"); providerEnv != "" {
		provider, err := parseProviderName(providerEnv)
		if err == nil && IsProviderAvailable(provider) {
			return provider, nil
		}
	}

	// 3. Last used provider from config
	if lastUsedProvider != "" && IsProviderAvailable(lastUsedProvider) {
		return lastUsedProvider, nil
	}

	// 4. First available provider based on API keys
	priorityOrder := []ClientType{
		OpenAIClientType,
		OpenRouterClientType,
		DeepInfraClientType,
		CerebrasClientType,
		GroqClientType,
		DeepSeekClientType,
		OllamaClientType,
	}

	for _, provider := range priorityOrder {
		if IsProviderAvailable(provider) {
			return provider, nil
		}
	}

	// 5. Final fallback
	return OllamaClientType, nil
}

// parseProviderName converts a string provider name to ClientType
func parseProviderName(name string) (ClientType, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "openai":
		return OpenAIClientType, nil
	case "openrouter":
		return OpenRouterClientType, nil
	case "deepinfra":
		return DeepInfraClientType, nil
	case "ollama":
		return OllamaClientType, nil
	case "cerebras":
		return CerebrasClientType, nil
	case "groq":
		return GroqClientType, nil
	case "deepseek":
		return DeepSeekClientType, nil
	default:
		return "", fmt.Errorf("unknown provider: %s", name)
	}
}

// GetDefaultModelForProvider returns the default model for each provider
// This is a compatibility wrapper around the provider registry
func GetDefaultModelForProvider(clientType ClientType) string {
	registry := GetProviderRegistry()
	return registry.GetDefaultModel(clientType)
}

// GetVisionModelForProvider returns the default vision-capable model for each provider
// This is a compatibility wrapper around the provider registry
func GetVisionModelForProvider(clientType ClientType) string {
	registry := GetProviderRegistry()
	return registry.GetDefaultVisionModel(clientType)
}

// IsProviderAvailable checks if a provider can be used
func IsProviderAvailable(provider ClientType) bool {
	switch provider {
	case OllamaClientType:
		// Ollama is always available (local)
		return true
	case OpenAIClientType:
		return os.Getenv("OPENAI_API_KEY") != ""
	case OpenRouterClientType:
		return os.Getenv("OPENROUTER_API_KEY") != ""
	case DeepInfraClientType:
		return os.Getenv("DEEPINFRA_API_KEY") != ""
	case CerebrasClientType:
		return os.Getenv("CEREBRAS_API_KEY") != ""
	case GroqClientType:
		return os.Getenv("GROQ_API_KEY") != ""
	case DeepSeekClientType:
		return os.Getenv("DEEPSEEK_API_KEY") != ""
	default:
		return false
	}
}

// GetAvailableProviders returns a list of all available providers
func GetAvailableProviders() []ClientType {
	registry := GetProviderRegistry()
	return registry.GetAvailableProviders()
}

// GetProviderName returns the human-readable name for a provider
func GetProviderName(clientType ClientType) string {
	registry := GetProviderRegistry()
	return registry.GetProviderName(clientType)
}

// DeepInfraClientWrapper wraps the existing DeepInfra client to implement ClientInterface
type DeepInfraClientWrapper struct {
	client *Client
}

func (w *DeepInfraClientWrapper) SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	// Calculate context-aware max_tokens to avoid exceeding model limits
	maxTokens := w.calculateMaxTokens(messages, tools)

	req := ChatRequest{
		Model:     w.client.model,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: maxTokens,
		Reasoning: reasoning,
	}
	return w.client.SendChatRequest(req)
}

// calculateMaxTokens calculates appropriate max_tokens based on input size and model limits
func (w *DeepInfraClientWrapper) calculateMaxTokens(messages []Message, tools []Tool) int {
	// Get model context limit
	contextLimit, err := w.GetModelContextLimit()
	if err != nil || contextLimit == 0 {
		contextLimit = 32000 // Conservative default
	}

	// Rough estimation: 1 token ≈ 4 characters
	inputTokens := 0

	// Estimate tokens from messages
	for _, msg := range messages {
		inputTokens += len(msg.Content) / 4
	}

	// Estimate tokens from tools (tools descriptions can be large)
	inputTokens += len(tools) * 200 // Rough estimate per tool

	// Reserve buffer for safety and leave room for response
	maxOutput := contextLimit - inputTokens - 1000 // 1000 token safety buffer

	// Ensure reasonable bounds
	if maxOutput > 16000 {
		maxOutput = 16000 // Cap at 16K for most responses
	} else if maxOutput < 1000 {
		maxOutput = 1000 // Minimum useful response size
	}

	return maxOutput
}

func (w *DeepInfraClientWrapper) CheckConnection() error {
	// For DeepInfra, we just check if the API key is set
	if os.Getenv("DEEPINFRA_API_KEY") == "" {
		return fmt.Errorf("DEEPINFRA_API_KEY environment variable not set")
	}
	return nil
}

func (w *DeepInfraClientWrapper) SetDebug(debug bool) {
	w.client.debug = debug
}

func (w *DeepInfraClientWrapper) SetModel(model string) error {
	w.client.model = model
	return nil
}

func (w *DeepInfraClientWrapper) GetModel() string {
	return w.client.model
}

func (w *DeepInfraClientWrapper) GetProvider() string {
	return "deepinfra"
}

func (w *DeepInfraClientWrapper) GetModelContextLimit() (int, error) {
	model := w.client.model

	// Try to get context length from model info API first
	models, err := getDeepInfraModels()
	if err == nil {
		for _, modelInfo := range models {
			if modelInfo.ID == model && modelInfo.ContextLength > 0 {
				return modelInfo.ContextLength, nil
			}
		}
	}

	// Fallback to model registry for consistent context limit lookup
	// Note: The registry handles pattern matching for models not in the API response
	registry := GetModelRegistry()
	contextLimit, err := registry.GetModelContextLength(model)
	if err != nil {
		// Return reasonable default for DeepInfra models
		return 32000, nil
	}
	return contextLimit, nil
}

func (w *DeepInfraClientWrapper) SupportsVision() bool {
	// DeepInfra has vision-capable models like Llama-3.2-11B-Vision-Instruct
	visionModel := w.GetVisionModel()
	return visionModel != ""
}

func (w *DeepInfraClientWrapper) GetVisionModel() string {
	// No default vision model for DeepInfra
	return ""
}

func (w *DeepInfraClientWrapper) SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	if !w.SupportsVision() {
		// Fallback to regular chat request if no vision model available
		return w.SendChatRequest(messages, tools, reasoning)
	}

	// Temporarily switch to vision model for this request
	originalModel := w.GetModel()
	visionModel := w.GetVisionModel()

	if err := w.SetModel(visionModel); err != nil {
		// If we can't set the vision model, fallback to regular request
		return w.SendChatRequest(messages, tools, reasoning)
	}

	// Send the vision request
	response, err := w.SendChatRequest(messages, tools, reasoning)

	// Restore original model
	w.SetModel(originalModel)

	return response, err
}
