package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShellQuotePath(t *testing.T) {
	cases := map[string]string{
		"/tmp/with space/msg": `"/tmp/with space/msg"`,
		"/tmp/a$b`c\"d":       `"/tmp/a\$b\` + "`" + `c\"d"`,
	}
	if runtime.GOOS == "windows" {
		cases[`C:\Users\alan\AppData\Local\Temp\sprout-commit-msg-1`] = `"C:/Users/alan/AppData/Local/Temp/sprout-commit-msg-1"`
	}
	for in, want := range cases {
		if got := shellQuotePath(in); got != want {
			t.Errorf("shellQuotePath(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestCommitMessage_RealRepoKeepsMessageAndExtraArgs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...) //nolint:gosec // G204: test-driven git invocations with controlled args
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	git("config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "a.txt")

	msg := "fix: keep `backticks` and $HOME literal"
	res, err := commitMessage(context.Background(), msg, dir)
	if err != nil || res.IsError {
		t.Fatalf("commit: err=%v output=%s", err, res.Output)
	}
	if got := git("log", "-1", "--format=%B"); got != msg {
		t.Fatalf("commit message = %q, want %q", got, msg)
	}

	// Extra flags must reach git, not be dropped.
	res, err = commitMessage(context.Background(), "amended message", dir, "--amend")
	if err != nil || res.IsError {
		t.Fatalf("amend: err=%v output=%s", err, res.Output)
	}
	if got := git("rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("commit count = %s, want 1 (--amend was dropped)", got)
	}
	if got := git("log", "-1", "--format=%B"); got != "amended message" {
		t.Fatalf("amended message = %q", got)
	}
}
