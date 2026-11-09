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
		cmd := exec.Command("git", args...)
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
