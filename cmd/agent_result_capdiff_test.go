//go:build !js

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func section(path, body string) string {
	return "diff --git a/" + path + " b/" + path + "\nindex 0000000..1111111 100644\n--- a/" + path + "\n+++ b/" + path + "\n" + body
}

// quotedSection is a section the way git emits it for a path it must quote
// (one holding a space, a quote or a non-ASCII byte).
func quotedSection(quoted, body string) string {
	return "diff --git \"a/" + quoted + "\" \"b/" + quoted + "\"\nindex 0000000..1111111 100644\n--- \"a/" + quoted + "\"\n+++ \"b/" + quoted + "\"\n" + body
}

// A quoted header path (git quotes a path holding a space, a quote or a
// non-ASCII byte) is named by its unquoted path, not by the raw header.
func TestCapDiff_QuotedPathNamed(t *testing.T) {
	big := quotedSection(`quo\"te.txt`, "@@ -1 +1 @@\n-"+strings.Repeat("z", 5000)+"\n+"+strings.Repeat("w", 5000)+"\n")
	small := section("src/a.go", "@@ -1 +1 @@\n-x\n+y\n")
	_, omitted := capDiff(big+small, len(small)+10)
	if len(omitted) != 1 || omitted[0] != `quo"te.txt` {
		t.Fatalf("expected the quoted path unquoted, got %q", omitted)
	}
}

// A path that itself contains " b/" is not split at that spot.
func TestCapDiff_PathContainingBSlash(t *testing.T) {
	for _, path := range []string{"x b/f.txt", "a b/c b/d.go"} {
		if got := diffSectionPath(section(path, "@@ -1 +1 @@\n-x\n+y\n")); got != path {
			t.Fatalf("path %q parsed as %q", path, got)
		}
	}
}

// A deletion has "+++ /dev/null", so its path comes from the "---" line.
func TestCapDiff_DeletionPath(t *testing.T) {
	sec := "diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\n--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n"
	if got := diffSectionPath(sec); got != "gone.txt" {
		t.Fatalf("deletion path parsed as %q", got)
	}
}

// A creation has "--- /dev/null", so its path comes from the "+++" line.
func TestCapDiff_CreationPath(t *testing.T) {
	sec := "diff --git a/new.txt b/new.txt\nnew file mode 100644\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+hi\n"
	if got := diffSectionPath(sec); got != "new.txt" {
		t.Fatalf("creation path parsed as %q", got)
	}
}

// Text before the first file header is its own section: it is never merged
// into a file it does not belong to, and never silently dropped.
func TestCapDiff_LeadingTextKeptSeparate(t *testing.T) {
	lead := "warning: some preamble\n"
	small := section("src/a.go", "@@ -1 +1 @@\n-x\n+y\n")
	big := section("dist/bundle.js", "@@ -1 +1 @@\n-"+strings.Repeat("z", 5000)+"\n+"+strings.Repeat("w", 5000)+"\n")
	got, omitted := capDiff(lead+small+big, len(lead)+len(small)+10)
	if got != lead+small {
		t.Fatalf("expected the preamble and the small file, got:\n%s", got)
	}
	if len(omitted) != 1 || omitted[0] != "dist/bundle.js" {
		t.Fatalf("expected only the bundle named, got %v", omitted)
	}
}

// One file over the limit on its own is still kept: an empty diff would read
// as "nothing changed".
func TestCapDiff_SingleOversizedFileKept(t *testing.T) {
	big := section("dist/bundle.js", "@@ -1 +1 @@\n-"+strings.Repeat("z", 5000)+"\n+"+strings.Repeat("w", 5000)+"\n")
	got, omitted := capDiff(big, 100)
	if got != big {
		t.Fatalf("the only file must be kept whole, got %d bytes", len(got))
	}
	if omitted != nil {
		t.Fatalf("nothing is left out, got %v", omitted)
	}
}

// A diff that fits is kept as it is, and nothing is named as left out.
func TestCapDiff_FitsUnchanged(t *testing.T) {
	d := section("a.go", "@@ -1 +1 @@\n-x\n+y\n") + section("b.go", "@@ -1 +1 @@\n-p\n+q\n")
	got, omitted := capDiff(d, 1<<20)
	if got != d || omitted != nil {
		t.Fatalf("a diff that fits must be unchanged: omitted=%v", omitted)
	}
}

// Over the limit, whole files are left out, largest first, the rest keep
// their order, and the left-out files are named.
func TestCapDiff_LeavesOutWholeLargestFiles(t *testing.T) {
	small1 := section("src/a.go", "@@ -1 +1 @@\n-x\n+y\n")
	big := section("internal/dist/bundle.js", "@@ -1 +1 @@\n-"+strings.Repeat("z", 5000)+"\n+"+strings.Repeat("w", 5000)+"\n")
	small2 := section("src/b.go", "@@ -1 +1 @@\n-p\n+q\n")
	d := small1 + big + small2
	got, omitted := capDiff(d, len(small1)+len(small2)+10)
	if got != small1+small2 {
		t.Fatalf("expected the two small files whole, in order; got:\n%s", got)
	}
	if len(omitted) != 1 || omitted[0] != "internal/dist/bundle.js" {
		t.Fatalf("expected the bundle named as left out, got %v", omitted)
	}
	if strings.Contains(got, "truncated") {
		t.Fatal("no marker is added inside the diff")
	}
}

// What is kept still applies with git apply (no file is ever cut).
func TestCapDiff_KeptDiffApplies(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: git on the test's own temp dir
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q")
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "one\n")
	write("big.txt", strings.Repeat("line\n", 4000))
	write("c.txt", "three\n")
	run("add", ".")
	run("commit", "-qm", "init")
	write("a.txt", "one changed\n")
	write("big.txt", strings.Repeat("other\n", 4000))
	write("c.txt", "three changed\n")
	full := run("diff", "HEAD")
	kept, omitted := capDiff(full, 2000)
	if len(omitted) != 1 || omitted[0] != "big.txt" {
		t.Fatalf("expected big.txt left out, got %v", omitted)
	}
	run("checkout", "-q", "--", ".")
	patch := filepath.Join(t.TempDir(), "p.diff")
	if err := os.WriteFile(patch, []byte(kept), 0o644); err != nil {
		t.Fatal(err)
	}
	run("apply", "--check", patch)
	run("apply", patch)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one changed\n" {
		t.Fatalf("a.txt not applied: %q", b)
	}
}

// Drives real git: every path shape git can emit, then checks the cap names
// each one correctly and the kept diff still applies.
func TestCapDiff_RealGitPathShapes(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: git on the test's own temp dir
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q")
	paths := []string{"plain.txt", "sp ace.txt", `quo"te.txt`, "ünï.txt", "x b/f.txt", "gone.txt"}
	for _, p := range paths {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("orig\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-qm", "init")
	for _, p := range paths {
		if p == "gone.txt" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte("changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	full := run("diff", "HEAD")
	// Each section names the path git actually changed.
	for _, sec := range splitDiffSections(full) {
		if strings.TrimSpace(sec) == "" {
			continue
		}
		got := diffSectionPath(sec)
		found := false
		for _, p := range paths {
			if got == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("section named %q, not one of the changed paths\nheader: %q", got, strings.SplitN(sec, "\n", 2)[0])
		}
	}
	// A cap that leaves some files out names exactly the ones dropped.
	kept, omitted := capDiff(full, len(full)/2)
	named := map[string]bool{}
	for _, o := range omitted {
		named[o] = true
	}
	for _, sec := range splitDiffSections(kept) {
		if strings.TrimSpace(sec) == "" {
			continue
		}
		if named[diffSectionPath(sec)] {
			t.Errorf("%q is both kept and named as left out", diffSectionPath(sec))
		}
	}
	if len(omitted) == 0 {
		t.Fatal("expected some files left out")
	}
	// The kept diff still applies to the pre-change tree.
	run("reset", "-q", "--hard", "HEAD")
	patch := filepath.Join(t.TempDir(), "p.diff")
	if err := os.WriteFile(patch, []byte(kept), 0o644); err != nil {
		t.Fatal(err)
	}
	run("apply", "--check", patch)
}
