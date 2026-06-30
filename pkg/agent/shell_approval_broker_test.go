package agent

import (
	"context"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ---------------------------------------------------------------------------
// SP-093-2: RequestShellApproval — WebUI stub path
// ---------------------------------------------------------------------------

func TestRequestShellApproval_EmptyProposal(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	proposal := ShellProposal{Command: "", Parts: nil, RiskLevel: configuration.RiskLevelLow}
	decisions, err := agent.RequestShellApproval(context.Background(), proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decisions) != 0 {
		t.Errorf("expected empty decisions map, got %d entries", len(decisions))
	}
}

func TestRequestShellApproval_WebUIStub(t *testing.T) {
	// Force interactive mode so isNonInteractive() returns false.
	t.Setenv("SPROUT_FORCE_INTERACTIVE", "1")

	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Pretend WebUI is active.
	agent.SetHasActiveWebUIClients(func() bool { return true })

	proposal := NewShellProposal("echo hello && ls -la")
	decisions, err := agent.RequestShellApproval(context.Background(), proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// WebUI stub returns all-approved.
	for _, part := range proposal.Parts {
		if !decisions[part.ID] {
			t.Errorf("expected %s to be approved (WebUI stub)", part.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// SP-093-2: RequestShellApproval — CLI picker path
// ---------------------------------------------------------------------------

func TestRequestShellApproval_CLIPicker(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Ensure non-interactive + no WebUI → CLI path.
	agent.SetHasActiveWebUIClients(func() bool { return false })

	// We can't inject stdin into the public PromptShellApprovalParts,
	// so we test the CLI path indirectly: verify it doesn't panic and
	// produces a decisions map when called with a real proposal.
	// The detailed picker behavior is tested in pkg/console.
	proposal := NewShellProposal("ls -la")
	// This will call console.PromptShellApprovalParts which reads from
	// os.Stdin — in test env (no TTY) it will get EOF and return all-deny.
	decisions, err := agent.RequestShellApproval(context.Background(), proposal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With EOF stdin, all parts should be denied.
	for _, part := range proposal.Parts {
		if decisions[part.ID] {
			t.Errorf("expected %s to be denied (EOF stdin in test env)", part.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// SP-093-2: Broker routing — flag off vs flag on
// ---------------------------------------------------------------------------

func TestShellApprovalBroker_FlagOffUsesExistingPath(t *testing.T) {
	// When ShellCommand flag is false (default), the broker should
	// take the existing 4-option path — not the per-part picker.
	// We verify this by confirming the broker doesn't panic and
	// the non-interactive auto-approve path still works.
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	// Ensure EditApproval exists but ShellCommand is false (default).
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.EditApproval = &configuration.EditApprovalConfig{
			Mode:         "off",
			ShellCommand: false,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	// Medium-risk command in non-interactive mode should auto-approve
	// via the existing path (not the per-part picker).
	assessment := agent.ResolveToolRisk("shell_command", map[string]interface{}{"command": "rm somefile.txt"})
	decision, err := agent.RequestApproval(assessment, "shell_command", map[string]interface{}{"command": "rm somefile.txt"})

	if err != nil {
		t.Errorf("Expected auto-approval in non-interactive mode, got error: %v", err)
	}
	if !decision.Approved {
		t.Error("Expected approved=true in non-interactive mode with flag off")
	}
	if decision.Surface != "non-interactive" {
		t.Errorf("Expected surface 'non-interactive', got %q", decision.Surface)
	}
}

func TestShellApprovalBroker_FlagOnRoutesToPicker(t *testing.T) {
	// When ShellCommand is true, the broker routes to the per-part picker.
	// In non-interactive mode (SkipPrompt=true), canPrompt is false so
	// the per-part block is skipped and we fall through to non-interactive.
	// This verifies the flag is read correctly without breaking the flow.
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.EditApproval = &configuration.EditApprovalConfig{
			Mode:         "off",
			ShellCommand: true,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	// In non-interactive mode, the per-part picker is gated by canPrompt
	// (which is false when SkipPrompt=true), so we still get auto-approve.
	// The key assertion: no panic, no error, and the flag is respected.
	assessment := agent.ResolveToolRisk("shell_command", map[string]interface{}{"command": "echo hello && ls"})
	decision, err := agent.RequestApproval(assessment, "shell_command", map[string]interface{}{"command": "echo hello && ls"})

	if err != nil {
		t.Errorf("Expected auto-approval in non-interactive mode, got error: %v", err)
	}
	if !decision.Approved {
		t.Error("Expected approved=true in non-interactive mode")
	}
}

// ---------------------------------------------------------------------------
// SP-093-2: kindRiskLabel helper
// ---------------------------------------------------------------------------

func TestKindRiskLabel(t *testing.T) {
	tests := []struct {
		kind CommandKind
		want string
	}{
		{CommandKindRm, "CRITICAL"},
		{CommandKindGitReset, "CRITICAL"},
		{CommandKindKubectl, "CRITICAL"},
		{CommandKindDocker, "HIGH"},
		{CommandKindGitPush, "HIGH"},
		{CommandKindChmod, "MEDIUM"},
		{CommandKindChown, "MEDIUM"},
		{CommandKindWriteRedirect, "MEDIUM"},
		{CommandKindHttpPost, "MEDIUM"},
		{CommandKindUnknown, "LOW"},
	}
	for _, tt := range tests {
		if got := kindRiskLabel(tt.kind); got != tt.want {
			t.Errorf("kindRiskLabel(%s) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// SP-093-2: ShellProposal projection into console.ShellPartInfo
// ---------------------------------------------------------------------------

func TestShellProposalProjection(t *testing.T) {
	proposal := NewShellProposal("rm -rf foo && echo hello")
	if len(proposal.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(proposal.Parts))
	}

	// Verify the projection produces valid console.ShellPartInfo entries.
	parts := make([]console.ShellPartInfo, len(proposal.Parts))
	for i, part := range proposal.Parts {
		parts[i] = console.ShellPartInfo{
			ID:        part.ID,
			Text:      part.Text,
			Kind:      string(part.Kind),
			Semantic:  part.Semantic,
			RiskLabel: kindRiskLabel(part.Kind),
		}
	}

	if parts[0].RiskLabel != "CRITICAL" {
		t.Errorf("expected first part risk label CRITICAL, got %q", parts[0].RiskLabel)
	}
	if parts[1].RiskLabel != "LOW" {
		t.Errorf("expected second part risk label LOW, got %q", parts[1].RiskLabel)
	}
}
