//go:build !js

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// =============================================================================
// Command registration & flags
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

func TestExplainCmd_HasFlags(t *testing.T) {
	for _, flag := range []string{"tool", "path", "operation", "json"} {
		if explainCmd.Flags().Lookup(flag) == nil {
			t.Errorf("explainCmd should have --%s flag", flag)
		}
	}
}

func TestExplainCmd_DefaultTool(t *testing.T) {
	tool, err := explainCmd.Flags().GetString("tool")
	if err != nil {
		t.Fatalf("getting --tool flag: %v", err)
	}
	if tool != "shell_command" {
		t.Errorf("default --tool = %q, want shell_command", tool)
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
	if !strings.Contains(out, "hard-block") {
		t.Errorf("expected hard-block indicator, got:\n%s", out)
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
	if !strings.Contains(out, "git-history-rewrite") {
		t.Errorf("expected git-history-rewrite source, got:\n%s", out)
	}
}

func TestExplain_git_rebase_interactive(t *testing.T) {
	out := captureExplainOutput(t, "git rebase -i HEAD~10")
	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level, got:\n%s", out)
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

func TestExplain_git_tag_delete(t *testing.T) {
	out := captureExplainOutput(t, "git tag -d v1.0")
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
	if !strings.Contains(out, "auto-approved") {
		t.Errorf("expected auto-approved indicator, got:\n%s", out)
	}
}

func TestExplain_echo_hello(t *testing.T) {
	out := captureExplainOutput(t, "echo hello")
	if !strings.Contains(out, "LOW") {
		t.Errorf("expected LOW level, got:\n%s", out)
	}
}

// =============================================================================
// runExplain output format tests
// =============================================================================

func TestExplain_OutputFormat(t *testing.T) {
	out := captureExplainOutput(t, "ls -la")
	requiredSections := []string{"Tool:", "Reason:", "Contributing checks:", "Command:"}
	for _, section := range requiredSections {
		if !strings.Contains(out, section) {
			t.Errorf("output missing required section %q:\n%s", section, out)
		}
	}
}

func TestExplain_OutputCheckOrder(t *testing.T) {
	out := captureExplainOutput(t, "rm -rf /")
	expectedChecks := []string{"classifier", "critical-op"}
	for _, check := range expectedChecks {
		if !strings.Contains(out, check) {
			t.Errorf("output missing check %q:\n%s", check, out)
		}
	}
}

func TestExplain_OutputContextDependent(t *testing.T) {
	out := captureExplainOutput(t, "ls -la")
	if !strings.Contains(out, "requires agent runtime context") {
		t.Errorf("expected context-dependent indicator in output:\n%s", out)
	}
}

// =============================================================================
// runExplain — suppression hints
// =============================================================================

func TestExplain_SuppressionHints_High(t *testing.T) {
	out := captureExplainOutput(t, "rm foo")
	if !strings.Contains(out, "To suppress this prompt") {
		t.Errorf("expected suppression hints for MEDIUM command:\n%s", out)
	}
}

func TestExplain_NoSuppressionHints_Critical(t *testing.T) {
	out := captureExplainOutput(t, "rm -rf /")
	if strings.Contains(out, "To suppress this prompt") {
		t.Errorf("critical command should not show suppression hints:\n%s", out)
	}
	if !strings.Contains(out, "unconditionally blocked") {
		t.Errorf("expected unconditional block message:\n%s", out)
	}
}

func TestExplain_NoSuppressionHints_Low(t *testing.T) {
	out := captureExplainOutput(t, "ls -la")
	if strings.Contains(out, "To suppress this prompt") {
		t.Errorf("low command should not show suppression hints:\n%s", out)
	}
}

// =============================================================================
// riskLevelFromSecurityResult (local helper)
// =============================================================================

func TestRiskLevelFromSecurityResult_Safe(t *testing.T) {
	res := tools.SecurityResult{Risk: tools.SecuritySafe}
	level := riskLevelFromSecurityResult(res)
	if string(level) != "low" {
		t.Errorf("riskLevelFromSecurityResult(SAFE) = %q, want \"low\"", level)
	}
}

func TestRiskLevelFromSecurityResult_Caution(t *testing.T) {
	res := tools.SecurityResult{Risk: tools.SecurityCaution}
	level := riskLevelFromSecurityResult(res)
	if string(level) != "medium" {
		t.Errorf("riskLevelFromSecurityResult(CAUTION) = %q, want \"medium\"", level)
	}
}

func TestRiskLevelFromSecurityResult_Dangerous(t *testing.T) {
	res := tools.SecurityResult{Risk: tools.SecurityDangerous}
	level := riskLevelFromSecurityResult(res)
	if string(level) != "high" {
		t.Errorf("riskLevelFromSecurityResult(DANGEROUS) = %q, want \"high\"", level)
	}
}

func TestRiskLevelFromSecurityResult_HardBlockOverridesRisk(t *testing.T) {
	res := tools.SecurityResult{Risk: tools.SecuritySafe, IsHardBlock: true}
	level := riskLevelFromSecurityResult(res)
	if string(level) != "critical" {
		t.Errorf("riskLevelFromSecurityResult(SAFE + hard-block) = %q, want \"critical\"", level)
	}
}

func TestRiskLevelFromSecurityResult_HardBlockWithDangerous(t *testing.T) {
	res := tools.SecurityResult{Risk: tools.SecurityDangerous, IsHardBlock: true}
	level := riskLevelFromSecurityResult(res)
	if string(level) != "critical" {
		t.Errorf("riskLevelFromSecurityResult(DANGEROUS + hard-block) = %q, want \"critical\"", level)
	}
}

func TestRiskLevelFromSecurityResult_UnknownDefaultToLow(t *testing.T) {
	res := tools.SecurityResult{Risk: 99}
	level := riskLevelFromSecurityResult(res)
	if string(level) != "low" {
		t.Errorf("riskLevelFromSecurityResult(unknown) = %q, want \"low\"", level)
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
			if got := isGitHistoryRewriteCommand(tt.cmd); got != tt.want {
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
		{"git reset --hard", false},
		{"git reset --hard HEAD", false},
		{"git reset --soft HEAD~5", false},
		{"git reset --mixed HEAD~5", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := isGitHistoryRewriteCommand(tt.cmd); got != tt.want {
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
			if got := isGitHistoryRewriteCommand(tt.cmd); got != tt.want {
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
			if got := isGitHistoryRewriteCommand(tt.cmd); got != tt.want {
				t.Errorf("isGitHistoryRewriteCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestIsGitHistoryRewriteCommand_not_git(t *testing.T) {
	for _, cmd := range []string{"ls -la", "echo hello", "cd /tmp", "make build"} {
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
			if got := stripQuotedContent(tt.input); got != tt.want {
				t.Errorf("stripQuotedContent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripQuotedContent_nested_quotes(t *testing.T) {
	input := `echo "it's here"`
	want := `echo "         "`
	if got := stripQuotedContent(input); got != want {
		t.Errorf("stripQuotedContent(%q) = %q, want %q", input, got, want)
	}
}

func TestStripQuotedContent_no_quotes(t *testing.T) {
	input := "git reset --hard HEAD~5"
	if got := stripQuotedContent(input); got != input {
		t.Errorf("stripQuotedContent(%q) = %q, want %q", input, got, input)
	}
}

func TestIsGitHistoryRewriteCommand_quoted_git_not_flagged(t *testing.T) {
	cmd := `echo "git reset --hard HEAD~5"`
	if isGitHistoryRewriteCommand(cmd) {
		t.Errorf("isGitHistoryRewriteCommand(%q) = true, want false", cmd)
	}
}

// =============================================================================
// isGitWriteCommand
// =============================================================================

func TestIsGitWriteCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git commit -m foo", true},
		{"git push origin main", true},
		{"git merge feature", true},
		{"git clone https://example.com/repo", true},
		{"git init", true},
		{"git status", false},
		{"git log", false},
		{"ls -la", false},
		{"git branch -d feature", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := isGitWriteCommand(tt.cmd); got != tt.want {
				t.Errorf("isGitWriteCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

// =============================================================================
// runExplain — edge cases
// =============================================================================

func TestExplain_empty_command(t *testing.T) {
	out := captureExplainOutput(t, "")
	if !strings.Contains(out, "Tool:") {
		t.Errorf("expected Tool: header even for empty command:\n%s", out)
	}
}

func TestExplain_command_with_special_chars(t *testing.T) {
	out := captureExplainOutput(t, "echo 'hello world' && ls -la")
	if !strings.Contains(out, "Tool:") {
		t.Errorf("expected Tool: header:\n%s", out)
	}
}

func TestExplain_dd_to_disk(t *testing.T) {
	out := captureExplainOutput(t, "dd if=/dev/sda of=/dev/sdb")
	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level for dd to block device, got:\n%s", out)
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
}

func TestExplain_killall(t *testing.T) {
	out := captureExplainOutput(t, "killall -9 something")
	if !strings.Contains(out, "CRITICAL") {
		t.Errorf("expected CRITICAL level for killall -9, got:\n%s", out)
	}
}

func TestExplain_git_write_check_present(t *testing.T) {
	out := captureExplainOutput(t, "git push")
	if !strings.Contains(out, "git-write") {
		t.Errorf("expected git-write check in output:\n%s", out)
	}
}

// =============================================================================
// validateExplainInput & buildExplainArgs
// =============================================================================

func TestValidateExplainInput_shellCommandEmpty(t *testing.T) {
	if msg := validateExplainInput("shell_command", map[string]interface{}{}); msg == "" {
		t.Error("expected non-empty error message for empty shell command")
	}
}

func TestValidateExplainInput_writeFileNoPath(t *testing.T) {
	if msg := validateExplainInput("write_file", map[string]interface{}{}); msg == "" {
		t.Error("expected non-empty error message for write_file without path")
	}
}

func TestValidateExplainInput_gitNoOp(t *testing.T) {
	if msg := validateExplainInput("git", map[string]interface{}{}); msg == "" {
		t.Error("expected non-empty error message for git without operation")
	}
}

func TestValidateExplainInput_ok(t *testing.T) {
	if msg := validateExplainInput("shell_command", map[string]interface{}{"command": "ls"}); msg != "" {
		t.Errorf("expected empty message for valid input, got %q", msg)
	}
}

func TestBuildExplainArgs_shellCommand(t *testing.T) {
	args := buildExplainArgs([]string{"ls", "-la"}, "shell_command", "", "")
	if args["command"] != "ls -la" {
		t.Errorf("expected joined command, got %v", args["command"])
	}
}

func TestBuildExplainArgs_writeFile(t *testing.T) {
	args := buildExplainArgs(nil, "write_file", "./foo.txt", "")
	if args["path"] != "./foo.txt" {
		t.Errorf("expected path ./foo.txt, got %v", args["path"])
	}
}

func TestBuildExplainArgs_git(t *testing.T) {
	args := buildExplainArgs(nil, "git", "", "push")
	if args["operation"] != "push" {
		t.Errorf("expected operation push, got %v", args["operation"])
	}
}

// =============================================================================
// explainToolList
// =============================================================================

func TestExplainToolList(t *testing.T) {
	list := explainToolList()
	if !strings.Contains(list, "shell_command") {
		t.Errorf("expected shell_command in tool list, got %q", list)
	}
	if !strings.Contains(list, "git") {
		t.Errorf("expected git in tool list, got %q", list)
	}
}

// =============================================================================
// JSON output
// =============================================================================

func TestExplainJSON_shellCommand(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	args := map[string]interface{}{"command": "ls"}
	secResult := tools.ClassifyToolCall("shell_command", args)
	level := riskLevelFromSecurityResult(secResult)
	sources := explainSourcesFor("shell_command", secResult, args)
	if err := printExplainJSON(cmd, secResult, level, sources, "shell_command", args); err != nil {
		t.Fatalf("printExplainJSON error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"risk_level"`) {
		t.Errorf("expected risk_level in JSON output:\n%s", out)
	}
	if !strings.Contains(out, `"result"`) {
		t.Errorf("expected result in JSON output:\n%s", out)
	}
}

// =============================================================================
// levelHeadline
// =============================================================================

func TestLevelHeadline_critical(t *testing.T) {
	h := levelHeadline(configuration.RiskLevelCritical, tools.SecurityResult{})
	if !strings.Contains(h, "CRITICAL") {
		t.Errorf("expected CRITICAL headline, got %q", h)
	}
	if !strings.Contains(h, "hard-block") {
		t.Errorf("expected hard-block in critical headline, got %q", h)
	}
}

func TestLevelHeadline_low(t *testing.T) {
	h := levelHeadline(configuration.RiskLevelLow, tools.SecurityResult{})
	if !strings.Contains(h, "LOW") {
		t.Errorf("expected LOW headline, got %q", h)
	}
	if !strings.Contains(h, "auto-approved") {
		t.Errorf("expected auto-approved in low headline, got %q", h)
	}
}
