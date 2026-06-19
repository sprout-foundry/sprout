package agent

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// TestSecurityApprovalWebuiFlow verifies that security approvals are correctly routed through the eventBus
// in webui mode, prioritizing webui approval over CLI interactive prompts when both are available.
func TestSecurityApprovalWebuiFlow(t *testing.T) {
	// Setup: Create an event bus and security approval manager
	eventBus := events.NewEventBus()
	approvalMgr := security.NewApprovalManager()

	eventCh := eventBus.Subscribe("security_test")
	defer eventBus.Unsubscribe("security_test")

	// The event bus publishes two events per approval request
	// (SecurityApprovalRequest + InputRequired). Filter to only the
	// approval-request events and retry the response briefly because the
	// request goroutine may not have registered its pending entry yet.
	respondToNextApproval := func(approve bool) {
		for {
			event := <-eventCh
			if event.Type != events.EventTypeSecurityApprovalRequest {
				continue // skip InputRequired and other companion events
			}
			data, ok := event.Data.(map[string]interface{})
			if !ok {
				t.Error("Expected event data to be a map")
				return
			}
			requestID, _ := data["request_id"].(string)
			// Retry until the pending entry exists (bounded polling).
			for !approvalMgr.RespondToApproval(requestID, approve) {
				time.Sleep(time.Millisecond)
			}
			return
		}
	}

	// Start a goroutine to listen for the first security approval request
	// and validate its payload before approving.
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

		// Respond with approval (retry until pending entry is registered)
		requestID, _ := data["request_id"].(string)
		for !approvalMgr.RespondToApproval(requestID, true) {
			time.Sleep(time.Millisecond)
		}
	}()

	// Test the webui approval flow: eventBus is available, so approval should go through webui
	approved := approvalMgr.RequestToolApproval(eventBus, "test-client", "", "shell_command", "CAUTION", "Test reasoning", map[string]string{
		"command": "echo test",
		"extras":  "should not appear",
	})

	if !approved {
		t.Error("Expected approval to be true when eventBus is available (webui mode)")
	}

	// Test rejection flow — drain the companion InputRequired event from the
	// first request, then respond to the second approval request with reject.
	go respondToNextApproval(false)

	rejected := approvalMgr.RequestToolApproval(eventBus, "test-client", "", "shell_command", "DANGEROUS", "Test rejection", map[string]string{
		"command": "rm -rf /",
	})

	if rejected {
		t.Error("Expected approval to be false (rejected)")
	}
}

// TestSecurityApprovalNoEventBus verifies that when eventBus is nil, approval is rejected for safety
func TestSecurityApprovalNoEventBus(t *testing.T) {
	approvalMgr := security.NewApprovalManager()

	// When eventBus is nil, RequestToolApproval should return false (reject for safety)
	approved := approvalMgr.RequestToolApproval(nil, "", "", "shell_command", "CAUTION", "Test", nil)

	if approved {
		t.Error("Expected approval to be false when eventBus is nil (reject for safety)")
	}
}

// TestSecurityApprovalNonInteractive verifies approval flow when logger.IsInteractive() is false
func TestSecurityApprovalNonInteractive(t *testing.T) {
	// When IsInteractive returns false (e.g., in webui or non-interactive mode),
	// and eventBus is available, approval should still work through webui
	eventBus := events.NewEventBus()
	approvalMgr := security.NewApprovalManager()

	eventCh := eventBus.Subscribe("noninteractive_test")
	defer eventBus.Unsubscribe("noninteractive_test")

	go func() {
		event := <-eventCh
		data, _ := event.Data.(map[string]interface{})
		requestID, _ := data["request_id"].(string)
		approvalMgr.RespondToApproval(requestID, true)
	}()

	// Even though IsInteractive might be false, webui should handle it
	approved := approvalMgr.RequestToolApproval(eventBus, "test-client", "", "shell_command", "CAUTION", "Non-interactive test", nil)

	if !approved {
		t.Error("Expected approval to be true when eventBus is available, even in non-interactive mode")
	}
}
