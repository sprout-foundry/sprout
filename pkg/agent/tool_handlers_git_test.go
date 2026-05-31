package agent

import (
	"testing"
)

func TestStripQuotedContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no quotes", "curl -s http://api", "curl -s http://api"},
		{"single-quoted content", "echo 'git commit'", "echo '          '"},
		{"double-quoted content", "echo \"git commit\"", "echo \"          \""},
		{"JSON in single quotes", "curl -d '{\"msg\":\"git commit title\"}'", "curl -d '                          '"},
		{"empty string", "", ""},
		{"preserves newlines", "echo 'line1\nline2'", "echo '     \n     '"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripQuotedContent(tc.input)
			if got != tc.want {
				t.Errorf("stripQuotedContent(%q)\n  got: %q\n want: %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsGitWriteCommand(t *testing.T) {
	tests := []struct {
		command string
		write   bool
	}{
		{"git status", false},
		{"git log --oneline -5", false},
		{"git diff HEAD~1", false},
		{"git show HEAD~1", false},
		{"git branch", false},
		{"git branch -a", false},
		{"git branch --list", false},
		{"git tag", false},
		{"git tag -l", false},
		{"git stash list", false},
		{"git stash show", false},
		{"git commit -m 'x'", true},
		{"git add .", false}, // staging is always allowed
		{"git checkout main", true},
		{"git branch feature-x", true},
		{"git branch -d feature-x", true},
		{"git tag v1.2.3", true},
		{"git tag -d v1.2.3", true},
		{"git stash", true},
		{"git stash pop", true},
		{"git fetch origin", true},
		{"git --no-pager push origin main", true},                                               // --no-pager skipped
		// Duplicate entries with descriptive names would require adding a name field to the struct.
		// Existing entries already cover git branch --list, git branch -a, and git tag -l as read-only.
		{"git branch -vv", false},                                                               // verbose, read-only
		{"git -c key=val add . ", false},                                                        // -c flag value skipped, add found as subcommand (broad add checked separately)
		{"git -c safe.directory=/tmp commit -m \"x\"", true},                                    // -c flag with key=value properly skipped
		{"git -c key=val push origin main", true},                                               // -c flag properly skipped
		{"git -C /path/to/repo reset --soft HEAD~1", true},                                      // -C path flag properly skipped
	}

	for _, tc := range tests {
		if got := isGitWriteCommand(tc.command); got != tc.write {
			t.Fatalf("isGitWriteCommand(%q) = %v, want %v", tc.command, got, tc.write)
		}
	}
	
	// Test compound commands (shell chaining)
	compoundTests := []struct {
		name    string
		command string
		write   bool
	}{
		// Should return true: compound commands with write operations
		{"cd && commit", "cd /some/path && git commit -m 'fix'", true},
		{"cd ; push", "cd /some/path; git push origin main", true},
		{"subshell merge", "(cd /some/path && git merge feature)", true},
		{"bash -c rebase", "bash -c 'cd /some/path && git rebase main'", false}, // single-quoted content is stripped; can't parse bash -c semantics
		{"status && commit", "git status && git commit -m 'fix'", true},
		{"multiple git, second push", "git log && git push", true},
		{"cd && reset", "cd /path && git reset HEAD", true},
		{"cd && restore", "cd /path && git restore file.txt", true},
		
		// Should return false: compound commands without write operations
		{"cd && status", "cd /some/path && git status", false},
		{"git log && diff", "git log && git diff", false},
		{"cd && add", "cd /path && git add file.txt", false}, // add is allowed, not considered a restricted write
		{"cd && branch list", "cd /path && git branch", false},
		
		// False positive regression: quoted content should not be scanned
		{"curl with git commit in JSON", `curl -s http://localhost/v1/chat -d '{"model":"gpt","messages":[{"role":"user","content":"Generate a git commit title"}]}'`, false},
		{"curl with git push in JSON", `curl -d '{"prompt":"git push origin main"}' http://api`, false},
		{"echo with git checkout", `echo "run git checkout main please"`, false},
		{"curl piped with git reset in JSON", `curl -s http://api -d '{"cmd":"git reset --hard"}' | python3 -m json.tool`, false},
	}
	
	for _, tc := range compoundTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGitWriteCommand(tc.command); got != tc.write {
				t.Errorf("isGitWriteCommand(%q) = %v, want %v", tc.command, got, tc.write)
			}
		})
	}
}

func TestIsGitCommitSubcommand(t *testing.T) {
	tests := []struct {
		command   string
		isCommit bool
	}{
		{"git commit -m 'x'", true},
		{"git commit", true},
		{"git commit --amend", true},
		{"git commit -m \"fix: typo\"", true},
		{"git status", false},
		{"git log", false},
		{"git push", false},
		{"git push origin main", false},
		{"git add .", false},
		{"git merge feature", false},
		{"git rebase main", false},
		{"git reset HEAD~1", false},
		{"git checkout main", false},
		{"not a git command", false},
		{"git", false},
		{"git commit -m 'fix' --allow-empty", true},
		{"git -c user.name=test commit -m 'x'", true},
		{"git -c commit.gpgsign=false commit -m 'x'", true},
	}

	for _, tc := range tests {
		if got := isGitCommitSubcommand(tc.command); got != tc.isCommit {
			t.Fatalf("isGitCommitSubcommand(%q) = %v, want %v", tc.command, got, tc.isCommit)
		}
	}
}

func TestIsGitCheckoutSubcommand(t *testing.T) {
	tests := []struct {
		command     string
		isCheckout bool
	}{
		{"git checkout main", true},
		{"git checkout -b feature", true},
		{"git checkout -- file.txt", true},
		{"git switch main", true},
		{"git switch -c new-branch", true},
		{"git --no-pager checkout main", true},
		{"git -C /path/to/repo checkout main", true},
		{"git status", false},
		{"git log --oneline", false},
		{"git diff", false},
		{"git commit -m 'fix'", false},
		{"git add .", false},
		{"git push origin main", false},
		{"git merge feature", false},
		{"not a git command", false},
		{"git", false},
		{"", false},
		{"   git checkout main", true},
	}

	for _, tc := range tests {
		if got := isGitCheckoutSubcommand(tc.command); got != tc.isCheckout {
			t.Fatalf("isGitCheckoutSubcommand(%q) = %v, want %v", tc.command, got, tc.isCheckout)
		}
	}
	
	// Test compound commands (shell chaining)
	compoundTests := []struct {
		name     string
		command  string
		isCheckout bool
	}{
		// Should return true: compound commands with checkout/switch
		{"cd && checkout", "cd /some/path && git checkout main", true},
		{"cd ; checkout", "cd /some/path; git checkout -b feature", true},
		{"status && checkout", "git status && git checkout main", true},
		{"multiple git, second checkout", "git log && git checkout branch", true},
		
		// Should return false: compound commands without checkout/switch
		{"cd && status", "cd /some/path && git status", false},
		{"git log && diff", "git log && git diff", false},
		{"cd && reset", "cd /path && git reset HEAD", false},
		
		// Note: These edge cases are not handled by the simple parser
		// They would require more complex shell parsing to detect
		{"subshell checkout (BLOCKED - finds git)", "(cd /some/path && git switch main)", true}, // Actually blocked because git is found
		{"bash -c checkout (now correctly ignored)", "bash -c 'cd /some/path && git checkout'", false}, // Fixed: quoted content is stripped

		// False positive regression: quoted content should not be scanned
		{"curl with git checkout in JSON", `curl -s http://api -d '{"cmd":"git checkout main"}'`, false},
		{"echo with git switch", `echo "run git switch feature"`, false},
	}
	
	for _, tc := range compoundTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGitCheckoutSubcommand(tc.command); got != tc.isCheckout {
				t.Errorf("isGitCheckoutSubcommand(%q) = %v, want %v", tc.command, got, tc.isCheckout)
			}
		})
	}
}

func TestExtractGitCommitArgs(t *testing.T) {
	tests := []struct {
		command     string
		wantMessage string
	}{
		// -m with double quotes (LLM may or may not include quotes)
		{`git commit -m "fix: typo"`, "fix: typo"},
		{`git commit -m "fix"`, "fix"},
		{`git commit -m 'fix: typo'`, "fix: typo"},
		// Unquoted message (most reliable case)
		{"git commit -m fix:typo", "fix:typo"},
		// --message long form
		{`git commit --message "fix: typo"`, "fix: typo"},
		// Multi-paragraph: multiple -m flags produce separate paragraphs
		{`git commit -m "title" -m "body"`, "title\n\nbody"},
		{`git commit -m title -m "body paragraph" -m "footer"`, "title\n\nbody paragraph\n\nfooter"},
		// --amend with -m (extract message, --amend handling is separate)
		{`git commit --amend -m "fix"`, "fix"},
		// No -m flag
		{`git commit`, ""},
		{`git commit --amend`, ""},
		// Flags before -m
		{`git commit --no-verify -m "fix"`, "fix"},
		// Irrelevant flags before message
		{`git commit -m "fix" --allow-empty`, "fix"},
		// Not a git command — function is a simple parser, returns empty
		{"git status", ""},
		{"", ""},
	}

	for _, tc := range tests {
		if got := extractGitCommitArgs(tc.command); got != tc.wantMessage {
			t.Fatalf("extractGitCommitArgs(%q) = %q, want %q", tc.command, got, tc.wantMessage)
		}
	}
}

func TestShellSplit(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"git commit -m \"fix: typo\"", []string{"git", "commit", "-m", "fix: typo"}},
		{"git commit -m 'fix: typo'", []string{"git", "commit", "-m", "fix: typo"}},
		{"git commit -m fix", []string{"git", "commit", "-m", "fix"}},
		{"git  status  ", []string{"git", "status"}},
		{"", nil},
		{"   ", nil},
		{`"hello world"`, []string{"hello world"}},
		{`"nested 'quotes' inside"`, []string{"nested 'quotes' inside"}},
		{`mix"ed"quotes`, []string{"mixedquotes"}},
		{`""`, []string{""}},           // empty quotes → empty token
		{`''`, []string{""}},           // empty single quotes → empty token
		{"hello", []string{"hello"}},    // single word
		{"a\nb", []string{"a", "b"}},    // newline as delimiter
		{"a\tb", []string{"a", "b"}},    // tab as delimiter
	}

	for _, tc := range tests {
		got := shellSplit(tc.input)
		if len(got) != len(tc.want) {
			t.Fatalf("shellSplit(%q) = %q (len %d), want %q (len %d)", tc.input, got, len(got), tc.want, len(tc.want))
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("shellSplit(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

func TestIsGitDiscardCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		command string
		discard bool
	}{
		// Should return true: commands that discard changes
		{"restore file", "git restore file.txt", true},
		{"restore --staged", "git restore --staged file.txt", true},
		{"restore --worktree", "git restore --worktree file.txt", true},
		{"restore --source", "git restore --source=HEAD -- file.txt", true},
		{"reset HEAD", "git reset HEAD", true},
		{"reset HEAD with file", "git reset HEAD -- file.txt", true},
		{"reset --hard", "git reset --hard", true},
		{"reset --soft", "git reset --soft HEAD~1", true},
		{"reset --mixed", "git reset --mixed", true},
		{"git global flag --no-pager restore", "git --no-pager restore file.txt", true},
		{"whitespace around restore", "  git restore file.txt  ", true},

		// Should return false: commands that don't discard changes
		{"git status", "git status", false},
		{"git log", "git log", false},
		{"git diff", "git diff", false},
		{"git add", "git add file.txt", false},
		{"git commit", "git commit -m \"message\"", false},
		{"empty string", "", false},
		{"not a git command", "not a git command", false},
		{"git branch", "git branch", false},
		{"git push", "git push", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGitDiscardCommand(tc.command); got != tc.discard {
				t.Errorf("isGitDiscardCommand(%q) = %v, want %v", tc.command, got, tc.discard)
			}
		})
	}
	
	// Test compound commands (shell chaining)
	compoundTests := []struct {
		name    string
		command string
		discard bool
	}{
		// Should return true: compound commands with reset/restore
		{"cd && reset", "cd /some/path && git reset --hard HEAD~1", true},
		{"cd ; reset", "cd /some/path; git reset --hard", true},
		{"status && reset", "git status && git reset --hard", true},
		{"multiple git, second reset", "git log && git reset HEAD", true},
		{"cd && restore", "cd /path && git restore file.txt", true},
		
		// Should return false: compound commands without reset/restore
		{"cd && status", "cd /some/path && git status", false},
		{"git log && diff", "git log && git diff", false},
		{"cd && add", "cd /path && git add file.txt", false},
		
		// Note: These edge cases are not handled by the simple parser
		// They would require more complex shell parsing to detect
		{"subshell reset (BLOCKED - finds git)", "(cd /some/path && git reset)", true}, // Actually blocked because git is found
		{"bash -c reset (now correctly ignored)", "bash -c 'cd /some/path && git reset'", false}, // Fixed: quoted content is stripped

		// False positive regression: quoted content should not be scanned
		{"curl with git reset in JSON", `curl -s http://api -d '{"cmd":"git reset --hard"}'`, false},
		{"curl with git restore in JSON", `curl -d '{"prompt":"git restore file"}' http://api`, false},
		{"echo with git reset", `echo "run git reset HEAD"`, false},
	}
	
	for _, tc := range compoundTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGitDiscardCommand(tc.command); got != tc.discard {
				t.Errorf("isGitDiscardCommand(%q) = %v, want %v", tc.command, got, tc.discard)
			}
		})
	}
}

func TestExtractGitSubcommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"status", "git status", "status"},
		{"log with flags", "git log --oneline", "log"},
		{"commit with flags", "git commit -m \"msg\"", "commit"},
		{"diff with --no-pager", "git --no-pager diff", "diff"},
		{"push with -c config", "git -c key=val push", "push"},
		{"branch with whitespace", "  git branch  ", "branch"},
		{"bare git", "git", "unknown"},
		{"empty string", "", "unknown"},
		{"not git", "abc", "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractGitSubcommand(tc.input); got != tc.want {
				t.Errorf("extractGitSubcommand(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}