package agent

import (
	"context"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestSproutProviderCurrentClient tests that sproutProvider.currentClient()
// returns the agent's live client when available, and falls back to the snapshot
// when the agent returns nil.
func TestSproutProviderCurrentClient(t *testing.T) {
	// Create a minimal agent with a mock client
	configManager, err := configuration.NewManagerSilent()
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}

	agent := &Agent{
		configManager: configManager,
		state:         NewAgentStateManager(false),
	}

	// Create a mock client
	mockClient := &MockClient{
		model: "test-model",
	}

	// Create a sproutProvider with the agent and mock client
	provider, err := NewSproutProvider(agent, mockClient)
	if err != nil {
		t.Fatalf("failed to create sprout provider: %v", err)
	}

	// Verify that currentClient returns the mock client
	c := provider.(*sproutProvider).currentClient()
	if c == nil {
		t.Error("expected currentClient to return a client, got nil")
	}
	if c.GetModel() != "test-model" {
		t.Errorf("expected currentClient to return mockClient with model 'test-model', got model '%s'", c.GetModel())
	}

	// Now swap the agent's client to a new mock client
	newMockClient := &MockClient{
		model: "new-model",
	}
	agent.setClient(newMockClient, api.OpenAIClientType)

	// Verify that currentClient now returns the new mock client
	c = provider.(*sproutProvider).currentClient()
	if c == nil {
		t.Error("expected currentClient to return a client after swap, got nil")
	}
	if c.GetModel() != "new-model" {
		t.Errorf("expected currentClient to return newMockClient with model 'new-model' after swap, got model '%s'", c.GetModel())
	}

	// Verify that the old mock client is no longer returned
	if c.GetModel() == "test-model" {
		t.Error("currentClient should not return the old mock client after swap")
	}
}

// TestSproutProviderCurrentClient_NilAgent tests that currentClient falls back
// to the snapshot client when the agent is nil.
func TestSproutProviderCurrentClient_NilAgent(t *testing.T) {
	// Create a sproutProvider with a nil agent
	mockClient := &MockClient{
		model: "test-model",
	}
	provider, err := NewSproutProvider(nil, mockClient)
	if err != nil {
		t.Fatalf("failed to create sprout provider: %v", err)
	}

	// Verify that currentClient returns the snapshot client
	c := provider.(*sproutProvider).currentClient()
	if c == nil {
		t.Error("expected currentClient to return a client when agent is nil, got nil")
	}
	if c.GetModel() != "test-model" {
		t.Errorf("expected currentClient to return snapshot client with model 'test-model' when agent is nil, got model '%s'", c.GetModel())
	}
}

// MockClient is a minimal mock implementation of api.ClientInterface for testing.
type MockClient struct {
	model string
}

func (m *MockClient) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return &api.ChatResponse{
		Choices: []api.ChatChoice{
			{
				Message: api.Message{
					Role:    "assistant",
					Content: "test response",
				},
			},
		},
		Usage: api.ChatUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *MockClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	return &api.ChatResponse{
		Choices: []api.ChatChoice{
			{
				Message: api.Message{
					Role:    "assistant",
					Content: "test response",
				},
			},
		},
		Usage: api.ChatUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *MockClient) CheckConnection() error {
	return nil
}

func (m *MockClient) SetDebug(debug bool) {
	// No-op for mock
}

func (m *MockClient) SetModel(model string) error {
	m.model = model
	return nil
}

func (m *MockClient) GetModel() string {
	return m.model
}

func (m *MockClient) GetProvider() string {
	return "test-provider"
}

func (m *MockClient) GetModelContextLimit() (int, error) {
	return 128000, nil
}

func (m *MockClient) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return []api.ModelInfo{
		{
			ID:            m.model,
			Name:          m.model,
			Provider:      "test-provider",
			ContextLength: 128000,
		},
	}, nil
}

func (m *MockClient) SupportsVision() bool {
	return false
}

func (m *MockClient) GetVisionModel() string {
	return ""
}

func (m *MockClient) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, nil
}

func (m *MockClient) GetLastTPS() float64 {
	return 0
}

func (m *MockClient) GetAverageTPS() float64 {
	return 0
}

func (m *MockClient) GetTPSStats() map[string]float64 {
	return nil
}

func (m *MockClient) ResetTPSStats() {
	// No-op for mock
}