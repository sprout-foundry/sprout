package api

import (
	"time"

	"github.com/alantheprice/ledit/pkg/agent_providers"
	types "github.com/alantheprice/ledit/pkg/agent_types"
)

// UnifiedProviderWrapper wraps any provider that implements types.ProviderInterface
type UnifiedProviderWrapper struct {
	*TPSBase
	provider types.ProviderInterface
}

// NewUnifiedProviderWrapper creates a wrapper for any provider
func NewUnifiedProviderWrapper(provider types.ProviderInterface) *UnifiedProviderWrapper {
	return &UnifiedProviderWrapper{
		TPSBase:  NewTPSBase(),
		provider: provider,
	}
}

// SendChatRequest converts types and forwards to provider
func (w *UnifiedProviderWrapper) SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	// Track request timing
	startTime := time.Now()

	// Convert API types to shared types
	typeMessages := make([]types.Message, len(messages))
	for i, msg := range messages {
		// Convert image data
		typeImages := make([]types.ImageData, len(msg.Images))
		for j, img := range msg.Images {
			typeImages[j] = types.ImageData{
				URL:    img.URL,
				Base64: img.Base64,
				Type:   img.Type,
			}
		}

		typeMessages[i] = types.Message{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Images:           typeImages,
		}
	}

	typeTools := make([]types.Tool, len(tools))
	for i, tool := range tools {
		typeTools[i] = types.Tool{
			Type: tool.Type,
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		}
	}

	// Call provider
	response, err := w.provider.SendChatRequest(typeMessages, typeTools, reasoning)

	// Calculate request duration AFTER the provider completes
	// This ensures we measure the full token generation time
	duration := time.Since(startTime)

	if err != nil {
		return nil, err
	}

	// Track TPS
	if w.GetTracker() != nil && response != nil && response.Usage.CompletionTokens > 0 {
		w.GetTracker().RecordRequest(duration, response.Usage.CompletionTokens)
	}

	// Convert response back to API types
	apiResponse := &ChatResponse{
		ID:      response.ID,
		Object:  response.Object,
		Created: response.Created,
		Model:   response.Model,
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
			PromptTokensDetails: struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			}{
				CachedTokens:     response.Usage.PromptTokensDetails.CachedTokens,
				CacheWriteTokens: response.Usage.PromptTokensDetails.CacheWriteTokens,
			},
		},
	}

	// Convert choices
	apiResponse.Choices = make([]Choice, len(response.Choices))
	for i, choice := range response.Choices {
		// Convert response message images
		responseImages := make([]ImageData, len(choice.Message.Images))
		for j, img := range choice.Message.Images {
			responseImages[j] = ImageData{
				URL:    img.URL,
				Base64: img.Base64,
				Type:   img.Type,
			}
		}

		apiResponse.Choices[i] = Choice{
			Index: choice.Index,
			Message: struct {
				Role             string      `json:"role"`
				Content          string      `json:"content"`
				ReasoningContent string      `json:"reasoning_content,omitempty"`
				Images           []ImageData `json:"images,omitempty"`
				ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:             choice.Message.Role,
				Content:          choice.Message.Content,
				ReasoningContent: choice.Message.ReasoningContent,
				Images:           responseImages,
			},
			FinishReason: choice.FinishReason,
		}

		// Convert tool calls
		if len(choice.Message.ToolCalls) > 0 {
			apiResponse.Choices[i].Message.ToolCalls = make([]ToolCall, len(choice.Message.ToolCalls))
			for j, toolCall := range choice.Message.ToolCalls {
				apiResponse.Choices[i].Message.ToolCalls[j] = ToolCall{
					ID:   toolCall.ID,
					Type: toolCall.Type,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      toolCall.Function.Name,
						Arguments: toolCall.Function.Arguments,
					},
				}
			}
		}
	}

	return apiResponse, nil
}

// Forward all other methods to the provider
func (w *UnifiedProviderWrapper) CheckConnection() error {
	return w.provider.CheckConnection()
}

func (w *UnifiedProviderWrapper) SetDebug(debug bool) {
	w.provider.SetDebug(debug)
}

func (w *UnifiedProviderWrapper) SetModel(model string) error {
	return w.provider.SetModel(model)
}

func (w *UnifiedProviderWrapper) GetModel() string {
	return w.provider.GetModel()
}

func (w *UnifiedProviderWrapper) GetProvider() string {
	return w.provider.GetProvider()
}

func (w *UnifiedProviderWrapper) GetModelContextLimit() (int, error) {
	return w.provider.GetModelContextLimit()
}

func (w *UnifiedProviderWrapper) ListModels() ([]types.ModelInfo, error) {
	return w.provider.ListModels()
}

func (w *UnifiedProviderWrapper) SupportsVision() bool {
	return w.provider.SupportsVision()
}

func (w *UnifiedProviderWrapper) GetVisionModel() string {
	// Delegate to the underlying provider if it supports vision model
	if visionProvider, ok := w.provider.(interface{ GetVisionModel() string }); ok {
		return visionProvider.GetVisionModel()
	}
	return ""
}

func (w *UnifiedProviderWrapper) SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error) {
	// Convert API types to shared types
	typeMessages := make([]types.Message, len(messages))
	for i, msg := range messages {
		// Convert image data
		typeImages := make([]types.ImageData, len(msg.Images))
		for j, img := range msg.Images {
			typeImages[j] = types.ImageData{
				URL:    img.URL,
				Base64: img.Base64,
				Type:   img.Type,
			}
		}

		typeMessages[i] = types.Message{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Images:           typeImages,
		}
	}

	typeTools := make([]types.Tool, len(tools))
	for i, tool := range tools {
		typeTools[i] = types.Tool{
			Type: tool.Type,
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		}
	}

	// Call provider vision method
	response, err := w.provider.SendVisionRequest(typeMessages, typeTools, reasoning)
	if err != nil {
		return nil, err
	}

	// Convert response back to API types (same as SendChatRequest)
	apiResponse := &ChatResponse{
		ID:      response.ID,
		Object:  response.Object,
		Created: response.Created,
		Model:   response.Model,
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
			PromptTokensDetails: struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			}{
				CachedTokens:     response.Usage.PromptTokensDetails.CachedTokens,
				CacheWriteTokens: response.Usage.PromptTokensDetails.CacheWriteTokens,
			},
		},
	}

	// Convert choices
	apiResponse.Choices = make([]Choice, len(response.Choices))
	for i, choice := range response.Choices {
		// Convert response message images
		responseImages := make([]ImageData, len(choice.Message.Images))
		for j, img := range choice.Message.Images {
			responseImages[j] = ImageData{
				URL:    img.URL,
				Base64: img.Base64,
				Type:   img.Type,
			}
		}

		apiResponse.Choices[i] = Choice{
			Index: choice.Index,
			Message: struct {
				Role             string      `json:"role"`
				Content          string      `json:"content"`
				ReasoningContent string      `json:"reasoning_content,omitempty"`
				Images           []ImageData `json:"images,omitempty"`
				ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:             choice.Message.Role,
				Content:          choice.Message.Content,
				ReasoningContent: choice.Message.ReasoningContent,
				Images:           responseImages,
			},
			FinishReason: choice.FinishReason,
		}

		// Convert tool calls
		if len(choice.Message.ToolCalls) > 0 {
			apiResponse.Choices[i].Message.ToolCalls = make([]ToolCall, len(choice.Message.ToolCalls))
			for j, toolCall := range choice.Message.ToolCalls {
				apiResponse.Choices[i].Message.ToolCalls[j] = ToolCall{
					ID:   toolCall.ID,
					Type: toolCall.Type,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      toolCall.Function.Name,
						Arguments: toolCall.Function.Arguments,
					},
				}
			}
		}
	}

	return apiResponse, nil
}

// SendChatRequestStream sends a streaming chat request (not yet implemented for unified providers)
func (w *UnifiedProviderWrapper) SendChatRequestStream(messages []Message, tools []Tool, reasoning string, callback StreamCallback) (*ChatResponse, error) {
	// Convert API types to provider types
	providerMessages := make([]types.Message, len(messages))
	for i, msg := range messages {
		// Convert image data
		providerImages := make([]types.ImageData, len(msg.Images))
		for j, img := range msg.Images {
			providerImages[j] = types.ImageData{
				URL:    img.URL,
				Base64: img.Base64,
				Type:   img.Type,
			}
		}

		providerMessages[i] = types.Message{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Images:           providerImages,
		}
	}

	providerTools := make([]types.Tool, len(tools))
	for i, tool := range tools {
		providerTools[i] = types.Tool{
			Type: tool.Type,
			Function: struct {
				Name        string      `json:"name"`
				Description string      `json:"description"`
				Parameters  interface{} `json:"parameters"`
			}{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		}
	}

	// Create a wrapper callback that converts strings to our StreamCallback type
	providerCallback := func(content string) {
		if callback != nil {
			callback(content)
		}
	}

	// Call provider's streaming method
	response, err := w.provider.SendChatRequestStream(providerMessages, providerTools, reasoning, providerCallback)
	if err != nil {
		return nil, err
	}

	// Convert response back to API types
	apiResponse := &ChatResponse{
		ID:      response.ID,
		Object:  response.Object,
		Created: response.Created,
		Model:   response.Model,
		Choices: make([]Choice, len(response.Choices)),
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
		},
	}

	// Convert choices
	for i, choice := range response.Choices {
		// Convert tool calls
		var toolCalls []ToolCall
		for _, tc := range choice.Message.ToolCalls {
			toolCalls = append(toolCalls, ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}

		// Convert images
		var images []ImageData
		for _, img := range choice.Message.Images {
			images = append(images, ImageData{
				URL:    img.URL,
				Base64: img.Base64,
				Type:   img.Type,
			})
		}

		apiResponse.Choices[i] = Choice{
			Index: choice.Index,
			Message: struct {
				Role             string      `json:"role"`
				Content          string      `json:"content"`
				ReasoningContent string      `json:"reasoning_content,omitempty"`
				Images           []ImageData `json:"images,omitempty"`
				ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:             choice.Message.Role,
				Content:          choice.Message.Content,
				ReasoningContent: choice.Message.ReasoningContent,
				Images:           images,
				ToolCalls:        toolCalls,
			},
			FinishReason: choice.FinishReason,
		}
	}

	return apiResponse, nil
}

// GetTPSStatistics returns tokens per second statistics
func (w *UnifiedProviderWrapper) GetTPSStatistics() (float64, float64, int) {
	// Delegate to underlying provider if it supports TPS tracking
	if tpsProvider, ok := w.provider.(interface {
		GetTPSStatistics() (float64, float64, int)
	}); ok {
		return tpsProvider.GetTPSStatistics()
	}
	return 0.0, 0.0, 0
}

// GetLastRequestTPS returns the TPS for the last API request
func (w *UnifiedProviderWrapper) GetLastRequestTPS() float64 {
	// Delegate to underlying provider if it supports TPS tracking
	if tpsProvider, ok := w.provider.(interface {
		GetLastRequestTPS() float64
	}); ok {
		return tpsProvider.GetLastRequestTPS()
	}
	return 0.0
}

// ResetTPSStatistics resets the TPS tracking
func (w *UnifiedProviderWrapper) ResetTPSStatistics() {
	// Delegate to underlying provider if it supports TPS tracking
	if tpsProvider, ok := w.provider.(interface {
		ResetTPSStatistics()
	}); ok {
		tpsProvider.ResetTPSStatistics()
	}
}

// Factory functions for creating providers
func NewOpenRouterProvider(model string) (ClientInterface, error) {
	provider, err := providers.NewOpenRouterProviderWithModel(model)
	if err != nil {
		return nil, err
	}
	return NewUnifiedProviderWrapper(provider), nil
}
