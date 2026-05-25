package agent

import (
	"context"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestIsShellCommandAllowlisted(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if a.IsShellCommandAllowlisted("rm -rf /tmp/build") {
		t.Fatal("fresh agent should not have any allowlisted commands")
	}

	// Inject one via the config manager (skip Save) so we can assert
	// the lookup path independently from persistence.
	if err := a.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ApprovedShellCommands = []string{"rm -rf /tmp/build"}
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	if !a.IsShellCommandAllowlisted("rm -rf /tmp/build") {
		t.Error("allowlisted command not recognized")
	}
	if a.IsShellCommandAllowlisted("rm -rf /tmp/other") {
		t.Error("literal-match should not approve a different command")
	}
	if a.IsShellCommandAllowlisted("") {
		t.Error("empty command must not match")
	}
}

func TestPersistShellCommandAllowlist(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	cmd := "kubectl delete pod some-pod"
	if err := a.PersistShellCommandAllowlist(cmd); err != nil {
		t.Fatalf("PersistShellCommandAllowlist: %v", err)
	}
	if !a.IsShellCommandAllowlisted(cmd) {
		t.Error("persisted command not reflected in IsShellCommandAllowlisted")
	}

	// Idempotency — persisting the same command twice should not duplicate.
	if err := a.PersistShellCommandAllowlist(cmd); err != nil {
		t.Fatalf("re-persist: %v", err)
	}
	got := a.GetConfig().ApprovedShellCommands
	count := 0
	for _, c := range got {
		if c == cmd {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry for %q, got %d (list: %v)", cmd, count, got)
	}
}

func TestPersistShellCommandAllowlist_RejectsEmpty(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if err := a.PersistShellCommandAllowlist(""); err == nil {
		t.Error("expected error for empty command")
	}
}

func TestElevateSessionToPermissive(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	if got := a.GetActiveRiskProfile(); got == configuration.RiskProfilePermissive {
		t.Fatalf("test precondition violated: profile already %q", got)
	}
	a.ElevateSessionToPermissive()
	if got := a.GetActiveRiskProfile(); got != configuration.RiskProfilePermissive {
		t.Errorf("after elevate: profile = %q, want permissive", got)
	}
}

// TestHighRiskApprovedForCommand_AllowlistShortCircuit verifies Gate 2
// auto-approves an allowlisted command without prompting (no event bus
// configured here, so a prompt would otherwise reject).
func TestHighRiskApprovedForCommand_AllowlistShortCircuit(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	cmd := "rm -rf /tmp/sprout-test-allowlist"
	if err := a.PersistShellCommandAllowlist(cmd); err != nil {
		t.Fatalf("PersistShellCommandAllowlist: %v", err)
	}
	ctx := context.Background()
	if !a.highRiskApprovedForCommand(ctx, cmd) {
		t.Error("allowlisted command should be auto-approved by Gate 2")
	}
	// Non-allowlisted variant should still take the prompt path.
	// With no interactive logger AND no event bus, it must reject.
	if a.highRiskApprovedForCommand(ctx, "rm -rf /tmp/sprout-different-path") {
		t.Error("non-allowlisted command should not be auto-approved")
	}
}
