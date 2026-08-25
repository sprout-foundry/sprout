package agent

import (
	"sync"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
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

// cliTestAdapter builds an adapter with all surfaces disabled except a
// scriptable CLI prompt, isolating the CLI-fallback path under test.
func cliTestAdapter(t *testing.T, agent *Agent, choiceFn func(prompt, command string) utils.ApprovalChoice) *toolsApprovalAdapter {
	t.Helper()
	return &toolsApprovalAdapter{
		agent:    agent,
		eventBus: events.NewEventBus(),
		cliPrompt: func(prompt, command string) utils.ApprovalChoice {
			return choiceFn(prompt, command)
		},
	}
}

// TestToolsApprovalAdapter_CLIFallbackApprove verifies the CLI fallback
// engages when no WebUI client is connected: the prompt is shown, the user's
// choice is honored, and allowlist side-effects run.
func TestToolsApprovalAdapter_CLIFallbackApprove(t *testing.T) {
	a := newIsolatedTestAgent(t)
	var gotPrompt, gotCommand string
	adapter := cliTestAdapter(t, a, func(prompt, command string) utils.ApprovalChoice {
		gotPrompt, gotCommand = prompt, command
		return utils.ApprovalChoiceApproveAlways
	})
	adapter.approvalMgr = security.NewApprovalManager()

	result := adapter.RequestApproval("id", "shell_command", "CAUTION", "Execute shell_command: ls -la /tmp", map[string]string{"command": "ls -la /tmp"})

	if !result.Approved {
		t.Fatalf("expected Approved=true, got %+v", result)
	}
	if gotPrompt != "Execute shell_command: ls -la /tmp" {
		t.Errorf("CLI prompt not shown: got %q", gotPrompt)
	}
	if gotCommand != "ls -la /tmp" {
		t.Errorf("command not passed to CLI picker: got %q", gotCommand)
	}
	if !a.IsShellCommandAllowlisted("ls -la /tmp") {
		t.Error("expected ApproveAlways to persist the command to the shell allowlist")
	}
}

// TestToolsApprovalAdapter_CLIFallbackDeny verifies a CLI rejection produces
// Approved=false with reason "rejected" and skips allowlist persistence.
func TestToolsApprovalAdapter_CLIFallbackDeny(t *testing.T) {
	a := newIsolatedTestAgent(t)
	adapter := cliTestAdapter(t, a, func(prompt, command string) utils.ApprovalChoice {
		return utils.ApprovalChoiceDeny
	})
	adapter.approvalMgr = security.NewApprovalManager()

	result := adapter.RequestApproval("id", "shell_command", "CAUTION", "test prompt", map[string]string{"command": "git push"})

	if result.Approved {
		t.Fatal("expected Approved=false on CLI deny")
	}
	if result.Reason != "rejected" {
		t.Errorf("expected Reason=%q, got %q", "rejected", result.Reason)
	}
}

// TestToolsApprovalAdapter_CLIAvailableNotInteractive verifies the CLI
// fallback is skipped (falls to the legacy publish) when no interactive
// surface exists — matching headless and subagent runs.
func TestToolsApprovalAdapter_CLIAvailableNotInteractive(t *testing.T) {
	mgr := security.NewApprovalManager()
	mgr.SetTimeout(50 * time.Millisecond)

	ch := make(chan struct{})
	var denied bool
	adapter := &toolsApprovalAdapter{
		approvalMgr: mgr,
		eventBus:    events.NewEventBus(),
		cliPrompt:   nil, // no injected prompt → real logger path
	}

	go func() {
		result := adapter.RequestApproval("id", "shell_command", "CAUTION", "test", nil)
		denied = !result.Approved
		ch <- struct{}{}
	}()
	select {
	case <-ch:
		if !denied {
			t.Error("expected denial (headless: no responder on bus)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("adapter blocked past timeout")
	}
}

// TestToolsApprovalAdapter_ElevateAppliesSideEffects verifies the Elevate
// choice elevates the agent session via applyApprovalDecision.
func TestToolsApprovalAdapter_ElevateAppliesSideEffects(t *testing.T) {
	a := newIsolatedTestAgent(t)
	adapter := cliTestAdapter(t, a, func(prompt, command string) utils.ApprovalChoice {
		return utils.ApprovalChoiceElevate
	})
	adapter.approvalMgr = security.NewApprovalManager()

	result := adapter.RequestApproval("id", "shell_command", "CAUTION", "test", map[string]string{"command": "make build"})

	if !result.Approved {
		t.Fatalf("expected Approved=true after elevate, got %+v", result)
	}
	if !a.IsSessionElevated() {
		t.Error("expected session to be elevated after Elevate choice")
	}
}

// TestToolsApprovalAdapter_WebUIPreferredOverCLI verifies that with an active
// WebUI client connected, the bus answer wins and the CLI is not prompted.
func TestToolsApprovalAdapter_WebUIPreferredOverCLI(t *testing.T) {
	a := newIsolatedTestAgent(t)
	adapter := cliTestAdapter(t, a, func(prompt, command string) utils.ApprovalChoice {
		t.Error("CLI prompt should not run when WebUI responds")
		return utils.ApprovalChoiceDeny
	})
	mgr := security.NewApprovalManager()
	mgr.SetTimeout(2 * time.Second)
	adapter.approvalMgr = mgr
	a.eventBus = adapter.eventBus
	a.SetHasActiveWebUIClients(func() bool { return true })

	ch := adapter.eventBus.Subscribe("test")
	defer adapter.eventBus.Unsubscribe("test")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ev := <-ch
		data, _ := ev.Data.(map[string]interface{})
		rid, _ := data["request_id"].(string)
		mgr.RespondToApproval(rid, true)
	}()

	result := adapter.RequestApproval("id", "shell_command", "CAUTION", "test", nil)
	wg.Wait()

	if !result.Approved {
		t.Fatal("expected Approved=true via webui surface")
	}
}

// TestToolsApprovalAdapter_NoRepeatTimeout verifies that after a WebUI
// timeout, the adapter does not republish to the bus: exactly one
// SecurityApprovalRequest event is published for the call.
func TestToolsApprovalAdapter_NoRepeatTimeout(t *testing.T) {
	adapter := cliTestAdapter(t, nil, func(prompt, command string) utils.ApprovalChoice {
		return utils.ApprovalChoiceApproveOnce
	})
	mgr := security.NewApprovalManager()
	mgr.SetTimeout(50 * time.Millisecond)
	adapter.approvalMgr = mgr

	a := newIsolatedTestAgent(t)
	a.eventBus = adapter.eventBus
	a.SetHasActiveWebUIClients(func() bool { return true })
	adapter.agent = a

	published := make(chan struct{}, 16)
	ch := adapter.eventBus.Subscribe("test")
	defer adapter.eventBus.Unsubscribe("test")
	go func() {
		for range ch {
			published <- struct{}{}
		}
	}()

	result := adapter.RequestApproval("id", "shell_command", "CAUTION", "test", nil)

	if !result.Approved {
		t.Fatalf("expected CLI fallback to answer after webui timeout, got %+v", result)
	}
	select {
	case <-published:
	default:
		t.Fatal("expected at least one bus publish")
	}
	// Drain briefly to catch a possible late second publish.
	time.Sleep(150 * time.Millisecond)
	if got := len(published); got != 1 {
		t.Errorf("expected exactly 1 bus publish after webui timeout, got %d (double publish = double blocking)", got+1)
	}
}

// TestToolsApprovalAdapter_SubagentNeverPromptsCLI pins the security guard
// that subagent flows can never reach the interactive CLI prompt, even when
// a stub is injected — the guard sits ahead of the test seam.
func TestToolsApprovalAdapter_SubagentNeverPromptsCLI(t *testing.T) {
	a := newIsolatedTestAgent(t)
	a.subagentDepth = 1
	adapter := &toolsApprovalAdapter{
		agent:       a,
		approvalMgr: security.NewApprovalManager(),
		eventBus:    events.NewEventBus(),
		cliPrompt: func(prompt, command string) utils.ApprovalChoice {
			t.Error("CLI prompt must never run for subagents")
			return utils.ApprovalChoiceDeny
		},
	}
	mgr := adapter.approvalMgr
	mgr.SetTimeout(50 * time.Millisecond)

	ch := make(chan tools.ApprovalResult, 1)
	go func() { ch <- adapter.RequestApproval("id", "shell_command", "CAUTION", "test", nil) }()

	select {
	case result := <-ch:
		if result.Approved {
			t.Error("expected denial when no surface can respond")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subagent request blocked past the approval timeout")
	}
}

// TestToolsApprovalAdapter_SkipPromptNeverPromptsCLI pins the headless
// guard: with SkipPrompt=true and no injected stub, the adapter must fall
// through to the bus (and its timeout denial), never the terminal.
func TestToolsApprovalAdapter_SkipPromptNeverPromptsCLI(t *testing.T) {
	a := newIsolatedTestAgent(t)
	mgrCfg, cleanup := configuration.NewTestManager(t)
	defer cleanup()
	a.configManager = mgrCfg
	if err := a.configManager.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.SkipPrompt = true
		return nil
	}); err != nil {
		t.Fatalf("set SkipPrompt: %v", err)
	}

	mgr := security.NewApprovalManager()
	mgr.SetTimeout(50 * time.Millisecond)

	adapter := &toolsApprovalAdapter{
		agent:       a,
		approvalMgr: mgr,
		eventBus:    events.NewEventBus(),
	}

	ch := make(chan tools.ApprovalResult, 1)
	go func() { ch <- adapter.RequestApproval("id", "shell_command", "CAUTION", "test", nil) }()

	select {
	case result := <-ch:
		if result.Approved {
			t.Error("expected denial (headless: bus timeout applies the safe default)")
		}
		if result.Reason != "timed_out" {
			t.Errorf("expected Reason=%q, got %q", "timed_out", result.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("headless request blocked past the approval timeout")
	}
}
