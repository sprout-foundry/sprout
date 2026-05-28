//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// shouldForwardEventToConnection — security event routing regression tests
// ---------------------------------------------------------------------------

// TestShouldForwardSecurityApprovalEvent_Global verifies that
// security_approval_request events without a client_id are forwarded
// to all connections (global event type).
func TestShouldForwardSecurityApprovalEvent_Global(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	event := events.UIEvent{
		Type: events.EventTypeSecurityApprovalRequest,
		Data: map[string]interface{}{
			"request_id": "sec_1",
			"tool_name":  "shell_command",
			"risk_level": "CAUTION",
			"reasoning":  "test",
		},
	}

	conn := &ConnectionInfo{ClientID: "client-a", ChatID: ""}
	if !ws.shouldForwardEventToConnection(event, conn) {
		t.Fatal("security_approval_request without client_id should be forwarded as a global event")
	}
}

// TestShouldForwardSecurityApprovalEvent_Targeted verifies that
// security_approval_request events with a matching client_id are forwarded
// only to the matching connection.
func TestShouldForwardSecurityApprovalEvent_Targeted(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	event := events.UIEvent{
		Type: events.EventTypeSecurityApprovalRequest,
		Data: map[string]interface{}{
			"client_id":  "client-a",
			"request_id": "sec_2",
			"tool_name":  "shell_command",
			"risk_level": "DANGEROUS",
			"reasoning":  "rm -rf /",
		},
	}

	connA := &ConnectionInfo{ClientID: "client-a", ChatID: ""}
	connB := &ConnectionInfo{ClientID: "client-b", ChatID: ""}

	if !ws.shouldForwardEventToConnection(event, connA) {
		t.Fatal("security_approval_request should be forwarded to matching client")
	}
	if ws.shouldForwardEventToConnection(event, connB) {
		t.Fatal("security_approval_request should NOT be forwarded to non-matching client")
	}
}

// TestShouldForwardSecurityPromptEvent_Global verifies that
// security_prompt_request events are forwarded as global events.
func TestShouldForwardSecurityPromptEvent_Global(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	event := events.UIEvent{
		Type: events.EventTypeSecurityPromptRequest,
		Data: map[string]interface{}{
			"request_id": "sec_3",
			"prompt":     "File outside working directory",
			"file_path":  "/etc/passwd",
		},
	}

	conn := &ConnectionInfo{ClientID: "client-a", ChatID: ""}
	if !ws.shouldForwardEventToConnection(event, conn) {
		t.Fatal("security_prompt_request should be forwarded as a global event")
	}
}

// ---------------------------------------------------------------------------
// Event bus delivery for security events
// ---------------------------------------------------------------------------

// TestSecurityApprovalEvent_DeliveredViaEventBus verifies that a
// security_approval_request published to the event bus is receivable
// by a subscriber. This tests the plumbing between the agent and
// the websocket layer.
func TestSecurityApprovalEvent_DeliveredViaEventBus(t *testing.T) {
	eb := events.NewEventBus()
	ws, err := NewReactWebServer(nil, eb, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
		}

	ch := eb.Subscribe("security_approval_test")
	defer eb.Unsubscribe("security_approval_test")

	payload := events.SecurityApprovalRequestEvent(
		"sec_test_1", "shell_command", "CAUTION",
		"potentially risky operation", map[string]string{"command": "rm -rf /tmp/test"},
	)
	ws.eventBus.Publish(events.EventTypeSecurityApprovalRequest, payload)

	select {
	case event := <-ch:
		if event.Type != events.EventTypeSecurityApprovalRequest {
			t.Fatalf("expected event type %s, got %s", events.EventTypeSecurityApprovalRequest, event.Type)
		}
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			t.Fatal("expected data to be map[string]interface{}")
		}
		if data["tool_name"] != "shell_command" {
			t.Errorf("expected tool_name=shell_command, got %v", data["tool_name"])
		}
		if data["request_id"] != "sec_test_1" {
			t.Errorf("expected request_id=sec_test_1, got %v", data["request_id"])
		}
		if data["command"] != "rm -rf /tmp/test" {
			t.Errorf("expected command='rm -rf /tmp/test', got %v", data["command"])
		}
	default:
		t.Fatal("expected to receive security_approval_request event from event bus")
	}
}

// TestSecurityPromptEvent_DeliveredViaEventBus verifies
// security_prompt_request events flow through the event bus.
func TestSecurityPromptEvent_DeliveredViaEventBus(t *testing.T) {
	eb := events.NewEventBus()
	ws, err := NewReactWebServer(nil, eb, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
		}

	ch := eb.Subscribe("security_prompt_test")
	defer eb.Unsubscribe("security_prompt_test")

	ws.eventBus.Publish(events.EventTypeSecurityPromptRequest, map[string]interface{}{
		"request_id": "sp_1",
		"prompt":     "File outside working directory",
		"file_path":  "/etc/shadow",
		"concern":    "system file access",
	})

	select {
	case event := <-ch:
		if event.Type != events.EventTypeSecurityPromptRequest {
			t.Fatalf("expected event type %s, got %s", events.EventTypeSecurityPromptRequest, event.Type)
		}
		data, ok := event.Data.(map[string]interface{})
		if !ok {
			t.Fatal("expected data to be map[string]interface{}")
		}
		if data["file_path"] != "/etc/shadow" {
			t.Errorf("expected file_path=/etc/shadow, got %v", data["file_path"])
		}
	default:
		t.Fatal("expected to receive security_prompt_request event from event bus")
	}
}

// ---------------------------------------------------------------------------
// Full round-trip: publish event → shouldForward → data integrity
// ---------------------------------------------------------------------------

// TestSecurityApprovalRoundTrip simulates the full backend flow:
//  1. Agent publishes security_approval_request via event bus
//  2. shouldForwardEventToConnection correctly routes to matching client
//  3. Event data fields are preserved for the WebUI dialog
func TestSecurityApprovalRoundTrip(t *testing.T) {
	eb := events.NewEventBus()
	ws, err := NewReactWebServer(nil, eb, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
		}

	ch := eb.Subscribe("roundtrip_test")
	defer eb.Unsubscribe("roundtrip_test")

	// Agent publishes request (directly in the same goroutine — EventBus is synchronous)
	payload := events.SecurityApprovalRequestEvent(
		"rt_1", "shell_command", "CAUTION",
		"test operation", map[string]string{"command": "echo hello"},
	)
	payload["client_id"] = "client-rt"
	eb.Publish(events.EventTypeSecurityApprovalRequest, payload)

	// Receive the event (simulating websocket read goroutine)
	var receivedData map[string]interface{}
	select {
	case event := <-ch:
		if event.Type != events.EventTypeSecurityApprovalRequest {
			t.Fatalf("expected %s, got %s", events.EventTypeSecurityApprovalRequest, event.Type)
		}
		receivedData = event.Data.(map[string]interface{})
	default:
		t.Fatal("timed out waiting for event")
	}

	// Verify shouldForward correctly routes
	event := events.UIEvent{Type: events.EventTypeSecurityApprovalRequest, Data: receivedData}
	connMatch := &ConnectionInfo{ClientID: "client-rt", ChatID: ""}
	connNoMatch := &ConnectionInfo{ClientID: "other-client", ChatID: ""}

	if !ws.shouldForwardEventToConnection(event, connMatch) {
		t.Fatal("event should be forwarded to matching client-rt")
	}
	if ws.shouldForwardEventToConnection(event, connNoMatch) {
		t.Fatal("event should NOT be forwarded to non-matching client")
	}

	// Verify event data integrity (these are the fields the dialog renders)
	if receivedData["tool_name"] != "shell_command" {
		t.Errorf("expected tool_name=shell_command, got %v", receivedData["tool_name"])
	}
	if receivedData["risk_level"] != "CAUTION" {
		t.Errorf("expected risk_level=CAUTION, got %v", receivedData["risk_level"])
	}
	if receivedData["reasoning"] != "test operation" {
		t.Errorf("expected reasoning='test operation', got %v", receivedData["reasoning"])
	}
	if receivedData["command"] != "echo hello" {
		t.Errorf("expected command='echo hello', got %v", receivedData["command"])
	}
	if receivedData["client_id"] != "client-rt" {
		t.Errorf("expected client_id=client-rt, got %v", receivedData["client_id"])
	}
}

// ---------------------------------------------------------------------------
// Security events with extras (command, target, risk_type fields)
// ---------------------------------------------------------------------------

// TestSecurityApprovalEventWithExtras verifies that extra fields
// (command, target, risk_type) are preserved through the event pipeline.
// These are the fields shown in the WebUI dialog.
func TestSecurityApprovalEventWithExtras(t *testing.T) {
	extras := map[string]string{
		"command":   "sudo apt-get install build-essential",
		"risk_type": "privilege_escalation",
	}
	payload := events.SecurityApprovalRequestEvent(
		"extras_1", "shell_command", "CAUTION",
		"privileged package installation", extras,
	)

	eb := events.NewEventBus()
	ch := eb.Subscribe("extras_test")
	defer eb.Unsubscribe("extras_test")

	eb.Publish(events.EventTypeSecurityApprovalRequest, payload)

	event := <-ch
	data := event.Data.(map[string]interface{})

	if data["command"] != "sudo apt-get install build-essential" {
		t.Errorf("expected command in event data, got %v", data["command"])
	}
	if data["risk_type"] != "privilege_escalation" {
		t.Errorf("expected risk_type in event data, got %v", data["risk_type"])
	}
}

// TestSecurityApprovalEventWithTarget verifies file/git operation targets
// are preserved through the event pipeline.
func TestSecurityApprovalEventWithTarget(t *testing.T) {
	extras := map[string]string{
		"target": "git push --force",
	}
	payload := events.SecurityApprovalRequestEvent(
		"target_1", "git", "DANGEROUS",
		"force push detected", extras,
	)

	eb := events.NewEventBus()
	ch := eb.Subscribe("target_test")
	defer eb.Unsubscribe("target_test")

	eb.Publish(events.EventTypeSecurityApprovalRequest, payload)

	event := <-ch
	data := event.Data.(map[string]interface{})

	if data["target"] != "git push --force" {
		t.Errorf("expected target='git push --force', got %v", data["target"])
	}
	if data["tool_name"] != "git" {
		t.Errorf("expected tool_name=git, got %v", data["tool_name"])
	}
}

// ---------------------------------------------------------------------------
// Regression: security events MUST remain in global event allowlist
// ---------------------------------------------------------------------------

// TestSecurityEventsInGlobalAllowlist is a REGRESSION test ensuring
// security_approval_request and security_prompt_request remain in the
// global event allowlist in shouldForwardEventToConnection.
//
// If these are accidentally removed from the allowlist, security dialogs
// will stop appearing in the WebUI entirely — the agent will block waiting
// for approval, the event will be silently dropped, and the request will
// time out (denying the operation) without the user ever seeing why.
func TestSecurityEventsInGlobalAllowlist(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	conn := &ConnectionInfo{ClientID: "any-client", ChatID: ""}

	approvalEvent := events.UIEvent{
		Type: events.EventTypeSecurityApprovalRequest,
		Data: map[string]interface{}{"request_id": "reg_1"},
	}
	promptEvent := events.UIEvent{
		Type: events.EventTypeSecurityPromptRequest,
		Data: map[string]interface{}{"request_id": "reg_2"},
	}

	if !ws.shouldForwardEventToConnection(approvalEvent, conn) {
		t.Error("REGRESSION: security_approval_request must be in global event allowlist — WebUI security dialogs depend on this")
	}
	if !ws.shouldForwardEventToConnection(promptEvent, conn) {
		t.Error("REGRESSION: security_prompt_request must be in global event allowlist — WebUI file security prompts depend on this")
	}

	askUserEvent := events.UIEvent{
		Type: events.EventTypeAskUserRequest,
		Data: map[string]interface{}{"request_id": "reg_3"},
	}
	if !ws.shouldForwardEventToConnection(askUserEvent, conn) {
		t.Error("REGRESSION: ask_user_request must be in global event allowlist — WebUI ask_user dialogs depend on this")
	}
}

// TestSecurityEventsNotForwardedToWrongClient verifies targeted security
// events are NOT leaked to unrelated clients.
func TestSecurityEventsNotForwardedToWrongClient(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	targetedApproval := events.UIEvent{
		Type: events.EventTypeSecurityApprovalRequest,
		Data: map[string]interface{}{
			"client_id":  "client-A",
			"request_id": "sec_t1",
		},
	}

	connA := &ConnectionInfo{ClientID: "client-A", ChatID: ""}
	connB := &ConnectionInfo{ClientID: "client-B", ChatID: ""}

	if !ws.shouldForwardEventToConnection(targetedApproval, connA) {
		t.Fatal("targeted security event should reach the correct client")
	}
	if ws.shouldForwardEventToConnection(targetedApproval, connB) {
		t.Fatal("REGRESSION: targeted security event leaked to wrong client — violates client isolation")
	}
}
