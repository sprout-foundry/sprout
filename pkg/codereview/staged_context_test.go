package codereview

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShouldSkipFileForContext(t *testing.T) {
	skip := []string{"go.sum", "yarn.lock", "web/package-lock.json", "app.min.js", "x.js.map",
		"api/foo.pb.go", "types_generated.go", "node_modules/a/b.js", "vendor/x/y.go",
		"coverage.out", "logo.svg", "img.png"}
	keep := []string{"main.go", "pkg/agent/agent.go", "src/app.tsx", "README.md", "Makefile"}
	for _, p := range skip {
		if !shouldSkipFileForContext(p) {
			t.Errorf("expected %q to be skipped", p)
		}
	}
	for _, p := range keep {
		if shouldSkipFileForContext(p) {
			t.Errorf("expected %q to be kept", p)
		}
	}
}

func TestDetectProjectType(t *testing.T) {
	cases := map[string]string{
		"go.mod": "Go project", "package.json": "Node.js project", "pyproject.toml": "Python project",
		"Cargo.toml": "Rust project", "Gemfile": "Ruby project",
	}
	for marker, want := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, marker), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := DetectProjectType(dir); got != want {
			t.Errorf("%s: got %q, want %q", marker, got, want)
		}
	}
	if got := DetectProjectType(t.TempDir()); got != "" {
		t.Errorf("empty dir: got %q", got)
	}
}

func TestExtractKeyComments(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n+// NOTE: must hold the lock here\n+x := 1 // plain\n" +
		"diff --git a/b.py b/b.py\n+# SECURITY: validate before use\n+++ b/b.py\n"
	got := ExtractKeyComments(diff)
	if !strings.Contains(got, "- a.go: // NOTE: must hold the lock here") || !strings.Contains(got, "- b.py: # SECURITY: validate before use") {
		t.Errorf("unexpected key comments:\n%s", got)
	}
	if strings.Contains(got, "plain") {
		t.Errorf("unimportant comment included:\n%s", got)
	}

	var many strings.Builder
	many.WriteString("diff --git a/a.go b/a.go\n")
	for i := 0; i < 15; i++ {
		fmt.Fprintf(&many, "+// TODO: item %d\n", i)
	}
	if n := strings.Count(ExtractKeyComments(many.String()), "\n") + 1; n != maxKeyComments {
		t.Errorf("got %d key comments, want cap of %d", n, maxKeyComments)
	}
}

func TestCategorizeChanges(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\nindex 1..2\n--- a/a.go\n+++ b/a.go\n" +
		"+if err != nil {\n+\trequire(\"github.com/x/y\")\n+func TestFoo() {}\n-old line\n+// SECURITY check\n"
	got := CategorizeChanges(diff)
	for _, want := range []string{"Error handling", "Dependency updates", "Test changes", "Code removal/refactoring (1 changes)", "Security fixes/improvements"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if got != CategorizeChanges(diff) {
		t.Error("output order is not deterministic")
	}
	if CategorizeChanges("") != "" {
		t.Error("empty diff should have no categories")
	}
}

func TestExcerptAroundHunks(t *testing.T) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}

	got := excerptAroundHunks(lines, []lineRange{{700, 702}})
	if !strings.Contains(got, "  700  line700") {
		t.Errorf("hunk far down the file not shown:\n%s", got)
	}
	if strings.Contains(got, "line1\n") || strings.Contains(got, "  600  ") {
		t.Errorf("excerpt not windowed around the hunk")
	}

	two := excerptAroundHunks(lines, []lineRange{{100, 100}, {500, 500}})
	if !strings.Contains(two, "...\n") || !strings.Contains(two, "line100") || !strings.Contains(two, "line500") {
		t.Errorf("separate hunks not shown as separate windows:\n%s", two)
	}

	var hunks []lineRange
	for i := 1; i < 1000; i += 50 {
		hunks = append(hunks, lineRange{i, i})
	}
	capped := excerptAroundHunks(lines, hunks)
	if !strings.Contains(capped, "omitted") || strings.Count(capped, "\n") > fileContextMaxLinesPerFile+20 {
		t.Errorf("per-file cap not applied (%d lines)", strings.Count(capped, "\n"))
	}
}

func TestResolveInRoot(t *testing.T) {
	root := t.TempDir()
	if _, ok := resolveInRoot(root, "pkg/a.go"); !ok {
		t.Error("in-root path rejected")
	}
	for _, bad := range []string{"../escape.go", "a/../../escape.go", "/etc/passwd", ""} {
		if _, ok := resolveInRoot(root, bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFenceLanguage(t *testing.T) {
	for path, want := range map[string]string{"a.go": "go", "b.TSX": "tsx", "c.py": "python", "Makefile": ""} {
		if got := fenceLanguage(path); got != want {
			t.Errorf("fenceLanguage(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestBuildStagedContext_ResolvesAgainstRepoRootFromSubdir(t *testing.T) {
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
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q")
	write("go.mod", "module x\n")
	write("pkg/a.py", "def f():\n    return 1\n")
	git("add", ".")
	git("commit", "-q", "-m", "init")
	write("pkg/a.py", "def f():\n    return 2\n")
	git("add", "pkg/a.py")
	write("pkg/a.py", "def f():\n    return 3  # unstaged\n")

	diffOut, err := exec.Command("git", "-C", root, "diff", "--cached").Output() //nolint:gosec // G204: git on the test's own temp dir
	if err != nil {
		t.Fatal(err)
	}

	sc := BuildStagedContext(context.Background(), filepath.Join(root, "pkg"), string(diffOut))

	if sc.ProjectType != "Go project" {
		t.Errorf("ProjectType = %q; want detection at repo root", sc.ProjectType)
	}
	if !strings.Contains(sc.CommitMessage, "1 file changed") {
		t.Errorf("CommitMessage = %q; want the stat totals line", sc.CommitMessage)
	}
	if !strings.Contains(sc.FullFileContext, "### pkg/a.py\n```python") || !strings.Contains(sc.FullFileContext, "return 2") {
		t.Errorf("FullFileContext not resolved from repo root:\n%s", sc.FullFileContext)
	}
	if strings.Contains(sc.FullFileContext, "unstaged") {
		t.Errorf("FullFileContext shows unstaged edits:\n%s", sc.FullFileContext)
	}
}
