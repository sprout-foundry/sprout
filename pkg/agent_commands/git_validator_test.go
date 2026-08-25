package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsGitCheckoutSubcommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		// checkout detection
		{
			name:    "simple checkout",
			command: "git checkout main",
			want:    true,
		},
		{
			name:    "checkout with branch",
			command: "git checkout -b new-branch",
			want:    true,
		},
		// switch detection
		{
			name:    "simple switch",
			command: "git switch main",
			want:    true,
		},
		{
			name:    "switch with -c flag",
			command: "git switch -c new-branch",
			want:    true,
		},
		// with flags
		{
			name:    "checkout with -c flag",
			command: "git -c core.safecrlf=false checkout main",
			want:    true,
		},
		{
			name:    "checkout with -C flag",
			command: "git -C /path/to/repo checkout main",
			want:    true,
		},
		{
			name:    "checkout with --exec-path flag",
			command: "git --exec-path=/usr/bin checkout main",
			want:    true,
		},
		{
			name:    "checkout with --git-dir flag",
			command: "git --git-dir=/path/to/.git checkout main",
			want:    true,
		},
		{
			name:    "checkout with --work-tree flag",
			command: "git --work-tree=/path/to/work checkout main",
			want:    true,
		},
		{
			name:    "checkout with multiple flags",
			command: "git -c core.safecrlf=false -C /path checkout main",
			want:    true,
		},
		// compound commands (&&)
		{
			name:    "compound command with checkout",
			command: "cd /path && git checkout main",
			want:    true,
		},
		{
			name:    "compound command with switch",
			command: "cd /path && git switch main && ls",
			want:    true,
		},
		{
			name:    "multiple git commands, one is checkout",
			command: "git status && git checkout main",
			want:    true,
		},
		// non-checkout commands
		{
			name:    "status command",
			command: "git status",
			want:    false,
		},
		{
			name:    "log command",
			command: "git log",
			want:    false,
		},
		{
			name:    "commit command",
			command: "git commit -m 'message'",
			want:    false,
		},
		{
			name:    "add command",
			command: "git add .",
			want:    false,
		},
		{
			name:    "push command",
			command: "git push origin main",
			want:    false,
		},
		{
			name:    "pull command",
			command: "git pull",
			want:    false,
		},
		{
			name:    "branch command",
			command: "git branch",
			want:    false,
		},
		{
			name:    "merge command",
			command: "git merge main",
			want:    false,
		},
		{
			name:    "diff command",
			command: "git diff",
			want:    false,
		},
		{
			name:    "non-git command",
			command: "ls -la",
			want:    false,
		},
		// edge cases
		{
			name:    "empty string",
			command: "",
			want:    false,
		},
		{
			name:    "git without subcommand",
			command: "git",
			want:    false,
		},
		{
			name:    "checkout with trailing punctuation",
			command: "if condition; then git checkout main; fi",
			want:    true,
		},
		{
			name:    "checkout in parentheses",
			command: "(git checkout main)",
			want:    true,
		},
		{
			name:    "checkout with quotes",
			command: "git checkout \"main\"",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsGitCheckoutSubcommand(tt.command)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsGitDiscardCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		// restore detection
		{
			name:    "simple restore",
			command: "git restore file.txt",
			want:    true,
		},
		{
			name:    "restore with --staged",
			command: "git restore --staged file.txt",
			want:    true,
		},
		{
			name:    "restore with --worktree",
			command: "git restore --worktree file.txt",
			want:    true,
		},
		{
			name:    "restore with multiple files",
			command: "git restore file1.txt file2.txt",
			want:    true,
		},
		// reset detection
		{
			name:    "simple reset",
			command: "git reset HEAD~1",
			want:    true,
		},
		{
			name:    "reset --hard",
			command: "git reset --hard",
			want:    true,
		},
		{
			name:    "reset --soft",
			command: "git reset --soft HEAD~1",
			want:    true,
		},
		{
			name:    "reset --mixed",
			command: "git reset --mixed HEAD~1",
			want:    true,
		},
		// with flags
		{
			name:    "restore with -c flag",
			command: "git -c core.safecrlf=false restore file.txt",
			want:    true,
		},
		{
			name:    "reset with -C flag",
			command: "git -C /path reset --hard",
			want:    true,
		},
		// compound commands
		{
			name:    "compound command with restore",
			command: "cd /path && git restore file.txt",
			want:    true,
		},
		{
			name:    "compound command with reset",
			command: "cd /path && git reset --hard && ls",
			want:    true,
		},
		{
			name:    "multiple git commands, one is restore",
			command: "git status && git restore file.txt",
			want:    true,
		},
		// non-discard commands
		{
			name:    "checkout command (not restore/reset)",
			command: "git checkout main",
			want:    false,
		},
		{
			name:    "status command",
			command: "git status",
			want:    false,
		},
		{
			name:    "log command",
			command: "git log",
			want:    false,
		},
		{
			name:    "commit command",
			command: "git commit -m 'message'",
			want:    false,
		},
		{
			name:    "add command",
			command: "git add .",
			want:    false,
		},
		{
			name:    "push command",
			command: "git push origin main",
			want:    false,
		},
		{
			name:    "pull command",
			command: "git pull",
			want:    false,
		},
		{
			name:    "branch command",
			command: "git branch",
			want:    false,
		},
		{
			name:    "non-git command",
			command: "ls -la",
			want:    false,
		},
		// edge cases
		{
			name:    "empty string",
			command: "",
			want:    false,
		},
		{
			name:    "git without subcommand",
			command: "git",
			want:    false,
		},
		{
			name:    "restore with trailing punctuation",
			command: "if condition; then git restore file.txt; fi",
			want:    true,
		},
		{
			name:    "reset in parentheses",
			command: "(git reset --hard)",
			want:    true,
		},
		// stash detection — all stash variants are gated as discard
		{
			name:    "bare git stash",
			command: "git stash",
			want:    true,
		},
		{
			name:    "git stash push",
			command: "git stash push",
			want:    true,
		},
		{
			name:    "git stash pop",
			command: "git stash pop",
			want:    true,
		},
		{
			name:    "git stash apply",
			command: "git stash apply",
			want:    true,
		},
		{
			name:    "git stash drop",
			command: "git stash drop",
			want:    true,
		},
		{
			name:    "git stash clear",
			command: "git stash clear",
			want:    true,
		},
		{
			name:    "compound command with stash",
			command: "cd /path && git stash && go build ./...",
			want:    true,
		},
		{
			name:    "stash list (read-only, not discard)",
			command: "git stash list",
			want:    false,
		},
		{
			name:    "stash show (read-only, not discard)",
			command: "git stash show",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsGitDiscardCommand(tt.command)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractGitSubcommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "simple checkout",
			command: "git checkout main",
			want:    "checkout",
		},
		{
			name:    "simple switch",
			command: "git switch main",
			want:    "switch",
		},
		{
			name:    "simple restore",
			command: "git restore file.txt",
			want:    "restore",
		},
		{
			name:    "simple reset",
			command: "git reset --hard",
			want:    "reset",
		},
		{
			name:    "with -c flag",
			command: "git -c core.safecrlf=false checkout main",
			want:    "checkout",
		},
		{
			name:    "with -C flag",
			command: "git -C /path checkout main",
			want:    "checkout",
		},
		{
			name:    "with --exec-path flag",
			command: "git --exec-path=/usr/bin checkout main",
			want:    "checkout",
		},
		{
			name:    "with --git-dir flag",
			command: "git --git-dir=/path/to/.git checkout main",
			want:    "checkout",
		},
		{
			name:    "with --work-tree flag",
			command: "git --work-tree=/path/to/work checkout main",
			want:    "checkout",
		},
		{
			name:    "with multiple flags",
			command: "git -c core.safecrlf=false -C /path checkout main",
			want:    "checkout",
		},
		{
			name:    "commit command",
			command: "git commit -m 'message'",
			want:    "commit",
		},
		{
			name:    "status command",
			command: "git status",
			want:    "status",
		},
		{
			name:    "no git prefix",
			command: "checkout main",
			want:    "unknown",
		},
		{
			name:    "empty string",
			command: "",
			want:    "unknown",
		},
		{
			name:    "git without subcommand",
			command: "git",
			want:    "unknown",
		},
		{
			name:    "unknown command",
			command: "git",
			want:    "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractGitSubcommand(tt.command)
			assert.Equal(t, tt.want, got)
		})
	}
}
