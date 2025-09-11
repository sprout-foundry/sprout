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
	DeepInfraClientType ClientType = "deepinfra"
	OllamaClientType    ClientType = "ollama"
	CerebrasClientType  ClientType = "cerebras"
	OpenRouterClientType ClientType = "openrouter"
	GroqClientType      ClientType = "groq"
	DeepSeekClientType  ClientType = "deepseek"
)

// NewUnifiedClient creates a client with default model for the provider
func NewUnifiedClient(clientType ClientType) (ClientInterface, error) {
	defaultModel := GetDefaultModelForProvider(clientType)
	return NewUnifiedClientWithModel(clientType, defaultModel)
}

// NewUnifiedClientWithModel creates a client with a specific model
func NewUnifiedClientWithModel(clientType ClientType, model string) (ClientInterface, error) {
	// Use default model if none specified
	if model == "" {
		model = GetDefaultModelForProvider(clientType)
	}
	
	switch clientType {
	case DeepInfraClientType:
		return NewDeepInfraClientWrapper(model)
	case OllamaClientType:
		return NewOllamaClient()
	case CerebrasClientType:
		return NewCerebrasClientWrapper(model)
	case OpenRouterClientType:
		return NewOpenRouterClientWrapper(model)
	case GroqClientType:
		return NewGroqClientWrapper(model)
	case DeepSeekClientType:
		return NewDeepSeekClientWrapper(model)
	default:
		return nil, fmt.Errorf("unknown client type: %s", clientType)
	}
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

// NewDeepSeekClientWrapper creates a DeepSeek client wrapper
func NewDeepSeekClientWrapper(model string) (ClientInterface, error) {
	// For now, return an error since DeepSeek provider is not fully implemented
	return nil, fmt.Errorf("DeepSeek provider not yet implemented")
}

// GetClientTypeFromEnv determines which client to use based on environment variables
func GetClientTypeFromEnv() ClientType {
	// Check provider environment variables in priority order (OpenRouter first as preferred)
	envProviders := []struct {
		envVar string
		client ClientType
	}{
		{"OPENROUTER_API_KEY", OpenRouterClientType},
		{"DEEPINFRA_API_KEY", DeepInfraClientType},
		{"CEREBRAS_API_KEY", CerebrasClientType},
		{"GROQ_API_KEY", GroqClientType},
		{"DEEPSEEK_API_KEY", DeepSeekClientType},
	}

	for _, provider := range envProviders {
		if apiKey := os.Getenv(provider.envVar); apiKey != "" {
			return provider.client
		}
	}

	// Otherwise, default to Ollama for local inference
	return OllamaClientType
}

// GetDefaultModelForProvider returns the best default model for each provider
func GetDefaultModelForProvider(clientType ClientType) string {
	switch clientType {
	case OpenRouterClientType:
		return "deepseek/deepseek-chat-v3.1:free"
	case DeepInfraClientType:
		return "deepseek-ai/DeepSeek-V3.1"
	case OllamaClientType:
		return "gpt-oss:20b"
	case CerebrasClientType:
		return "cerebras/btlm-3b-8k-base"
	case GroqClientType:
		return "llama3-70b-8192"
	case DeepSeekClientType:
		return "deepseek-chat"
	default:
		return "deepseek/deepseek-chat" // Default to OpenRouter
	}
}

// GetVisionModelForProvider returns the vision-capable model for each provider
// Returns empty string if provider doesn't support vision
func GetVisionModelForProvider(clientType ClientType) string {
	switch clientType {
	case OpenRouterClientType:
		return "openai/gpt-4o"
	case DeepInfraClientType:
		return "google/gemma-3-27b-it"
	case OllamaClientType:
		return "llava:latest" // Popular local vision model
	case CerebrasClientType:
		return "" // Cerebras doesn't have vision models yet
	case GroqClientType:
		return "llama-3.2-11b-vision-preview" // Groq has vision models
	case DeepSeekClientType:
		return "" // DeepSeek doesn't have vision models in their API yet
	default:
		return "" // No vision support by default
	}
}

// GetClientTypeWithFallback determines client type and falls back if unavailable
func GetClientTypeWithFallback() (ClientType, error) {
	// Try primary selection
	primaryType := GetClientTypeFromEnv()
	
	// For non-Ollama providers, verify API key exists
	if primaryType != OllamaClientType {
		if _, err := NewUnifiedClient(primaryType); err == nil {
			return primaryType, nil
		}
		// If primary fails, fall back to Ollama
		fmt.Printf("⚠️  Primary provider %s unavailable, falling back to Ollama\n", GetProviderName(primaryType))
		return OllamaClientType, nil
	}
	
	// If Ollama was selected, check if it's running
	if _, err := NewUnifiedClient(OllamaClientType); err == nil {
		return OllamaClientType, nil
	}
	
	// Ollama not available, try other providers as fallback (OpenRouter first as preferred)
	envProviders := []struct {
		envVar string
		client ClientType
	}{
		{"OPENROUTER_API_KEY", OpenRouterClientType},
		{"DEEPINFRA_API_KEY", DeepInfraClientType},
		{"CEREBRAS_API_KEY", CerebrasClientType},
		{"GROQ_API_KEY", GroqClientType},
		{"DEEPSEEK_API_KEY", DeepSeekClientType},
	}

	for _, provider := range envProviders {
		if apiKey := os.Getenv(provider.envVar); apiKey != "" {
			if _, err := NewUnifiedClient(provider.client); err == nil {
				fmt.Printf("⚠️  Ollama unavailable, using %s as fallback\n", GetProviderName(provider.client))
				return provider.client, nil
			}
		}
	}
	
	return "", fmt.Errorf("no available providers found. Please set up either Ollama or a provider API key")
}

// GetAvailableProviders returns a list of all available providers
func GetAvailableProviders() []ClientType {
	return []ClientType{
		DeepInfraClientType,
		OllamaClientType,
		CerebrasClientType,
		OpenRouterClientType,
		GroqClientType,
		DeepSeekClientType,
	}
}

// GetProviderName returns the human-readable name for a provider
func GetProviderName(clientType ClientType) string {
	switch clientType {
	case DeepInfraClientType:
		return "DeepInfra"
	case OllamaClientType:
		return "Ollama (Local)"
	case CerebrasClientType:
		return "Cerebras"
	case OpenRouterClientType:
		return "OpenRouter"
	case GroqClientType:
		return "Groq"
	case DeepSeekClientType:
		return "DeepSeek"
	default:
		return string(clientType)
	}
}

// GetProviderFromString converts a string to ClientType
func GetProviderFromString(providerStr string) (ClientType, error) {
	providerStr = strings.ToLower(providerStr)
	switch providerStr {
	case "deepinfra":
		return DeepInfraClientType, nil
	case "ollama":
		return OllamaClientType, nil
	case "cerebras":
		return CerebrasClientType, nil
	case "openrouter":
		return OpenRouterClientType, nil
	case "groq":
		return GroqClientType, nil
	case "deepseek":
		return DeepSeekClientType, nil
	default:
		return "", fmt.Errorf("unknown provider: %s", providerStr)
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
	
	// Model-specific context limits based on official documentation
	switch {
	case strings.Contains(model, "DeepSeek-V3.1"):
		return 128000, nil // DeepSeek-V3.1 supports 128K context
	case strings.Contains(model, "DeepSeek-V3"):
		return 128000, nil // DeepSeek-V3 supports 128K context
	case strings.Contains(model, "DeepSeek-R1"):
		return 64000, nil  // DeepSeek-R1 supports 64K context
	case strings.Contains(model, "deepseek"):
		return 32000, nil  // Other DeepSeek models typically 32K
	case strings.Contains(model, "gpt-oss"):
		return 120000, nil // GPT-OSS models typically have ~120k context
	case strings.Contains(model, "llama"):
		return 32000, nil  // Llama models typically have ~32k context
	case strings.Contains(model, "qwen"):
		if strings.Contains(model, "Qwen3-Coder-480B") {
			return 128000, nil // Large Qwen models have bigger context
		}
		return 32000, nil  // Standard Qwen models typically have ~32k context
	case strings.Contains(model, "claude"):
		return 200000, nil // Claude models have large context windows
	case strings.Contains(model, "gemini"):
		return 128000, nil // Gemini models have large context windows
	default:
		return 32000, nil  // Conservative default
	}
}

func (w *DeepInfraClientWrapper) SupportsVision() bool {
	// DeepInfra has vision-capable models like Llama-3.2-11B-Vision-Instruct
	visionModel := w.GetVisionModel()
	return visionModel != ""
}

func (w *DeepInfraClientWrapper) GetVisionModel() string {
	return GetVisionModelForProvider(DeepInfraClientType)
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



