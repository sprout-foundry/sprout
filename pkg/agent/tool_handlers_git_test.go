package agent

import (
	"testing"
)

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
	}

	for _, tc := range tests {
		if got := isGitWriteCommand(tc.command); got != tc.write {
			t.Fatalf("isGitWriteCommand(%q) = %v, want %v", tc.command, got, tc.write)
		}
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