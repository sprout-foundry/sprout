package api

import (
	"fmt"
	"os"
	"strings"
)

// ClientInterface defines the common interface for all API clients
type ClientInterface interface {
	SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error)
	SendChatRequestStream(messages []Message, tools []Tool, reasoning string, callback StreamCallback) (*ChatResponse, error)
	CheckConnection() error
	SetDebug(debug bool)
	SetModel(model string) error
	GetModel() string
	GetProvider() string
	GetModelContextLimit() (int, error)
	SupportsVision() bool
	GetVisionModel() string
	SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error)
	// TPS (Tokens Per Second) tracking methods
	GetLastTPS() float64
	GetAverageTPS() float64
	GetTPSStats() map[string]float64
	ResetTPSStats()
}

// ClientType represents the type of client to use
type ClientType string

const (
	DeepInfraClientType   ClientType = "deepinfra"
	OllamaClientType      ClientType = "ollama" // Maps to local ollama
	OllamaLocalClientType ClientType = "ollama-local"
	OllamaTurboClientType ClientType = "ollama-turbo"
	OpenRouterClientType  ClientType = "openrouter"
	OpenAIClientType      ClientType = "openai"
)

// NewUnifiedClient creates a client with default model for the provider
func NewUnifiedClient(clientType ClientType) (ClientInterface, error) {
	return NewUnifiedClientWithModel(clientType, "")
}

// NewUnifiedClientWithModel creates a client with a specific model
func NewUnifiedClientWithModel(clientType ClientType, model string) (ClientInterface, error) {
	return CreateProviderClient(clientType, model)
}

// NewDeepInfraClientWrapper creates a DeepInfra client wrapper
func NewDeepInfraClientWrapper(model string) (ClientInterface, error) {
	client, err := NewClientWithModel(model)
	if err != nil {
		return nil, err
	}
	return &DeepInfraClientWrapper{client: client}, nil
}

// NewOpenRouterClientWrapper creates an OpenRouter client wrapper
func NewOpenRouterClientWrapper(model string) (ClientInterface, error) {
	return NewOpenRouterProvider(model)
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
// DeepSeek provider removed (unimplemented)

// DetermineProvider provides unified provider detection with clear precedence:
// 1. Command-line flag (if provided)
// 2. Environment variable (LEDIT_PROVIDER)
// 3. Config file (last_used_provider)
// 4. First available provider based on API keys
// 5. Fallback to Ollama
func DetermineProvider(explicitProvider string, lastUsedProvider ClientType) (ClientType, error) {
	// 1. Command-line flag has highest priority
	if explicitProvider != "" {
		provider, err := ParseProviderName(explicitProvider)
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
		provider, err := ParseProviderName(providerEnv)
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
		OllamaTurboClientType,
		OllamaLocalClientType,
	}

	for _, provider := range priorityOrder {
		if IsProviderAvailable(provider) {
			return provider, nil
		}
	}

	// 5. Final fallback
	return OllamaLocalClientType, nil
}

// ParseProviderName converts a string provider name to ClientType
func ParseProviderName(name string) (ClientType, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "openai":
		return OpenAIClientType, nil
	case "openrouter":
		return OpenRouterClientType, nil
	case "deepinfra":
		return DeepInfraClientType, nil
	case "ollama":
		// "ollama" maps to local
		return OllamaLocalClientType, nil
	case "ollama-local":
		return OllamaLocalClientType, nil
	case "ollama-turbo":
		return OllamaTurboClientType, nil
	default:
		return "", fmt.Errorf("unknown provider: %s", name)
	}
}

// IsProviderAvailable checks if a provider can be used
func IsProviderAvailable(provider ClientType) bool {
	switch provider {
	case OllamaClientType, OllamaLocalClientType:
		// Ollama local is always available (we'll check actual model availability later)
		return true
	case OllamaTurboClientType:
		return os.Getenv("OLLAMA_API_KEY") != ""
	case OpenAIClientType:
		return os.Getenv("OPENAI_API_KEY") != ""
	case OpenRouterClientType:
		return os.Getenv("OPENROUTER_API_KEY") != ""
	case DeepInfraClientType:
		return os.Getenv("DEEPINFRA_API_KEY") != ""
	default:
		return false
	}
}

// GetAvailableProviders returns a list of all available providers
func GetAvailableProviders() []ClientType {
	providers := []ClientType{
		OpenAIClientType,
		DeepInfraClientType,
		OllamaLocalClientType,
		OllamaTurboClientType,
		OpenRouterClientType,
	}

	available := make([]ClientType, 0, len(providers))
	for _, provider := range providers {
		if IsProviderAvailable(provider) {
			available = append(available, provider)
		}
	}
	return available
}

// GetProviderName returns the human-readable name for a provider
func GetProviderName(clientType ClientType) string {
	switch clientType {
	case OpenAIClientType:
		return "OpenAI"
	case DeepInfraClientType:
		return "DeepInfra"
	case OllamaClientType, OllamaLocalClientType:
		return "Ollama (Local)"
	case OllamaTurboClientType:
		return "Ollama Turbo"
	case OpenRouterClientType:
		return "OpenRouter"
	default:
		return string(clientType)
	}
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

func (w *DeepInfraClientWrapper) SendChatRequestStream(messages []Message, tools []Tool, reasoning string, callback StreamCallback) (*ChatResponse, error) {
	// Calculate context-aware max_tokens to avoid exceeding model limits
	maxTokens := w.calculateMaxTokens(messages, tools)

	req := ChatRequest{
		Model:     w.client.model,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: maxTokens,
		Reasoning: reasoning,
	}
	return w.client.SendChatRequestStream(req, callback)
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

	// Return reasonable default for DeepInfra models
	// The provider should be queried for actual context limits
	return 128000, nil
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

// GetLastTPS returns the most recent TPS measurement
func (w *DeepInfraClientWrapper) GetLastTPS() float64 {
	return w.client.GetLastTPS()
}

// GetAverageTPS returns the average TPS across all requests
func (w *DeepInfraClientWrapper) GetAverageTPS() float64 {
	return w.client.GetAverageTPS()
}

// GetTPSStats returns comprehensive TPS statistics
func (w *DeepInfraClientWrapper) GetTPSStats() map[string]float64 {
	return w.client.GetTPSStats()
}

// ResetTPSStats clears all TPS tracking data
func (w *DeepInfraClientWrapper) ResetTPSStats() {
	w.client.ResetTPSStats()
}
