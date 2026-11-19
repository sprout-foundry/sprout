package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatMergeMessage(t *testing.T) {
	raw := "Merge remote-tracking branch 'origin/main'\n\n# Conflicts:\n#\tdocs/ISSUES.md\n#\tinternal/a.go\n#\n# It looks like you may be committing a merge.\n"
	got := formatMergeMessage(raw)
	want := "Merge remote-tracking branch 'origin/main'\n\nResolved conflicts in:\n- docs/ISSUES.md\n- internal/a.go"
	if got != want {
		t.Fatalf("formatMergeMessage =\n%q\nwant\n%q", got, want)
	}
	if got := formatMergeMessage("Merge branch 'feature'\n"); got != "Merge branch 'feature'" {
		t.Fatalf("a clean merge keeps git's line: %q", got)
	}
}

// A merge in progress is described by git's own merge message, with the
// files whose conflicts were resolved; with no merge there is none.
func TestMergeCommitMessageDuringAMerge(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...) // #nosec G204 -- test helper: fixed git binary, arguments come from the test
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		_ = cmd.Run()
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("a.txt", "base\n")
	git("add", "a.txt")
	git("commit", "-q", "-m", "base")
	git("checkout", "-q", "-b", "feature")
	write("a.txt", "feature\n")
	git("commit", "-q", "-am", "feature")
	git("checkout", "-q", "main")
	write("a.txt", "main\n")
	git("commit", "-q", "-am", "main")

	SetGitDir(dir)
	defer SetGitDir("")
	if _, ok := mergeCommitMessage(); ok {
		t.Fatal("no merge is in progress yet")
	}
	git("merge", "--no-commit", "feature")
	write("a.txt", "resolved\n")
	git("add", "a.txt")

	msg, ok := mergeCommitMessage()
	if !ok {
		t.Fatal("a merge is in progress")
	}
	if !strings.HasPrefix(msg, "Merge branch 'feature'") || !strings.Contains(msg, "Resolved conflicts in:\n- a.txt") {
		t.Fatalf("message = %q", msg)
	}
}
