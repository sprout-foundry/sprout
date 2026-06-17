package agent

import (
	"sync"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// TestToolsApprovalAdapter_NilAgent verifies the adapter constructor returns
// nil (not a panic) when the agent is nil or has no security manager.
func TestToolsApprovalAdapter_NilAgent(t *testing.T) {
	if got := newToolsApprovalAdapter(nil); got != nil {
		t.Errorf("newToolsApprovalAdapter(nil) = %v, want nil", got)
	}

	// A bare &Agent{} has security == nil until ensureDefaults is called,
	// so GetSecurityApprovalMgr returns nil and the adapter should be nil.
	a := &Agent{}
	if got := newToolsApprovalAdapter(a); got != nil {
		t.Errorf("newToolsApprovalAdapter(bare agent) = %v, want nil", got)
	}
}

// TestToolsApprovalAdapter_Approved verifies that when the security layer
// approves a request, the adapter returns Approved=true.
func TestToolsApprovalAdapter_Approved(t *testing.T) {
	eb := events.NewEventBus()
	mgr := security.NewApprovalManager()

	// Subscribe and auto-approve in the background.
	ch := eb.Subscribe("test")
	defer eb.Unsubscribe("test")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ev := <-ch
		data, _ := ev.Data.(map[string]interface{})
		rid, _ := data["request_id"].(string)
		mgr.RespondToApproval(rid, true)
	}()

	adapter := &toolsApprovalAdapter{
		approvalMgr: mgr,
		eventBus:    eb,
	}
	result := adapter.RequestApproval("ignored-id", "shell_command", "CAUTION", "run something", nil)
	wg.Wait()

	if !result.Approved {
		t.Errorf("expected Approved=true, got false (reason=%q)", result.Reason)
	}
	if result.Reason != "" {
		t.Errorf("expected empty Reason on approval, got %q", result.Reason)
	}
}

// TestToolsApprovalAdapter_Rejected verifies that when the security layer
// rejects a request, the adapter returns Approved=false with reason "rejected".
func TestToolsApprovalAdapter_Rejected(t *testing.T) {
	eb := events.NewEventBus()
	mgr := security.NewApprovalManager()

	ch := eb.Subscribe("test")
	defer eb.Unsubscribe("test")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ev := <-ch
		data, _ := ev.Data.(map[string]interface{})
		rid, _ := data["request_id"].(string)
		mgr.RespondToApproval(rid, false)
	}()

	adapter := &toolsApprovalAdapter{
		approvalMgr: mgr,
		eventBus:    eb,
	}
	result := adapter.RequestApproval("ignored-id", "shell_command", "DANGEROUS", "rm -rf /", nil)
	wg.Wait()

	if result.Approved {
		t.Error("expected Approved=false")
	}
	if result.Reason != "rejected" {
		t.Errorf("expected Reason=%q, got %q", "rejected", result.Reason)
	}
}

// TestToolsApprovalAdapter_NilEventBus verifies the adapter degrades
// gracefully (reject for safety) when the event bus is nil.
func TestToolsApprovalAdapter_NilEventBus(t *testing.T) {
	mgr := security.NewApprovalManager()
	adapter := &toolsApprovalAdapter{
		approvalMgr: mgr,
		eventBus:    nil,
	}
	result := adapter.RequestApproval("ignored-id", "shell_command", "CAUTION", "test", nil)

	if result.Approved {
		t.Error("expected Approved=false when event bus is nil")
	}
	if result.Reason != "no_channel" {
		t.Errorf("expected Reason=%q, got %q", "no_channel", result.Reason)
	}
}

// TestToolsApprovalAdapter_InterfaceCompliance is a compile-time check that
// toolsApprovalAdapter satisfies the tools.ApprovalManager interface.
func TestToolsApprovalAdapter_InterfaceCompliance(t *testing.T) {
	var _ tools.ApprovalManager = (*toolsApprovalAdapter)(nil)
}

// TestToolsApprovalAdapter_ExtrasForwarded verifies that the extras map is
// forwarded to the security layer's event payload.
func TestToolsApprovalAdapter_ExtrasForwarded(t *testing.T) {
	eb := events.NewEventBus()
	mgr := security.NewApprovalManager()

	ch := eb.Subscribe("test")
	defer eb.Unsubscribe("test")

	extras := map[string]string{"command": "git push", "risk": "high"}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ev := <-ch
		data, _ := ev.Data.(map[string]interface{})
		rid, _ := data["request_id"].(string)

		// Verify extras were forwarded
		if cmd, ok := data["command"].(string); !ok || cmd != "git push" {
			t.Errorf("expected extras[command]=%q in event, got %v", "git push", data["command"])
		}
		if risk, ok := data["risk"].(string); !ok || risk != "high" {
			t.Errorf("expected extras[risk]=%q in event, got %v", "high", data["risk"])
		}

		mgr.RespondToApproval(rid, true)
	}()

	adapter := &toolsApprovalAdapter{
		approvalMgr: mgr,
		eventBus:    eb,
	}
	result := adapter.RequestApproval("ignored-id", "shell_command", "CAUTION", "git push origin", extras)
	wg.Wait()

	if !result.Approved {
		t.Error("expected Approved=true")
	}
}
