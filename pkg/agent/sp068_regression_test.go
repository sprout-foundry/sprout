package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// ============================================================================
// SP-068 Regression Tests
//
// These tests lock in the two success criteria that the dual-gate → unified
// resolver consolidation must never regress:
//
//   Criterion 2: A gated command prompts the user exactly ONCE, not twice.
//                The old dual-gate architecture (Gate 1 = static classifier,
//                Gate 2 = persona risk cascade) could fire two prompts for
//                the same command. The unified resolver eliminates the
//                second gate entirely. SP-068 Phase 3 further removed the
//                approval-bridge plumbing (markShellCommandApproved,
//                consumeShellCommandApproval, recentlyApprovedShellCommands)
//                that was only needed to suppress the second prompt.
//
//   Criterion 5: Critical operations (rm -rf /, fork bombs, mkfs) still
//                hard-block even if the user adds a safe-pattern allowlist
//                entry that would match them. The Critical tier is
//                unoverridable.
// ============================================================================

// ---------------------------------------------------------------------------
// Criterion 2 — Single approval per gated call (no double-prompt)
// ---------------------------------------------------------------------------

// TestSP068_UnifiedGateApprovesMediumCommand verifies that the unified gate
// approves a Medium-risk command in non-interactive mode (permissive-by-default)
// without any error. This is the core behavior that replaces the former
// suppression-map test: since there is no Gate 2, the gate call itself is
// the single and only approval decision point.
func TestSP068_UnifiedGateApprovesMediumCommand(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	// Ensure UnifiedRiskResolver is on (the default, but be explicit).
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.UnifiedRiskResolver = true
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	// A Medium-risk command that is NOT on the allowlist. In non-interactive
	// mode (SkipPrompt=true), this goes through unifiedSecurityPrompt →
	// non-interactive auto-approve path and returns nil.
	args := map[string]interface{}{"command": "rm somefile.txt"}

	err = agent.unifiedSecurityGate("shell_command", args)

	// Non-interactive mode auto-approves Medium-risk (permissive-by-default).
	if err != nil {
		t.Errorf("unifiedSecurityGate should approve Medium-risk command in "+
			"non-interactive mode, got error: %v", err)
	}
}

// TestSP068_UnifiedGateIdempotent verifies that calling unifiedSecurityGate
// multiple times for the same command returns the same result each time
// (no accumulated state, no side effects between calls). In the old
// dual-gate path, each Gate 1 approval would mark the command for Gate 2
// consumption. The unified path is stateless across calls.
func TestSP068_UnifiedGateIdempotent(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.UnifiedRiskResolver = true
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	args := map[string]interface{}{"command": "rm temp_build_artifact.txt"}

	// Call the unified gate three times. Each call resolves independently
	// with no state leakage between them.
	for i := 0; i < 3; i++ {
		if err := agent.unifiedSecurityGate("shell_command", args); err != nil {
			t.Errorf("call %d failed: %v", i, err)
		}
	}
}

// TestSP068_AllowlistedCommandApproved verifies the allowlist short-circuit
// works correctly in the unified gate: an allowlisted command should be
// approved immediately without prompting. This replaces the former test that
// checked the suppression map had exactly one entry — the map is now deleted,
// so we verify behavior (approval) instead of implementation (map state).
func TestSP068_AllowlistedCommandApproved(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	// Add a Medium-risk command to the persistent allowlist.
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

	// The allowlist should be recognized.
	if !agent.IsShellCommandAllowlisted(allowlistedCmd) {
		t.Fatalf("IsShellCommandAllowlisted should return true for allowlisted command")
	}

	// First call: allowlist short-circuit → nil (approved).
	if err := agent.unifiedSecurityGate("shell_command", args); err != nil {
		t.Fatalf("first call should approve allowlisted command, got error: %v", err)
	}

	// Second call: should also succeed (allowlist is persistent).
	if err := agent.unifiedSecurityGate("shell_command", args); err != nil {
		t.Errorf("second call should also approve via allowlist, got error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Criterion 5 — Critical hard-block is unoverridable
// ---------------------------------------------------------------------------

// TestSP068_CriticalHardBlockUnoverridable verifies that critical operations
// (rm -rf /, fork bombs, mkfs, dd to disk) are ALWAYS classified as Critical
// with IsHardBlock=true — even when the exact command string has been added
// to the persistent allowlist (ApprovedShellCommands). This locks the
// invariant that no safe-pattern or allowlist can override a critical-tier
// hard-block.
//
// The test exercises two layers:
//  1. ResolveToolRisk — must return Critical + IsHardBlock regardless of
//     the allowlist entry.
//  2. unifiedSecurityGate — must return a non-nil security error regardless
//     of the allowlist entry.
func TestSP068_CriticalHardBlockUnoverridable(t *testing.T) {
	// Critical command corpus. Each must be recognized by
	// configuration.IsCriticalOperation (the canonical pattern list that
	// both the static classifier and the persona cascade share).
	//
	// Note: the alternate fork bomb syntax ".(){ .|.& };." is deliberately
	// excluded — IsCriticalOperation detects fork bombs via the ":()" +
	// ":|:" token pair, and the dot-variant doesn't match. The classifier
	// explicitly documents this as a known limitation.
	criticalCommands := []string{
		"rm -rf /",
		"rm -rf /*",
		"mkfs.ext3 /dev/sda",
		"mkfs.ext4 /dev/sdb",
		"dd if=/dev/zero of=/dev/sda",
		":(){ :|:& };:", // fork bomb — canonical bash syntax
		":(){:|:&};:",   // fork bomb — no spaces, still detected (:() + :|:)
	}

	for _, cmd := range criticalCommands {
		t.Run(cmd, func(t *testing.T) {
			agent := newTestAgent(t)
			defer agent.Shutdown()

			workspace := t.TempDir()
			agent.SetWorkspaceRoot(workspace)

			// BYPASS ATTEMPT: add the exact critical command to the
			// persistent allowlist. A broken implementation might let this
			// short-circuit the critical check.
			err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
				cfg.UnifiedRiskResolver = true
				cfg.ApprovedShellCommands = append(cfg.ApprovedShellCommands, cmd)
				return nil
			})
			if err != nil {
				t.Fatalf("UpdateConfigNoSave failed: %v", err)
			}

			// Confirm the allowlist entry IS recognized (the bypass is in
			// place — the critical block must override it).
			if !agent.IsShellCommandAllowlisted(cmd) {
				t.Fatalf("IsShellCommandAllowlisted should return true — bypass is in place")
			}

			args := map[string]interface{}{"command": cmd}

			// LAYER 1: ResolveToolRisk must return Critical + IsHardBlock
			// regardless of the allowlist.
			assessment := agent.ResolveToolRisk("shell_command", args)

			if assessment.Level != configuration.RiskLevelCritical {
				t.Errorf("Level = %q, want Critical (allowlist must not override critical tier)\n%s",
					assessment.Level, assessment.Explain())
			}
			if !assessment.IsHardBlock {
				t.Errorf("IsHardBlock = false, want true for critical operation\n%s",
					assessment.Explain())
			}

			// The sources must include RiskSourceCriticalOp (the built-in
			// hard-block), not just RiskSourceClassifier.
			foundCriticalOp := false
			for _, src := range assessment.Sources {
				if src == RiskSourceCriticalOp {
					foundCriticalOp = true
					break
				}
			}
			if !foundCriticalOp {
				t.Errorf("Sources %v must contain RiskSourceCriticalOp for a hard-blocked critical op",
					assessment.Sources)
			}

			// LAYER 2: unifiedSecurityGate must return a non-nil error
			// (the hard-block security error) regardless of the allowlist.
			err = agent.unifiedSecurityGate("shell_command", args)
			if err == nil {
				t.Fatal("unifiedSecurityGate should return error for critical operation " +
					"even with allowlist entry — got nil")
			}

			// The error must be a security error (ActionEscalate), not
			// some other category like a transient retry.
			if action := ClassifyError(err); action != ActionEscalate {
				t.Errorf("unifiedSecurityGate error should classify as ActionEscalate (security), got %v: %v", action, err)
			}
		})
	}
}

// TestSP068_CriticalBlocksWhileAllowlistWorksForNonCritical is the
// complementary assertion to the critical-block test: it proves the
// allowlist is not broken for NON-critical commands. If the allowlist
// were globally disabled (a broken fix for the critical-block concern),
// this test would fail. The allowlist must work for High/Medium commands
// while critical ops still punch through.
func TestSP068_CriticalBlocksWhileAllowlistWorksForNonCritical(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)

	// Put both a critical command and a safe command on the same allowlist.
	err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.UnifiedRiskResolver = true
		cfg.ApprovedShellCommands = []string{
			"rm -rf /",   // critical — must still block
			"ls -la",     // safe (Low) — allowlist works
			"git status", // safe (Low) — allowlist works
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	// The critical command must block despite being allowlisted.
	t.Run("critical_command_blocks", func(t *testing.T) {
		args := map[string]interface{}{"command": "rm -rf /"}
		err := agent.unifiedSecurityGate("shell_command", args)
		if err == nil {
			t.Fatal("rm -rf / must block even when on the allowlist")
		}
	})

	// The safe commands must pass through the gate (allowlist works).
	t.Run("safe_command_passes", func(t *testing.T) {
		args := map[string]interface{}{"command": "ls -la"}
		err := agent.unifiedSecurityGate("shell_command", args)
		if err != nil {
			t.Errorf("ls -la should pass (Low risk / allowlisted), got error: %v", err)
		}
	})

	t.Run("safe_command_passes_2", func(t *testing.T) {
		args := map[string]interface{}{"command": "git status"}
		err := agent.unifiedSecurityGate("shell_command", args)
		if err != nil {
			t.Errorf("git status should pass (Low risk / allowlisted), got error: %v", err)
		}
	})
}

// TestSP068_CriticalPersistsAcrossRiskProfiles verifies that changing the
// risk profile to "permissive" or "unrestricted" does NOT weaken the
// critical-tier block. The Critical tier is orthogonal to risk profiles —
// no profile setting can auto-approve a critical operation.
func TestSP068_CriticalPersistsAcrossRiskProfiles(t *testing.T) {
	profiles := []configuration.RiskProfile{
		configuration.RiskProfilePermissive,
		configuration.RiskProfileUnrestricted,
	}

	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			agent := newTestAgent(t)
			defer agent.Shutdown()

			workspace := t.TempDir()
			agent.SetWorkspaceRoot(workspace)

			err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
				cfg.UnifiedRiskResolver = true
				cfg.RiskProfile = string(profile)
				// Also add to allowlist — belt and suspenders.
				cfg.ApprovedShellCommands = []string{"rm -rf /"}
				return nil
			})
			if err != nil {
				t.Fatalf("UpdateConfigNoSave failed: %v", err)
			}

			agent.state.SetActivePersona(personas.IDOrchestrator)

			args := map[string]interface{}{"command": "rm -rf /"}
			assessment := agent.ResolveToolRisk("shell_command", args)

			if assessment.Level != configuration.RiskLevelCritical {
				t.Errorf("Level = %q, want Critical even under %s profile\n%s",
					assessment.Level, profile, assessment.Explain())
			}
			if !assessment.IsHardBlock {
				t.Errorf("IsHardBlock = false, want true under %s profile", profile)
			}

			err = agent.unifiedSecurityGate("shell_command", args)
			if err == nil {
				t.Errorf("rm -rf / must block under %s profile even with allowlist", profile)
			}
		})
	}
}
