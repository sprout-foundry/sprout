//go:build !js

package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// UseMockLLM, when true, causes the agent creation path to return a
// MockLLMProvider instead of the real provider. Set from the --mock-llm
// CLI flag.
var UseMockLLM bool

// MockLLMProvider implements api.ClientInterface with canned responses.
// Thread-safe: all mutable state is protected by mu.
type MockLLMProvider struct {
	mu                    sync.Mutex
	ResponsesByPrompt     map[string]string // substring match (case-insensitive) on last user message
	DefaultResponse       string
	CallCount             int
	model                 string
	debug                 bool
}

// NewMockLLMProvider creates a new mock LLM provider with sensible defaults.
func NewMockLLMProvider() *MockLLMProvider {
	return &MockLLMProvider{
		ResponsesByPrompt: make(map[string]string),
		DefaultResponse:   "", // empty means use the auto-generated default
	}
}

// getLastUserMessage extracts the text content of the last user message
// from the messages list.
func getLastUserMessage(messages []api.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(messages[i].Role, "user") {
			return messages[i].Content
		}
	}
	return ""
}

// resolveResponse picks the canned response for the given user message.
// Returns (response, matched) where matched is true if a custom response
// was found or a built-in stub matched.
func (m *MockLLMProvider) resolveResponse(userMsg string) (string, bool) {
	lower := strings.ToLower(userMsg)

	// 1. Check registered custom responses (substring match)
	m.mu.Lock()
	for key, resp := range m.ResponsesByPrompt {
		if strings.Contains(lower, strings.ToLower(key)) {
			m.mu.Unlock()
			return resp, true
		}
	}
	m.mu.Unlock()

	// 2. Built-in stubs
	if strings.Contains(lower, "list files") || strings.Contains(lower, " ls ") || strings.HasSuffix(lower, " ls") {
		return "[stub] ls -la\n[stub output]", true
	}
	if strings.Contains(lower, "echo") {
		return "[stub] echo response", true
	}

	return "", false
}

// buildDefaultResponse generates the fallback response from the user message.
func buildDefaultResponse(userMsg string) string {
	truncated := userMsg
	if len(truncated) > 100 {
		truncated = truncated[:100]
	}
	return fmt.Sprintf("[stub LLM response]\n\nI received: %s", truncated)
}

// makeChatResponse wraps a text response in a ChatResponse.
func makeChatResponse(content, model string) *api.ChatResponse {
	return &api.ChatResponse{
		ID:      "mock-response-id",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   model,
		Choices: []api.ChatChoice{
			{
				Index: 0,
				Message: api.Message{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: api.ChatUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			EstimatedCost:    0.0,
			Cost:             0.0,
		},
	}
}

// SendChatRequest sends a chat request and returns a canned response.
func (m *MockLLMProvider) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m.mu.Lock()
	m.CallCount++
	m.mu.Unlock()

	userMsg := getLastUserMessage(messages)
	resp, matched := m.resolveResponse(userMsg)
	if !matched {
		resp = m.getDefaultResponse(userMsg)
	}

	return makeChatResponse(resp, m.GetModel()), nil
}

// getDefaultResponse returns the configured default or an auto-generated one.
func (m *MockLLMProvider) getDefaultResponse(userMsg string) string {
	m.mu.Lock()
	def := m.DefaultResponse
	m.mu.Unlock()
	if def != "" {
		return def
	}
	return buildDefaultResponse(userMsg)
}

// SendChatRequestStream streams a canned response.
func (m *MockLLMProvider) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	resp, err := m.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
	if err != nil {
		return nil, err
	}

	// Send the full response as a single chunk
	if callback != nil {
		content := resp.Choices[0].Message.Content
		callback(content, "assistant_text")
	}
	return resp, nil
}

// CheckConnection always succeeds.
func (m *MockLLMProvider) CheckConnection() error {
	return nil
}

// SetDebug sets debug mode.
func (m *MockLLMProvider) SetDebug(debug bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debug = debug
}

// SetModel sets the model name.
func (m *MockLLMProvider) SetModel(model string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = model
	return nil
}

// GetModel returns the current model name.
func (m *MockLLMProvider) GetModel() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.model == "" {
		return "mock-model"
	}
	return m.model
}

// GetProvider returns the provider name.
func (m *MockLLMProvider) GetProvider() string {
	return "mock"
}

// GetModelContextLimit returns a fixed context limit.
func (m *MockLLMProvider) GetModelContextLimit() (int, error) {
	return 4096, nil
}

// ListModels returns a single mock model.
func (m *MockLLMProvider) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return []api.ModelInfo{
		{ID: "mock-model", Name: "mock-model", ContextLength: 4096},
	}, nil
}

// SupportsVision returns false.
func (m *MockLLMProvider) SupportsVision() bool {
	return false
}

// SupportsConversationalVision returns false; the mock never participates
// in inline multimodal turns.
func (m *MockLLMProvider) SupportsConversationalVision() bool {
	return false
}

// GetVisionModel returns empty string.
func (m *MockLLMProvider) GetVisionModel() string {
	return ""
}

// SendVisionRequest returns an error (vision not supported).
func (m *MockLLMProvider) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("vision not supported in mock provider")
}

// GetLastTPS returns a mock TPS value.
func (m *MockLLMProvider) GetLastTPS() float64 {
	return 100.0
}

// GetAverageTPS returns a mock TPS value.
func (m *MockLLMProvider) GetAverageTPS() float64 {
	return 100.0
}

// GetTPSStats returns mock TPS stats.
func (m *MockLLMProvider) GetTPSStats() map[string]float64 {
	return map[string]float64{"last": 100.0, "average": 100.0}
}

// ResetTPSStats is a no-op.
func (m *MockLLMProvider) ResetTPSStats() {
}
