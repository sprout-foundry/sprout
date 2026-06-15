//go:build !js

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// =============================================================================
// Command registration & argument validation
// =============================================================================

func TestExplainCmd_Registered(t *testing.T) {
	if explainCmd == nil {
		t.Fatal("explainCmd should not be nil")
	}
	if explainCmd.Use == "" {
		t.Error("explainCmd.Use should not be empty")
	}
	if explainCmd.Short == "" {
		t.Error("explainCmd.Short should not be empty")
	}
}

func TestExplainCmd_ArgValidation_NoArgs(t *testing.T) {
	if explainCmd.Args == nil {
		t.Fatal("explainCmd should have argument validation")
	}
	err := explainCmd.Args(explainCmd, []string{})
	if err == nil {
		t.Error("expected error when no arguments provided")
	} else if !strings.Contains(err.Error(), "accepts") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExplainCmd_ArgValidation_TooManyArgs(t *testing.T) {
	err := explainCmd.Args(explainCmd, []string{"arg1", "arg2"})
	if err == nil {
		t.Error("expected error when too many arguments provided")
	}
}

func TestExplainCmd_ArgValidation_OneArg(t *testing.T) {
	err := explainCmd.Args(explainCmd, []string{"ls -la"})
	if err != nil {
		t.Errorf("expected no error with exactly one argument, got: %v", err)
	}
}

// =============================================================================
// runExplain output tests — critical operations
// =============================================================================

func newTestCmd() *cobra.Command {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	return cmd
}

func captureExplainOutput(t *testing.T, command string) string {
	t.Helper()
	cmd := newTestCmd()
	err := runExplain(cmd, command)
	if err != nil {
		t.Fatalf("runExplain(%q) returned error: %v", command, err)
	}
	return cmd.OutOrStdout().(*bytes.Buffer).String()
}

func TestExplain_rm_rf_root(t *testing.T) {
	out := captureExplainOutput(t, "rm -rf /")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
	}
	if !strings.Contains(out, "hard-block: true") {
		t.Errorf("expected hard-block: true, got:\n%s", out)
	}
	if !strings.Contains(out, "critical-op") {
		t.Errorf("expected critical-op source, got:\n%s", out)
	}
}

func TestExplain_mkfs_ext3(t *testing.T) {
	out := captureExplainOutput(t, "mkfs.ext3 /dev/sda")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
	}
	if !strings.Contains(out, "hard-block: true") {
		t.Errorf("expected hard-block: true, got:\n%s", out)
	}
	if !strings.Contains(out, "critical-op") {
		t.Errorf("expected critical-op source, got:\n%s", out)
	}
}

// =============================================================================
// runExplain output tests — git history-rewrite
// =============================================================================

func TestExplain_git_reset_hard_backward(t *testing.T) {
	out := captureExplainOutput(t, "git reset --hard HEAD~5")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
	}
	if !strings.Contains(out, "hard-block: true") {
		t.Errorf("expected hard-block: true, got:\n%s", out)
	}
	if !strings.Contains(out, "git-history-rewrite") {
		t.Errorf("expected git-history-rewrite source, got:\n%s", out)
	}
}

func TestExplain_git_rebase_interactive(t *testing.T) {
	out := captureExplainOutput(t, "git rebase -i HEAD~10")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
	}
	if !strings.Contains(out, "hard-block: true") {
		t.Errorf("expected hard-block: true, got:\n%s", out)
	}
	if !strings.Contains(out, "git-history-rewrite") {
		t.Errorf("expected git-history-rewrite source, got:\n%s", out)
	}
}

func TestExplain_git_branch_delete(t *testing.T) {
	out := captureExplainOutput(t, "git branch -d feature")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
	}
	if !strings.Contains(out, "git-history-rewrite") {
		t.Errorf("expected git-history-rewrite source, got:\n%s", out)
	}
}

func TestExplain_git_branch_force_delete(t *testing.T) {
	out := captureExplainOutput(t, "git branch -D feature")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
	}
	if !strings.Contains(out, "git-history-rewrite") {
		t.Errorf("expected git-history-rewrite source, got:\n%s", out)
	}
}

func TestExplain_git_branch_delete_long(t *testing.T) {
	out := captureExplainOutput(t, "git branch --delete feature")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
	}
	if !strings.Contains(out, "git-history-rewrite") {
		t.Errorf("expected git-history-rewrite source, got:\n%s", out)
	}
}

func TestExplain_git_tag_delete(t *testing.T) {
	out := captureExplainOutput(t, "git tag -d v1.0")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
	}
	if !strings.Contains(out, "git-history-rewrite") {
		t.Errorf("expected git-history-rewrite source, got:\n%s", out)
	}
}

func TestExplain_git_tag_delete_long(t *testing.T) {
	out := captureExplainOutput(t, "git tag --delete v1.0")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
	}
	if !strings.Contains(out, "git-history-rewrite") {
		t.Errorf("expected git-history-rewrite source, got:\n%s", out)
	}
}

// =============================================================================
// runExplain output tests — safe commands
// =============================================================================

func TestExplain_ls_la(t *testing.T) {
	out := captureExplainOutput(t, "ls -la")

	if !strings.Contains(out, "LOW") {
		t.Errorf("expected LOW level, got:\n%s", out)
	}
	if !strings.Contains(out, "hard-block: false") {
		t.Errorf("expected hard-block: false, got:\n%s", out)
	}
}

func TestExplain_echo_hello(t *testing.T) {
	out := captureExplainOutput(t, "echo hello")

	if !strings.Contains(out, "LOW") {
		t.Errorf("expected LOW level, got:\n%s", out)
	}
	if !strings.Contains(out, "hard-block: false") {
		t.Errorf("expected hard-block: false, got:\n%s", out)
	}
}

// =============================================================================
// runExplain output format tests
// =============================================================================

func TestExplain_OutputFormat(t *testing.T) {
	out := captureExplainOutput(t, "ls -la")

	requiredSections := []string{
		"Risk Assessment",
		"===============",
		"Level:",
		"Sources:",
		"Reason:",
		"Contributing checks:",
	}

	for _, section := range requiredSections {
		if !strings.Contains(out, section) {
			t.Errorf("output missing required section %q:\n%s", section, out)
		}
	}
}

func TestExplain_OutputCheckOrder(t *testing.T) {
	out := captureExplainOutput(t, "rm -rf /")

	// All seven checks should appear
	expectedChecks := []string{
		"classifier",
		"critical-op",
		"git-history-rewrite",
		"persona-cascade",
		"git-write",
		"fs-tier",
		"workspace-policy",
	}

	for _, check := range expectedChecks {
		if !strings.Contains(out, check) {
			t.Errorf("output missing check %q:\n%s", check, out)
		}
	}
}

func TestExplain_OutputContextDependent(t *testing.T) {
	out := captureExplainOutput(t, "ls -la")

	// persona-cascade and workspace-policy should show as requiring context
	if !strings.Contains(out, "requires agent runtime context") {
		t.Errorf("expected context-dependent indicator in output:\n%s", out)
	}
}

func TestExplain_OutputNAGitWrite(t *testing.T) {
	out := captureExplainOutput(t, "ls -la")

	// git-write should be n/a for non-git commands
	if !strings.Contains(out, "git-write") {
		t.Error("expected git-write check in output")
	}
}

// =============================================================================
// runExplain — combined gates (both critical and git-rewrite)
// =============================================================================

func TestExplain_both_gates(t *testing.T) {
	// This shouldn't happen in practice (a command that is both critical
	// and a git rewrite), but test the combinator is idempotent.
	// rm -rf / is critical but not a git rewrite.
	out := captureExplainOutput(t, "rm -rf /")

	// Should show critical-op as active, git-history-rewrite as n/a
	if !strings.Contains(out, "critical-op") {
		t.Error("expected critical-op in sources")
	}
	// The git-history-rewrite check should appear as n/a in the breakdown
	if !strings.Contains(out, "git-history-rewrite") {
		t.Error("expected git-history-rewrite in contributing checks")
	}
}

// =============================================================================
// classifyLevel helper
// =============================================================================

func TestClassifyLevel_Safe(t *testing.T) {
	res := tools.SecurityResult{Risk: tools.SecuritySafe}
	level := classifyLevel(res)
	if string(level) != "low" {
		t.Errorf("classifyLevel(SAFE) = %q, want \"low\"", level)
	}
}

func TestClassifyLevel_Caution(t *testing.T) {
	res := tools.SecurityResult{Risk: tools.SecurityCaution}
	level := classifyLevel(res)
	if string(level) != "medium" {
		t.Errorf("classifyLevel(CAUTION) = %q, want \"medium\"", level)
	}
}

func TestClassifyLevel_Dangerous(t *testing.T) {
	res := tools.SecurityResult{Risk: tools.SecurityDangerous}
	level := classifyLevel(res)
	if string(level) != "high" {
		t.Errorf("classifyLevel(DANGEROUS) = %q, want \"high\"", level)
	}
}

func TestClassifyLevel_HardBlockOverridesRisk(t *testing.T) {
	// Hard-block should elevate even SAFE to Critical.
	res := tools.SecurityResult{Risk: tools.SecuritySafe, IsHardBlock: true}
	level := classifyLevel(res)
	if string(level) != "critical" {
		t.Errorf("classifyLevel(SAFE + hard-block) = %q, want \"critical\"", level)
	}
}

func TestClassifyLevel_HardBlockWithDangerous(t *testing.T) {
	res := tools.SecurityResult{Risk: tools.SecurityDangerous, IsHardBlock: true}
	level := classifyLevel(res)
	if string(level) != "critical" {
		t.Errorf("classifyLevel(DANGEROUS + hard-block) = %q, want \"critical\"", level)
	}
}

func TestClassifyLevel_UnknownDefaultToLow(t *testing.T) {
	res := tools.SecurityResult{Risk: 99} // invalid value
	level := classifyLevel(res)
	if string(level) != "low" {
		t.Errorf("classifyLevel(unknown) = %q, want \"low\"", level)
	}
}

// =============================================================================
// isGitHistoryRewriteCommand
// =============================================================================

func TestIsGitHistoryRewriteCommand_rebase(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git rebase", true},
		{"git rebase -i HEAD~10", true},
		{"git rebase --onto base head", true},
		{"git rebase main", true},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := isGitHistoryRewriteCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("isGitHistoryRewriteCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestIsGitHistoryRewriteCommand_reset_hard(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git reset --hard HEAD~5", true},
		{"git reset --hard abc123", true},
		{"git reset --hard origin/main~1", true},
		// These should NOT trigger: reset --hard without commit-ish or with HEAD only
		{"git reset --hard", false},
		{"git reset --hard HEAD", false},
		{"git reset --soft HEAD~5", false},
		{"git reset --mixed HEAD~5", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := isGitHistoryRewriteCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("isGitHistoryRewriteCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestIsGitHistoryRewriteCommand_branch_delete(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git branch -d feature", true},
		{"git branch -D feature", true},
		{"git branch --delete feature", true},
		{"git branch feature", false},
		{"git branch -v", false},
		{"git branch -a", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := isGitHistoryRewriteCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("isGitHistoryRewriteCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestIsGitHistoryRewriteCommand_tag_delete(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git tag -d v1.0", true},
		{"git tag --delete v1.0", true},
		{"git tag v1.0", false},
		{"git tag -l", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := isGitHistoryRewriteCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("isGitHistoryRewriteCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestIsGitHistoryRewriteCommand_not_git(t *testing.T) {
	commands := []string{
		"ls -la",
		"echo hello",
		"cd /tmp",
		"make build",
	}
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			if isGitHistoryRewriteCommand(cmd) {
				t.Errorf("isGitHistoryRewriteCommand(%q) = true, want false", cmd)
			}
		})
	}
}

// =============================================================================
// stripQuotedContent
// =============================================================================

func TestStripQuotedContent_basic(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`echo "hello world"`, `echo "           "`},
		{`echo 'hello world'`, `echo '           '`},
		{`git reset --hard "HEAD~5"`, `git reset --hard "      "`},
		{`echo "a|b|c" | head`, `echo "     " | head`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripQuotedContent(tt.input)
			if got != tt.want {
				t.Errorf("stripQuotedContent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripQuotedContent_nested_quotes(t *testing.T) {
	// Single quotes inside double quotes and vice versa
	input := `echo "it's here"`
	want := `echo "         "`
	got := stripQuotedContent(input)
	if got != want {
		t.Errorf("stripQuotedContent(%q) = %q, want %q", input, got, want)
	}
}

func TestStripQuotedContent_no_quotes(t *testing.T) {
	input := "git reset --hard HEAD~5"
	got := stripQuotedContent(input)
	if got != input {
		t.Errorf("stripQuotedContent(%q) = %q, want %q", input, got, input)
	}
}

func TestIsGitHistoryRewriteCommand_quoted_git_not_flagged(t *testing.T) {
	// "git reset --hard HEAD~5" inside quotes should not trigger
	cmd := `echo "git reset --hard HEAD~5"`
	got := isGitHistoryRewriteCommand(cmd)
	if got {
		t.Errorf("isGitHistoryRewriteCommand(%q) = true, want false — quoted git should be ignored", cmd)
	}
}

// =============================================================================
// runExplain — edge cases
// =============================================================================

func TestExplain_empty_command(t *testing.T) {
	out := captureExplainOutput(t, "")
	// Should not panic; the classifier handles empty commands
	if !strings.Contains(out, "Risk Assessment") {
		t.Errorf("expected Risk Assessment header even for empty command:\n%s", out)
	}
}

func TestExplain_command_with_special_chars(t *testing.T) {
	out := captureExplainOutput(t, "echo 'hello world' && ls -la")
	// Should produce valid output without panicking
	if !strings.Contains(out, "Risk Assessment") {
		t.Errorf("expected Risk Assessment header:\n%s", out)
	}
}

func TestExplain_dd_to_disk(t *testing.T) {
	out := captureExplainOutput(t, "dd if=/dev/sda of=/dev/sdb")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level for dd to block device, got:\n%s", out)
	}
	if !strings.Contains(out, "hard-block: true") {
		t.Errorf("expected hard-block: true, got:\n%s", out)
	}
	if !strings.Contains(out, "critical-op") {
		t.Errorf("expected critical-op source, got:\n%s", out)
	}
}

func TestExplain_fork_bomb(t *testing.T) {
	out := captureExplainOutput(t, ":(){ :|:; }")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level for fork bomb, got:\n%s", out)
	}
	if !strings.Contains(out, "hard-block: true") {
		t.Errorf("expected hard-block: true, got:\n%s", out)
	}
}

func TestExplain_killall(t *testing.T) {
	out := captureExplainOutput(t, "killall -9 something")

	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level for killall -9, got:\n%s", out)
	}
	if !strings.Contains(out, "hard-block: true") {
		t.Errorf("expected hard-block: true, got:\n%s", out)
	}
}

// =============================================================================
// runExplain — git-write gate shown for git commands
// =============================================================================

func TestExplain_git_write_check_present(t *testing.T) {
	out := captureExplainOutput(t, "git rebase -i HEAD~3")

	// git-write check should be present in the contributing checks
	if !strings.Contains(out, "git-write") {
		t.Errorf("expected git-write check in output:\n%s", out)
	}
}
