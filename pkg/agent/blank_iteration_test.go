package agent

import (
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
)

// Test that a blank assistant turn appends a user reminder and one-time tool-call guidance,
// and does not stop the conversation on the first blank iteration.
func TestProcessResponse_BlankIterationAddsReminderAndGuidance(t *testing.T) {
	agent, err := NewAgent()
	assert.NoError(t, err)
	ch := NewConversationHandler(agent)

	// Simulate a blank assistant response (no content, no tool calls)
	choice := api.Choice{}
	choice.Message.Role = "assistant"
	choice.Message.Content = "   \n\t  " // whitespace-only
	resp := &api.ChatResponse{Choices: []api.Choice{choice}}

	stopped := ch.processResponse(resp)
	assert.False(t, stopped, "expected conversation to continue after first blank iteration")

	// Verify reminder was enqueued and guidance suppressed
	reminderFound := false
	guidanceFound := false
	for _, m := range ch.transientMessages {
		if m.Role == "user" && strings.Contains(m.Content, "You provided no content.") && strings.Contains(m.Content, "[[TASK_COMPLETE]]") {
			reminderFound = true
		}
		if m.Role == "system" && strings.Contains(m.Content, "Use the exact tool name from the tools list.") {
			guidanceFound = true
		}
	}

	assert.True(t, reminderFound, "expected user reminder to be enqueued for blank iteration")
	assert.False(t, guidanceFound, "expected guidance to remain suppressed for now")
}
