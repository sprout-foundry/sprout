package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Constructor Tests ---

func TestNewClarificationManager(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManager(eb)

	require.NotNil(t, cm)
	assert.Equal(t, DefaultClarificationTimeout, cm.timeout)
	assert.NotNil(t, cm.requests)
	assert.Equal(t, eb, cm.eventBus)
}

func TestNewClarificationManagerWithTimeout(t *testing.T) {
	eb := events.NewEventBus()
	customTimeout := 45 * time.Second
	cm := NewClarificationManagerWithTimeout(eb, customTimeout)

	require.NotNil(t, cm)
	assert.Equal(t, customTimeout, cm.timeout)
	assert.NotNil(t, cm.requests)
	assert.Equal(t, eb, cm.eventBus)
}

func TestNewClarificationManager_NilEventBus(t *testing.T) {
	cm := NewClarificationManager(nil)

	require.NotNil(t, cm)
	assert.Nil(t, cm.eventBus)
	assert.NotNil(t, cm.requests)
}

// --- Request/Response Success ---

func TestRequestClarification_Success(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	done := make(chan string, 1)
	go func() {
		resp, err := cm.RequestClarification(context.Background(), "delegate-1", "What should I do?")
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- resp
		}
	}()

	// Wait for request to be registered
	time.Sleep(100 * time.Millisecond)

	// Get pending and respond
	pending := cm.GetPendingClarifications("delegate-1")
	require.NotEmpty(t, pending)

	err := cm.RespondClarification(pending[0].RequestID, "Do the thing")
	require.NoError(t, err)

	// Wait for response
	select {
	case result := <-done:
		assert.Equal(t, "Do the thing", result)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for clarification response")
	}
}

func TestRequestClarification_MultipleSuccess(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	// Start two requests in parallel
	done := make(chan string, 2)
	go func() {
		resp, err := cm.RequestClarification(context.Background(), "delegate-1", "Question 1")
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- resp
		}
	}()
	go func() {
		resp, err := cm.RequestClarification(context.Background(), "delegate-2", "Question 2")
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- resp
		}
	}()

	time.Sleep(100 * time.Millisecond)

	pending := cm.GetPendingClarifications("")
	require.Len(t, pending, 2)

	// Respond to each
	var req1ID, req2ID string
	for _, p := range pending {
		switch p.Question {
		case "Question 1":
			req1ID = p.RequestID
		case "Question 2":
			req2ID = p.RequestID
		}
	}
	require.NotEmpty(t, req1ID)
	require.NotEmpty(t, req2ID)

	require.NoError(t, cm.RespondClarification(req1ID, "Answer 1"))
	require.NoError(t, cm.RespondClarification(req2ID, "Answer 2"))

	// Collect results
	results := make([]string, 0, 2)
	timeout := time.After(2 * time.Second)
	for len(results) < 2 {
		select {
		case r := <-done:
			results = append(results, r)
		case <-timeout:
			t.Fatalf("timed out waiting for responses, got %d: %v", len(results), results)
		}
	}
	assert.Contains(t, results, "Answer 1")
	assert.Contains(t, results, "Answer 2")
}

// --- Timeout ---

func TestRequestClarification_Timeout(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 200*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := cm.RequestClarification(context.Background(), "delegate-1", "Will timeout?")
		done <- err
	}()

	// Do NOT respond — let it timeout
	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timed out")
	case <-time.After(1 * time.Second):
		t.Fatal("expected timeout error, but never got a response")
	}

	// Verify the request was cleaned up
	pending := cm.GetPendingClarifications("delegate-1")
	assert.Empty(t, pending)
}

// --- Context Cancellation ---

func TestRequestClarification_ContextCancellation(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := cm.RequestClarification(ctx, "delegate-1", "Will be cancelled?")
		done <- err
	}()

	// Cancel after a short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cancelled")
	case <-time.After(2 * time.Second):
		t.Fatal("expected cancellation error, but never got a response")
	}

	// Verify the request was cleaned up
	pending := cm.GetPendingClarifications("delegate-1")
	assert.Empty(t, pending)
}

// --- Respond Not Found ---

func TestRespondClarification_NotFound(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManager(eb)

	err := cm.RespondClarification("non-existent-id", "some response")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- GetPendingClarifications ---

func TestGetPendingClarifications(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	// Start multiple requests for same delegate
	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func(n int) {
			cm.RequestClarification(context.Background(), "delegate-1", fmt.Sprintf("Question %d", n))
			done <- struct{}{}
		}(i)
	}

	time.Sleep(200 * time.Millisecond)

	pending := cm.GetPendingClarifications("delegate-1")
	require.Len(t, pending, 3)

	for _, p := range pending {
		assert.Equal(t, "delegate-1", p.SubagentID)
		assert.NotEmpty(t, p.RequestID)
		assert.NotEmpty(t, p.Question)
		assert.NotZero(t, p.CreatedAt)
	}
}

func TestGetPendingClarifications_DifferentDelegates(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	// Start requests for different delegates
	done := make(chan struct{}, 3)
	go func() {
		cm.RequestClarification(context.Background(), "delegate-a", "Question A")
		done <- struct{}{}
	}()
	go func() {
		cm.RequestClarification(context.Background(), "delegate-b", "Question B")
		done <- struct{}{}
	}()
	go func() {
		cm.RequestClarification(context.Background(), "delegate-b", "Question B2")
		done <- struct{}{}
	}()

	time.Sleep(200 * time.Millisecond)

	// Filter by delegate-a
	pendingA := cm.GetPendingClarifications("delegate-a")
	require.Len(t, pendingA, 1)
	assert.Equal(t, "delegate-a", pendingA[0].SubagentID)

	// Filter by delegate-b
	pendingB := cm.GetPendingClarifications("delegate-b")
	require.Len(t, pendingB, 2)
	for _, p := range pendingB {
		assert.Equal(t, "delegate-b", p.SubagentID)
	}

	// Get all (empty subagentID means all)
	pendingAll := cm.GetPendingClarifications("")
	require.Len(t, pendingAll, 3)

	// Non-existent delegate returns empty
	pendingNone := cm.GetPendingClarifications("delegate-unknown")
	assert.Empty(t, pendingNone)
}

func TestGetPendingClarifications_Empty(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManager(eb)

	pending := cm.GetPendingClarifications("anyone")
	assert.Empty(t, pending)
}

// --- Respond Removes From Pending ---

func TestRespondClarification_RemovesFromPending(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	done := make(chan string, 1)
	go func() {
		resp, err := cm.RequestClarification(context.Background(), "delegate-1", "What to do?")
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- resp
		}
	}()

	time.Sleep(100 * time.Millisecond)

	pending := cm.GetPendingClarifications("delegate-1")
	require.Len(t, pending, 1)
	requestID := pending[0].RequestID

	// Respond
	require.NoError(t, cm.RespondClarification(requestID, "Answer"))

	// Verify it's no longer pending
	pendingAfter := cm.GetPendingClarifications("delegate-1")
	assert.Empty(t, pendingAfter)

	// Verify the goroutine received the response
	select {
	case result := <-done:
		assert.Equal(t, "Answer", result)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}
}

// --- Cleanup ---

func TestCleanup_RemovesExpired(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 100*time.Millisecond)

	// Start request in goroutine (it will timeout)
	go func() {
		cm.RequestClarification(context.Background(), "delegate-1", "Will expire")
	}()

	// Poll until the goroutine registers the pending entry, with a
	// generous timeout. The previous time.Sleep(50ms) approach flaked
	// on slow CI/sandboxed runners where the goroutine wasn't scheduled
	// in time.
	require.Eventually(t, func() bool {
		return len(cm.GetPendingClarifications("delegate-1")) == 1
	}, 2*time.Second, 5*time.Millisecond, "pending entry should register within 2s")

	// Wait past the 100ms timeout (test setup + Eventually overhead is
	// typically >100ms by now, but sleep to be deterministic).
	time.Sleep(150 * time.Millisecond)

	// Cleanup should remove expired entry
	cm.Cleanup()

	pendingAfter := cm.GetPendingClarifications("delegate-1")
	assert.Empty(t, pendingAfter)
}

func TestCleanup_KeepsNonExpired(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	// Start request in goroutine
	go func() {
		cm.RequestClarification(context.Background(), "delegate-1", "Should stay")
	}()

	// Wait briefly for request to register
	time.Sleep(50 * time.Millisecond)

	pending := cm.GetPendingClarifications("delegate-1")
	require.Len(t, pending, 1)

	cm.Cleanup()

	pendingAfter := cm.GetPendingClarifications("delegate-1")
	require.Len(t, pendingAfter, 1)
	assert.Equal(t, pending[0].RequestID, pendingAfter[0].RequestID)
}

// --- Concurrent Access ---

func TestConcurrentAccess(t *testing.T) {
	eb := events.NewEventBus()
	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	const numRequests = 10
	var wg sync.WaitGroup
	wg.Add(numRequests)

	results := make([]string, numRequests)
	errs := make([]error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(n int) {
			defer wg.Done()
			resp, err := cm.RequestClarification(context.Background(), fmt.Sprintf("delegate-%d", n), fmt.Sprintf("Q%d", n))
			results[n] = resp
			errs[n] = err
		}(i)
	}

	// Wait for requests to register
	time.Sleep(300 * time.Millisecond)

	// Respond to all pending
	pending := cm.GetPendingClarifications("")
	for _, p := range pending {
		err := cm.RespondClarification(p.RequestID, "Answer for "+p.SubagentID)
		if err != nil {
			t.Logf("respond error (expected if request already completed): %v", err)
		}
	}

	wg.Wait()

	// Verify all completed without error
	for i := 0; i < numRequests; i++ {
		assert.NoError(t, errs[i], "request %d should not have errored", i)
		assert.Contains(t, results[i], "Answer for", "request %d should have received answer", i)
	}
}

// --- Event Publishing ---

func TestRequestClarification_PublishesEvent(t *testing.T) {
	eb := events.NewEventBus()
	sub := eb.Subscribe("test-cl-event")

	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	done := make(chan string, 1)
	go func() {
		resp, err := cm.RequestClarification(context.Background(), "delegate-1", "What do?")
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- resp
		}
	}()

	// Wait for event to be published
	var capturedEvent events.UIEvent
	select {
	case capturedEvent = <-sub:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for clarification_requested event")
	}

	assert.Equal(t, events.EventTypeDelegateClarificationRequested, capturedEvent.Type)
	data, ok := capturedEvent.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "delegate-1", data["subagent_id"])
	assert.Equal(t, "What do?", data["question"])
	assert.NotEmpty(t, data["request_id"])
	assert.NotEmpty(t, data["timestamp"])

	// Clean up: respond to avoid hanging goroutine
	pending := cm.GetPendingClarifications("delegate-1")
	if len(pending) > 0 {
		cm.RespondClarification(pending[0].RequestID, "ok")
	}
	<-done
}

func TestRespondClarification_PublishesEvent(t *testing.T) {
	eb := events.NewEventBus()
	sub := eb.Subscribe("test-cr-event")

	cm := NewClarificationManagerWithTimeout(eb, 5*time.Second)

	done := make(chan string, 1)
	go func() {
		resp, err := cm.RequestClarification(context.Background(), "delegate-1", "Question")
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- resp
		}
	}()

	time.Sleep(100 * time.Millisecond)

	pending := cm.GetPendingClarifications("delegate-1")
	require.NotEmpty(t, pending)
	requestID := pending[0].RequestID

	// Drain the clarification_requested event that was already published
	select {
	case reqEvent := <-sub:
		assert.Equal(t, events.EventTypeDelegateClarificationRequested, reqEvent.Type)
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for clarification_requested event")
	}

	// Respond — this publishes the clarification_responded event
	err := cm.RespondClarification(requestID, "The answer")
	require.NoError(t, err)

	var capturedEvent events.UIEvent
	select {
	case capturedEvent = <-sub:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for clarification_responded event")
	}

	assert.Equal(t, events.EventTypeDelegateClarificationResponded, capturedEvent.Type)
	data, ok := capturedEvent.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "delegate-1", data["subagent_id"])
	assert.Equal(t, requestID, data["request_id"])
	assert.Equal(t, "The answer", data["response"])
	assert.NotEmpty(t, data["timestamp"])

	<-done
}

func TestPublishEvent_NilEventBus(t *testing.T) {
	// Create manager with nil eventBus — publishEvent should not panic
	cm := NewClarificationManager(nil)

	done := make(chan string, 1)
	go func() {
		resp, err := cm.RequestClarification(context.Background(), "delegate-1", "Question")
		if err != nil {
			done <- fmt.Sprintf("error: %v", err)
		} else {
			done <- resp
		}
	}()

	// Respond via RespondClarification (which also calls publishEvent)
	time.Sleep(100 * time.Millisecond)
	pending := cm.GetPendingClarifications("delegate-1")
	require.NotEmpty(t, pending)

	require.NoError(t, cm.RespondClarification(pending[0].RequestID, "Answer"))

	select {
	case result := <-done:
		assert.Equal(t, "Answer", result)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}
}
