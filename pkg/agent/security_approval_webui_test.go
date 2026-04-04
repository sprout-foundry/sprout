package agent

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/events"
)

// TestSecurityApprovalWebuiFlow verifies that security approvals are correctly routed through the eventBus
// in webui mode, prioritizing webui approval over CLI interactive prompts when both are available.
func TestSecurityApprovalWebuiFlow(t *testing.T) {
	// Setup: Create an event bus and security approval manager
	eventBus := events.NewEventBus()
	approvalMgr := NewSecurityApprovalManager()

	eventCh := eventBus.Subscribe("security_test")
	defer eventBus.Unsubscribe("security_test")

	// Track the approval request
	requestID := ""

	// Start a goroutine to listen for security approval requests
	go func() {
		event := <-eventCh
		if event.Type != events.EventTypeSecurityApprovalRequest {
			t.Errorf("Expected security approval request event, got: %s", event.Type)
			return
		}

		data, ok := event.Data.(map[string]interface{})
		if !ok {
			t.Error("Expected event data to be a map")
			return
		}

		requestID = data["request_id"].(string)
		toolName := data["tool_name"].(string)
		riskLevel := data["risk_level"].(string)
		reasoning := data["reasoning"].(string)

		// Verify the approval request contains the expected information
		if toolName != "shell_command" {
			t.Errorf("Expected tool_name to be 'shell_command', got: %s", toolName)
		}
		if riskLevel != "CAUTION" {
			t.Errorf("Expected risk_level to be 'CAUTION', got: %s", riskLevel)
		}
		if reasoning != "Test reasoning" {
			t.Errorf("Expected reasoning to be 'Test reasoning', got: %s", reasoning)
		}

		// Respond with approval
		approvalMgr.RespondToApproval(requestID, true)
	}()

	// Test the webui approval flow: eventBus is available, so approval should go through webui
	approved := approvalMgr.RequestApproval(eventBus, "test-client", "shell_command", "CAUTION", "Test reasoning", map[string]string{
		"command": "echo test",
		"extras":  "should not appear",
	})

	if !approved {
		t.Error("Expected approval to be true when eventBus is available (webui mode)")
	}

	// Test rejection flow
	go func() {
		event := <-eventCh
		data, _ := event.Data.(map[string]interface{})
		requestID, _ := data["request_id"].(string)
		approvalMgr.RespondToApproval(requestID, false)
	}()

	rejected := approvalMgr.RequestApproval(eventBus, "test-client", "shell_command", "DANGEROUS", "Test rejection", map[string]string{
		"command": "rm -rf /",
	})

	if rejected {
		t.Error("Expected approval to be false (rejected)")
	}
}

// TestSecurityApprovalNoEventBus verifies that when eventBus is nil, approval is rejected for safety
func TestSecurityApprovalNoEventBus(t *testing.T) {
	approvalMgr := NewSecurityApprovalManager()

	// When eventBus is nil, RequestApproval should return false (reject for safety)
	approved := approvalMgr.RequestApproval(nil, "", "shell_command", "CAUTION", "Test", nil)

	if approved {
		t.Error("Expected approval to be false when eventBus is nil (reject for safety)")
	}
}

// TestSecurityApprovalNonInteractive verifies approval flow when logger.IsInteractive() is false
func TestSecurityApprovalNonInteractive(t *testing.T) {
	// When IsInteractive returns false (e.g., in webui or non-interactive mode),
	// and eventBus is available, approval should still work through webui
	eventBus := events.NewEventBus()
	approvalMgr := NewSecurityApprovalManager()

	eventCh := eventBus.Subscribe("noninteractive_test")
	defer eventBus.Unsubscribe("noninteractive_test")

	go func() {
		event := <-eventCh
		data, _ := event.Data.(map[string]interface{})
		requestID, _ := data["request_id"].(string)
		approvalMgr.RespondToApproval(requestID, true)
	}()

	// Even though IsInteractive might be false, webui should handle it
	approved := approvalMgr.RequestApproval(eventBus, "test-client", "shell_command", "CAUTION", "Non-interactive test", nil)

	if !approved {
		t.Error("Expected approval to be true when eventBus is available, even in non-interactive mode")
	}
}
