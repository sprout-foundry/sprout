package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInputInjectionContext tests the new context-based input injection system
func TestInputInjectionContext(t *testing.T) {
	agent, err := NewAgent()
	require.NoError(t, err)

	// Test basic input injection
	t.Run("BasicInputInjection", func(t *testing.T) {
		input := "Test input"
		err := agent.InjectInputContext(input)
		assert.NoError(t, err)

		select {
		case injectedInput := <-agent.inputInjectionChan:
			assert.Equal(t, input, injectedInput)
		case <-time.After(100 * time.Millisecond):
			t.Error("Input was not injected")
		}
	})

	// Test multiple input injections
	t.Run("MultipleInputInjections", func(t *testing.T) {
		inputs := []string{"First", "Second"}
		for _, input := range inputs {
			err := agent.InjectInputContext(input)
			assert.NoError(t, err)
		}

		for _, expected := range inputs {
			select {
			case injectedInput := <-agent.inputInjectionChan:
				assert.Equal(t, expected, injectedInput)
			case <-time.After(100 * time.Millisecond):
				t.Errorf("Input was not injected: %s", expected)
			}
		}
	})
}

// TestCheckForInputInjection tests the conversation handler's input injection checking
func TestCheckForInputInjection(t *testing.T) {
	agent, err := NewAgent()
	require.NoError(t, err)

	handler := NewConversationHandler(agent)

	// Test no input injection
	t.Run("NoInputInjection", func(t *testing.T) {
		// The checkForInterrupt method handles both interrupts and input injection
		interrupted := handler.checkForInterrupt()
		assert.False(t, interrupted)
	})

	// Test input injection present
	t.Run("InputInjectionPresent", func(t *testing.T) {
		testInput := "Injected input"
		err := agent.InjectInputContext(testInput)
		require.NoError(t, err)

		// The checkForInterrupt method will detect and process the injected input
		interrupted := handler.checkForInterrupt()
		assert.False(t, interrupted) // Should continue processing, not interrupt
	})
}

// TestInputInjectionIntegration tests the integration between agent and conversation handler
func TestInputInjectionIntegration(t *testing.T) {
	agent, err := NewAgent()
	require.NoError(t, err)

	handler := NewConversationHandler(agent)

	// Test full integration flow
	t.Run("IntegrationFlow", func(t *testing.T) {
		err := agent.InjectInputContext("Integration test")
		assert.NoError(t, err)

		// Verify the conversation handler processes the injection
		interrupted := handler.checkForInterrupt()
		assert.False(t, interrupted) // Should continue processing with injected input

		// Verify the message was added to the agent's messages
		assert.Greater(t, len(agent.messages), 0)
		if len(agent.messages) > 0 {
			lastMessage := agent.messages[len(agent.messages)-1]
			assert.Equal(t, "user", lastMessage.Role)
			assert.Contains(t, lastMessage.Content, "Integration test")
		}
	})
}
