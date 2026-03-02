package agent

import "testing"

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
		{"git add .", true},
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
