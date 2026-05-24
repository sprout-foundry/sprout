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

func TestHandleRespondClarification_MissingRequestID(t *testing.T) {
	a := &Agent{}

	result, err := handleRespondClarification(context.Background(), a, map[string]interface{}{})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request_id parameter is required")
}

func TestHandleRespondClarification_MissingRequestID_NonString(t *testing.T) {
	a := &Agent{}

	result, err := handleRespondClarification(context.Background(), a, map[string]interface{}{
		"request_id": 123, // wrong type
	})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request_id parameter is required")
}

func TestHandleRespondClarification_MissingResponse(t *testing.T) {
	a := &Agent{}

	result, err := handleRespondClarification(context.Background(), a, map[string]interface{}{
		"request_id": "some-id",
	})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "response parameter is required")
}

func TestHandleRespondClarification_MissingResponse_NonString(t *testing.T) {
	a := &Agent{}

	result, err := handleRespondClarification(context.Background(), a, map[string]interface{}{
		"request_id": "some-id",
		"response":   123, // wrong type
	})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "response parameter is required")
}

func TestHandleRespondClarification_NoClarificationManager(t *testing.T) {
	// Create agent WITHOUT eventBus so initSubManagers won't create clarificationManager
	a := &Agent{}
	a.initSubManagers()

	require.Nil(t, a.clarificationManager)

	result, err := handleRespondClarification(context.Background(), a, map[string]interface{}{
		"request_id": "some-id",
		"response":   "some answer",
	})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clarification manager not available")
}

func TestHandleRespondClarification_NotFound(t *testing.T) {
	eb := events.NewEventBus()
	a := &Agent{
		eventBus: eb,
	}
	a.initSubManagers()
	require.NotNil(t, a.clarificationManager)

	result, err := handleRespondClarification(context.Background(), a, map[string]interface{}{
		"request_id": "non-existent-id",
		"response":   "some answer",
	})
	assert.Empty(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestHandleRespondClarification_Success(t *testing.T) {
	eb := events.NewEventBus()
	a := &Agent{
		eventBus: eb,
	}

	// Create a clarification manager
	cm := NewClarificationManager(eb)
	a.clarificationManager = cm

	// Start a request in a goroutine
	done := make(chan string, 1)
	go func() {
		resp, err := cm.RequestClarification(context.Background(), "delegate-1", "test question")
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- resp
		}
	}()

	// Wait for request to register
	time.Sleep(100 * time.Millisecond)

	pending := cm.GetPendingClarifications("delegate-1")
	require.NotEmpty(t, pending)

	// Call handler
	result, err := handleRespondClarification(context.Background(), a, map[string]interface{}{
		"request_id": pending[0].RequestID,
		"response":   "test response",
	})
	require.NoError(t, err)
	assert.Contains(t, result, "delivered")
	assert.Contains(t, result, pending[0].RequestID)

	// Verify the goroutine received the response
	select {
	case r := <-done:
		assert.Equal(t, "test response", r)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestHandleRespondClarification_DeliversToWaitingRequest(t *testing.T) {
	eb := events.NewEventBus()

	// Shared clarification manager for both delegate and parent
	cm := NewClarificationManager(eb)

	// Delegate agent (has delegateID) - requests clarification
	delegateAgent := &Agent{
		eventBus:           eb,
		delegateID:         "delegate-multi",
		clarificationManager: cm,
	}

	// Parent agent (no delegateID) - responds to clarification
	parentAgent := &Agent{
		eventBus:           eb,
		clarificationManager: cm,
	}

	// Launch a request in a goroutine as the delegate
	done := make(chan string, 1)
	go func() {
		result, err := handleRequestClarification(context.Background(), delegateAgent, map[string]interface{}{
			"question": "What should I do?",
		})
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- result
		}
	}()

	// Wait for request to register
	time.Sleep(100 * time.Millisecond)

	pending := cm.GetPendingClarifications("delegate-multi")
	require.NotEmpty(t, pending)

	// Respond via handleRespondClarification as the parent
	result, err := handleRespondClarification(context.Background(), parentAgent, map[string]interface{}{
		"request_id": pending[0].RequestID,
		"response":   "Build the thing",
	})
	require.NoError(t, err)
	assert.Contains(t, result, "delivered")

	// Verify the request handler got the response
	select {
	case r := <-done:
		assert.Contains(t, r, "Build the thing")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for clarification response")
	}
}
