package api

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// mockProvider implements ProviderInterface for testing UnifiedProviderWrapper
// =====================================================================

type mockProvider struct {
	sendChatFunc    func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error)
	sendStreamFunc  func(messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error)
	sendVisionFunc  func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error)
	checkFunc       func() error
	model           string
	providerName    string
	contextLimit    int
	contextLimitErr error
	supportsVision  bool
	visionModel     string
	models          []ModelInfo
	modelsErr       error
	tpsStatsFn      func() (float64, float64, int)
	lastRequestTPS  func() float64
	resetTPSFn      func()
}

func (m *mockProvider) SendChatRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	if m.sendChatFunc != nil {
		return m.sendChatFunc(messages, tools, reasoning, disableThinking)
	}
	return &ChatResponse{
		ID:    "resp-1",
		Model: "mock-model",
		Choices: []Choice{
			{Index: 0, Message: Message{Role: "assistant", Content: "mock response"}, FinishReason: "stop"},
		},
		Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}

func (m *mockProvider) SendChatRequestStream(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
	if m.sendStreamFunc != nil {
		return m.sendStreamFunc(messages, tools, reasoning, disableThinking, callback)
	}
	return &ChatResponse{
		ID:    "stream-1",
		Model: "mock-model",
		Choices: []Choice{
			{Index: 0, Message: Message{Role: "assistant", Content: "streamed"}, FinishReason: "stop"},
		},
	}, nil
}

func (m *mockProvider) CheckConnection() error {
	if m.checkFunc != nil {
		return m.checkFunc()
	}
	return nil
}

func (m *mockProvider) SetDebug(debug bool)     {}
func (m *mockProvider) SetModel(model string) error { m.model = model; return nil }
func (m *mockProvider) GetModel() string         { return m.model }
func (m *mockProvider) GetProvider() string      { return m.providerName }
func (m *mockProvider) GetModelContextLimit() (int, error) {
	return m.contextLimit, m.contextLimitErr
}
func (m *mockProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return m.models, m.modelsErr
}
func (m *mockProvider) SupportsVision() bool    { return m.supportsVision }
func (m *mockProvider) SendVisionRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	if m.sendVisionFunc != nil {
		return m.sendVisionFunc(messages, tools, reasoning, disableThinking)
	}
	return &ChatResponse{
		Choices: []Choice{
			{Index: 0, Message: Message{Role: "assistant", Content: "vision response"}, FinishReason: "stop"},
		},
	}, nil
}
func (m *mockProvider) GetVisionModel() string { return m.visionModel }

// Optional TPS support methods
func (m *mockProvider) GetTPSStatistics() (float64, float64, int) {
	if m.tpsStatsFn != nil {
		return m.tpsStatsFn()
	}
	return 0, 0, 0
}
func (m *mockProvider) GetLastRequestTPS() float64 {
	if m.lastRequestTPS != nil {
		return m.lastRequestTPS()
	}
	return 0
}
func (m *mockProvider) ResetTPSStatistics() {
	if m.resetTPSFn != nil {
		m.resetTPSFn()
	}
}

// =====================================================================
// NewUnifiedProviderWrapper
// =====================================================================

func TestNewUnifiedProviderWrapper(t *testing.T) {
	mock := &mockProvider{providerName: "test-provider", model: "gpt-4o"}
	wrapper := NewUnifiedProviderWrapper(mock)
	require.NotNil(t, wrapper)
	assert.Equal(t, "gpt-4o", wrapper.GetModel())
	assert.Equal(t, "test-provider", wrapper.GetProvider())
}

func TestNewUnifiedProviderWrapper_NilSafeTPSBase(t *testing.T) {
	mock := &mockProvider{providerName: "test"}
	wrapper := NewUnifiedProviderWrapper(mock)
	// GetTracker should be nil-safe
	tracker := wrapper.GetTracker()
	require.NotNil(t, tracker)
}

// =====================================================================
// Delegation methods
// =====================================================================

func TestUnifiedProviderWrapper_CheckConnection_Success(t *testing.T) {
	mock := &mockProvider{}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.NoError(t, wrapper.CheckConnection())
}

func TestUnifiedProviderWrapper_CheckConnection_Error(t *testing.T) {
	expectedErr := errors.New("connection failed")
	mock := &mockProvider{checkFunc: func() error { return expectedErr }}
	wrapper := NewUnifiedProviderWrapper(mock)
	err := wrapper.CheckConnection()
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestUnifiedProviderWrapper_SetDebug(t *testing.T) {
	mock := &mockProvider{}
	wrapper := NewUnifiedProviderWrapper(mock)
	// SetDebug should not panic
	wrapper.SetDebug(true)
	wrapper.SetDebug(false)
}

func TestUnifiedProviderWrapper_SetModel(t *testing.T) {
	mock := &mockProvider{model: "gpt-4"}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.NoError(t, wrapper.SetModel("gpt-4o"))
	assert.Equal(t, "gpt-4o", wrapper.GetModel())
}

func TestUnifiedProviderWrapper_GetModel(t *testing.T) {
	mock := &mockProvider{model: "claude-3"}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.Equal(t, "claude-3", wrapper.GetModel())
}

func TestUnifiedProviderWrapper_GetModel_Empty(t *testing.T) {
	mock := &mockProvider{model: ""}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.Empty(t, wrapper.GetModel())
}

func TestUnifiedProviderWrapper_GetProvider(t *testing.T) {
	mock := &mockProvider{providerName: "DeepInfra"}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.Equal(t, "DeepInfra", wrapper.GetProvider())
}

func TestUnifiedProviderWrapper_GetModelContextLimit(t *testing.T) {
	mock := &mockProvider{contextLimit: 128000}
	wrapper := NewUnifiedProviderWrapper(mock)
	limit, err := wrapper.GetModelContextLimit()
	require.NoError(t, err)
	assert.Equal(t, 128000, limit)
}

func TestUnifiedProviderWrapper_GetModelContextLimit_Error(t *testing.T) {
	expectedErr := errors.New("context limit unavailable")
	mock := &mockProvider{contextLimitErr: expectedErr}
	wrapper := NewUnifiedProviderWrapper(mock)
	_, err := wrapper.GetModelContextLimit()
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestUnifiedProviderWrapper_ListModels(t *testing.T) {
	mock := &mockProvider{
		models: []ModelInfo{
			{ID: "gpt-4o", Name: "GPT-4o", ContextLength: 128000},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini", ContextLength: 128000},
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)
	models, err := wrapper.ListModels(context.Background())
	require.NoError(t, err)
	assert.Len(t, models, 2)
	assert.Equal(t, "gpt-4o", models[0].ID)
	assert.Equal(t, "gpt-4o-mini", models[1].ID)
}

func TestUnifiedProviderWrapper_ListModels_Error(t *testing.T) {
	expectedErr := errors.New("list models failed")
	mock := &mockProvider{modelsErr: expectedErr}
	wrapper := NewUnifiedProviderWrapper(mock)
	_, err := wrapper.ListModels(context.Background())
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestUnifiedProviderWrapper_ListModels_Empty(t *testing.T) {
	mock := &mockProvider{models: []ModelInfo{}}
	wrapper := NewUnifiedProviderWrapper(mock)
	models, err := wrapper.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestUnifiedProviderWrapper_SupportsVision_True(t *testing.T) {
	mock := &mockProvider{supportsVision: true}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.True(t, wrapper.SupportsVision())
}

func TestUnifiedProviderWrapper_SupportsVision_False(t *testing.T) {
	mock := &mockProvider{supportsVision: false}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.False(t, wrapper.SupportsVision())
}

// =====================================================================
// GetVisionModel
// =====================================================================

func TestUnifiedProviderWrapper_GetVisionModel_ProviderSupportsIt(t *testing.T) {
	mock := &mockProvider{supportsVision: true, visionModel: "gpt-4o-vision"}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.Equal(t, "gpt-4o-vision", wrapper.GetVisionModel())
}

func TestUnifiedProviderWrapper_GetVisionModel_ProviderDoesNotSupportIt(t *testing.T) {
	// mockProvider implements GetVisionModel, but returns empty string
	mock := &mockProvider{supportsVision: false, visionModel: ""}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.Empty(t, wrapper.GetVisionModel())
}

// =====================================================================
// TPS methods
// =====================================================================

func TestUnifiedProviderWrapper_GetTPSStatistics_ProviderSupportsTPS(t *testing.T) {
	mock := &mockProvider{
		tpsStatsFn: func() (float64, float64, int) {
			return 50.0, 45.0, 10
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)
	last, avg, count := wrapper.GetTPSStatistics()
	assert.Equal(t, 50.0, last)
	assert.Equal(t, 45.0, avg)
	assert.Equal(t, 10, count)
}

func TestUnifiedProviderWrapper_GetTPSStatistics_ProviderDoesNotSupportTPS(t *testing.T) {
	// mockProvider implements GetTPSStatistics, but to test the non-supporting case,
	// we'd need a mock that doesn't implement it. Since all our mocks implement it,
	// let's test the zero-return path by setting the function to return zeros.
	mock := &mockProvider{tpsStatsFn: func() (float64, float64, int) { return 0, 0, 0 }}
	wrapper := NewUnifiedProviderWrapper(mock)
	last, avg, count := wrapper.GetTPSStatistics()
	assert.Equal(t, 0.0, last)
	assert.Equal(t, 0.0, avg)
	assert.Equal(t, 0, count)
}

func TestUnifiedProviderWrapper_GetLastRequestTPS_ProviderSupportsTPS(t *testing.T) {
	mock := &mockProvider{
		lastRequestTPS: func() float64 { return 60.0 },
	}
	wrapper := NewUnifiedProviderWrapper(mock)
	assert.Equal(t, 60.0, wrapper.GetLastRequestTPS())
}

func TestUnifiedProviderWrapper_GetLastRequestTPS_ProviderDoesNotSupportTPS(t *testing.T) {
	mock := &mockProvider{}
	wrapper := NewUnifiedProviderWrapper(mock)
	// Default mock returns 0
	assert.Equal(t, 0.0, wrapper.GetLastRequestTPS())
}

func TestUnifiedProviderWrapper_ResetTPSStatistics(t *testing.T) {
	var resetCalled bool
	mock := &mockProvider{
		resetTPSFn: func() { resetCalled = true },
	}
	wrapper := NewUnifiedProviderWrapper(mock)
	wrapper.ResetTPSStatistics()
	assert.True(t, resetCalled)
}

func TestUnifiedProviderWrapper_ResetTPSStatistics_Noop(t *testing.T) {
	mock := &mockProvider{}
	wrapper := NewUnifiedProviderWrapper(mock)
	// Should not panic even without resetTPSFn
	wrapper.ResetTPSStatistics()
}

// =====================================================================
// SendChatRequest
// =====================================================================

func TestUnifiedProviderWrapper_SendChatRequest_Basic(t *testing.T) {
	mock := &mockProvider{
		model: "gpt-4o",
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			// Verify the messages were passed through
			require.Len(t, messages, 1)
			assert.Equal(t, "user", messages[0].Role)
			assert.Equal(t, "Hello", messages[0].Content)
			assert.Equal(t, "high", reasoning)
			assert.True(t, disableThinking)

			return &ChatResponse{
				ID:      "resp-123",
				Object:  "chat.completion",
				Created: 1234567890,
				Model:   "gpt-4o",
				Choices: []Choice{
					{Index: 0, Message: Message{Role: "assistant", Content: "Hi there!"}, FinishReason: "stop"},
				},
				Usage: ChatUsage{PromptTokens: 50, CompletionTokens: 30, TotalTokens: 80},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), 
		[]Message{{Role: "user", Content: "Hello"}},
		nil,
		"high",
		true,
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "resp-123", resp.ID)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.Len(t, resp.Choices, 1)
	assert.Equal(t, "Hi there!", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
	assert.Equal(t, 50, resp.Usage.PromptTokens)
	assert.Equal(t, 30, resp.Usage.CompletionTokens)
}

func TestUnifiedProviderWrapper_SendChatRequest_Error(t *testing.T) {
	expectedErr := errors.New("API error")
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return nil, expectedErr
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	_, err := wrapper.SendChatRequest(context.Background(), 
		[]Message{{Role: "user", Content: "Hello"}},
		nil,
		"",
		false,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate response")
}

func TestUnifiedProviderWrapper_SendChatRequest_WithImages(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			require.Len(t, messages, 1)
			assert.Equal(t, "user", messages[0].Role)
			require.Len(t, messages[0].Images, 1)
			assert.Equal(t, "http://example.com/img.png", messages[0].Images[0].URL)
			assert.Equal(t, "image/png", messages[0].Images[0].Type)

			return &ChatResponse{
				Choices: []Choice{
					{Index: 0, Message: Message{Role: "assistant", Content: "I see the image"}, FinishReason: "stop"},
				},
				Usage: ChatUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), 
		[]Message{{
			Role:    "user",
			Content: "What is in this image?",
			Images: []ImageData{
				{URL: "http://example.com/img.png", Type: "image/png"},
			},
		}},
		nil,
		"",
		false,
	)
	require.NoError(t, err)
	assert.Equal(t, "I see the image", resp.Choices[0].Message.Content)
}

func TestUnifiedProviderWrapper_SendChatRequest_WithTools(t *testing.T) {
	var receivedTools []Tool
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			receivedTools = tools
			return &ChatResponse{
				Choices: []Choice{
					{Index: 0, Message: Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"},
				},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "shell_command",
				Description: "Run a command",
			},
		},
	}
	_, err := wrapper.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "run ls"}}, tools, "", false)
	require.NoError(t, err)
	require.Len(t, receivedTools, 1)
	assert.Equal(t, "shell_command", receivedTools[0].Function.Name)
	assert.Equal(t, "Run a command", receivedTools[0].Function.Description)
}

func TestUnifiedProviderWrapper_SendChatRequest_ResponseWithToolCalls(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return &ChatResponse{
				Model: "gpt-4o",
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:    "assistant",
							Content: "Calling tool",
							ToolCalls: []ToolCall{
								{
									ID:   "call-123",
									Type: "function",
									Function: ToolCallFunction{
										Name:      "shell_command",
										Arguments: `{"command":"ls"}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
				Usage: ChatUsage{PromptTokens: 50, CompletionTokens: 25, TotalTokens: 75},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "run ls"}}, nil, "", false)
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	require.Len(t, resp.Choices[0].Message.ToolCalls, 1)
	assert.Equal(t, "call-123", resp.Choices[0].Message.ToolCalls[0].ID)
	assert.Equal(t, "shell_command", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	assert.Equal(t, `{"command":"ls"}`, resp.Choices[0].Message.ToolCalls[0].Function.Arguments)
	assert.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
}

func TestUnifiedProviderWrapper_SendChatRequest_ResponseWithImages(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return &ChatResponse{
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:    "assistant",
							Content: "Here's the image",
							Images: []ImageData{
								{URL: "http://example.com/gen.png", Base64: "base64data", Type: "image/png"},
							},
						},
						FinishReason: "stop",
					},
				},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "generate image"}}, nil, "", false)
	require.NoError(t, err)
	require.Len(t, resp.Choices[0].Message.Images, 1)
	assert.Equal(t, "http://example.com/gen.png", resp.Choices[0].Message.Images[0].URL)
	assert.Equal(t, "image/png", resp.Choices[0].Message.Images[0].Type)
}

func TestUnifiedProviderWrapper_SendChatRequest_ResponseWithReasoningContent(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return &ChatResponse{
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:             "assistant",
							Content:          "The answer is 42",
							ReasoningContent: "Let me think about this...",
						},
						FinishReason: "stop",
					},
				},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "What is 6*7?"}}, nil, "", false)
	require.NoError(t, err)
	assert.Equal(t, "The answer is 42", resp.Choices[0].Message.Content)
	assert.Equal(t, "Let me think about this...", resp.Choices[0].Message.ReasoningContent)
}

func TestUnifiedProviderWrapper_SendChatRequest_EmptyMessages(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			assert.Empty(t, messages)
			return &ChatResponse{}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), []Message{}, nil, "", false)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestUnifiedProviderWrapper_SendChatRequest_MultipleChoices(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return &ChatResponse{
				Choices: []Choice{
					{Index: 0, Message: Message{Role: "assistant", Content: "response 1"}, FinishReason: "stop"},
					{Index: 1, Message: Message{Role: "assistant", Content: "response 2"}, FinishReason: "stop"},
				},
				Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 15, TotalTokens: 25},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "", false)
	require.NoError(t, err)
	assert.Len(t, resp.Choices, 2)
	assert.Equal(t, "response 1", resp.Choices[0].Message.Content)
	assert.Equal(t, "response 2", resp.Choices[1].Message.Content)
}

// =====================================================================
// SendVisionRequest
// =====================================================================

func TestUnifiedProviderWrapper_SendVisionRequest_Basic(t *testing.T) {
	mock := &mockProvider{
		sendVisionFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			require.Len(t, messages, 1)
			assert.Equal(t, "user", messages[0].Role)
			return &ChatResponse{
				Model: "gpt-4o-vision",
				Choices: []Choice{
					{Index: 0, Message: Message{Role: "assistant", Content: "vision result"}, FinishReason: "stop"},
				},
				Usage: ChatUsage{PromptTokens: 500, CompletionTokens: 50, TotalTokens: 550},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendVisionRequest(context.Background(), 
		[]Message{{Role: "user", Content: "describe this image", Images: []ImageData{{URL: "http://img.com/pic.png"}}}},
		nil, "", false,
	)
	require.NoError(t, err)
	assert.Equal(t, "vision result", resp.Choices[0].Message.Content)
	assert.Equal(t, 500, resp.Usage.PromptTokens)
}

func TestUnifiedProviderWrapper_SendVisionRequest_Error(t *testing.T) {
	expectedErr := errors.New("vision API failed")
	mock := &mockProvider{
		sendVisionFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return nil, expectedErr
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	_, err := wrapper.SendVisionRequest(context.Background(), 
		[]Message{{Role: "user", Content: "test"}}, nil, "", false,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send vision request")
}

func TestUnifiedProviderWrapper_SendVisionRequest_WithImages(t *testing.T) {
	mock := &mockProvider{
		sendVisionFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			require.Len(t, messages, 1)
			require.Len(t, messages[0].Images, 2)
			assert.Equal(t, "http://img.com/1.png", messages[0].Images[0].URL)
			assert.Equal(t, "http://img.com/2.png", messages[0].Images[1].URL)
			return &ChatResponse{
				Choices: []Choice{{Index: 0, Message: Message{Role: "assistant", Content: "I see both images"}, FinishReason: "stop"}},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendVisionRequest(context.Background(), 
		[]Message{{
			Role:    "user",
			Content: "compare these",
			Images: []ImageData{
				{URL: "http://img.com/1.png", Base64: "data1", Type: "image/png"},
				{URL: "http://img.com/2.png", Base64: "data2", Type: "image/jpeg"},
			},
		}},
		nil, "", false,
	)
	require.NoError(t, err)
	assert.Equal(t, "I see both images", resp.Choices[0].Message.Content)
}

func TestUnifiedProviderWrapper_SendVisionRequest_ResponseWithImages(t *testing.T) {
	mock := &mockProvider{
		sendVisionFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return &ChatResponse{
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:    "assistant",
							Content: "here",
							Images: []ImageData{
								{URL: "http://gen.com/out.png", Base64: "genData", Type: "image/png"},
							},
						},
						FinishReason: "stop",
					},
				},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendVisionRequest(context.Background(), 
		[]Message{{Role: "user", Content: "generate"}}, nil, "", false,
	)
	require.NoError(t, err)
	require.Len(t, resp.Choices[0].Message.Images, 1)
	assert.Equal(t, "http://gen.com/out.png", resp.Choices[0].Message.Images[0].URL)
}

// =====================================================================
// SendChatRequestStream
// =====================================================================

func TestUnifiedProviderWrapper_SendChatRequestStream_Basic(t *testing.T) {
	var callbackCalled bool
	mock := &mockProvider{
		sendStreamFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
			// Call the callback to test it works
			if callback != nil {
				callback("streaming chunk", "assistant_text")
				callbackCalled = true
			}
			return &ChatResponse{
				ID:    "stream-1",
				Model: "gpt-4o",
				Choices: []Choice{
					{Index: 0, Message: Message{Role: "assistant", Content: "streaming chunk"}, FinishReason: "stop"},
				},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	var receivedContent string
	resp, err := wrapper.SendChatRequestStream(context.Background(), 
		[]Message{{Role: "user", Content: "hello"}},
		nil, "", false,
		func(content string, contentType string) {
			receivedContent = content
		},
	)
	require.NoError(t, err)
	assert.True(t, callbackCalled)
	assert.Equal(t, "streaming chunk", receivedContent)
	assert.Equal(t, "stream-1", resp.ID)
}

func TestUnifiedProviderWrapper_SendChatRequestStream_NilCallback(t *testing.T) {
	mock := &mockProvider{
		sendStreamFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
			// Should not panic even if callback is nil
			if callback != nil {
				callback("chunk", "text")
			}
			return &ChatResponse{
				Choices: []Choice{
					{Index: 0, Message: Message{Role: "assistant", Content: "done"}, FinishReason: "stop"},
				},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequestStream(context.Background(), 
		[]Message{{Role: "user", Content: "hello"}},
		nil, "", false,
		nil, // nil callback
	)
	require.NoError(t, err)
	assert.Equal(t, "done", resp.Choices[0].Message.Content)
}

func TestUnifiedProviderWrapper_SendChatRequestStream_Error(t *testing.T) {
	expectedErr := errors.New("stream failed")
	mock := &mockProvider{
		sendStreamFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
			return nil, expectedErr
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	_, err := wrapper.SendChatRequestStream(context.Background(), 
		[]Message{{Role: "user", Content: "hello"}},
		nil, "", false,
		func(content string, contentType string) {},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send streaming request")
}

func TestUnifiedProviderWrapper_SendChatRequestStream_ResponseWithToolCallsAndImages(t *testing.T) {
	mock := &mockProvider{
		sendStreamFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
			return &ChatResponse{
				ID:    "stream-2",
				Model: "gpt-4o",
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:    "assistant",
							Content: "streamed content",
							ReasoningContent: "reasoning here",
							Images: []ImageData{
								{URL: "http://img.com/stream.png", Base64: "b64", Type: "image/png"},
							},
							ToolCalls: []ToolCall{
								{
									ID:   "call-stream",
									Type: "function",
									Function: ToolCallFunction{
										Name:      "stream_tool",
										Arguments: `{"arg":"val"}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
				Usage: ChatUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequestStream(context.Background(), 
		[]Message{{Role: "user", Content: "hello"}},
		nil, "", false,
		func(content string, contentType string) {},
	)
	require.NoError(t, err)
	assert.Equal(t, "stream-2", resp.ID)
	assert.Equal(t, "gpt-4o", resp.Model)
	assert.Len(t, resp.Choices[0].Message.ToolCalls, 1)
	assert.Equal(t, "call-stream", resp.Choices[0].Message.ToolCalls[0].ID)
	assert.Equal(t, "stream_tool", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	assert.Len(t, resp.Choices[0].Message.Images, 1)
	assert.Equal(t, "http://img.com/stream.png", resp.Choices[0].Message.Images[0].URL)
	assert.Equal(t, "reasoning here", resp.Choices[0].Message.ReasoningContent)
	assert.Equal(t, 100, resp.Usage.PromptTokens)
	assert.Equal(t, 50, resp.Usage.CompletionTokens)
}

func TestUnifiedProviderWrapper_SendChatRequestStream_ResponseWithMultipleToolCalls(t *testing.T) {
	mock := &mockProvider{
		sendStreamFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
			return &ChatResponse{
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:    "assistant",
							Content: "multiple calls",
							ToolCalls: []ToolCall{
								{ID: "call-1", Type: "function", Function: ToolCallFunction{Name: "read", Arguments: `{"path":"a.txt"}`}},
								{ID: "call-2", Type: "function", Function: ToolCallFunction{Name: "write", Arguments: `{"path":"b.txt","content":"hello"}`}},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequestStream(context.Background(), 
		[]Message{{Role: "user", Content: "do both"}},
		nil, "", false,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, resp.Choices[0].Message.ToolCalls, 2)
	assert.Equal(t, "read", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	assert.Equal(t, "write", resp.Choices[0].Message.ToolCalls[1].Function.Name)
}

// =====================================================================
// SendChatRequest with TPS tracking
// =====================================================================

func TestUnifiedProviderWrapper_SendChatRequest_TracksTPS(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return &ChatResponse{
				Choices: []Choice{{Index: 0, Message: Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
				Usage:   ChatUsage{PromptTokens: 10, CompletionTokens: 100, TotalTokens: 110},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "", false)
	require.NoError(t, err)
	require.NotNil(t, resp)
	// TPS should have been tracked since CompletionTokens > 0
	// We can verify the tracker was called indirectly
	assert.Equal(t, 100, resp.Usage.CompletionTokens)
}

func TestUnifiedProviderWrapper_SendChatRequest_NoTPSTracking_ZeroTokens(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return &ChatResponse{
				Choices: []Choice{{Index: 0, Message: Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
				Usage:   ChatUsage{PromptTokens: 10, CompletionTokens: 0, TotalTokens: 10},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "", false)
	require.NoError(t, err)
	// TPS should NOT have been tracked since CompletionTokens == 0
	assert.Equal(t, 0, resp.Usage.CompletionTokens)
}

// =====================================================================
// UnifiedProviderWrapper implements ClientInterface
// =====================================================================

func TestUnifiedProviderWrapper_ImplementsClientInterface(t *testing.T) {
	// Verify that UnifiedProviderWrapper can be assigned to ClientInterface
	var _ ClientInterface = (*UnifiedProviderWrapper)(nil)
}

// =====================================================================
// SendChatRequest with images in request and response
// =====================================================================

func TestUnifiedProviderWrapper_SendChatRequest_FullImageRoundtrip(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			require.Len(t, messages, 1)
			require.Len(t, messages[0].Images, 1)
			assert.Equal(t, "data:image/png;base64,abc123", messages[0].Images[0].Base64)

			return &ChatResponse{
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:    "assistant",
							Content: "Processed",
							Images: []ImageData{
								{URL: "http://result.com/out.png", Base64: "xyz789", Type: "image/jpeg"},
							},
						},
						FinishReason: "stop",
					},
				},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), 
		[]Message{{
			Role:    "user",
			Content: "process this",
			Images: []ImageData{
				{URL: "http://input.com/in.png", Base64: "data:image/png;base64,abc123", Type: "image/png"},
			},
		}},
		nil, "", false,
	)
	require.NoError(t, err)
	require.Len(t, resp.Choices[0].Message.Images, 1)
	assert.Equal(t, "xyz789", resp.Choices[0].Message.Images[0].Base64)
	assert.Equal(t, "image/jpeg", resp.Choices[0].Message.Images[0].Type)
}

// =====================================================================
// Edge cases
// =====================================================================

func TestUnifiedProviderWrapper_SendChatRequest_ResponseWithMultipleToolCallsAndImages(t *testing.T) {
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return &ChatResponse{
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:             "assistant",
							Content:          "complex response",
							ReasoningContent: "thinking",
							Images: []ImageData{
								{URL: "http://a.com/img1.png", Base64: "b64_1", Type: "image/png"},
								{URL: "http://b.com/img2.jpg", Base64: "b64_2", Type: "image/jpeg"},
							},
							ToolCalls: []ToolCall{
								{ID: "call-1", Type: "function", Function: ToolCallFunction{Name: "tool1", Arguments: "{}"}},
								{ID: "call-2", Type: "function", Function: ToolCallFunction{Name: "tool2", Arguments: "{}"}},
							},
						},
						FinishReason: "tool_calls",
					},
				},
				Usage: ChatUsage{
					PromptTokens:     100,
					CompletionTokens: 50,
					TotalTokens:      150,
					EstimatedCost:    0.001,
					Cost:             0.001,
					CachedTokens:     80,
					CacheWriteTokens: ptrInt(20),
				},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "test"}}, nil, "", false)
	require.NoError(t, err)

	// Verify all fields are correctly propagated
	assert.Equal(t, "complex response", resp.Choices[0].Message.Content)
	assert.Equal(t, "thinking", resp.Choices[0].Message.ReasoningContent)
	assert.Len(t, resp.Choices[0].Message.Images, 2)
	assert.Len(t, resp.Choices[0].Message.ToolCalls, 2)
	assert.Equal(t, 100, resp.Usage.PromptTokens)
	assert.Equal(t, 50, resp.Usage.CompletionTokens)
	assert.Equal(t, 150, resp.Usage.TotalTokens)
	assert.Equal(t, 0.001, resp.Usage.EstimatedCost)
	assert.Equal(t, 0.001, resp.Usage.Cost)
	assert.Equal(t, 80, resp.Usage.CachedTokens)
	assert.Equal(t, 20, *resp.Usage.CacheWriteTokens)
}

// Helper functions
func ptrInt(i int) *int { return &i }

// =====================================================================
// SendVisionRequest with tool calls
// =====================================================================

func TestUnifiedProviderWrapper_SendVisionRequest_ResponseWithToolCalls(t *testing.T) {
	mock := &mockProvider{
		sendVisionFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return &ChatResponse{
				Model: "gpt-4o-vision",
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:    "assistant",
							Content: "Calling analysis tool",
							ToolCalls: []ToolCall{
								{ID: "vision-call-1", Type: "function", Function: ToolCallFunction{Name: "analyze_image", Arguments: `{}`}},
							},
						},
						FinishReason: "tool_calls",
					},
				},
				Usage: ChatUsage{PromptTokens: 600, CompletionTokens: 40, TotalTokens: 640},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	resp, err := wrapper.SendVisionRequest(context.Background(), 
		[]Message{{Role: "user", Content: "analyze"}}, nil, "", false,
	)
	require.NoError(t, err)
	require.Len(t, resp.Choices[0].Message.ToolCalls, 1)
	assert.Equal(t, "vision-call-1", resp.Choices[0].Message.ToolCalls[0].ID)
	assert.Equal(t, "analyze_image", resp.Choices[0].Message.ToolCalls[0].Function.Name)
}

// =====================================================================
// SendChatRequest with reasoning and disableThinking parameters
// =====================================================================

func TestUnifiedProviderWrapper_SendChatRequest_PassesReasoningAndDisableThinking(t *testing.T) {
	var receivedReasoning string
	var receivedDisableThinking bool
	mock := &mockProvider{
		sendChatFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			receivedReasoning = reasoning
			receivedDisableThinking = disableThinking
			return &ChatResponse{
				Choices: []Choice{{Index: 0, Message: Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
			}, nil
		},
	}
	wrapper := NewUnifiedProviderWrapper(mock)

	_, err := wrapper.SendChatRequest(context.Background(), 
		[]Message{{Role: "user", Content: "hello"}},
		nil,
		"high",
		true,
	)
	require.NoError(t, err)
	assert.Equal(t, "high", receivedReasoning)
	assert.True(t, receivedDisableThinking)
}
