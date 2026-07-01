package api

import (
	"context"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// UnifiedProviderWrapper wraps any provider that implements ProviderInterface
type UnifiedProviderWrapper struct {
	*TPSBase
	provider ProviderInterface
}

// NewUnifiedProviderWrapper creates a wrapper for any provider
func NewUnifiedProviderWrapper(provider ProviderInterface) *UnifiedProviderWrapper {
	return &UnifiedProviderWrapper{
		TPSBase:  NewTPSBase(),
		provider: provider,
	}
}

// SendChatRequest converts types and forwards to provider
func (w *UnifiedProviderWrapper) SendChatRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	// Track request timing
	startTime := time.Now()

	// Convert API types to shared types
	typeMessages := make([]Message, len(messages))
	for i, msg := range messages {
		// Convert image data
		typeImages := make([]ImageData, len(msg.Images))
		for j, img := range msg.Images {
			typeImages[j] = ImageData{
				URL:    img.URL,
				Base64: img.Base64,
				Type:   img.Type,
			}
		}

		typeMessages[i] = Message{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Images:           typeImages,
		}
	}

	typeTools := make([]Tool, len(tools))
	for i, tool := range tools {
		typeTools[i] = Tool{
			Type: tool.Type,
			Function: ToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		}
	}

	// Call provider
	response, err := w.provider.SendChatRequest(ctx, typeMessages, typeTools, reasoning, disableThinking)

	// Calculate request duration AFTER the provider completes
	// This ensures we measure the full token generation time
	duration := time.Since(startTime)

	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to generate response")
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
		Usage: ChatUsage{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
			Cost:             response.Usage.Cost,
			CachedTokens:     response.Usage.CachedTokens,
			CacheWriteTokens: response.Usage.CacheWriteTokens,
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
			Message: Message{
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
					Function: ToolCallFunction{
						Name:      toolCall.Function.Name,
						Arguments: toolCall.Function.Arguments,
					},
				}
			}
		}

		// Fallback: some Mistral-family models emit tool calls as a
		// `[TOOL_CALLS]…` marker in the text content instead of the structured
		// tool_calls field. When tools were offered and nothing was parsed
		// structurally, recover them so the agent can act on the call instead
		// of treating it as a plain-text answer.
		if len(apiResponse.Choices[i].Message.ToolCalls) == 0 && len(tools) > 0 {
			if recovered, rest, recoveredOK := RecoverMistralToolCalls(apiResponse.Choices[i].Message.Content); recoveredOK {
				apiResponse.Choices[i].Message.ToolCalls = recovered
				apiResponse.Choices[i].Message.Content = rest
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

func (w *UnifiedProviderWrapper) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return w.provider.ListModels(ctx)
}

func (w *UnifiedProviderWrapper) SupportsVision() bool {
	return w.provider.SupportsVision()
}


// SupportsConversationalVision reports whether inline multimodal turns
// should embed the image. Defaults to false; overridden per client.
func (w *UnifiedProviderWrapper) SupportsConversationalVision() bool {
	return false
}
func (w *UnifiedProviderWrapper) GetVisionModel() string {
	// Delegate to the underlying provider if it supports vision model
	if visionProvider, ok := w.provider.(interface{ GetVisionModel() string }); ok {
		return visionProvider.GetVisionModel()
	}
	return ""
}

func (w *UnifiedProviderWrapper) SendVisionRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	// Convert API types to shared types
	typeMessages := make([]Message, len(messages))
	for i, msg := range messages {
		// Convert image data
		typeImages := make([]ImageData, len(msg.Images))
		for j, img := range msg.Images {
			typeImages[j] = ImageData{
				URL:    img.URL,
				Base64: img.Base64,
				Type:   img.Type,
			}
		}

		typeMessages[i] = Message{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Images:           typeImages,
		}
	}

	typeTools := make([]Tool, len(tools))
	for i, tool := range tools {
		typeTools[i] = Tool{
			Type: tool.Type,
			Function: ToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		}
	}

	// Call provider vision method
	response, err := w.provider.SendVisionRequest(ctx, typeMessages, typeTools, reasoning, disableThinking)
	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to send vision request")
	}

	// Convert response back to API types (same as SendChatRequest)
	apiResponse := &ChatResponse{
		ID:      response.ID,
		Object:  response.Object,
		Created: response.Created,
		Model:   response.Model,
		Usage: ChatUsage{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
			Cost:             response.Usage.Cost,
			CachedTokens:     response.Usage.CachedTokens,
			CacheWriteTokens: response.Usage.CacheWriteTokens,
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
			Message: Message{
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
					Function: ToolCallFunction{
						Name:      toolCall.Function.Name,
						Arguments: toolCall.Function.Arguments,
					},
				}
			}
		}

		// Fallback: some Mistral-family models emit tool calls as a
		// `[TOOL_CALLS]…` marker in the text content instead of the structured
		// tool_calls field. When tools were offered and nothing was parsed
		// structurally, recover them so the agent can act on the call instead
		// of treating it as a plain-text answer.
		if len(apiResponse.Choices[i].Message.ToolCalls) == 0 && len(tools) > 0 {
			if recovered, rest, recoveredOK := RecoverMistralToolCalls(apiResponse.Choices[i].Message.Content); recoveredOK {
				apiResponse.Choices[i].Message.ToolCalls = recovered
				apiResponse.Choices[i].Message.Content = rest
			}
		}
	}

	return apiResponse, nil
}

// SendChatRequestStream sends a streaming chat request (not yet implemented for unified providers)
func (w *UnifiedProviderWrapper) SendChatRequestStream(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
	// Convert API types to provider types
	providerMessages := make([]Message, len(messages))
	for i, msg := range messages {
		// Convert image data
		providerImages := make([]ImageData, len(msg.Images))
		for j, img := range msg.Images {
			providerImages[j] = ImageData{
				URL:    img.URL,
				Base64: img.Base64,
				Type:   img.Type,
			}
		}

		providerMessages[i] = Message{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Images:           providerImages,
		}
	}

	providerTools := make([]Tool, len(tools))
	for i, tool := range tools {
		providerTools[i] = Tool{
			Type: tool.Type,
			Function: ToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		}
	}

	// Create a wrapper callback that converts to our StreamCallback type
	providerCallback := func(content string, contentType string) {
		if callback != nil {
			callback(content, contentType)
		}
	}

	// Call provider's streaming method
	response, err := w.provider.SendChatRequestStream(ctx, providerMessages, providerTools, reasoning, disableThinking, providerCallback)
	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to send streaming request")
	}

	// Convert response back to API types
	apiResponse := &ChatResponse{
		ID:      response.ID,
		Object:  response.Object,
		Created: response.Created,
		Model:   response.Model,
		Choices: make([]Choice, len(response.Choices)),
		Usage: ChatUsage{
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
			TotalTokens:      response.Usage.TotalTokens,
			EstimatedCost:    response.Usage.EstimatedCost,
			Cost:             response.Usage.Cost,
			CachedTokens:     response.Usage.CachedTokens,
			CacheWriteTokens: response.Usage.CacheWriteTokens,
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
				Function: ToolCallFunction{
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
			Message: Message{
				Role:             choice.Message.Role,
				Content:          choice.Message.Content,
				ReasoningContent: choice.Message.ReasoningContent,
				Images:           images,
				ToolCalls:        toolCalls,
			},
			FinishReason: choice.FinishReason,
		}

		// Recover Mistral-family `[TOOL_CALLS]…` text-format tool calls that the
		// provider didn't translate into structured tool_calls (streamed path).
		if len(apiResponse.Choices[i].Message.ToolCalls) == 0 && len(tools) > 0 {
			if recovered, rest, recoveredOK := RecoverMistralToolCalls(apiResponse.Choices[i].Message.Content); recoveredOK {
				apiResponse.Choices[i].Message.ToolCalls = recovered
				apiResponse.Choices[i].Message.Content = rest
			}
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

// NewOpenRouterProvider is deprecated - import cycle resolved by moving to factory package
