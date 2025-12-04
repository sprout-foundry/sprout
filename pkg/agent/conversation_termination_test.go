package agent

import (
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/stretchr/testify/assert"
)

type stubClient struct {
	*factory.TestClient
	provider string
	model    string
}

func newStubClient(provider, model string) *stubClient {
	tc := &factory.TestClient{}
	if model != "" {
		_ = tc.SetModel(model)
	}
	return &stubClient{
		TestClient: tc,
		provider:   provider,
		model:      model,
	}
}

func (s *stubClient) GetProvider() string {
	return s.provider
}

func (s *stubClient) GetModel() string {
	if s.model != "" {
		return s.model
	}
	return s.TestClient.GetModel()
}

func (s *stubClient) SetModel(model string) error {
	s.model = model
	return s.TestClient.SetModel(model)
}

// Test that a complete-looking response without the explicit [[TASK_COMPLETE]] continues when policy requires explicit stop
func TestProcessResponse_RequiresExplicitCompletionWhenPolicyDisallowsImplicit(t *testing.T) {
	agent := &Agent{client: newStubClient("openrouter", "anthropic/claude-3")}
	ch := NewConversationHandler(agent)

	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "Here is the answer. Everything is done."
	resp := &api.ChatResponse{
		Choices: []api.Choice{choice},
	}

	stopped := ch.processResponse(resp)
	assert.False(t, stopped, "Expected conversation to continue until explicit completion signal is provided")

	reminderFound := false
	for _, m := range ch.transientMessages {
		if m.Role == "user" && strings.Contains(m.Content, "[[TASK_COMPLETE]]") {
			reminderFound = true
			break
		}
	}
	assert.True(t, reminderFound, "Expected reminder requesting [[TASK_COMPLETE]] to be appended")
}

// Test that an incomplete-looking response causes the handler to request continuation
func TestProcessResponse_RequestsContinuationForIncomplete(t *testing.T) {
	agent := &Agent{client: newStubClient("openrouter", "anthropic/claude-3")}
	ch := NewConversationHandler(agent)

	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "This is incomplete..."
	resp := &api.ChatResponse{Choices: []api.Choice{choice}}

	stopped := ch.processResponse(resp)
	assert.False(t, stopped, "Expected conversation to continue for incomplete response")
	// Verify that a user prompt asking to continue was enqueued
	found := false
	for _, m := range ch.transientMessages {
		if m.Role == "user" && m.Content == "Please continue with your response. The previous response appears incomplete." {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected the handler to append a continuation prompt to agent messages")
}

// Test that implicit completion is allowed for providers/models that opt in
func TestProcessResponse_AllowsImplicitCompletionForAllowedModel(t *testing.T) {
	agent := &Agent{client: newStubClient("openai", "gpt-4o")}
	ch := NewConversationHandler(agent)

	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "Here is the answer. Everything is done."
	resp := &api.ChatResponse{Choices: []api.Choice{choice}}

	stopped := ch.processResponse(resp)
	assert.True(t, stopped, "Expected implicit completion to be accepted for allowed provider/model")

	// Ensure no additional reminder was added
	for _, m := range agent.messages {
		if m.Role == "user" && strings.Contains(m.Content, "[[TASK_COMPLETE]]") {
			t.Fatalf("did not expect explicit completion reminder for allowed provider")
		}
	}
}

// Test that LMStudio does NOT allow implicit completion
func TestProcessResponse_LMStudioDisallowsImplicitCompletion(t *testing.T) {
	agent := &Agent{client: newStubClient("lmstudio", "test-model")}

	// Debug: Check the policy
	t.Logf("Provider: %s", agent.GetProvider())
	t.Logf("Should allow implicit completion: %t", agent.shouldAllowImplicitCompletion())

	ch := NewConversationHandler(agent)

	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "Here is the answer. Everything is done."
	resp := &api.ChatResponse{
		Choices: []api.Choice{choice},
	}

	stopped := ch.processResponse(resp)
	assert.False(t, stopped, "Expected conversation to continue until explicit completion signal is provided for LMStudio")

	reminderFound := false
	for _, m := range ch.transientMessages {
		if m.Role == "user" && strings.Contains(m.Content, "[[TASK_COMPLETE]]") {
			reminderFound = true
			break
		}
	}
	assert.True(t, reminderFound, "Expected reminder requesting [[TASK_COMPLETE]] to be appended for LMStudio")
}

// Test that explicit [[TASK_COMPLETE]] is handled and stripped
func TestProcessResponse_ExplicitCompletionSignal(t *testing.T) {
	agent := &Agent{client: newStubClient("openrouter", "anthropic/claude-3")}
	ch := NewConversationHandler(agent)

	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "All done. [[TASK_COMPLETE]]"
	resp := &api.ChatResponse{Choices: []api.Choice{choice}}

	stopped := ch.processResponse(resp)
	assert.True(t, stopped, "Expected conversation to stop when explicit completion signal provided")
	// Last assistant message content should have the signal stripped
	if len(agent.messages) < 1 {
		t.Fatalf("expected at least 1 message in agent.messages, got %d", len(agent.messages))
	}
	last := agent.messages[len(agent.messages)-1]
	assert.Equal(t, "All done.", last.Content)
}
