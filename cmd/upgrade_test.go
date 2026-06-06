//go:build !js

package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"v1.2.3", "v1.2.3"},
		{"V1.2.3", "v1.2.3"},
		{"1.2.3", "v1.2.3"},
		{" v1.2.3 ", "v1.2.3"},
		{"dev", "dev"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := normalizeVersion(c.in); got != c.want {
				t.Fatalf("normalizeVersion(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// archiveNameForPlatform encodes the release.yml asset matrix. Lock it
// down so that adding a new GOOS/GOARCH later doesn't accidentally fall
// through to an empty name (which would surface as a confusing
// "no release archive published" error at runtime).
func TestArchiveNameForPlatform(t *testing.T) {
	name, isZip := archiveNameForPlatform()
	switch {
	case runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"):
		want := "sprout-linux-" + runtime.GOARCH + ".tar.gz"
		if name != want || isZip {
			t.Fatalf("linux/%s: got (%q, %v), want (%q, false)", runtime.GOARCH, name, isZip, want)
		}
	case runtime.GOOS == "darwin" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"):
		want := "sprout-darwin-" + runtime.GOARCH + ".tar.gz"
		if name != want || isZip {
			t.Fatalf("darwin/%s: got (%q, %v), want (%q, false)", runtime.GOARCH, name, isZip, want)
		}
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		if name != "sprout-windows-amd64.zip" || !isZip {
			t.Fatalf("windows/amd64: got (%q, %v), want (\"sprout-windows-amd64.zip\", true)", name, isZip)
		}
	default:
		// Unsupported runtime — just confirm we return empty (not a wrong asset).
		if name != "" {
			t.Fatalf("expected empty name for %s/%s, got %q", runtime.GOOS, runtime.GOARCH, name)
		}
	}
}

func TestFindChecksumLine(t *testing.T) {
	dir := t.TempDir()
	sums := filepath.Join(dir, "SHA256SUMS")
	content := strings.Join([]string{
		"abc123  sprout-linux-amd64.tar.gz",
		"def456 *sprout-windows-amd64.zip", // sha256sum binary-mode prefix
		"# a comment that should be skipped",
		"",
		"789ghi  sprout-darwin-arm64.tar.gz",
	}, "\n")
	if err := os.WriteFile(sums, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("found", func(t *testing.T) {
		got, err := findChecksumLine(sums, "sprout-linux-amd64.tar.gz")
		if err != nil || got != "abc123" {
			t.Fatalf("got (%q, %v), want (abc123, nil)", got, err)
		}
	})
	t.Run("binary-mode-prefix", func(t *testing.T) {
		got, err := findChecksumLine(sums, "sprout-windows-amd64.zip")
		if err != nil || got != "def456" {
			t.Fatalf("got (%q, %v), want (def456, nil)", got, err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		_, err := findChecksumLine(sums, "sprout-no-such-arch.tar.gz")
		if err == nil {
			t.Fatal("expected error for missing entry")
		}
	})
}

func TestSha256OfFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "blob")
	if err := os.WriteFile(p, []byte("hello sprout"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := sha256OfFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// printf 'hello sprout' | shasum -a 256
	want := "8163272d0b1f64d34826a82c4917e882fd63384373a785636309191abfd8f1a8"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
