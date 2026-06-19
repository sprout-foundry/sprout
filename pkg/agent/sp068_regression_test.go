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
//                second gate entirely.
//
//   Criterion 5: Critical operations (rm -rf /, fork bombs, mkfs) still
//                hard-block even if the user adds a safe-pattern allowlist
//                entry that would match them. The Critical tier is
//                unoverridable.
// ============================================================================

// ---------------------------------------------------------------------------
// Criterion 2 — Single approval per gated call (no double-prompt)
// ---------------------------------------------------------------------------

// TestSP068_UnifiedGateDoesNotPopulateSuppressionMap is the core regression
// test for criterion 2. The old dual-gate architecture relied on the
// recentlyApprovedShellCommands map (markShellCommandApproved →
// consumeShellCommandApproval) to prevent Gate 2 from re-prompting after
// Gate 1 already approved. If the unified gate were to populate this map,
// it would mean the gate plumbing is doing redundant work — the telltale
// sign of a double-prompt regression.
//
// The invariant: under UnifiedRiskResolver=true, the approval-suppression
// map is NEVER populated by the unified gate for a non-allowlisted command.
// The map is only touched when a command IS allowlisted (the one
// markShellCommandApproved call in unifiedSecurityPrompt), which is correct
// — the allowlist path marks approval so a hypothetical Gate 2 would skip,
// but since Gate 2 doesn't fire in unified mode, the entry is harmless.
//
// For a command that goes through the interactive/non-interactive prompt
// path (not allowlisted), the map must remain empty — proving the
// suppression plumbing is dead in unified mode.
func TestSP068_UnifiedGateDoesNotPopulateSuppressionMap(t *testing.T) {
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
	// non-interactive auto-approve path and returns nil WITHOUT touching
	// the suppression map.
	args := map[string]interface{}{"command": "rm somefile.txt"}

	err = agent.unifiedSecurityGate("shell_command", args)

	// Non-interactive mode auto-approves Medium-risk (permissive-by-default).
	// The gate must either return nil (approved) or an error (rejected),
	// but it must NOT populate the suppression map.
	if err != nil {
		// An error is acceptable if the command was classified differently
		// than expected — but let's check that it's a security error, not a
		// crash. The key assertion below is about the map, not the result.
		t.Logf("unifiedSecurityGate returned error for Medium command (acceptable): %v", err)
	}

	// THE KEY ASSERTION: the legacy approval-suppression map must be empty
	// after a unified gate call for a non-allowlisted command. This proves
	// the suppression plumbing (markShellCommandApproved →
	// consumeShellCommandApproval) is not being used — there is no Gate 2
	// to suppress, so the double-prompt bug cannot occur.
	mapPopulated := false
	agent.recentlyApprovedShellCommands.Range(func(key, value any) bool {
		mapPopulated = true
		return false
	})
	if mapPopulated {
		t.Error("recentlyApprovedShellCommands map should be empty after unified gate " +
			"call for a non-allowlisted command — the suppression plumbing is dead in unified mode")
	}
}

// TestSP068_UnifiedGateIdempotentNoDoubleApproval verifies that calling
// unifiedSecurityGate multiple times for the same command does not
// accumulate entries in the suppression map. In the old dual-gate path,
// each Gate 1 approval would mark the command, and Gate 2 would consume
// it — a fresh Gate 1 call would mark again. In the unified path, the
// gate is stateless across calls (for non-allowlisted commands), so
// repeated calls must not accumulate state.
func TestSP068_UnifiedGateIdempotentNoDoubleApproval(t *testing.T) {
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

	// Call the unified gate three times — simulating what would have been
	// Gate1 + Gate2 + a retry in the old architecture. Each call resolves
	// independently; no state should leak between them.
	for i := 0; i < 3; i++ {
		_ = agent.unifiedSecurityGate("shell_command", args)
	}

	// After multiple calls, the suppression map must still be empty for
	// a non-allowlisted command. The old dual-gate path would have left
	// stale entries (one per Gate 1 invocation) that Gate 2 consumed —
	// the unified path doesn't produce any.
	count := 0
	agent.recentlyApprovedShellCommands.Range(func(key, value any) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("suppression map should have 0 entries after 3 unified gate calls "+
			"for a non-allowlisted command, got %d", count)
	}
}

// TestSP068_AllowlistedCommandMarksSuppressionButNoDoublePrompt confirms
// the ONE scenario where the suppression map IS legitimately populated in
// unified mode: when a command is on the persistent allowlist. In that
// case, unifiedSecurityPrompt calls markShellCommandApproved before
// returning nil. This is correct and safe — it doesn't cause a
// double-prompt because there's no Gate 2 to consume the entry.
//
// The test proves: (a) the allowlist short-circuit works, (b) the map
// gets exactly ONE entry (not two), and (c) the entry doesn't cause
// any observable double-prompt behavior.
func TestSP068_AllowlistedCommandMarksSuppressionButNoDoublePrompt(t *testing.T) {
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

	// First call: allowlist short-circuit → markShellCommandApproved → nil.
	err = agent.unifiedSecurityGate("shell_command", args)
	if err != nil {
		t.Fatalf("unifiedSecurityGate should approve allowlisted command, got error: %v", err)
	}

	// The map should have exactly ONE entry (the allowlisted command).
	count := 0
	var foundKey string
	agent.recentlyApprovedShellCommands.Range(func(key, value any) bool {
		count++
		if k, ok := key.(string); ok {
			foundKey = k
		}
		return true
	})
	if count != 1 {
		t.Errorf("suppression map should have exactly 1 entry after allowlisted command, got %d", count)
	}
	if foundKey != allowlistedCmd {
		t.Errorf("suppression map key = %q, want %q", foundKey, allowlistedCmd)
	}

	// Second call: should still succeed (allowlist is checked before the
	// map; the stale entry doesn't interfere). This proves no double-prompt:
	// the second call doesn't re-prompt because the allowlist short-circuits.
	err = agent.unifiedSecurityGate("shell_command", args)
	if err != nil {
		t.Errorf("second unifiedSecurityGate call should also approve via allowlist, got error: %v", err)
	}

	// The map should STILL have exactly one entry — the second call's
	// markShellCommandApproved overwrote the same key (Store is idempotent
	// for the same key), not added a new one.
	count = 0
	agent.recentlyApprovedShellCommands.Range(func(key, value any) bool {
		count++
		return true
	})
	if count != 1 {
		t.Errorf("suppression map should still have 1 entry (overwritten, not duplicated) "+
			"after second allowlisted call, got %d", count)
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
//   1. ResolveToolRisk — must return Critical + IsHardBlock regardless of
//      the allowlist entry.
//   2. unifiedSecurityGate — must return a non-nil security error regardless
//      of the allowlist entry.
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
			"rm -rf /",     // critical — must still block
			"ls -la",       // safe (Low) — allowlist works
			"git status",   // safe (Low) — allowlist works
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
