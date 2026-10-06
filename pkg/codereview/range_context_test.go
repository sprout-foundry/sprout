package codereview

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRangeDiffAndContext(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...) //nolint:gosec // G204: git on the test's own temp dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", "-b", "main")
	write("go.mod", "module x\n")
	write("a.go", "package x\n\nfunc F() int { return 1 }\n")
	git("add", ".")
	git("commit", "-q", "-m", "init")
	git("checkout", "-q", "-b", "feature")
	write("a.go", "package x\n\nfunc F() int { return 2 }\n")
	git("commit", "-q", "-am", "make F return 2")
	write("a.go", "package x\n\nfunc F() int { return 3 } // uncommitted\n")

	ctx := context.Background()
	diff, err := RangeDiff(ctx, root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+func F() int { return 2 }") || strings.Contains(diff, "uncommitted") {
		t.Errorf("RangeDiff should hold committed branch work only:\n%s", diff)
	}

	sc := BuildRangeContext(ctx, root, "main", diff)
	if !strings.Contains(sc.CommitMessage, "- make F return 2") {
		t.Errorf("CommitMessage = %q; want the branch's commit subjects", sc.CommitMessage)
	}
	if !strings.Contains(sc.FullFileContext, "return 2") || strings.Contains(sc.FullFileContext, "uncommitted") {
		t.Errorf("FullFileContext should come from HEAD:\n%s", sc.FullFileContext)
	}

	for _, bad := range []string{"", "--output=/tmp/x", "no-such-ref"} {
		if _, err := RangeDiff(ctx, root, bad); err == nil {
			t.Errorf("RangeDiff(%q) succeeded; want an error", bad)
		}
	}
}
