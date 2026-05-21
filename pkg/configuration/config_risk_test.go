package configuration

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/personas"
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

// =============================================================================
// EA persona auto_approve_rules loaded from JSON config
// =============================================================================

func TestNewConfig_EA_AutoApproveRules_LoadedFromJSON(t *testing.T) {
	cfg := NewConfig()

	ea, ok := cfg.SubagentTypes["executive_assistant"]
	if !ok {
		t.Fatalf("expected executive_assistant in default subagent types")
	}

	// Verify the EA has explicit auto_approve_rules (not nil)
	if ea.AutoApproveRules == nil {
		t.Fatal("expected executive_assistant to have AutoApproveRules loaded from JSON config, got nil")
	}

	// Verify GetAutoApproveRules returns the configured values (not defaults)
	rules := ea.GetAutoApproveRules()
	if len(rules.LowRiskOps) == 0 {
		t.Fatal("GetAutoApproveRules returned empty rules")
	}

	// Verify low_risk ops
	expectedLowRisk := []string{"git_add", "git_status", "git_log", "git_diff", "read_file"}
	if len(rules.LowRiskOps) != len(expectedLowRisk) {
		t.Errorf("low_risk: expected %d items, got %d: %v", len(expectedLowRisk), len(rules.LowRiskOps), rules.LowRiskOps)
	}
	for _, expected := range expectedLowRisk {
		found := false
		for _, op := range rules.LowRiskOps {
			if op == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("low_risk: expected to contain %q, got %v", expected, rules.LowRiskOps)
		}
	}

	// Verify medium_risk ops
	expectedMediumRisk := []string{"git_commit", "git_push", "git_pull", "git_fetch",
		"write_file", "edit_file", "shell_command", "rm_command", "docker",
		"subagent_spawn", "cross_directory"}
	if len(rules.MediumRiskOps) != len(expectedMediumRisk) {
		t.Errorf("medium_risk: expected %d items, got %d: %v", len(expectedMediumRisk), len(rules.MediumRiskOps), rules.MediumRiskOps)
	}
	for _, expected := range expectedMediumRisk {
		found := false
		for _, op := range rules.MediumRiskOps {
			if op == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("medium_risk: expected to contain %q, got %v", expected, rules.MediumRiskOps)
		}
	}

	// Verify high_risk_never ops
	expectedHighRisk := []string{"force_flag", "rm_recursive", "git_reset_hard",
		"git_clean", "docker_prune", "git_push_force",
		"git_checkout", "git_switch", "git_restore", "git_branch_delete"}
	if len(rules.HighRiskNever) != len(expectedHighRisk) {
		t.Errorf("high_risk_never: expected %d items, got %d: %v", len(expectedHighRisk), len(rules.HighRiskNever), rules.HighRiskNever)
	}
	for _, expected := range expectedHighRisk {
		found := false
		for _, op := range rules.HighRiskNever {
			if op == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("high_risk_never: expected to contain %q, got %v", expected, rules.HighRiskNever)
		}
	}
}

func TestNewConfig_EA_AutoApproveRules_MatchDefaults(t *testing.T) {
	cfg := NewConfig()

	ea, ok := cfg.SubagentTypes["executive_assistant"]
	if !ok {
		t.Fatalf("expected executive_assistant in default subagent types")
	}

	// The JSON values were intentionally copied from DefaultAutoApproveRules()
	// so they should match exactly.
	rules := ea.GetAutoApproveRules()
	defaults := DefaultAutoApproveRules()

	// Compare low_risk
	if len(rules.LowRiskOps) != len(defaults.LowRiskOps) {
		t.Errorf("low_risk length mismatch: got %d, want %d", len(rules.LowRiskOps), len(defaults.LowRiskOps))
	} else {
		for _, op := range defaults.LowRiskOps {
			found := false
			for _, got := range rules.LowRiskOps {
				if got == op {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("low_risk: missing %q from defaults", op)
			}
		}
	}

	// Compare medium_risk
	if len(rules.MediumRiskOps) != len(defaults.MediumRiskOps) {
		t.Errorf("medium_risk length mismatch: got %d, want %d", len(rules.MediumRiskOps), len(defaults.MediumRiskOps))
	} else {
		for _, op := range defaults.MediumRiskOps {
			found := false
			for _, got := range rules.MediumRiskOps {
				if got == op {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("medium_risk: missing %q from defaults", op)
			}
		}
	}

	// Compare high_risk_never
	if len(rules.HighRiskNever) != len(defaults.HighRiskNever) {
		t.Errorf("high_risk_never length mismatch: got %d, want %d", len(rules.HighRiskNever), len(defaults.HighRiskNever))
	} else {
		for _, op := range defaults.HighRiskNever {
			found := false
			for _, got := range rules.HighRiskNever {
				if got == op {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("high_risk_never: missing %q from defaults", op)
			}
		}
	}
}

func TestNewConfig_EA_AutoApproveRules_EvaluateOperationRisk(t *testing.T) {
	cfg := NewConfig()

	ea, ok := cfg.SubagentTypes["executive_assistant"]
	if !ok {
		t.Fatalf("expected executive_assistant in default subagent types")
	}

	tests := []struct {
		name     string
		command  string
		expected RiskLevel
	}{
		{"git status is low risk", "git status", RiskLevelLow},
		{"git log is low risk", "git log", RiskLevelLow},
		{"git diff is low risk", "git diff", RiskLevelLow},
		{"git add is low risk", "git add .", RiskLevelLow},
		{"cat is low risk", "cat file.txt", RiskLevelLow},
		{"git commit is medium risk", "git commit -m test", RiskLevelMedium},
		{"git push is medium risk", "git push", RiskLevelMedium},
		{"git pull is medium risk", "git pull", RiskLevelMedium},
		{"write_file is medium risk", "write_file test.txt", RiskLevelMedium},
		{"edit_file is medium risk", "edit_file test.txt", RiskLevelMedium},
		{"shell_command is medium risk", "make build", RiskLevelMedium},
		{"rm is medium risk", "rm file.txt", RiskLevelMedium},
		{"git reset --hard is high risk", "git reset --hard HEAD", RiskLevelHigh},
		{"git clean is high risk", "git clean -fd", RiskLevelHigh},
		{"rm -rf is high risk", "rm -rf /tmp/test", RiskLevelHigh},
		{"git push --force is high risk", "git push --force", RiskLevelHigh},
		{"git checkout is high risk", "git checkout main", RiskLevelHigh},
		{"git switch is high risk", "git switch main", RiskLevelHigh},
		{"git restore is high risk", "git restore file.txt", RiskLevelHigh},
		{"docker prune is high risk", "docker system prune", RiskLevelHigh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ea.EvaluateOperationRisk(tt.command)
			if got != tt.expected {
				t.Errorf("EA.EvaluateOperationRisk(%q) = %q, want %q", tt.command, got, tt.expected)
			}
		})
	}
}

func TestNewConfig_NonEAPersonas_NoAutoApproveRules(t *testing.T) {
	cfg := NewConfig()

	// Personas without explicit auto_approve_rules in their JSON should have nil,
	// but GetAutoApproveRules should still return the defaults.
	for _, id := range []string{"general", "coder", "tester", "debugger", "orchestrator", "web_scraper", "refactor"} {
		t.Run(id, func(t *testing.T) {
			persona, ok := cfg.SubagentTypes[id]
			if !ok {
				t.Fatalf("expected persona %q in default subagent types", id)
			}

			// The raw field should be nil (no JSON config)
			if persona.AutoApproveRules != nil {
				t.Errorf("expected persona %q to have nil AutoApproveRules (no JSON config)", id)
			}

			// But GetAutoApproveRules should fall back to defaults
			rules := persona.GetAutoApproveRules()
			if len(rules.LowRiskOps) == 0 {
				t.Errorf("persona %q: GetAutoApproveRules returned empty rules", id)
			} else {
				defaults := DefaultAutoApproveRules()
				if len(rules.LowRiskOps) != len(defaults.LowRiskOps) {
					t.Errorf("persona %q: low_risk length mismatch", id)
				}
			}
		})
	}
}

func TestConvertAutoApproveRules_NilReturnsNil(t *testing.T) {
	result := convertAutoApproveRules(nil)
	if result != nil {
		t.Errorf("convertAutoApproveRules(nil) = %+v, want nil", result)
	}
}

func TestConvertAutoApproveRules_CreatesDeepCopy(t *testing.T) {
	src := &personas.AutoApproveRules{
		LowRiskOps:    []string{"git_status"},
		MediumRiskOps: []string{"git_commit"},
		HighRiskNever: []string{"force_flag"},
	}

	result := convertAutoApproveRules(src)
	if result == nil {
		t.Fatal("convertAutoApproveRules returned nil for non-nil input")
	}
	if len(result.LowRiskOps) != len(src.LowRiskOps) {
		t.Errorf("low_risk length: got %d, want %d", len(result.LowRiskOps), len(src.LowRiskOps))
	}
	if len(result.MediumRiskOps) != len(src.MediumRiskOps) {
		t.Errorf("medium_risk length: got %d, want %d", len(result.MediumRiskOps), len(src.MediumRiskOps))
	}
	if len(result.HighRiskNever) != len(src.HighRiskNever) {
		t.Errorf("high_risk_never length: got %d, want %d", len(result.HighRiskNever), len(src.HighRiskNever))
	}

	// Verify deep copy — mutating result should not affect source
	result.LowRiskOps[0] = "modified"
	if src.LowRiskOps[0] != "git_status" {
		t.Errorf("source was mutated: got %q, want %q", src.LowRiskOps[0], "git_status")
	}
}
