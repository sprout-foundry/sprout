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
	// After the lockdown relaxation, isGitWriteCommand only fires for
	// intent-gated subcommands: commit, push, merge, clone, init,
	// worktree, and branch/tag CREATE operations. Working-tree-only
	// mutations (checkout, switch, restore, reset, clean, rm, mv, stash
	// mutations, cherry-pick, revert, am, apply, fetch, pull) flow
	// through normally — the change tracker captures pre-mutation
	// content so recover_file / recover_bulk handle restoration.
	// History-loss ops (rebase, reset --hard <commit-ish>, branch -D,
	// tag -d) are caught upstream by isGitHistoryRewriteCommand and not
	// expected to flag here.
	tests := []struct {
		command string
		write   bool
	}{
		// Read-only.
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
		{"git branch -vv", false},
		// Staging — always allowed.
		{"git add .", false},

		// Tier C — intent-gated.
		{"git commit -m 'x'", true},
		{"git --no-pager push origin main", true},
		{"git -c safe.directory=/tmp commit -m \"x\"", true},
		{"git -c key=val push origin main", true},
		{"git -c key=val add . ", false},
		// branch/tag CREATE still intent-gated.
		{"git branch feature-x", true},
		{"git tag v1.2.3", true},

		// Tier A — relaxed, no longer treated as a write here.
		// (These flow through to shell_command; the change tracker
		// handles recovery for working-tree clobbers.)
		{"git checkout main", false},
		{"git switch main", false},
		{"git restore file.txt", false},
		{"git restore --staged file.txt", false},
		{"git reset HEAD~1", false}, // mixed reset
		{"git reset --soft HEAD~1", false},
		{"git -C /path/to/repo reset --soft HEAD~1", false},
		{"git clean -fd", false},
		{"git rm src/old.go", false},
		{"git mv src/a.go src/b.go", false},
		{"git stash", false},
		{"git stash pop", false},
		{"git stash apply", false},
		{"git fetch origin", false},
		{"git pull origin main", false},
		{"git cherry-pick abc123", false},
		{"git revert HEAD", false},
		{"git am patches/0001.patch", false},
		{"git apply patch.diff", false},

		// Tier B — handled by isGitHistoryRewriteCommand, NOT here.
		// They should not flag write either; the history-rewrite gate
		// gives a clearer error and respects AllowGitHistoryRewrite.
		{"git rebase main", false},
		{"git reset --hard HEAD~5", false},
		{"git branch -d feature-x", false},
		{"git tag -d v1.2.3", false},
	}

	for _, tc := range tests {
		if got := isGitWriteCommand(tc.command); got != tc.write {
			t.Fatalf("isGitWriteCommand(%q) = %v, want %v", tc.command, got, tc.write)
		}
	}

	// Compound commands (shell chaining).
	compoundTests := []struct {
		name    string
		command string
		write   bool
	}{
		// Intent-gated still fires inside compound expressions.
		{"cd && commit", "cd /some/path && git commit -m 'fix'", true},
		{"cd ; push", "cd /some/path; git push origin main", true},
		{"subshell merge", "(cd /some/path && git merge feature)", true},
		{"status && commit", "git status && git commit -m 'fix'", true},
		{"multiple git, second push", "git log && git push", true},

		// Tier A inside compound — no longer gated as a write.
		{"cd && reset", "cd /path && git reset HEAD", false},
		{"cd && restore", "cd /path && git restore file.txt", false},
		{"cd && checkout", "cd /path && git checkout main", false},

		// Single-quoted segments are stripped by stripQuotedContent.
		{"bash -c rebase", "bash -c 'cd /some/path && git rebase main'", false},

		// Read-only / staging inside compound.
		{"cd && status", "cd /some/path && git status", false},
		{"git log && diff", "git log && git diff", false},
		{"cd && add", "cd /path && git add file.txt", false},
		{"cd && branch list", "cd /path && git branch", false},

		// False-positive regression: quoted content should not be scanned.
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

func TestIsGitHistoryRewriteCommand(t *testing.T) {
	tests := []struct {
		command string
		rewrite bool
	}{
		// rebase — any form rewrites commit history.
		// Exception: `--abort` is a recovery op, not a history rewrite.
		{"git rebase main", true},
		{"git rebase --abort", false},
		{"git rebase --abort --no-verify", true}, // --abort with other flags is still a rewrite
		{"git rebase -i HEAD~5", true},
		{"git -C /repo rebase main", true},

		// reset --hard — only with a non-HEAD commit-ish.
		{"git reset --hard", false},
		{"git reset --hard HEAD", false},
		{"git reset --hard HEAD~1", true},
		{"git reset --hard HEAD~5", true},
		{"git reset --hard abc1234", true},
		{"git reset --hard origin/main", true},
		// `--soft` and `--mixed` don't lose commits via reset's pointer move;
		// they leave the working tree (and possibly index) intact.
		{"git reset --soft HEAD~5", false},
		{"git reset --mixed HEAD~5", false},
		{"git reset HEAD~5", false},

		// branch delete — loses ref + potentially commits.
		{"git branch -d old-feature", true},
		{"git branch -D feature", true},
		{"git branch --delete feature", true},
		{"git branch feature", false}, // create
		{"git branch -a", false},      // list
		{"git branch --list", false},  // list

		// tag delete.
		{"git tag -d v1.0", true},
		{"git tag --delete v1.0", true},
		{"git tag v1.0", false},
		{"git tag -l", false},

		// Compound: any destructive segment wins.
		{"git status && git rebase main", true},
		{"cd /repo && git reset --hard HEAD~1", true},
		{"git rebase main || true", true},

		// Non-destructive.
		{"git status", false},
		{"git checkout main", false},
		{"git restore .", false},
		{"git clean -fd", false},
		{"git commit -m 'x'", false},
		{"git push", false},
		{"git pull", false},

		// Quoted content should not be scanned.
		{`curl -d '{"cmd":"git reset --hard HEAD~1"}' http://api`, false},
		{`echo "run git rebase main"`, false},
	}

	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			got := isGitHistoryRewriteCommand(tc.command)
			if got != tc.rewrite {
				t.Errorf("isGitHistoryRewriteCommand(%q) = %v, want %v", tc.command, got, tc.rewrite)
			}
		})
	}
}

func TestIsGitRebaseCommand(t *testing.T) {
	// isGitRebaseCommand separates the AGENTS.md "never rebase" rule from
	// the looser "git history-rewrite" gate. The ONLY rebase invocation
	// that returns false is pure `git rebase --abort` (recovery from a
	// prior session's interrupted rebase). Every other form returns true,
	// including `git pull --rebase`/`-r` and `--abort` mixed with other
	// arguments.
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		// Standard rebase forms — all banned.
		{"bare rebase", "git rebase", true},
		{"rebase main", "git rebase main", true},
		{"interactive", "git rebase -i HEAD~5", true},
		{"rebase --onto", "git rebase --onto base head", true},
		{"rebase with -C", "git -C /repo rebase main", true},
		{"rebase --continue", "git rebase --continue", true},
		{"rebase --skip", "git rebase --skip", true},

		// --abort cases. Only the PURE form is permitted.
		{"pure --abort", "git rebase --abort", false},
		{"--abort with positional", "git rebase main --abort", true},        // positional means real rebase target, abort-with-target is invalid git anyway
		{"--abort with HEAD positional", "git rebase HEAD --abort", true},   // HEAD + --abort is nonsensical; treat as banned
		{"--abort with extra flag", "git rebase --abort --no-verify", true}, // mixing --abort with another flag = rewrite attempt

		// git pull --rebase — subcommand is pull, but AGENTS.md bans this too.
		{"pull --rebase", "git pull --rebase origin main", true},
		{"pull -r short form", "git pull -r origin main", true},
		{"pull --rebase-preserve", "git pull --rebase-preserve", true},
		{"plain git pull (allowed)", "git pull origin main", false},
		{"pull without --rebase flag (allowed)", "git pull --no-rebase origin main", false},

		// Quoted content should not trigger false positive.
		{"quoted mention of rebase", `echo "git rebase main"`, false},
		{"quoted mention of pull --rebase", `echo "git pull --rebase"`, false},

		// Compound commands.
		{"rebase in chain", "cd /repo && git rebase main && echo done", true},
		{"pull --rebase in chain", "cd /repo && git pull --rebase", true},

		// Non-rebase commands.
		{"plain git pull (not a rebase)", "git pull origin main", false},
		{"git status", "git status", false},
		{"git commit", "git commit -m 'x'", false},
		{"git merge (not a rebase)", "git merge feature", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isGitRebaseCommand(tc.command); got != tc.want {
				t.Errorf("isGitRebaseCommand(%q) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}

func TestIsGitCommitSubcommand(t *testing.T) {
	tests := []struct {
		command  string
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
		{`""`, []string{""}},         // empty quotes → empty token
		{`''`, []string{""}},         // empty single quotes → empty token
		{"hello", []string{"hello"}}, // single word
		{"a\nb", []string{"a", "b"}}, // newline as delimiter
		{"a\tb", []string{"a", "b"}}, // tab as delimiter
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

func TestIsGitStashCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		// Blocked (destructive stash operations)
		{"bare git stash", "git stash", true},
		{"git stash push", "git stash push", true},
		{"git stash push -m msg", "git stash push -m 'wip'", true},
		{"git stash pop", "git stash pop", true},
		{"git stash apply", "git stash apply", true},
		{"git stash apply stash@{0}", "git stash apply stash@{0}", true},
		{"git stash drop", "git stash drop", true},
		{"git stash clear", "git stash clear", true},
		{"compound stash + build", "cd /repo && git stash && go build ./...", true},
		{"stash with -c flag", "git -c core.safecrlf=false stash", true},
		{"stash with -C flag", "git -C /path stash pop", true},

		// Allowed (read-only)
		{"stash list", "git stash list", false},
		{"stash show", "git stash show", false},
		{"stash show -p", "git stash show -p", false},
		{"stash list with flags", "git stash list --oneline", false},

		// Non-stash commands
		{"git status", "git status", false},
		{"git log", "git log", false},
		{"git add", "git add file.go", false},
		{"git commit", "git commit -m 'msg'", false},
		{"non-git command", "ls -la", false},
		{"empty string", "", false},
		{"git checkout (gated elsewhere)", "git checkout main", false},
		{"git restore (gated elsewhere)", "git restore file.go", false},

		// Edge: quoted content shouldn't trigger false positive
		{"stash mentioned in quotes", `echo "git stash is dangerous"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGitStashCommand(tt.command)
			if got != tt.want {
				t.Errorf("isGitStashCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}
