//go:build windows

package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Git Bash maps /tmp to the user's temp dir; the file tools must agree so a
// file written as /tmp/x by one is the file the other reads.
func TestPosixTmpPath_FollowsGitBash(t *testing.T) {
	cases := map[string]string{
		"/tmp":            os.TempDir(),
		"/tmp/p.json":     filepath.Join(os.TempDir(), "p.json"),
		`\tmp\sub\p.json`: filepath.Join(os.TempDir(), "sub", "p.json"),
		"/tmp/a/../b.txt": filepath.Join(os.TempDir(), "b.txt"),
	}
	for in, want := range cases {
		got, ok := PosixTmpPath(in)
		if !ok || got != want {
			t.Errorf("PosixTmpPath(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"/tmp/../etc/passwd", "/tmpfoo", "/etc/tmp", `C:\tmp\x`, "tmp/x", ""} {
		if got, ok := PosixTmpPath(in); ok {
			t.Errorf("PosixTmpPath(%q) = %q; must not map", in, got)
		}
	}
}

func TestSafeResolveAbs_TmpMatchesGitBash(t *testing.T) {
	ws := t.TempDir()
	ctx := WithWorkspaceRoot(context.Background(), ws)
	got, err := SafeResolveAbs(ctx, "/tmp/sprout-probe.json")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(os.TempDir(), "sprout-probe.json"); got != want {
		t.Fatalf("SafeResolveAbs(/tmp/…) = %q, want %q (Git Bash's /tmp)", got, want)
	}
	got, err = SafeResolveAbs(ctx, "/etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.VolumeName(ws) + `\etc\passwd`; got != want {
		t.Fatalf("SafeResolveAbs(/etc/passwd) = %q, want drive root %q", got, want)
	}
}
