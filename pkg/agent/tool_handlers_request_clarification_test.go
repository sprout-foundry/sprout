package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestHandleRequestClarification_MissingQuestion(t *testing.T) {
	a := &Agent{}

	result, err := handleRequestClarification(context.Background(), a, map[string]interface{}{})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "question parameter is required")
}

func TestHandleRequestClarification_MissingQuestion_NonString(t *testing.T) {
	a := &Agent{}

	result, err := handleRequestClarification(context.Background(), a, map[string]interface{}{
		"question": 123, // wrong type
	})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "question parameter is required")
}

func TestHandleRequestClarification_NoClarificationManager(t *testing.T) {
	// Create agent WITHOUT eventBus so initSubManagers won't create clarificationManager
	a := &Agent{}
	a.initSubManagers()

	// Ensure clarificationManager is nil (no eventBus means it won't be created)
	require.Nil(t, a.clarificationManager)

	result, err := handleRequestClarification(context.Background(), a, map[string]interface{}{
		"question": "What should I do?",
	})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clarification manager not available")
}

func TestHandleRequestClarification_NotDelegateAgent(t *testing.T) {
	eb := events.NewEventBus()
	a := &Agent{
		eventBus: eb,
		// delegateID is empty — not a delegate
	}

	result, err := handleRequestClarification(context.Background(), a, map[string]interface{}{
		"question": "What should I do?",
	})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only available for subagents")
}

func TestHandleRequestClarification_Success(t *testing.T) {
	eb := events.NewEventBus()
	a := &Agent{
		eventBus:   eb,
		delegateID: "delegate-test-1",
	}

	// initSubManagers will create clarificationManager
	a.initSubManagers()
	require.NotNil(t, a.clarificationManager)

	// Call handler in a goroutine (it blocks waiting for response)
	done := make(chan string, 1)
	go func() {
		result, err := handleRequestClarification(context.Background(), a, map[string]interface{}{
			"question": "What should I do?",
		})
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- result
		}
	}()

	// Wait for request to be registered
	time.Sleep(100 * time.Millisecond)

	// Get pending and respond
	pending := a.clarificationManager.GetPendingClarifications("delegate-test-1")
	require.NotEmpty(t, pending)

	err := a.clarificationManager.RespondClarification(pending[0].RequestID, "Do the thing")
	require.NoError(t, err)

	// Wait for result
	select {
	case result := <-done:
		assert.Contains(t, result, "Clarification received:")
		assert.Contains(t, result, "Do the thing")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for clarification response")
	}
}

func TestHandleRequestClarification_ContextCancelled(t *testing.T) {
	eb := events.NewEventBus()
	a := &Agent{
		eventBus:   eb,
		delegateID: "delegate-test-2",
	}
	a.initSubManagers()
	require.NotNil(t, a.clarificationManager)

	// Set a short timeout
	a.clarificationManager = NewClarificationManagerWithTimeout(eb, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan string, 1)
	go func() {
		result, err := handleRequestClarification(ctx, a, map[string]interface{}{
			"question": "Will be cancelled?",
		})
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- result
		}
	}()

	// Cancel the context
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case result := <-done:
		assert.Contains(t, result, "failed")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for context cancellation")
	}
}

func TestHandleRequestClarification_Timeout(t *testing.T) {
	eb := events.NewEventBus()
	a := &Agent{
		eventBus:   eb,
		delegateID: "delegate-test-3",
	}
	a.initSubManagers()
	a.clarificationManager = NewClarificationManagerWithTimeout(eb, 100*time.Millisecond)

	done := make(chan string, 1)
	go func() {
		result, err := handleRequestClarification(context.Background(), a, map[string]interface{}{
			"question": "Will timeout?",
		})
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- result
		}
	}()

	select {
	case result := <-done:
		assert.Contains(t, result, "failed")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for timeout error")
	}
}
