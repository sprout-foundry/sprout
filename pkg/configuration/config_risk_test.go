package configuration

import (
	"strings"
	"testing"
)

// =============================================================================
// DefaultAutoApproveRules tests
// =============================================================================

func TestDefaultAutoApproveRules_ReturnsNonEmptyLists(t *testing.T) {
	rules := DefaultAutoApproveRules()

	if len(rules.LowRiskOps) == 0 {
		t.Fatal("expected non-empty LowRiskOps")
	}
	if len(rules.MediumRiskOps) == 0 {
		t.Fatal("expected non-empty MediumRiskOps")
	}
	if len(rules.HighRiskNever) == 0 {
		t.Fatal("expected non-empty HighRiskNever")
	}
}

func TestDefaultAutoApproveRules_ContainsExpectedCategories(t *testing.T) {
	rules := DefaultAutoApproveRules()

	// Low-risk ops
	expectedLow := []string{"git_add", "git_status", "git_log", "git_diff", "read_file"}
	for _, op := range expectedLow {
		found := false
		for _, l := range rules.LowRiskOps {
			if l == op {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected LowRiskOps to contain %q", op)
		}
	}

	// Medium-risk ops
	expectedMedium := []string{"git_commit", "git_push", "write_file", "edit_file", "shell_command"}
	for _, op := range expectedMedium {
		found := false
		for _, m := range rules.MediumRiskOps {
			if m == op {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected MediumRiskOps to contain %q", op)
		}
	}

	// High-risk never ops
	expectedHigh := []string{"force_flag", "rm_recursive", "git_reset_hard",
		"git_clean", "docker_prune", "git_push_force",
		"git_checkout", "git_switch", "git_restore", "git_branch_delete"}
	for _, op := range expectedHigh {
		found := false
		for _, h := range rules.HighRiskNever {
			if h == op {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected HighRiskNever to contain %q", op)
		}
	}
}

// =============================================================================
// SubagentType.GetAutoApproveRules tests
// =============================================================================

func TestSubagentTypeGetAutoApproveRules_WithRules(t *testing.T) {
	rules := AutoApproveRules{
		LowRiskOps:     []string{"custom_op"},
		MediumRiskOps:  []string{"another_op"},
		HighRiskNever:  []string{"bad_op"},
	}
	st := SubagentType{
		ID:               "custom",
		Name:             "Custom",
		Enabled:          true,
		AutoApproveRules: &rules,
	}

	got := st.GetAutoApproveRules()
	if len(got.LowRiskOps) != 1 || got.LowRiskOps[0] != "custom_op" {
		t.Errorf("expected custom LowRiskOps, got %v", got.LowRiskOps)
	}
}

func TestSubagentTypeGetAutoApproveRules_WithoutRules(t *testing.T) {
	st := SubagentType{
		ID:        "default_persona",
		Name:      "Default",
		Enabled:   true,
	}
	st.AutoApproveRules = nil

	got := st.GetAutoApproveRules()
	defaults := DefaultAutoApproveRules()
	if len(got.LowRiskOps) != len(defaults.LowRiskOps) {
		t.Errorf("expected default LowRiskOps length %d, got %d", len(defaults.LowRiskOps), len(got.LowRiskOps))
	}
}

// =============================================================================
// containsForceFlag tests
// =============================================================================

func TestContainsForceFlag_ExactFlags(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    bool
	}{
		{"--force flag", "some --force command", true},
		{"-f standalone flag", "git commit -f -m msg", true},
		{"-f at end (-rf is combined flag)", "rm -rf", true}, // -rf is caught as combined short flag with f
		{"--force-with-lease is NOT force", "git push --force-with-lease", false},
		{"no force flag", "git status", false},
		{"empty string", "", false},
		{"-f for non-force command python3", "python3 -f script.py", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsForceFlag(tt.cmd)
			if got != tt.want {
				t.Errorf("containsForceFlag(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestContainsForceFlag_CombinedShortFlags(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    bool
	}{
		{"-rf combined", "rm -rf /tmp", true},
		{"-fr combined", "rm -fr /tmp", true},
		{"-af combined", "git add -af", true},
		{"word containing f", "diff something", false},
		{"config word", "conf file.txt", false},
		{"flag with digit -1f", "cmd -1f", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsForceFlag(tt.cmd)
			if got != tt.want {
				t.Errorf("containsForceFlag(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

// =============================================================================
// categorizeCommand tests
// =============================================================================

func TestCategorizeCommand_GitSubcommands(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    string
	}{
		{"git status", "git status", "git_status"},
		{"git log", "git log", "git_log"},
		{"git diff", "git diff", "git_diff"},
		{"git add file.go", "git add file.go", "git_add"},
		{"git commit", "git commit -m msg", "git_commit"},
		{"git push", "git push origin main", "git_push"},
		{"git pull", "git pull", "git_pull"},
		{"git fetch", "git fetch", "git_fetch"},
		{"git reset", "git reset --hard HEAD~1", "git_reset_hard"},
		{"git clean", "git clean -fd", "git_clean"},
		{"git branch listing", "git branch", "git_status"},
		{"git branch delete", "git branch -d feature", "git_branch_delete"},
		{"git checkout", "git checkout main", "git_checkout"},
		{"git switch", "git switch main", "git_switch"},
		{"git restore", "git restore file.go", "git_restore"},
		{"git stash", "git stash", "git_status"},
		{"git tag", "git tag v1.0", "git_add"},
		{"git merge", "git merge feature", "git_commit"},
		{"git rebase", "git rebase main", "git_commit"},
		{"git unknown", "git foo", "shell_command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := categorizeCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("categorizeCommand(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestCategorizeCommand_NonGitCommands(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    string
	}{
		{"rm command", "rm file.txt", "rm_command"},
		{"docker command", "docker ps", "docker"},
		{"cat command", "cat file.go", "read_file"},
		{"head command", "head -n 10 file.go", "read_file"},
		{"ls command", "ls -la", "read_file"},
		{"find command", "find . -name '*.go'", "read_file"},
		{"which command", "which go", "read_file"},
		{"file command", "file myfile.txt", "read_file"},
		{"write_file tool", "write_file path/to/file", "write_file"},
		{"edit_file tool", "edit_file path/to/file", "write_file"},
		{"unknown command", "docker compose up", "docker"},
		{"random command", "python3 script.py", "shell_command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := categorizeCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("categorizeCommand(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}

// =============================================================================
// matchesRiskPattern tests
// =============================================================================

func TestMatchesRiskPattern_ForceFlag(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    bool
	}{
		{"with --force", "cmd --force arg", true},
		{"without --force", "cmd arg", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesRiskPattern(tt.cmd, "force_flag")
			if got != tt.want {
				t.Errorf("matchesRiskPattern(%q, \"force_flag\") = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestMatchesRiskPattern_RmRecursive(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    bool
	}{
		{"rm -r", "rm -r dir/", true},
		{"rm -rf", "rm -rf /tmp/*", true},
		{"rm --recursive", "rm --recursive dir/", true},
		{"rm without -r", "rm file.txt", false},
		{"no rm command", "cat file.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesRiskPattern(tt.cmd, "rm_recursive")
			if got != tt.want {
				t.Errorf("matchesRiskPattern(%q, \"rm_recursive\") = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestMatchesRiskPattern_GitResetHard(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    bool
	}{
		{"git reset --hard", "git reset --hard HEAD~1", true},
		{"git reset without --hard", "git reset HEAD~1", false},
		{"no reset command", "git status", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesRiskPattern(tt.cmd, "git_reset_hard")
			if got != tt.want {
				t.Errorf("matchesRiskPattern(%q, \"git_reset_hard\") = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestMatchesRiskPattern_GitClean(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    bool
	}{
		{"git clean", "git clean -fd", true},
		{"no clean", "git status", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesRiskPattern(tt.cmd, "git_clean")
			if got != tt.want {
				t.Errorf("matchesRiskPattern(%q, \"git_clean\") = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestMatchesRiskPattern_GitPushForce(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    bool
	}{
		{"git push --force", "git push --force origin main", true},
		{"git push -f", "git push -f origin main", true},
		{"git push without force", "git push origin main", false},
		{"git push --force-with-lease is NOT force", "git push --force-with-lease origin main", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesRiskPattern(tt.cmd, "git_push_force")
			if got != tt.want {
				t.Errorf("matchesRiskPattern(%q, \"git_push_force\") = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestMatchesRiskPattern_DockerPrune(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    bool
	}{
		{"docker prune", "docker system prune", true},
		{"docker without prune", "docker ps", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesRiskPattern(tt.cmd, "docker_prune")
			if got != tt.want {
				t.Errorf("matchesRiskPattern(%q, \"docker_prune\") = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestMatchesRiskPattern_UnknownPattern(t *testing.T) {
	got := matchesRiskPattern("anything", "unknown_pattern")
	if got != false {
		t.Errorf("matchesRiskPattern(\"anything\", \"unknown_pattern\") = %v, want false", got)
	}
}

// =============================================================================
// firstFieldAfter tests
// =============================================================================

func TestFirstFieldAfter(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		prefix  string
		want    string
	}{
		{"simple", "git status --short", "git", "status"},
		{"no match", "hello world", "git", "hello"},
		{"empty after prefix", "git  ", "git", ""},
		{"multiple spaces", "git   reset   --hard", "git", "reset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstFieldAfter(tt.s, tt.prefix)
			if got != tt.want {
				t.Errorf("firstFieldAfter(%q, %q) = %q, want %q", tt.s, tt.prefix, got, tt.want)
			}
		})
	}
}

// =============================================================================
// SubagentType.EvaluateOperationRisk tests
// =============================================================================

// evalRiskHelper creates a SubagentType with default auto-approve rules for testing
func evalRiskHelper() SubagentType {
	rules := DefaultAutoApproveRules()
	return SubagentType{
		ID:               "tester",
		Name:             "Tester",
		Enabled:          true,
		AutoApproveRules: &rules,
	}
}

func TestSubagentTypeEvaluateOperationRisk_LowRiskCommands(t *testing.T) {
	st := evalRiskHelper()

	tests := []struct {
		name string
		cmd  string
	}{
		{"git status", "git status"},
		{"git log", "git log --oneline"},
		{"git diff", "git diff HEAD"},
		{"git add file.go", "git add file.go"},
		{"cat file", "cat file.go"},
		{"ls directory", "ls -la"},
		{"find files", "find . -name '*.go'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := st.EvaluateOperationRisk(tt.cmd)
			if got != RiskLevelLow {
				t.Errorf("EvaluateOperationRisk(%q) = %q, want %q", tt.cmd, got, RiskLevelLow)
			}
		})
	}
}

func TestSubagentTypeEvaluateOperationRisk_MediumRiskCommands(t *testing.T) {
	st := evalRiskHelper()
	st.ID = "coder"
	st.Name = "Coder"

	tests := []struct {
		name string
		cmd  string
	}{
		{"git commit", "git commit -m msg"},
		{"git push", "git push origin main"},
		{"git pull", "git pull"},
		{"git fetch", "git fetch"},
		{"write_file", "write_file path/file.go"},
		{"edit_file", "edit_file path/file.go"},
		{"shell_command", "python3 script.py"},
		{"subagent_spawn", "subagent_spawn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := st.EvaluateOperationRisk(tt.cmd)
			if got != RiskLevelMedium {
				t.Errorf("EvaluateOperationRisk(%q) = %q, want %q", tt.cmd, got, RiskLevelMedium)
			}
		})
	}
}

func TestSubagentTypeEvaluateOperationRisk_HighRiskCommands(t *testing.T) {
	st := evalRiskHelper()
	st.ID = "debugger"
	st.Name = "Debugger"

	tests := []struct {
		name string
		cmd  string
	}{
		{"rm -rf", "rm -rf /tmp/*"},
		{"rm -r", "rm -r directory/"},
		{"rm --recursive", "rm --recursive dir/"},
		{"git reset --hard", "git reset --hard HEAD~1"},
		{"git clean", "git clean -fd"},
		{"docker prune", "docker system prune"},
		{"git push --force", "git push --force origin main"},
		{"git push -f", "git push -f origin main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := st.EvaluateOperationRisk(tt.cmd)
			if got != RiskLevelHigh {
				t.Errorf("EvaluateOperationRisk(%q) = %q, want %q", tt.cmd, got, RiskLevelHigh)
			}
		})
	}
}

func TestSubagentTypeEvaluateOperationRisk_ForceFlagEscalation(t *testing.T) {
	st := evalRiskHelper()

	// Force flag should escalate any command to high risk
	forceCommands := []string{
		"git status --force",
		"cat --force file.txt",
		"command --force",
		"rm -f file",
		"mv -f src dst",
	}
	for _, cmd := range forceCommands {
		t.Run("force:"+cmd, func(t *testing.T) {
			got := st.EvaluateOperationRisk(cmd)
			if got != RiskLevelHigh {
				t.Errorf("EvaluateOperationRisk(%q) = %q, want %q (force flag escalation)", cmd, got, RiskLevelHigh)
			}
		})
	}

	// --force-with-lease should NOT be high risk (it's safer than --force)
	t.Run("force_with_lease_is_not_high_risk", func(t *testing.T) {
		got := st.EvaluateOperationRisk("git push --force-with-lease")
		if got == RiskLevelHigh {
			t.Errorf("EvaluateOperationRisk('git push --force-with-lease') = %q, should not be high (force-with-lease is safe)", got)
		}
	})
}

func TestSubagentTypeEvaluateOperationRisk_NoAutoApproveRules(t *testing.T) {
	st := SubagentType{
		ID:              "default_persona",
		Name:            "Default",
		Enabled:         true,
	}
	// No AutoApproveRules set — should use defaults
	got := st.EvaluateOperationRisk("git status")
	if got != RiskLevelLow {
		t.Errorf("EvaluateOperationRisk with nil rules(%q) = %q, want %q", "git status", got, RiskLevelLow)
	}

	// Even with defaults, high-risk commands should be caught
	gotHigh := st.EvaluateOperationRisk("rm -rf /tmp")
	if gotHigh != RiskLevelHigh {
		t.Errorf("EvaluateOperationRisk with nil rules(%q) = %q, want %q", "rm -rf /tmp", gotHigh, RiskLevelHigh)
	}
}

func TestSubagentTypeEvaluateOperationRisk_CaseInsensitive(t *testing.T) {
	st := evalRiskHelper()

	tests := []struct {
		name    string
		cmd     string
		want    RiskLevel
	}{
		{"mixed case rm", "RM -rf /tmp", RiskLevelHigh},
		{"mixed case git", "GIT STATUS", RiskLevelLow},
		{"mixed case git commit", "Git Commit -m msg", RiskLevelMedium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := st.EvaluateOperationRisk(tt.cmd)
			if got != tt.want {
				t.Errorf("EvaluateOperationRisk(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestSubagentTypeEvaluateOperationRisk_CustomRules(t *testing.T) {
	// Create custom rules where everything is low risk
	customRules := &AutoApproveRules{
		LowRiskOps:     []string{"shell_command", "write_file", "git_commit", "git_push"},
		MediumRiskOps:  []string{},
		HighRiskNever:  []string{"dangerous_op"},
	}
	st := SubagentType{
		ID:               "custom_persona",
		Name:             "Custom",
		Enabled:          true,
		AutoApproveRules: customRules,
	}

	// git_commit should be low risk with custom rules
	got := st.EvaluateOperationRisk("git commit -m msg")
	if got != RiskLevelLow {
		t.Errorf("custom rules: EvaluateOperationRisk(%q) = %q, want %q", "git commit -m msg", got, RiskLevelLow)
	}

	// shell_command should be low risk with custom rules
	gotShell := st.EvaluateOperationRisk("python3 script.py")
	if gotShell != RiskLevelLow {
		t.Errorf("custom rules: EvaluateOperationRisk(%q) = %q, want %q", "python3 script.py", gotShell, RiskLevelLow)
	}
}

func TestSubagentTypeEvaluateOperationRisk_CustomHighRiskPattern(t *testing.T) {
	// Create rules with a custom high-risk pattern
	customRules := &AutoApproveRules{
		LowRiskOps:     []string{"read_file"},
		MediumRiskOps:  []string{"write_file"},
		HighRiskNever:  []string{"dangerous_op", "force_flag", "rm_recursive"},
	}
	st := SubagentType{
		ID:               "custom_persona",
		Name:             "Custom",
		Enabled:          true,
		AutoApproveRules: customRules,
	}

	got := st.EvaluateOperationRisk("rm -rf /tmp")
	if got != RiskLevelHigh {
		t.Errorf("custom rules: EvaluateOperationRisk(%q) = %q, want %q", "rm -rf /tmp", got, RiskLevelHigh)
	}
}

// =============================================================================
// SubagentType.EvaluateOperationRisk edge cases
// =============================================================================

func TestSubagentTypeEvaluateOperationRisk_EmptyCommand(t *testing.T) {
	st := evalRiskHelper()

	got := st.EvaluateOperationRisk("")
	// Empty command should default to medium (unrecognized)
	if got != RiskLevelMedium {
		t.Errorf("EvaluateOperationRisk(\"\") = %q, want %q", got, RiskLevelMedium)
	}
}

func TestSubagentTypeEvaluateOperationRisk_GitBranchDelete(t *testing.T) {
	st := evalRiskHelper()

	got := st.EvaluateOperationRisk("git branch -d feature")
	// git_branch_delete is now in HighRiskNever, so it should be high risk
	if got != RiskLevelHigh {
		t.Errorf("EvaluateOperationRisk('git branch -d feature') = %q, want %q", got, RiskLevelHigh)
	}
}

// =============================================================================
// Risk level constant tests
// =============================================================================

func TestRiskLevelConstants(t *testing.T) {
	if RiskLevelLow != "low" {
		t.Errorf("RiskLevelLow = %q, want \"low\"", RiskLevelLow)
	}
	if RiskLevelMedium != "medium" {
		t.Errorf("RiskLevelMedium = %q, want \"medium\"", RiskLevelMedium)
	}
	if RiskLevelHigh != "high" {
		t.Errorf("RiskLevelHigh = %q, want \"high\"", RiskLevelHigh)
	}
}

// =============================================================================
// CategorizeCommand edge cases
// =============================================================================

func TestCategorizeCommand_CaseVariations(t *testing.T) {
	// categorizeCommand expects pre-lowercased input (caller lowercases first)
	tests := []struct {
		name    string
		cmd     string
		want    string
	}{
		{"lowercase git", "git status", "git_status"},
		{"lowercase git commit", "git commit -m msg", "git_commit"},
		{"lowercase rm", "rm -rf /tmp", "rm_command"},
		{"lowercase docker", "docker prune", "docker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := categorizeCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("categorizeCommand(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}

// =============================================================================
// containsForceFlag edge cases
// =============================================================================

func TestContainsForceFlag_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		want    bool
	}{
		{"diff is not -f", "diff file1 file2", false},
		{"diff contains f but not flag", "diff -u file1 file2", false},
		{"conf is not force", "config --help", false},
		{"--force anywhere", "docker push --force", true},
		{"-f as arg to git", "git -f", true},
		{"-f for rm", "rm -f file", true},
		{"-f for grep (not force)", "grep -f patterns.txt", false},
		{"-f for tail (not force)", "tail -f logfile", false},
		{"-f for mv", "mv -f src dst", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsForceFlag(tt.cmd)
			if got != tt.want {
				t.Errorf("containsForceFlag(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

// =============================================================================
// EvaluateOperationRisk with empty command
// =============================================================================

func TestSubagentTypeEvaluateOperationRisk_WhitespaceOnly(t *testing.T) {
	st := evalRiskHelper()

	tests := []string{"   ", "\t", "\n"}
	for _, cmd := range tests {
		t.Run("whitespace:"+strings.ReplaceAll(cmd, "\n", "\\n"), func(t *testing.T) {
			got := st.EvaluateOperationRisk(cmd)
			if got != RiskLevelMedium {
				t.Errorf("EvaluateOperationRisk(whitespace) = %q, want %q", got, RiskLevelMedium)
			}
		})
	}
}
