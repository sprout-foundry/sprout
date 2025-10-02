package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
)

// Test that a complete-looking response without the explicit [[TASK_COMPLETE]] stops the conversation
func TestProcessResponse_TreatsCompleteWithoutSignalAsFinal(t *testing.T) {
	agent := &Agent{}
	ch := NewConversationHandler(agent)

	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "Here is the answer. Everything is done."
	resp := &api.ChatResponse{
		Choices: []api.Choice{choice},
	}

	stopped := ch.processResponse(resp)
	assert.True(t, stopped, "Expected conversation to stop for complete-looking response without signal")
}

// Test that an incomplete-looking response causes the handler to request continuation
func TestProcessResponse_RequestsContinuationForIncomplete(t *testing.T) {
	agent := &Agent{}
	ch := NewConversationHandler(agent)

	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "This is incomplete..."
	resp := &api.ChatResponse{Choices: []api.Choice{choice}}

	stopped := ch.processResponse(resp)
	assert.False(t, stopped, "Expected conversation to continue for incomplete response")
	// Verify that a user prompt asking to continue was appended
	found := false
	for _, m := range agent.messages {
		if m.Role == "user" && m.Content == "Please continue with your response. The previous response appears incomplete." {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected the handler to append a continuation prompt to agent messages")
}

// Test that explicit [[TASK_COMPLETE]] is handled and stripped
func TestProcessResponse_ExplicitCompletionSignal(t *testing.T) {
	agent := &Agent{}
	ch := NewConversationHandler(agent)

	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "All done. [[TASK_COMPLETE]]"
	resp := &api.ChatResponse{Choices: []api.Choice{choice}}

	stopped := ch.processResponse(resp)
	assert.True(t, stopped, "Expected conversation to stop when explicit completion signal provided")
	// Last assistant message content should have the signal stripped
	// The last assistant message should be at len(agent.messages)-2 because system summary was appended after
	if len(agent.messages) < 2 {
		t.Fatalf("expected at least 2 messages in agent.messages, got %d", len(agent.messages))
	}
	last := agent.messages[len(agent.messages)-2]
	assert.Equal(t, "All done.", last.Content)
}
