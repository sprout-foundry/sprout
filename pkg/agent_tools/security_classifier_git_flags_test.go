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
	// reset --hard/--keep/--merge are now CAUTION (downgraded from DANGEROUS)
	// but still categorized as destructive with destructive_git_operation RiskType
	destructiveCaution := SecurityResult{
		Risk: SecurityCaution, ShouldBlock: false, ShouldPrompt: true,
		IsHardBlock: false, RiskType: "destructive_git_operation",
		Category: RiskCategoryDestructive,
	}
	caution := SecurityResult{
		Risk: SecurityCaution, ShouldPrompt: true,
		Category: RiskCategoryFileWrite,
	}

	tests := []struct {
		name string
		args map[string]interface{}
		want SecurityResult
	}{
		// Destructive resets — now CAUTION (downgraded from DANGEROUS)
		{"reset --hard", map[string]interface{}{"operation": "reset", "args": "--hard HEAD~5"}, destructiveCaution},
		{"reset --keep", map[string]interface{}{"operation": "reset", "args": "--keep"}, destructiveCaution},
		{"reset --merge", map[string]interface{}{"operation": "reset", "args": "--merge"}, destructiveCaution},
		{"reset --hard mid-pos", map[string]interface{}{"operation": "reset", "args": "HEAD~3 --hard"}, destructiveCaution},
		// Safe resets (stay CAUTION)
		{"reset --soft", map[string]interface{}{"operation": "reset", "args": "--soft HEAD~1"}, caution},
		{"reset --mixed", map[string]interface{}{"operation": "reset", "args": "--mixed"}, caution},
		{"reset no args", map[string]interface{}{"operation": "reset"}, caution},
		{"reset empty args", map[string]interface{}{"operation": "reset", "args": ""}, caution},
		// Whole-token: --hardlink must NOT match --hard
		{"reset --hardlink-test", map[string]interface{}{"operation": "reset", "args": "--hardlink-test"}, caution},
		{"reset --hardcore-foo", map[string]interface{}{"operation": "reset", "args": "--hardcore-foo"}, caution},
		// --hard alone (no trailing ref)
		{"reset --hard only", map[string]interface{}{"operation": "reset", "args": "--hard"}, destructiveCaution},
		// Case sensitivity: operation names are lowercased inside classifyGitOperation
		{"reset --hard (MixedCase)", map[string]interface{}{"operation": "Reset", "args": "--hard"}, destructiveCaution},
		{"reset --hard (UPPERCASE)", map[string]interface{}{"operation": "RESET", "args": "--hard"}, destructiveCaution},
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
	// rebase -i/--onto are now CAUTION (downgraded from DANGEROUS)
	// but still categorized as destructive with destructive_git_operation RiskType
	destructiveCaution := SecurityResult{
		Risk: SecurityCaution, ShouldBlock: false, ShouldPrompt: true,
		IsHardBlock: false, RiskType: "destructive_git_operation",
		Category: RiskCategoryDestructive,
	}
	caution := SecurityResult{
		Risk: SecurityCaution, ShouldPrompt: true,
		Category: RiskCategoryFileWrite,
	}

	tests := []struct {
		name string
		args map[string]interface{}
		want SecurityResult
	}{
		// Destructive rebases — now CAUTION (downgraded from DANGEROUS)
		{"rebase -i", map[string]interface{}{"operation": "rebase", "args": "-i HEAD~3"}, destructiveCaution},
		{"rebase -i -p", map[string]interface{}{"operation": "rebase", "args": "-i -p"}, destructiveCaution},
		{"rebase --onto", map[string]interface{}{"operation": "rebase", "args": "--onto master feature branch"}, destructiveCaution},
		// Safe rebases (stay CAUTION)
		{"rebase no flag", map[string]interface{}{"operation": "rebase", "args": "master"}, caution},
		{"rebase no args", map[string]interface{}{"operation": "rebase"}, caution},
		{"rebase empty args", map[string]interface{}{"operation": "rebase", "args": ""}, caution},
		// Whole-token: -i sandwiched between other flags
		{"rebase -x -i -y", map[string]interface{}{"operation": "rebase", "args": "-x -i -y"}, destructiveCaution},
		// Whole-token negatives: similar-looking flags must NOT match
		{"rebase --no-onto", map[string]interface{}{"operation": "rebase", "args": "--no-onto"}, caution},
		{"rebase -n", map[string]interface{}{"operation": "rebase", "args": "-n"}, caution},
		// Case sensitivity
		{"rebase -i (UPPERCASE)", map[string]interface{}{"operation": "REBASE", "args": "-i"}, destructiveCaution},
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

// TestClassifyGitOperation_NeverDangerousOrHardBlock asserts the invariant
// that classifyGitOperation only ever returns SecuritySafe or
// SecurityCaution — never SecurityDangerous, and never sets IsHardBlock=true.
//
// Why this matters: the git handler's security model depends on this
// assumption. The case SecurityDangerous: branch in gitHandler.Execute is
// effectively dead code, and historically there was no IsHardBlock
// early-return (unlike the shell handler). A defensive early-return now
// exists, but this test guards the underlying classifier invariant so a
// future change that adds `return SecurityResult{Risk: SecurityDangerous...}`
// or `IsHardBlock: true` is caught immediately. If git needs true hard-block
// semantics, that change must be made deliberately and this test updated.
func TestClassifyGitOperation_NeverDangerousOrHardBlock(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		// Safe ops — covers the safeOps list branch
		{"commit", map[string]interface{}{"operation": "commit"}},
		{"add", map[string]interface{}{"operation": "add"}},
		{"status", map[string]interface{}{"operation": "status"}},
		{"log", map[string]interface{}{"operation": "log"}},
		{"diff", map[string]interface{}{"operation": "diff"}},
		{"show", map[string]interface{}{"operation": "show"}},
		{"branch", map[string]interface{}{"operation": "branch"}},
		{"remote", map[string]interface{}{"operation": "remote"}},
		{"stash", map[string]interface{}{"operation": "stash"}},
		{"tag", map[string]interface{}{"operation": "tag"}},
		{"revert", map[string]interface{}{"operation": "revert"}},
		{"fetch", map[string]interface{}{"operation": "fetch"}},
		{"merge", map[string]interface{}{"operation": "merge"}},
		{"pull", map[string]interface{}{"operation": "pull"}},
		{"push", map[string]interface{}{"operation": "push"}},

		// Flag-aware reset — destructive flag branch
		{"reset --hard", map[string]interface{}{"operation": "reset", "args": "--hard HEAD~5"}},
		{"reset --keep", map[string]interface{}{"operation": "reset", "args": "--keep"}},
		{"reset --merge", map[string]interface{}{"operation": "reset", "args": "--merge"}},
		// Flag-aware reset — safe flags fall to cautionOps branch
		{"reset --soft", map[string]interface{}{"operation": "reset", "args": "--soft HEAD~1"}},
		{"reset --mixed", map[string]interface{}{"operation": "reset", "args": "--mixed"}},
		// Plain reset — cautionOps branch
		{"reset no args", map[string]interface{}{"operation": "reset"}},
		{"reset empty args", map[string]interface{}{"operation": "reset", "args": ""}},

		// Flag-aware rebase — destructive flag branch
		{"rebase -i", map[string]interface{}{"operation": "rebase", "args": "-i HEAD~3"}},
		{"rebase --onto", map[string]interface{}{"operation": "rebase", "args": "--onto master feature branch"}},
		// Plain rebase — cautionOps branch
		{"rebase no flag", map[string]interface{}{"operation": "rebase", "args": "master"}},
		{"rebase no args", map[string]interface{}{"operation": "rebase"}},

		// Caution ops — cautionOps branch
		{"cherry_pick", map[string]interface{}{"operation": "cherry_pick"}},
		{"am", map[string]interface{}{"operation": "am"}},
		{"apply", map[string]interface{}{"operation": "apply"}},
		{"rm", map[string]interface{}{"operation": "rm"}},
		{"mv", map[string]interface{}{"operation": "mv"}},
		{"clean", map[string]interface{}{"operation": "clean"}},

		// Dangerous ops — dangerousOps branch (still returns Caution, not Dangerous)
		{"branch_delete", map[string]interface{}{"operation": "branch_delete"}},
		{"push --force", map[string]interface{}{"operation": "push --force"}},
		{"push -f", map[string]interface{}{"operation": "push -f"}},
		{"push --force via args", map[string]interface{}{"operation": "push", "args": "--force origin main"}},
		// Exercises the strings.HasPrefix(op, "push") && strings.Contains(opRaw, "--force")
		// sub-condition uniquely — operation contains "--force" but doesn't match
		// any exact dangerousOps entry.
		{"push --force-with-lease", map[string]interface{}{"operation": "push --force-with-lease"}},

		// Unknown / empty — fallback branches
		{"empty operation", map[string]interface{}{"operation": ""}},
		{"unknown op", map[string]interface{}{"operation": "nonexistent-op"}},
		{"nil args", map[string]interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyGitOperation(tt.args)
			if result.Risk == SecurityDangerous {
				t.Errorf("classifyGitOperation returned SecurityDangerous for %v — invariant violation: git classifier must only return Safe or Caution (got %+v)", tt.args, result)
			}
			if result.IsHardBlock {
				t.Errorf("classifyGitOperation returned IsHardBlock=true for %v — invariant violation: git classifier must never set IsHardBlock (got %+v)", tt.args, result)
			}
		})
	}
}
