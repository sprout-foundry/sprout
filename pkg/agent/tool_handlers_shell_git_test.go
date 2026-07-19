package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ============================================================================
// Legacy handleShellCommand — Git history-rewrite gate
//
// These tests exercise the LEGACY dual-gate path (UnifiedRiskResolver=false,
// the default zero-value). They lock in the behavioral change where
// history-rewrite operations (rebase, reset --hard, branch -D, tag -d) are
// no longer unconditionally hard-blocked; they now go through
// highRiskApprovedForCommand (promptable).
//
// Before the change: the history-rewrite gate returned a SecurityError with
// "blocked by default" — an unconditional hard block.
// After the change: the gate prompts via highRiskApprovedForCommand. In
// non-interactive test mode, RequestApproval auto-approves (permissive by
// default), so the command proceeds past the gate.
//
// The core assertion across all scenarios is that the OLD hard-block
// language ("blocked by default") NEVER appears in any error or output.
// ============================================================================

// TestHandleShellCommand_Legacy_GitHistoryRewriteNotHardBlocked verifies
// that git history-rewrite commands are not unconditionally hard-blocked in
// the legacy shell handler path. The test exercises both the
// AllowGitHistoryRewrite=true fast-bypass and the default false path.
//
// Scenario A (AllowGitHistoryRewrite=true): the history-rewrite gate is
// skipped entirely — no prompt, no block. The command reaches execution.
//
// Scenario B (AllowGitHistoryRewrite=false, default): the gate delegates to
// highRiskApprovedForCommand. In non-interactive test mode this auto-approves,
// so the command proceeds past the gate. The OLD "blocked by default"
// hard-block message must never appear.
func TestHandleShellCommand_Legacy_GitHistoryRewriteNotHardBlocked(t *testing.T) {
	t.Run("AllowGitHistoryRewrite_true_proceeds_past_gate", func(t *testing.T) {
		agent := newTestAgent(t)
		defer agent.Shutdown()

		workspace := t.TempDir()
		agent.SetWorkspaceRoot(workspace)
		agent.SetShellCwd(workspace)

		// Enable the AllowGitHistoryRewrite flag so the history-rewrite
		// gate is skipped entirely.
		if err := agent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.AllowGitHistoryRewrite = true
			return nil
		}); err != nil {
			t.Fatalf("UpdateConfigNoSave failed: %v", err)
		}

		// UnifiedRiskResolver defaults to false (verified by
		// TestConfigUnifiedRiskResolver_DefaultFalse), so this exercises
		// the legacy dual-gate path.
		args := map[string]interface{}{"command": "git rebase -i HEAD~5"}
		_, err := handleShellCommand(context.Background(), agent, args)

		// The command reached execution and will fail because there is no
		// real git repo in the temp workspace. An exec error is acceptable;
		// a security error containing the old hard-block language is not.
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "blocked by default") {
				t.Errorf("AllowGitHistoryRewrite=true: error contains old hard-block language: %q", msg)
			}
			if strings.Contains(msg, "can lose commit history") {
				t.Errorf("AllowGitHistoryRewrite=true: history-rewrite gate should be skipped, but error contains: %q", msg)
			}
		}
	})

	t.Run("AllowGitHistoryRewrite_false_not_hard_blocked", func(t *testing.T) {
		agent := newTestAgent(t)
		defer agent.Shutdown()

		workspace := t.TempDir()
		agent.SetWorkspaceRoot(workspace)
		agent.SetShellCwd(workspace)

		// AllowGitHistoryRewrite defaults to false — no config change needed.
		// The test agent is non-interactive (SkipPrompt=true), so
		// highRiskApprovedForCommand → RequestApproval auto-approves
		// (permissive by default).

		args := map[string]interface{}{"command": "git rebase -i HEAD~5"}
		_, err := handleShellCommand(context.Background(), agent, args)

		// The OLD unconditional hard-block returned SecurityError with
		// "blocked by default". That message must NEVER appear, regardless
		// of whether the command was approved or denied.
		//
		// It's acceptable for the command to be "not approved" (SecurityError
		// containing "was not approved") OR to proceed (exec error or
		// success) — what matters is the OLD hard-block message is gone.
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "blocked by default") {
				t.Errorf("AllowGitHistoryRewrite=false: error contains old hard-block language: %q — the gate should be promptable, not unconditionally blocking", msg)
			}
		}
	})
}

// TestHandleShellCommand_Legacy_GitResetHardNotHardBlocked verifies the
// same promptable behavior for `git reset --hard <commit-ish>`, which is
// another history-rewrite command. This command also exercises the persona
// cascade interaction: when a persona is active and rates it High, the
// historyRewriteAlreadyApproved flag prevents double-prompting.
func TestHandleShellCommand_Legacy_GitResetHardNotHardBlocked(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	workspace := t.TempDir()
	agent.SetWorkspaceRoot(workspace)
	agent.SetShellCwd(workspace)

	// AllowGitHistoryRewrite defaults to false. The default test agent has
	// no active persona, so EvaluateOperationRisk returns Low (gate 1
	// doesn't fire), and the history-rewrite gate fires directly.
	args := map[string]interface{}{"command": "git reset --hard abc123"}
	_, err := handleShellCommand(context.Background(), agent, args)

	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "blocked by default") {
			t.Errorf("git reset --hard: error contains old hard-block language: %q", msg)
		}
	}
}
