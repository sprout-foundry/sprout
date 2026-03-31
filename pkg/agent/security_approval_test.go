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

func TestSecurityApprovalManager_RequestApprovalExtraFields(t *testing.T) {
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
		// Verify standard fields are present
		if data["tool_name"] != "shell_command" {
			t.Errorf("expected tool_name shell_command, got %v", data["tool_name"])
		}
		// Verify extra fields are merged
		if data["command"] != "rm -rf /tmp/test" {
			t.Errorf("expected command 'rm -rf /tmp/test', got %v", data["command"])
		}
		if data["risk_type"] != "source_code_destruction" {
			t.Errorf("expected risk_type 'source_code_destruction', got %v", data["risk_type"])
		}
		requestID, _ := data["request_id"].(string)
		mgr.RespondToApproval(requestID, true)
	}()

	extra := map[string]interface{}{
		"command":   "rm -rf /tmp/test",
		"risk_type": "source_code_destruction",
	}
	approved := mgr.RequestApproval(eb, "client-456", "shell_command", "DANGEROUS", "dangerous", extra)
	if !approved {
		t.Error("expected approval")
	}
}
