package shelltext

import (
	"strings"
	"testing"
)

func sp(n int) string { return strings.Repeat(" ", n) }

func TestStripQuotedContent(t *testing.T) {
	qChecks := []struct {
		name  string
		input string
		want  string
	}{
		{"no quotes", "curl -s http://api", "curl -s http://api"},
		{"single-quoted", "echo 'git commit'", "echo '" + sp(10) + "'"},
		{"double-quoted", "echo \"git commit\"", "echo \"" + sp(10) + "\""},
		{"JSON single quotes", "curl -d '{\"msg\":\"git commit title\"}'", "curl -d '" + sp(26) + "'"},
		{"empty", "", ""},
		{"newlines preserved", "echo 'line1\nline2'", "echo '" + sp(5) + "\n" + sp(5) + "'"},
		{"pipe in quotes", "grep 'rgba|gradient|shadow'", "grep '" + sp(20) + "'"},
		{"mixed", "echo \"hello\" 'world'", "echo \"" + sp(5) + "\" '" + sp(5) + "'"},
		{"unclosed quote", "echo \"hello", "echo \"" + sp(5)},
		{"nested quote", `echo "it's here"`, `echo "         "`},
	}
	for _, tc := range qChecks {
		t.Run(tc.name, func(t *testing.T) {
			got := StripQuotedContent(tc.input)
			if got != tc.want {
				t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestStripHeredocBodies(t *testing.T) {
	t.Run("body replaced, delimiters and newlines preserved", func(t *testing.T) {
		in := "cat > file <<'EOF'\nhello world\nEOF\n"
		want := "cat > file <<'EOF'\n" + sp(11) + "\n" + sp(3) + "\n"
		if got := StripHeredocBodies(in); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})

	t.Run("unterminated heredoc left as-is", func(t *testing.T) {
		in := "cat > file <<'EOF'\nhello world\n"
		if got := StripHeredocBodies(in); got != in {
			t.Errorf("got %q want %q", got, in)
		}
	})

	t.Run("dash heredoc", func(t *testing.T) {
		in := "cat > file <<-TAB\nfoo bar\nTAB\n"
		want := "cat > file <<-TAB\n" + sp(7) + "\n" + sp(3) + "\n"
		if got := StripHeredocBodies(in); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})
}

func TestStripHeredocAndQuotes(t *testing.T) {
	t.Run("heredoc plus quoted string after", func(t *testing.T) {
		in := "cat > file <<'EOF'\nhello world\nEOF\n echo \"git checkout\""
		want := "cat > file <<'   '\n" + sp(11) + "\n" + sp(3) + "\n echo \"" + sp(12) + "\""
		if got := StripHeredocAndQuotes(in); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})

	t.Run("rm -rf inside heredoc body is data, not a command", func(t *testing.T) {
		in := "cat > file <<'EOF'\nrm -rf /data\nEOF\n"
		got := StripHeredocAndQuotes(in)
		if strings.Contains(got, "rm -rf") {
			t.Errorf("heredoc body leaked into output: %q", got)
		}
	})

	t.Run("no quotes or heredoc unchanged", func(t *testing.T) {
		if got := StripHeredocAndQuotes("git status"); got != "git status" {
			t.Errorf("got %q", got)
		}
	})
}

func TestIsGitHistoryRewriteCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git rebase", true},
		{"git rebase -i HEAD~10", true},
		{"git rebase --onto base head", true},
		{"git rebase main", true},
		{"git rebase --abort", false},
		{"git rebase --abort --no-verify", true},
		{"git reset --hard HEAD~5", true},
		{"git reset --hard abc123", true},
		{"git reset --hard origin/main~1", true},
		{"git reset --hard", false},
		{"git reset --hard HEAD", false},
		{"git reset --soft HEAD~5", false},
		{"git reset --mixed HEAD~5", false},
		{"git branch -d feature", true},
		{"git branch -D feature", true},
		{"git branch --delete feature", true},
		{"git branch feature", false},
		{"git branch -v", false},
		{"git branch -a", false},
		{"git tag -d v1.0", true},
		{"git tag --delete v1.0", true},
		{"git tag v1.0", false},
		{"git tag -l", false},
		{"ls -la", false},
		{"echo hello", false},
		{"cd /tmp", false},
		{"make build", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := IsGitHistoryRewriteCommand(tt.cmd); got != tt.want {
				t.Errorf("IsGitHistoryRewriteCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestIsGitHistoryRewriteCommand_quoted_git_not_flagged(t *testing.T) {
	cmd := `echo "git reset --hard HEAD~5"`
	if IsGitHistoryRewriteCommand(cmd) {
		t.Errorf("IsGitHistoryRewriteCommand(%q) = true, want false", cmd)
	}
}
