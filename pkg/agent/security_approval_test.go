package agent

import (
	"sync"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
)

func TestSecurityApprovalManager_BasicApproval(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewSecurityApprovalManager()

	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	go func() {
		event := <-eventCh
		if event.Type != events.EventTypeSecurityApprovalRequest {
			t.Errorf("expected event type %s, got %s", events.EventTypeSecurityApprovalRequest, event.Type)
		}
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			t.Error("expected data to be map[string]interface{}")
			return
		}
		requestID, _ := data["request_id"].(string)
		if requestID == "" {
			t.Error("expected request_id in event data")
			return
		}
		mgr.RespondToApproval(requestID, true)
	}()

	approved := mgr.RequestApproval(eb, "", "shell_command", "CAUTION", "potentially risky operation", nil)
	if !approved {
		t.Error("expected approval to be true")
	}
}

func TestSecurityApprovalManager_Rejection(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewSecurityApprovalManager()

	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	go func() {
		event := <-eventCh
		data, _ := event.Data.(map[string]interface{})
		requestID, _ := data["request_id"].(string)
		mgr.RespondToApproval(requestID, false)
	}()

	approved := mgr.RequestApproval(eb, "", "shell_command", "DANGEROUS", "rm -rf /", nil)
	if approved {
		t.Error("expected approval to be false (rejected)")
	}
}

func TestSecurityApprovalManager_NilEventBus(t *testing.T) {
	mgr := NewSecurityApprovalManager()
	approved := mgr.RequestApproval(nil, "", "shell_command", "CAUTION", "test", nil)
	if approved {
		t.Error("expected false when event bus is nil")
	}
}

func TestSecurityApprovalManager_ConcurrentRequests(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewSecurityApprovalManager()

	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	const n = 5
	var wg sync.WaitGroup
	results := make([]bool, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = mgr.RequestApproval(eb, "", "shell_command", "CAUTION", "test", nil)
		}(i)
	}

	// Respond to each request
	for i := 0; i < n; i++ {
		event := <-eventCh
		data, _ := event.Data.(map[string]interface{})
		requestID, _ := data["request_id"].(string)
		mgr.RespondToApproval(requestID, true)
	}

	wg.Wait()

	for i, result := range results {
		if !result {
			t.Errorf("request %d: expected approval", i)
		}
	}
}

func TestSecurityApprovalManager_RespondToUnknownRequest(t *testing.T) {
	mgr := NewSecurityApprovalManager()
	result := mgr.RespondToApproval("nonexistent_id", true)
	if result {
		t.Error("expected false for unknown request ID")
	}
}

func TestSecurityApprovalManager_RequestEventData(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewSecurityApprovalManager()

	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	// Collect event data in a goroutine, then respond
	go func() {
		event := <-eventCh
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			t.Error("expected data to be map[string]interface{}")
			return
		}
		if data["tool_name"] != "shell_command" {
			t.Errorf("expected tool_name 'shell_command', got %v", data["tool_name"])
		}
		if data["risk_level"] != "CAUTION" {
			t.Errorf("expected risk_level 'CAUTION', got %v", data["risk_level"])
		}
		if data["reasoning"] != "potentially risky operation - review carefully" {
			t.Errorf("unexpected reasoning: %v", data["reasoning"])
		}
		// Respond to unblock the caller
		requestID, _ := data["request_id"].(string)
		mgr.RespondToApproval(requestID, true)
	}()

	// Use a timeout so tests don't hang forever
	done := make(chan bool, 1)
	go func() {
		approved := mgr.RequestApproval(eb, "", "shell_command", "CAUTION", "potentially risky operation - review carefully", nil)
		done <- approved
	}()

	select {
	case approved := <-done:
		if !approved {
			t.Error("expected approval")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval response")
	}
}

func TestSecurityApprovalManager_RequestEventIncludesClientIDWhenProvided(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewSecurityApprovalManager()

	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	go func() {
		event := <-eventCh
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			t.Error("expected data to be map[string]interface{}")
			return
		}
		if data["client_id"] != "client-123" {
			t.Errorf("expected client_id client-123, got %v", data["client_id"])
		}
		requestID, _ := data["request_id"].(string)
		mgr.RespondToApproval(requestID, true)
	}()

	approved := mgr.RequestApproval(eb, "client-123", "shell_command", "CAUTION", "test", nil)
	if !approved {
		t.Error("expected approval")
	}
}

func TestSecurityApprovalManager_ExtrasInEvent(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewSecurityApprovalManager()

	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	go func() {
		event := <-eventCh
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			t.Error("expected data to be map[string]interface{}")
			return
		}
		if data["command"] != "rm -rf /tmp/test" {
			t.Errorf("expected command 'rm -rf /tmp/test', got %v", data["command"])
		}
		if data["risk_type"] != "some risk" {
			t.Errorf("expected risk_type 'some risk', got %v", data["risk_type"])
		}
		requestID, _ := data["request_id"].(string)
		mgr.RespondToApproval(requestID, true)
	}()

	extras := map[string]string{
		"command": "rm -rf /tmp/test",
		"risk_type": "some risk",
	}

	approved := mgr.RequestApproval(eb, "", "shell_command", "CAUTION", "test", extras)
	if !approved {
		t.Error("expected approval")
	}
}

// --- Timeout tests ---

func TestSecurityApprovalManager_TimeoutReturnsFalse(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewSecurityApprovalManager()

	// Set a very short timeout so the test doesn't wait 5 minutes
	mgr.SetApprovalTimeout(50 * time.Millisecond)

	// Drain the published event so the EventBus doesn't block
	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	done := make(chan bool, 1)
	go func() {
		approved := mgr.RequestApproval(eb, "", "shell_command", "CAUTION", "test timeout", nil)
		done <- approved
	}()

	// Consume the event in a goroutine but intentionally never respond,
	// so RequestApproval must hit the timeout path.
	go func() {
		<-eventCh // drain the event, then do nothing
	}()

	select {
	case approved := <-done:
		if approved {
			t.Error("expected false when approval times out (no response sent)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("test timed out — RequestApproval did not return within 2s")
	}
}

func TestSecurityApprovalManager_SetApprovalTimeout(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewSecurityApprovalManager()

	// Verify default timeout is applied: request with default 5min timeout
	// won't return false within 100ms if we don't respond (so we just check
	// that the mechanism works by setting a short timeout below).
	mgr.SetApprovalTimeout(30 * time.Millisecond)
	eventCh := eb.Subscribe("timeout_test")
	defer eb.Unsubscribe("timeout_test")

	done := make(chan bool, 1)
	go func() {
		approved := mgr.RequestApproval(eb, "", "tool", "LOW", "test", nil)
		done <- approved
	}()

	// Drain event but don't respond
	go func() {
		<-eventCh
	}()

	select {
	case approved := <-done:
		if approved {
			t.Error("expected false when custom short timeout expires")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("test timed out")
	}

	// Reset to default via zero value and verify request still works
	mgr.SetApprovalTimeout(0)
	eventCh2 := eb.Subscribe("timeout_test2")
	defer eb.Unsubscribe("timeout_test2")

	go func() {
		event := <-eventCh2
		data, _ := event.Data.(map[string]interface{})
		requestID, _ := data["request_id"].(string)
		mgr.RespondToApproval(requestID, true)
	}()

	approved := mgr.RequestApproval(eb, "", "tool", "LOW", "test default reset", nil)
	if !approved {
		t.Error("expected approval after resetting timeout to default (response sent immediately)")
	}
}

func TestSecurityApprovalManager_TimeoutDoesNotBlockIfResponseArrives(t *testing.T) {
	eb := events.NewEventBus()
	mgr := NewSecurityApprovalManager()

	// Set a long timeout (10 seconds) but respond immediately
	mgr.SetApprovalTimeout(10 * time.Second)

	eventCh := eb.Subscribe("test_sub")
	defer eb.Unsubscribe("test_sub")

	// Respond immediately upon receiving the event
	go func() {
		event := <-eventCh
		data, _ := event.Data.(map[string]interface{})
		requestID, _ := data["request_id"].(string)
		mgr.RespondToApproval(requestID, true)
	}()

	start := time.Now()
	approved := mgr.RequestApproval(eb, "", "shell_command", "LOW", "quick response test", nil)
	elapsed := time.Since(start)

	if !approved {
		t.Error("expected approval when response arrives before timeout")
	}
	if elapsed > 2*time.Second {
		t.Errorf("RequestApproval took too long (%v) — should have returned immediately on response", elapsed)
	}
}
