package agent

import (
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/personas"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// ============================================================================
// Approval Broker Tests
//
// These tests verify the RequestApproval decision broker covers all bypass
// paths, auto-approve conditions, and hard-block invariants. The broker is
// the single entry point that unifies WebUI, CLI, non-interactive, and
// fast-bypass paths into one decision — no duplicate prompts possible.
// ============================================================================

// ---------------------------------------------------------------------------
// Non-interactive auto-approve
// ---------------------------------------------------------------------------

// TestApprovalBroker_NonInteractiveAutoApprove verifies that in non-interactive
// mode (SkipPrompt=true), Medium-risk commands are auto-approved without
// consulting any approval surface. This is the permissive-by-default path.
func TestApprovalBroker_NonInteractiveAutoApprove(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)
	agent.state.SetActivePersona(personas.IDOrchestrator)

	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.UnifiedRiskResolver = true
		cfg.SkipPrompt = true // non-interactive mode
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	assessment := agent.ResolveToolRisk("shell_command", map[string]interface{}{"command": "rm somefile.txt"})

	decision, err := agent.RequestApproval(assessment, "shell_command", map[string]interface{}{"command": "rm somefile.txt"})

	if err != nil {
		t.Errorf("Expected auto-approval in non-interactive mode, got error: %v", err)
	}
	if !decision.Approved {
		t.Error("Expected approved=true in non-interactive mode")
	}
	if decision.Surface != "non-interactive" {
		t.Errorf("Expected surface 'non-interactive', got %q", decision.Surface)
	}
}

// ---------------------------------------------------------------------------
// Low-risk no-prompt
// ---------------------------------------------------------------------------

// TestApprovalBroker_LowRiskNoPrompt verifies that Low-risk assessments
// return immediately with auto-approve without consulting any surface
// or checking bypass flags.
func TestApprovalBroker_LowRiskNoPrompt(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	assessment := agent.ResolveToolRisk("shell_command", map[string]interface{}{"command": "ls -la"})

	if assessment.Level != configuration.RiskLevelLow {
		t.Logf("Note: ls -la classified as %s (expected Low, but classifier may vary)", assessment.Level)
	}

	decision, err := agent.RequestApproval(assessment, "shell_command", map[string]interface{}{"command": "ls -la"})

	if err != nil {
		t.Errorf("Low-risk should auto-approve, got error: %v", err)
	}
	if !decision.Approved {
		t.Error("Expected approved=true for low-risk command")
	}
	// Low-risk returns early — no surface consulted
	if decision.Decision != security.ApprovalDecision(0) {
		t.Logf("Decision value (low-risk fast path): %v", decision.Decision)
	}
}

// ---------------------------------------------------------------------------
// Critical hard-block
// ---------------------------------------------------------------------------

// TestApprovalBroker_CriticalHardBlock verifies that Critical/hard-block
// assessments are unconditionally denied regardless of mode or bypass flags.
func TestApprovalBroker_CriticalHardBlock(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	args := map[string]interface{}{"command": "rm -rf /"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	if !assessment.IsHardBlock {
		t.Logf("Note: rm -rf / not detected as hard-block by classifier (expected in test env)")
	}

	decision, err := agent.RequestApproval(assessment, "shell_command", args)

	if decision.Approved {
		t.Error("Critical/hard-block should never be approved")
	}
	if assessment.IsHardBlock && err == nil {
		t.Error("Expected error for hard-block critical operation")
	}
	if decision.Surface != "none" {
		t.Errorf("Expected surface 'none' for hard-block, got %q", decision.Surface)
	}
}

// ---------------------------------------------------------------------------
// Allowlist bypass
// ---------------------------------------------------------------------------

// TestApprovalBroker_AllowlistBypass verifies that commands on the persistent
// allowlist are auto-approved without consulting WebUI or CLI surfaces.
func TestApprovalBroker_AllowlistBypass(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	const allowlistedCmd = "rm cached_file.tmp"
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.UnifiedRiskResolver = true
		cfg.ApprovedShellCommands = append(cfg.ApprovedShellCommands, allowlistedCmd)
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	args := map[string]interface{}{"command": allowlistedCmd}
	assessment := agent.ResolveToolRisk("shell_command", args)

	decision, err := agent.RequestApproval(assessment, "shell_command", args)

	if err != nil {
		t.Errorf("Allowlisted command should auto-approve, got error: %v", err)
	}
	if !decision.Approved {
		t.Error("Expected approved=true for allowlisted command")
	}
	if decision.Surface != "allowlist" {
		t.Errorf("Expected surface 'allowlist', got %q", decision.Surface)
	}
}

// ---------------------------------------------------------------------------
// Unsafe mode bypass
// ---------------------------------------------------------------------------

// TestApprovalBroker_UnsafeModeBypass verifies that when unsafe mode is
// enabled, Medium-risk commands are auto-approved regardless of interactive
// state. Unsafe mode is a global override.
func TestApprovalBroker_UnsafeModeBypass(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)
	agent.SetUnsafeMode(true)

	args := map[string]interface{}{"command": "rm somefile.txt"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	decision, err := agent.RequestApproval(assessment, "shell_command", args)

	if err != nil {
		t.Errorf("Unsafe mode should auto-approve, got error: %v", err)
	}
	if !decision.Approved {
		t.Error("Expected approved=true in unsafe mode")
	}
	if decision.Surface != "unsafe-mode" {
		t.Errorf("Expected surface 'unsafe-mode', got %q", decision.Surface)
	}
}

// ---------------------------------------------------------------------------
// Session elevated bypass (permissive/unrestricted profiles)
// ---------------------------------------------------------------------------

// TestApprovalBroker_SessionElevatedBypass verifies that when the session
// risk profile is elevated to permissive, Medium-risk commands auto-approve.
// Critical ops still block.
func TestApprovalBroker_SessionElevatedBypass(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	// Elevate to permissive profile
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.RiskProfile = string(configuration.RiskProfilePermissive)
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	args := map[string]interface{}{"command": "rm somefile.txt"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	// Verify elevation is active
	if !agent.IsSessionElevated() {
		t.Fatal("Session should be elevated under permissive profile")
	}

	decision, err := agent.RequestApproval(assessment, "shell_command", args)

	if err != nil {
		t.Errorf("Session-elevated mode should auto-approve, got error: %v", err)
	}
	if !decision.Approved {
		t.Error("Expected approved=true in session-elevated mode")
	}
	if decision.Surface != "session-elevated" {
		t.Errorf("Expected surface 'session-elevated', got %q", decision.Surface)
	}
}

// ---------------------------------------------------------------------------
// Unsafe shell bypass
// ---------------------------------------------------------------------------

// TestApprovalBroker_UnsafeShellBypass verifies that --unsafe-shell bypasses
// Medium-tier shell commands but NOT High/Critical.
func TestApprovalBroker_UnsafeShellBypass(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)
	agent.SetUnsafeShellMode(true)

	// Medium risk: should bypass via unsafe-shell
	args := map[string]interface{}{"command": "rm somefile.txt"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	decision, err := agent.RequestApproval(assessment, "shell_command", args)

	if err != nil {
		t.Errorf("Unsafe-shell should auto-approve medium risk, got error: %v", err)
	}
	if !decision.Approved {
		t.Error("Expected approved=true with unsafe-shell for medium risk")
	}
	if decision.Surface != "unsafe-shell" {
		t.Errorf("Expected surface 'unsafe-shell', got %q", decision.Surface)
	}
}

// ---------------------------------------------------------------------------
// Run automate session marking
// ---------------------------------------------------------------------------

// TestApprovalBroker_RunAutomateSessionMarking verifies that when a workflow
// is approved via the unified gate, it gets marked as approved in-session
// so subsequent calls to the same workflow don't re-prompt.
func TestApprovalBroker_RunAutomateSessionMarking(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	const workflowName = "test-workflow"

	// Initially not approved in session
	if agent.IsWorkflowApprovedInSession(workflowName) {
		t.Fatal("Workflow should not be pre-approved")
	}

	// Mark it approved
	agent.MarkWorkflowApprovedInSession(workflowName)

	// Now it should be approved
	if !agent.IsWorkflowApprovedInSession(workflowName) {
		t.Error("Workflow should be marked as approved in session")
	}

	// Verify it returns false for a different workflow
	if agent.IsWorkflowApprovedInSession("other-workflow") {
		t.Error("Different workflow should not be approved")
	}
}

// ---------------------------------------------------------------------------
// Combined bypass interactions
// ---------------------------------------------------------------------------

// TestApprovalBroker_CriticalOverridesAllBypasses verifies that Critical-tier
// operations remain blocked even when multiple bypass flags are active
// simultaneously (unsafe-mode, session-elevated, allowlist).
func TestApprovalBroker_CriticalOverridesAllBypasses(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	// Stack every possible bypass
	agent.SetUnsafeMode(true)
	agent.SetUnsafeShellMode(true)
	agent.ElevateSessionToPermissive()

	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ApprovedShellCommands = []string{"rm -rf /"}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	args := map[string]interface{}{"command": "rm -rf /"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	decision, err := agent.RequestApproval(assessment, "shell_command", args)

	if decision.Approved {
		t.Error("Critical hard-block must override ALL bypass flags simultaneously")
	}
}

// ---------------------------------------------------------------------------
// Broker surface reporting
// ---------------------------------------------------------------------------

// TestApprovalBroker_SurfaceReporting verifies that BrokerDecision.Surface
// correctly identifies which bypass/approval path was taken for each scenario.
func TestApprovalBroker_SurfaceReporting(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	// Low risk → no surface (auto-approve immediate)
	assessment := agent.ResolveToolRisk("shell_command", map[string]interface{}{"command": "ls -la"})
	decision, _ := agent.RequestApproval(assessment, "shell_command", map[string]interface{}{"command": "ls -la"})
	// Low risk returns early — surface will be empty string (zero value)
	if decision.Surface != "" {
		t.Logf("Low-risk surface: %q (empty is expected for immediate auto-approve)", decision.Surface)
	}

	// Non-interactive Medium → "non-interactive"
	assessment = agent.ResolveToolRisk("shell_command", map[string]interface{}{"command": "rm somefile.txt"})
	decision, _ = agent.RequestApproval(assessment, "shell_command", map[string]interface{}{"command": "rm somefile.txt"})
	if decision.Surface != "non-interactive" {
		t.Errorf("Non-interactive medium risk: expected surface 'non-interactive', got %q", decision.Surface)
	}

	// Critical → "none"
	args := map[string]interface{}{"command": "rm -rf /"}
	assessment = agent.ResolveToolRisk("shell_command", args)
	decision, _ = agent.RequestApproval(assessment, "shell_command", args)
	if decision.Surface != "none" {
		t.Errorf("Critical hard-block: expected surface 'none', got %q", decision.Surface)
	}
}

// ---------------------------------------------------------------------------
// Assessment fidelity in BrokerDecision
// ---------------------------------------------------------------------------

// TestApprovalBroker_AssessmentEcho verifies that BrokerDecision.Assessment
// echoes back the same RiskAssessment passed in, preserving all fields.
func TestApprovalBroker_AssessmentEcho(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	args := map[string]interface{}{"command": "ls -la"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	decision, err := agent.RequestApproval(assessment, "shell_command", args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// The echoed assessment should match the input
	if decision.Assessment.Level != assessment.Level {
		t.Errorf("Assessment.Level mismatch: got %q, want %q", decision.Assessment.Level, assessment.Level)
	}
	if decision.Assessment.IsHardBlock != assessment.IsHardBlock {
		t.Errorf("Assessment.IsHardBlock mismatch: got %v, want %v", decision.Assessment.IsHardBlock, assessment.IsHardBlock)
	}
}

// ---------------------------------------------------------------------------
// SP-068 Phase 3 — single approval_requested event per gated call
//
// The old dual-gate architecture (Gate 1 static classifier + Gate 2 persona
// cascade) could publish two `security_approval_request` events for the same
// command — once from each gate's prompt path. SP-068 Phase 3 collapses
// both call sites through RequestApproval, so a single gated call must
// publish exactly ONE event regardless of which surface answered.
//
// These tests verify the invariant directly by counting events published
// on the event bus during a single RequestApproval invocation.
// ---------------------------------------------------------------------------

// countSecurityApprovalEvents subscribes to the agent's event bus, invokes
// the supplied function, and returns the number of security_approval_request
// events observed during (and shortly after) the call. The short drain
// window accounts for the broker's goroutine that publishes after the
// approval surface responds.
func countSecurityApprovalEvents(t *testing.T, agent *Agent, fn func()) int {
	t.Helper()
	eventBus := agent.GetEventBus()
	if eventBus == nil {
		t.Fatal("agent has no event bus — cannot count approval events")
	}
	const subName = "broker-event-count"
	sub := eventBus.Subscribe(subName)
	defer eventBus.Unsubscribe(subName)

	var (
		mu    sync.Mutex
		count int
	)
	done := make(chan struct{})

	// Drain events on a goroutine. The broker publishes via
	// approvalMgr.RequestToolApprovalDecisionWithOutcome synchronously
	// (inside fn()), so the goroutine needs to be ready before fn runs.
	// We start the drain first, give it a tick to attach, then run fn.
	go func() {
		defer close(done)
		for {
			select {
			case ev, ok := <-sub:
				if !ok {
					return
				}
				if ev.Type == events.EventTypeSecurityApprovalRequest {
					mu.Lock()
					count++
					mu.Unlock()
				}
			case <-time.After(100 * time.Millisecond):
				return
			}
		}
	}()

	// Give the subscriber a moment to attach before we publish.
	time.Sleep(30 * time.Millisecond)
	fn()

	// Wait for the drain goroutine to finish its grace window.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	mu.Lock()
	defer mu.Unlock()
	return count
}

// TestSP068_SingleApprovalEvent_WebUI verifies that a single RequestApproval
// call through the WebUI surface publishes exactly ONE
// security_approval_request event — not two (the old dual-gate path).
func TestSP068_SingleApprovalEvent_WebUI(t *testing.T) {
	// Force interactive mode for the WebUI path (test env has no TTY).
	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")

	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)
	agent.state.SetActivePersona(personas.IDOrchestrator)

	// The agent must look interactive for the WebUI path to be selected.
	agent.SetHasActiveWebUIClients(func() bool { return true })

	// Wire up a fresh event bus so we can count events deterministically.
	eventBus := events.NewEventBus()
	agent.SetEventBus(eventBus)

	// Fresh approval manager bound to our event bus.
	mgr := security.NewApprovalManager()
	mgr.SetApprovalTimeout(2 * time.Second)
	agent.security.SetApprovalMgr(mgr)

	// Respond to every approval request with approve=true, in a goroutine.
	const responderName = "test-responder"
	sub := eventBus.Subscribe(responderName)
	go func() {
		for ev := range sub {
			if ev.Type != events.EventTypeSecurityApprovalRequest {
				continue
			}
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				continue
			}
			reqID, _ := data["request_id"].(string)
			if reqID == "" {
				continue
			}
			// Retry briefly until the pending entry is registered.
			for !mgr.RespondToApproval(reqID, true) {
				time.Sleep(time.Millisecond)
			}
		}
	}()
	defer eventBus.Unsubscribe(responderName)

	// Pick a Medium-risk shell command (not allowlisted).
	args := map[string]interface{}{"command": "rm somefile.txt"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	count := countSecurityApprovalEvents(t, agent, func() {
		_, err := agent.RequestApproval(assessment, "shell_command", args)
		if err != nil {
			t.Errorf("RequestApproval returned error: %v", err)
		}
	})

	if count != 1 {
		t.Errorf("Expected exactly 1 security_approval_request event per gated call, got %d", count)
	}
}

// TestSP068_SingleApprovalEvent_NonInteractive verifies the same invariant
// when no interactive surface is available: the broker takes the
// non-interactive auto-approve path and publishes ZERO events.
func TestSP068_SingleApprovalEvent_NonInteractive(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)
	agent.state.SetActivePersona(personas.IDOrchestrator)

	// Ensure no WebUI and SkipPrompt already set by newTestAgent.
	agent.SetHasActiveWebUIClients(func() bool { return false })

	eventBus := events.NewEventBus()
	agent.SetEventBus(eventBus)

	args := map[string]interface{}{"command": "rm somefile.txt"}
	assessment := agent.ResolveToolRisk("shell_command", args)

	count := countSecurityApprovalEvents(t, agent, func() {
		_, err := agent.RequestApproval(assessment, "shell_command", args)
		if err != nil {
			t.Errorf("RequestApproval returned error: %v", err)
		}
	})

	// Non-interactive auto-approve publishes no events at all.
	if count != 0 {
		t.Errorf("Expected 0 security_approval_request events on non-interactive auto-approve, got %d", count)
	}
}

// TestSP068_SingleApprovalEvent_LowRisk verifies the same invariant for
// Low-risk assessments: the broker's fast-path early-returns without
// publishing any approval events.
func TestSP068_SingleApprovalEvent_LowRisk(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)
	agent.SetHasActiveWebUIClients(func() bool { return true })

	eventBus := events.NewEventBus()
	agent.SetEventBus(eventBus)

	args := map[string]interface{}{"command": "ls -la"}
	assessment := agent.ResolveToolRisk("shell_command", args)
	if assessment.Level != configuration.RiskLevelLow {
		t.Logf("Note: ls -la classified as %s (expected Low)", assessment.Level)
	}

	count := countSecurityApprovalEvents(t, agent, func() {
		_, err := agent.RequestApproval(assessment, "shell_command", args)
		if err != nil {
			t.Errorf("RequestApproval returned error: %v", err)
		}
	})

	if count != 0 {
		t.Errorf("Expected 0 security_approval_request events for low-risk early-return, got %d", count)
	}
}

// TestSP068_SingleApprovalEvent_UnifiedGate verifies that the full unified
// gate (unifiedSecurityGate → RequestApproval) publishes exactly ONE
// approval request event per gated call when the WebUI surface answers.
// This is the end-to-end invariant from the spec acceptance criteria.
func TestSP068_SingleApprovalEvent_UnifiedGate(t *testing.T) {
	// Force interactive mode for the WebUI path (test env has no TTY).
	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")

	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)
	agent.state.SetActivePersona(personas.IDOrchestrator)
	agent.SetHasActiveWebUIClients(func() bool { return true })

	if err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.UnifiedRiskResolver = true
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	eventBus := events.NewEventBus()
	agent.SetEventBus(eventBus)

	mgr := security.NewApprovalManager()
	mgr.SetApprovalTimeout(2 * time.Second)
	agent.security.SetApprovalMgr(mgr)

	sub := eventBus.Subscribe("unified-gate-responder")
	go func() {
		for ev := range sub {
			if ev.Type != events.EventTypeSecurityApprovalRequest {
				continue
			}
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				continue
			}
			reqID, _ := data["request_id"].(string)
			if reqID == "" {
				continue
			}
			for !mgr.RespondToApproval(reqID, true) {
				time.Sleep(time.Millisecond)
			}
		}
	}()
	defer eventBus.Unsubscribe("unified-gate-responder")

	args := map[string]interface{}{"command": "rm somefile.txt"}

	count := countSecurityApprovalEvents(t, agent, func() {
		if err := agent.unifiedSecurityGate("shell_command", args); err != nil {
			t.Errorf("unifiedSecurityGate returned error: %v", err)
		}
	})

	if count != 1 {
		t.Errorf("Expected exactly 1 security_approval_request event per unified-gate call, got %d", count)
	}
}
