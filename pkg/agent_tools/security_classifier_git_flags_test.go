package tools

import (
	"testing"
)

func TestHasToken(t *testing.T) {
	tests := []struct {
		input  string
		token  string
		expect bool
	}{
		{"--hard HEAD~5", "--hard", true},
		{"HEAD~5 --hard", "--hard", true},
		{"--soft HEAD~1", "--hard", false},
		{"--hardcore-foo", "--hard", false},
		{"--hardlink-test", "--hard", false},
		{"-i HEAD~3", "-i", true},
		{"-i -p", "-i", true},
		{"-ii", "-i", false},
		{"--onto master feature branch", "--onto", true},
		{"--ontological", "--onto", false},
		{"--keep", "--keep", true},
		{"--keeper", "--keep", false},
		{"--merge", "--merge", true},
		{"--merged", "--merge", false},
		{"", "--hard", false},
		{"  ", "--hard", false},
		{"--soft --mixed", "--soft", true},
		{"--soft --mixed", "--mixed", true},
		{"--soft --mixed", "--hard", false},
	}
	for _, tt := range tests {
		if got := hasToken(tt.input, tt.token); got != tt.expect {
			t.Errorf("hasToken(%q, %q) = %v, want %v", tt.input, tt.token, got, tt.expect)
		}
	}
}

func TestClassifyGitOperation_FlagAwareReset(t *testing.T) {
	dangerous := SecurityResult{
		Risk: SecurityDangerous, ShouldBlock: true, ShouldPrompt: true,
		IsHardBlock: true, RiskType: "destructive_git_operation",
		Category: RiskCategoryDestructive,
	}
	caution := SecurityResult{
		Risk: SecurityCaution, ShouldPrompt: true,
		Category: RiskCategoryFileWrite,
	}

	tests := []struct {
		name    string
		args    map[string]interface{}
		want    SecurityResult
	}{
		// Dangerous resets
		{"reset --hard", map[string]interface{}{"operation": "reset", "args": "--hard HEAD~5"}, dangerous},
		{"reset --keep", map[string]interface{}{"operation": "reset", "args": "--keep"}, dangerous},
		{"reset --merge", map[string]interface{}{"operation": "reset", "args": "--merge"}, dangerous},
		{"reset --hard mid-pos", map[string]interface{}{"operation": "reset", "args": "HEAD~3 --hard"}, dangerous},
		// Safe resets (stay CAUTION)
		{"reset --soft", map[string]interface{}{"operation": "reset", "args": "--soft HEAD~1"}, caution},
		{"reset --mixed", map[string]interface{}{"operation": "reset", "args": "--mixed"}, caution},
		{"reset no args", map[string]interface{}{"operation": "reset"}, caution},
		{"reset empty args", map[string]interface{}{"operation": "reset", "args": ""}, caution},
		// Whole-token: --hardlink must NOT match --hard
		{"reset --hardlink-test", map[string]interface{}{"operation": "reset", "args": "--hardlink-test"}, caution},
		{"reset --hardcore-foo", map[string]interface{}{"operation": "reset", "args": "--hardcore-foo"}, caution},
		// --hard alone (no trailing ref)
		{"reset --hard only", map[string]interface{}{"operation": "reset", "args": "--hard"}, dangerous},
		// Case sensitivity: operation names are lowercased inside classifyGitOperation
		{"reset --hard (MixedCase)", map[string]interface{}{"operation": "Reset", "args": "--hard"}, dangerous},
		{"reset --hard (UPPERCASE)", map[string]interface{}{"operation": "RESET", "args": "--hard"}, dangerous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyGitOperation(tt.args)
			if result.Risk != tt.want.Risk {
				t.Errorf("Risk = %v, want %v", result.Risk, tt.want.Risk)
			}
			if result.ShouldBlock != tt.want.ShouldBlock {
				t.Errorf("ShouldBlock = %v, want %v", result.ShouldBlock, tt.want.ShouldBlock)
			}
			if result.IsHardBlock != tt.want.IsHardBlock {
				t.Errorf("IsHardBlock = %v, want %v", result.IsHardBlock, tt.want.IsHardBlock)
			}
			if result.Category != tt.want.Category {
				t.Errorf("Category = %q, want %q", result.Category, tt.want.Category)
			}
			if result.RiskType != tt.want.RiskType {
				t.Errorf("RiskType = %q, want %q", result.RiskType, tt.want.RiskType)
			}
		})
	}
}

func TestClassifyGitOperation_FlagAwareRebase(t *testing.T) {
	dangerous := SecurityResult{
		Risk: SecurityDangerous, ShouldBlock: true, ShouldPrompt: true,
		IsHardBlock: true, RiskType: "destructive_git_operation",
		Category: RiskCategoryDestructive,
	}
	caution := SecurityResult{
		Risk: SecurityCaution, ShouldPrompt: true,
		Category: RiskCategoryFileWrite,
	}

	tests := []struct {
		name    string
		args    map[string]interface{}
		want    SecurityResult
	}{
		// Dangerous rebases
		{"rebase -i", map[string]interface{}{"operation": "rebase", "args": "-i HEAD~3"}, dangerous},
		{"rebase -i -p", map[string]interface{}{"operation": "rebase", "args": "-i -p"}, dangerous},
		{"rebase --onto", map[string]interface{}{"operation": "rebase", "args": "--onto master feature branch"}, dangerous},
		// Safe rebases (stay CAUTION)
		{"rebase no flag", map[string]interface{}{"operation": "rebase", "args": "master"}, caution},
		{"rebase no args", map[string]interface{}{"operation": "rebase"}, caution},
		{"rebase empty args", map[string]interface{}{"operation": "rebase", "args": ""}, caution},
		// Whole-token: -i sandwiched between other flags
		{"rebase -x -i -y", map[string]interface{}{"operation": "rebase", "args": "-x -i -y"}, dangerous},
		// Whole-token negatives: similar-looking flags must NOT match
		{"rebase --no-onto", map[string]interface{}{"operation": "rebase", "args": "--no-onto"}, caution},
		{"rebase -n", map[string]interface{}{"operation": "rebase", "args": "-n"}, caution},
		// Case sensitivity
		{"rebase -i (UPPERCASE)", map[string]interface{}{"operation": "REBASE", "args": "-i"}, dangerous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyGitOperation(tt.args)
			if result.Risk != tt.want.Risk {
				t.Errorf("Risk = %v, want %v", result.Risk, tt.want.Risk)
			}
			if result.ShouldBlock != tt.want.ShouldBlock {
				t.Errorf("ShouldBlock = %v, want %v", result.ShouldBlock, tt.want.ShouldBlock)
			}
			if result.IsHardBlock != tt.want.IsHardBlock {
				t.Errorf("IsHardBlock = %v, want %v", result.IsHardBlock, tt.want.IsHardBlock)
			}
			if result.Category != tt.want.Category {
				t.Errorf("Category = %q, want %q", result.Category, tt.want.Category)
			}
			if result.RiskType != tt.want.RiskType {
				t.Errorf("RiskType = %q, want %q", result.RiskType, tt.want.RiskType)
			}
		})
	}
}

func TestClassifyGitOperation_SafeOpsUnaffected(t *testing.T) {
	// Safe operations should not be affected by destructive flag arguments.
	// The flag-aware logic only applies to "reset" and "rebase".
	safeResult := func(op string) SecurityResult {
		return SecurityResult{
			Risk:         SecuritySafe,
			ShouldBlock:  false,
			ShouldPrompt: false,
			Category:     RiskCategoryReadOnly,
		}
	}
	tests := []struct {
		name string
		args map[string]interface{}
		want SecurityResult
	}{
		{"commit with --hard in msg", map[string]interface{}{"operation": "commit", "args": "-m \"revert --hard\""}, safeResult("commit")},
		{"add with --hard flag", map[string]interface{}{"operation": "add", "args": "--hard"}, safeResult("add")},
		{"status normal", map[string]interface{}{"operation": "status"}, safeResult("status")},
		{"log with -i flag", map[string]interface{}{"operation": "log", "args": "-i"}, safeResult("log")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyGitOperation(tt.args)
			if result.Risk != tt.want.Risk {
				t.Errorf("Risk = %v, want %v", result.Risk, tt.want.Risk)
			}
			if result.ShouldBlock != tt.want.ShouldBlock {
				t.Errorf("ShouldBlock = %v, want %v", result.ShouldBlock, tt.want.ShouldBlock)
			}
			if result.Category != tt.want.Category {
				t.Errorf("Category = %q, want %q", result.Category, tt.want.Category)
			}
		})
	}
}
