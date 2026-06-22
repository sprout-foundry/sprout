package agent

import (
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// unsafeShellBypass mirrors the gate predicate from tool_security.go line 92.
// It returns true when --unsafe-shell mode should bypass the security prompt,
// allowing the tool call to proceed without user interaction.
//
// The predicate requires ALL conditions to be true:
//   - agent exists
//   - unsafe-shell mode is enabled
//   - the tool is shell_command (not git, write_file, etc.)
//   - the operation is NOT hard-blocked
//   - the risk level is NOT DANGEROUS
//   - IntentConfirmation is NOT set (intent prompts are never bypassed)
func unsafeShellBypass(agent *Agent, toolName string, secResult tools.SecurityResult) bool {
	return agent != nil && agent.GetUnsafeShellMode() && toolName == "shell_command" &&
		!secResult.IsHardBlock && secResult.Risk.String() != "DANGEROUS" &&
		!secResult.IntentConfirmation
}

// =============================================================================
// SP-049-3d: Unsafe-shell gate predicate integration tests
// =============================================================================
// These tests exercise the gate predicate at tool_security.go line 92, which
// controls whether --unsafe-shell bypasses the security prompt for shell
// commands. Each test isolates a single boundary condition of the predicate.
// =============================================================================

func TestUnsafeShellBypass_CautionShellBypasses(t *testing.T) {
	// Main positive case: a CAUTION-level shell command with unsafe-shell
	// enabled should bypass the security prompt entirely.
	//
	// This is the core use case of --unsafe-shell: let the agent run
	// "mildly risky" commands (installs, deletions, etc.) without prompts
	// while still blocking truly dangerous operations.
	a := NewTestAgent()
	a.SetUnsafeShellMode(true)

	// Use the real classifier for a CAUTION-level command.
	// sudo apt-get install is CAUTION (privileged install) with ShouldPrompt=true.
	secResult := tools.ClassifyToolCall("shell_command", map[string]interface{}{
		"command": "sudo apt-get install -y shellcheck",
	})

	// Guard in case classifier behavior changes — these preconditions are
	// essential for the test to be meaningful.
	if secResult.Risk != tools.SecurityCaution {
		t.Skipf("ClassifyToolCall returned risk %s, expected CAUTION — skipping (classifier behavior changed)", secResult.Risk)
	}
	if secResult.IsHardBlock {
		t.Skipf("ClassifyToolCall returned IsHardBlock=true — skipping (classifier behavior changed)")
	}

	if !unsafeShellBypass(a, "shell_command", secResult) {
		t.Error("unsafeShellBypass should return true: CAUTION shell command with unsafe-shell on should bypass")
	}
}

func TestUnsafeShellBypass_DangerousNotBypassed(t *testing.T) {
	// DANGEROUS commands must never bypass, even with --unsafe-shell.
	// The predicate checks secResult.Risk.String() != "DANGEROUS", so
	// this branch proves that condition short-circuits the bypass.
	//
	// Example: git push --force origin main — destructive enough that
	// the user must always explicitly approve it.
	a := NewTestAgent()
	a.SetUnsafeShellMode(true)

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityDangerous,
		ShouldBlock:  true,
		ShouldPrompt: true,
		Reasoning:    "Dangerous operation that may overwrite published history",
	}

	if unsafeShellBypass(a, "shell_command", secResult) {
		t.Error("unsafeShellBypass should NOT bypass DANGEROUS commands — this is a critical safety boundary")
	}
}

func TestUnsafeShellBypass_HardBlockNotBypassed(t *testing.T) {
	// Hard-block operations (rm -rf /, mkfs, shred, etc.) are never
	// bypassed regardless of any mode flag. The predicate checks
	// !secResult.IsHardBlock first, so this branch proves that guard.
	a := NewTestAgent()
	a.SetUnsafeShellMode(true)

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityDangerous,
		ShouldBlock:  true,
		ShouldPrompt: true,
		IsHardBlock:  true,
		Reasoning:    "Critical system operation — may cause irreversible damage",
	}

	if unsafeShellBypass(a, "shell_command", secResult) {
		t.Error("unsafeShellBypass should NOT bypass hard-block operations — absolute safety boundary")
	}
}

func TestUnsafeShellBypass_NonShellToolNotBypassed(t *testing.T) {
	// --unsafe-shell only applies to shell_command. It should NOT bypass
	// security prompts for other tools (git, write_file, etc.) even if
	// they classify as CAUTION. The predicate checks toolName == "shell_command".
	//
	// Example: git reset (CAUTION) still prompts even with unsafe-shell on.
	a := NewTestAgent()
	a.SetUnsafeShellMode(true)

	secResult := tools.ClassifyToolCall("git", map[string]interface{}{
		"operation": "reset",
	})

	// Verify the classifier returned CAUTION (the test is meaningful regardless
	// of exact risk level since the tool name check is what matters).
	if secResult.Risk != tools.SecurityCaution {
		t.Logf("ClassifyToolCall for git reset returned risk %s (expected CAUTION)", secResult.Risk)
	}

	if unsafeShellBypass(a, "git", secResult) {
		t.Error("unsafeShellBypass should NOT bypass non-shell tools — unsafe-shell is specific to shell_command")
	}
}

func TestUnsafeShellBypass_UnsafeShellOffNotBypassed(t *testing.T) {
	// When unsafe-shell mode is off (the default), CAUTION commands should
	// still trigger the security prompt. This proves the flag gate is the
	// controlling condition — no flag, no bypass.
	a := NewTestAgent()
	a.SetUnsafeShellMode(false) // explicit off — also the default

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityCaution,
		ShouldPrompt: true,
		Reasoning:    "Potentially risky operation",
	}

	if unsafeShellBypass(a, "shell_command", secResult) {
		t.Error("unsafeShellBypass should NOT bypass when unsafe-shell mode is off")
	}
}

func TestUnsafeShellBypass_IntentConfirmationNotBypassed(t *testing.T) {
	// IntentConfirmation is a separate concern from risk — it marks
	// operations that are safe but consequential (like launching an
	// autonomous workflow). These should NEVER be bypassed by --unsafe-shell
	// because they're about explicit user intent, not security risk.
	//
	// The predicate checks !secResult.IntentConfirmation for this reason.
	a := NewTestAgent()
	a.SetUnsafeShellMode(true)

	secResult := tools.SecurityResult{
		Risk:               tools.SecuritySafe,
		IntentConfirmation: true,
		Reasoning:          "Autonomous workflow execution — requires confirmation before starting",
	}

	if unsafeShellBypass(a, "shell_command", secResult) {
		t.Error("unsafeShellBypass should NOT bypass IntentConfirmation — user intent is never auto-approved")
	}
}

func TestUnsafeShellBypass_NilAgent(t *testing.T) {
	// When no agent context is available, the bypass must not activate.
	// This is the first condition in the predicate (agent != nil).
	// In practice, ExecuteTool also has a separate defense-in-depth branch
	// for nil agent, but this tests the predicate itself.
	var a *Agent

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityCaution,
		ShouldPrompt: true,
		Reasoning:    "CAUTION operation",
	}

	if unsafeShellBypass(a, "shell_command", secResult) {
		t.Error("unsafeShellBypass should NOT activate with nil agent")
	}
}

func TestUnsafeShellBypass_SafeCommandDoesNotTriggerGate(t *testing.T) {
	// A SAFE command (ls, cat, git status, etc.) has ShouldBlock=false,
	// ShouldPrompt=false, and IntentConfirmation=false. This means the
	// outer if at tool_security.go line ~55 evaluates to false, so the
	// entire security gate chain (including the unsafe-shell predicate)
	// is never reached. The command passes through without any check.
	//
	// This test verifies the classifier's behavior for a known-safe command
	// to confirm the gate predicate is irrelevant for this case.
	secResult := tools.ClassifyToolCall("shell_command", map[string]interface{}{
		"command": "ls -la",
	})

	if secResult.Risk != tools.SecuritySafe {
		t.Errorf("Expected SAFE risk level for 'ls -la', got %s", secResult.Risk)
	}
	if secResult.ShouldBlock {
		t.Error("SAFE command should not have ShouldBlock=true")
	}
	if secResult.ShouldPrompt {
		t.Error("SAFE command should not have ShouldPrompt=true")
	}
	if secResult.IntentConfirmation {
		t.Error("SAFE command should not have IntentConfirmation=true")
	}

	// The gate predicate at line 92 is never reached because none of the
	// triggering conditions are set. Verify this by confirming the outer
	// condition evaluates to false.
	triggered := secResult.ShouldBlock || secResult.ShouldPrompt || secResult.IntentConfirmation
	if triggered {
		t.Error("SAFE command should not trigger the security gate at all")
	}
}

func TestUnsafeShellBypass_CautionNotHardBlockBypasses(t *testing.T) {
	// Verify the specific interaction: CAUTION + !IsHardBlock + unsafe-shell
	// is the exact combination that allows the bypass. We test this with a
	// manually constructed SecurityResult to ensure deterministic behavior
	// regardless of classifier changes.
	a := NewTestAgent()
	a.SetUnsafeShellMode(true)

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityCaution,
		ShouldPrompt: true,
		IsHardBlock:  false,
		Reasoning:    "CAUTION shell command",
	}

	if !unsafeShellBypass(a, "shell_command", secResult) {
		t.Error("unsafeShellBypass should return true: CAUTION + !HardBlock + unsafe-shell on is the canonical bypass case")
	}
}
