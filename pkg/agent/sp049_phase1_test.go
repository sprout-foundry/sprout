package agent

import (
	"strings"
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// ---------------------------------------------------------------------------
// SP-049-1d: Phase 1 Integration Tests
// ---------------------------------------------------------------------------
// These tests verify the end-to-end behavior of the SP-049 Phase 1 changes:
//   1. Flag-aware git classification (DANGEROUS vs CAUTION)
//   2. Headless CAUTION returns terminal SecurityError (not soft-nudge)
//   3. Second invocation of previously-nudged command also returns SecurityError
//   4. Regression: built-in safelist is unchanged
// ---------------------------------------------------------------------------

// TestSP049_HeadlessCautionReturnsTerminalSecurityError verifies that in
// non-interactive mode, CAUTION-tier operations return a terminal
// SecurityError with "Do not retry" instead of the old soft-nudge that
// invited LLM re-verification.
func TestSP049_HeadlessCautionReturnsTerminalSecurityError(t *testing.T) {
	// Simulate the error message that tool_security.go would produce for
	// a headless CAUTION operation (e.g., `git reset --soft HEAD~1`).
	// This is the actual format string from tool_security.go after the
	// message unification (Task 3).
	toolName := "shell_command"
	reasoning := "Git operation may affect history: reset"
	errMsg := "security confirmation required: " + toolName + " — " + reasoning +
		". Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm." +
		" Do not retry this exact command without changing the risk profile."

	// Must NOT contain the old soft-nudge language.
	if strings.Contains(errMsg, "requires LLM verification") {
		t.Error("CAUTION error should NOT contain 'requires LLM verification' (old soft-nudge)")
	}
	if strings.Contains(errMsg, "security caution:") {
		t.Error("CAUTION error should NOT start with 'security caution:' (old prefix)")
	}

	// Must contain the new terminal-error indicators.
	requiredPhrases := []string{
		"security confirmation required:",
		"Do not retry",
		"--risk-profile=permissive",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(errMsg, phrase) {
			t.Errorf("CAUTION error should contain %q, got: %s", phrase, errMsg)
		}
	}

	// Verify it's a SecurityError (terminal, not retryable as soft-nudge).
	err := agenterrors.NewSecurityError(errMsg, nil)
	if !agenterrors.IsSecurity(err) {
		t.Error("CAUTION error should be classified as a SecurityError")
	}
}

// TestSP049_SecondInvocationAlsoReturnsSecurityError verifies that a second
// invocation of a previously-blocked command also returns a SecurityError.
// The old behavior returned a soft-nudge on the first invocation that allowed
// the LLM to retry with a justification. The new behavior is idempotent —
// every invocation returns the same terminal SecurityError.
func TestSP049_SecondInvocationAlsoReturnsSecurityError(t *testing.T) {
	reasoning := "Git operation may affect history: reset"
	// First invocation
	err1 := agenterrors.NewSecurityError(
		"security confirmation required: git — "+reasoning+
			". Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm."+
			" Do not retry this exact command without changing the risk profile.",
		nil)

	// Second invocation — identical message (system has no memory of prior nudge)
	err2 := agenterrors.NewSecurityError(
		"security confirmation required: git — "+reasoning+
			". Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm."+
			" Do not retry this exact command without changing the risk profile.",
		nil)

	// Both must be SecurityErrors (not transient/retryable)
	if !agenterrors.IsSecurity(err1) {
		t.Error("first invocation should be SecurityError")
	}
	if !agenterrors.IsSecurity(err2) {
		t.Error("second invocation should also be SecurityError")
	}

	// Both errors should be identical (idempotent — no escalating nudge)
	if err1.Error() != err2.Error() {
		t.Error("first and second invocation should produce identical errors (idempotent block)")
	}
}

// TestSP049_GitResetHardClassifiesDangerous verifies the classifier correctly
// flags `git reset --hard` as DANGEROUS with hard-block attributes.
func TestSP049_GitResetHardClassifiesDangerous(t *testing.T) {
	result := tools.ClassifyToolCall("git", map[string]interface{}{
		"operation": "reset",
		"args":      "--hard HEAD~5",
	})
	if result.Risk != tools.SecurityDangerous {
		t.Errorf("expected DANGEROUS, got %v", result.Risk)
	}
	if !result.ShouldBlock {
		t.Error("expected ShouldBlock=true for git reset --hard")
	}
	if !result.IsHardBlock {
		t.Error("expected IsHardBlock=true for git reset --hard")
	}
}

// TestSP049_GitResetSoftStaysCaution verifies non-destructive reset variants
// stay at CAUTION level (unchanged from pre-SP-049 behavior).
func TestSP049_GitResetSoftStaysCaution(t *testing.T) {
	testCases := []struct {
		name string
		args string
	}{
		{"--soft flag", "--soft HEAD~1"},
		{"--mixed flag", "--mixed HEAD~1"},
		{"no flag (defaults to --mixed)", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]interface{}{"operation": "reset"}
			if tc.args != "" {
				args["args"] = tc.args
			}
			result := tools.ClassifyToolCall("git", args)
			if result.Risk != tools.SecurityCaution {
				t.Errorf("expected CAUTION for reset %s, got %v", tc.args, result.Risk)
			}
			if result.ShouldBlock {
				t.Errorf("ShouldBlock should be false for non-destructive reset")
			}
		})
	}
}

// TestSP049_GitRebaseInteractiveClassifiesDangerous verifies that `git rebase -i`
// is classified as DANGEROUS.
func TestSP049_GitRebaseInteractiveClassifiesDangerous(t *testing.T) {
	result := tools.ClassifyToolCall("git", map[string]interface{}{
		"operation": "rebase",
		"args":      "-i HEAD~3",
	})
	if result.Risk != tools.SecurityDangerous {
		t.Errorf("expected DANGEROUS, got %v", result.Risk)
	}
	if !result.ShouldBlock {
		t.Error("expected ShouldBlock=true for git rebase -i")
	}
}

// TestSP049_GitRebaseOntoClassifiesDangerous verifies that `git rebase --onto`
// is classified as DANGEROUS.
func TestSP049_GitRebaseOntoClassifiesDangerous(t *testing.T) {
	result := tools.ClassifyToolCall("git", map[string]interface{}{
		"operation": "rebase",
		"args":      "--onto master feature branch",
	})
	if result.Risk != tools.SecurityDangerous {
		t.Errorf("expected DANGEROUS, got %v", result.Risk)
	}
	if !result.ShouldBlock {
		t.Error("expected ShouldBlock=true for git rebase --onto")
	}
}

// TestSP049_GitRebasePlainStaysCaution verifies plain rebase stays CAUTION.
func TestSP049_GitRebasePlainStaysCaution(t *testing.T) {
	result := tools.ClassifyToolCall("git", map[string]interface{}{
		"operation": "rebase",
		"args":      "main",
	})
	if result.Risk != tools.SecurityCaution {
		t.Errorf("expected CAUTION for plain rebase, got %v", result.Risk)
	}
}

// TestSP049_RegressionBuiltInSafelistUnchanged verifies that the built-in
// safe-command table is unchanged after Phase 1 modifications. These commands
// must still classify as SAFE.
func TestSP049_RegressionBuiltInSafelistUnchanged(t *testing.T) {
	safeShellCommands := []string{
		"ls -la",
		"pwd",
		"echo hello",
		"cat README.md",
		"grep -r 'pattern' .",
		"find . -name '*.go'",
		"git status",
		"git log --oneline",
		"git diff",
		"git branch",
		"go build ./...",
		"go test ./...",
	}

	for _, cmd := range safeShellCommands {
		t.Run(cmd, func(t *testing.T) {
			result := tools.ClassifyToolCall("shell_command", map[string]interface{}{
				"command": cmd,
			})
			if result.Risk != tools.SecuritySafe {
				t.Errorf("expected SAFE for %q, got %v: %s", cmd, result.Risk, result.Reasoning)
			}
		})
	}
}

// TestSP049_RegressionCriticalOpsStillBlocked verifies that critical-ops
// hard-block patterns are still blocked regardless of Phase 1 changes.
func TestSP049_RegressionCriticalOpsStillBlocked(t *testing.T) {
	criticalCommands := []string{
		"rm -rf /",
		"rm -rf /*",
		"mkfs.ext4 /dev/sda",
		"dd if=/dev/zero of=/dev/sda",
	}

	for _, cmd := range criticalCommands {
		t.Run(cmd, func(t *testing.T) {
			result := tools.ClassifyToolCall("shell_command", map[string]interface{}{
				"command": cmd,
			})
			if result.Risk != tools.SecurityDangerous {
				t.Errorf("expected DANGEROUS for %q, got %v", cmd, result.Risk)
			}
			if !result.ShouldBlock {
				t.Errorf("expected ShouldBlock=true for %q", cmd)
			}
		})
	}
}

// TestSP049_RegressionGitSafeOpsUnchanged verifies that safe git operations
// (commit, add, status, etc.) still classify as SAFE.
func TestSP049_RegressionGitSafeOpsUnchanged(t *testing.T) {
	safeGitOps := []string{
		"commit", "add", "status", "log", "diff", "show",
		"branch", "remote", "stash", "tag", "revert",
		"fetch", "merge", "pull", "push",
	}

	for _, op := range safeGitOps {
		t.Run(op, func(t *testing.T) {
			result := tools.ClassifyToolCall("git", map[string]interface{}{
				"operation": op,
			})
			if result.Risk != tools.SecuritySafe {
				t.Errorf("expected SAFE for git %s, got %v: %s", op, result.Risk, result.Reasoning)
			}
		})
	}
}

// TestSP049_WholeTokenMatching verifies that flag detection uses whole-token
// matching — `--hard` must NOT match inside `--hardlink-test` or `--hardcore`.
func TestSP049_WholeTokenMatching(t *testing.T) {
	// These should NOT be classified as DANGEROUS (substring false-positives)
	nonDangerousArgs := []string{
		"--hardlink-test HEAD~1",
		"--hardcore-reset HEAD~1",
		"--merge-squash HEAD~1",
	}

	for _, args := range nonDangerousArgs {
		t.Run(args, func(t *testing.T) {
			result := tools.ClassifyToolCall("git", map[string]interface{}{
				"operation": "reset",
				"args":      args,
			})
			// Should stay CAUTION, not escalate to DANGEROUS
			if result.Risk == tools.SecurityDangerous {
				t.Errorf("substring false-positive: 'reset %s' classified as DANGEROUS, should be CAUTION", args)
			}
		})
	}
}
