package agent

import "testing"

func TestShellIsDestructive(t *testing.T) {
	cases := []struct {
		cmd         string
		destructive bool
	}{
		// `git checkout` — the prototypical case.
		{"git checkout .", true},
		{"git checkout -- file.go", true},
		{"git checkout HEAD~1 -- src/", true},
		{"git checkout feature/auth", true}, // branch switch still touches the working tree

		// `git restore` — all variants.
		{"git restore .", true},
		{"git restore --staged .", true},
		{"git restore --source=HEAD~1 file.go", true},

		// `git reset` — only --hard / --merge / --keep flip the working tree.
		{"git reset HEAD", false},
		{"git reset --soft HEAD~1", false},
		{"git reset --hard HEAD~5", true},
		{"git reset --merge", true},
		{"git reset --keep", true},

		// `git stash` — pop/apply/drop/clear clobber active state; bare/push save it.
		{"git stash", false},
		{"git stash push", false},
		{"git stash pop", true},
		{"git stash apply", true},
		{"git stash drop stash@{0}", true},
		{"git stash clear", true},
		{"git stash list", false},

		// `git clean` — destructive only when forced.
		{"git clean", false}, // refuses to run, but we don't need to special-case
		{"git clean -n", false},
		{"git clean -f", true},
		{"git clean -fd", true},
		{"git clean -df", true},
		{"git clean -fdx", true},
		{"git clean --force", true},

		// `git rebase`, `merge`, `pull`, `revert`, `cherry-pick`, `am`.
		{"git rebase main", true},
		{"git rebase --abort", true},
		{"git merge main", true},
		{"git merge --no-commit feature", true},
		{"git pull", true},
		{"git pull origin main", true},
		{"git revert HEAD", true},
		{"git cherry-pick abc123", true},
		{"git am patches/0001.patch", true},

		// `git apply` — destructive by default; read-only flags exempt.
		{"git apply patch.diff", true},
		{"git apply --check patch.diff", false},
		{"git apply --stat patch.diff", false},

		// `git rm` / `git mv` / `git switch`.
		{"git rm src/old.go", true},
		{"git mv src/a.go src/b.go", true},
		{"git switch main", true},

		// Read-only git ops should never classify destructive.
		{"git status", false},
		{"git log --oneline", false},
		{"git diff HEAD", false},
		{"git show HEAD", false},
		{"git config --get user.name", false},

		// Non-git commands aren't classified here (they go through the
		// regular walk; destructive mode is currently git-only).
		{"rm -rf node_modules", false},
		{"npm install", false},
		{"ls", false},
		{"", false},

		// Chains: any destructive segment wins.
		{"git status && git checkout .", true},
		{"git pull || true", true},
		{"git log; git diff", false},
		{"git status | head", false},
		{"git diff HEAD && git status", false},

		// Global flags don't throw off subcommand detection.
		{"git -C /repo checkout .", true},
		{"git -c color.ui=false stash pop", true},
	}

	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			got := shellIsDestructive(tc.cmd)
			if got != tc.destructive {
				t.Errorf("shellIsDestructive(%q) = %v, want %v", tc.cmd, got, tc.destructive)
			}
		})
	}
}
