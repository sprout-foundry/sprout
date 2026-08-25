//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeReleaseTagCandidate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.0.0", "v1.0.0"},
		{"v1.2.3", "v1.2.3"},
		{"1.0.0", ""},          // missing v prefix
		{"v1.0.0-0.beta1", ""}, // pre-release
		{"v1.0.0+dirty", ""},   // dirty
		{"v1.0.0 (devel)", ""}, // devel
		{"", ""},
		{"  v1.0.0  ", "v1.0.0"}, // whitespace trimmed
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeReleaseTagCandidate(tt.input)
			if got != tt.want {
				t.Errorf("normalizeReleaseTagCandidate(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFingerprintFile(t *testing.T) {
	t.Run("nonexistent file returns error", func(t *testing.T) {
		_, err := fingerprintFile("/nonexistent/path/definitely_not_here.txt")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("same file returns same fingerprint", func(t *testing.T) {
		// Create a known file in a temp directory so the test is independent of CWD.
		dir := t.TempDir()
		fp := filepath.Join(dir, "testfile.bin")
		content := []byte("hello fingerprint")
		if err := os.WriteFile(fp, content, 0o644); err != nil {
			t.Fatal(err)
		}

		f1, err1 := fingerprintFile(fp)
		f2, err2 := fingerprintFile(fp)
		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected errors: %v, %v", err1, err2)
		}
		if f1 != f2 {
			t.Errorf("expected same fingerprint, got %q and %q", f1, f2)
		}
		if len(f1) != 16 {
			t.Errorf("expected 16-char fingerprint, got %d chars: %q", len(f1), f1)
		}
	})

	t.Run("different files return different fingerprints", func(t *testing.T) {
		dir := t.TempDir()
		p1 := filepath.Join(dir, "a.txt")
		p2 := filepath.Join(dir, "b.txt")
		os.WriteFile(p1, []byte("content A"), 0o644)
		os.WriteFile(p2, []byte("content B"), 0o644)

		f1, err1 := fingerprintFile(p1)
		f2, err2 := fingerprintFile(p2)
		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected errors: %v, %v", err1, err2)
		}
		if f1 == f2 {
			t.Errorf("expected different fingerprints for different files, got same: %q", f1)
		}
	})
}

func TestResolveGitHubReleaseAssetURL(t *testing.T) {
	t.Run("empty asset name returns error", func(t *testing.T) {
		logger := &sshLaunchLogger{}
		_, err := resolveGitHubReleaseAssetURL("v1.0.0", "", logger)
		if err == nil {
			t.Fatal("expected error for empty asset name")
		}
		if !strings.Contains(err.Error(), "artifact name") {
			t.Errorf("error should mention 'artifact name', got: %v", err)
		}
	})

	t.Run("whitespace-only asset name returns error", func(t *testing.T) {
		logger := &sshLaunchLogger{}
		_, err := resolveGitHubReleaseAssetURL("v1.0.0", "   ", logger)
		if err == nil {
			t.Fatal("expected error for whitespace-only asset name")
		}
	})

	t.Run("empty tag resolves to latest URL", func(t *testing.T) {
		logger := &sshLaunchLogger{}
		url, err := resolveGitHubReleaseAssetURL("", "sprout-linux-amd64.tar.gz", logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(url, "/releases/latest/download/") {
			t.Errorf("expected latest URL pattern, got: %s", url)
		}
		if !strings.Contains(url, "sprout-linux-amd64.tar.gz") {
			t.Errorf("expected asset name in URL, got: %s", url)
		}
	})

	t.Run("latest tag resolves to latest URL", func(t *testing.T) {
		logger := &sshLaunchLogger{}
		url, err := resolveGitHubReleaseAssetURL("latest", "sprout-linux-amd64.tar.gz", logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(url, "/releases/latest/download/") {
			t.Errorf("expected latest URL pattern, got: %s", url)
		}
	})

	t.Run("tagged version resolves to tagged URL", func(t *testing.T) {
		logger := &sshLaunchLogger{}
		url, err := resolveGitHubReleaseAssetURL("v1.2.3", "sprout-linux-amd64.tar.gz", logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(url, "/releases/download/v1.2.3/") {
			t.Errorf("expected tagged URL pattern, got: %s", url)
		}
		if !strings.Contains(url, "sprout-linux-amd64.tar.gz") {
			t.Errorf("expected asset name in URL, got: %s", url)
		}
	})

	t.Run("tag with whitespace is trimmed", func(t *testing.T) {
		logger := &sshLaunchLogger{}
		url, err := resolveGitHubReleaseAssetURL("  v2.0.0  ", "asset.tar.gz", logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(url, "/releases/download/v2.0.0/") {
			t.Errorf("expected trimmed tag in URL, got: %s", url)
		}
	})

	t.Run("URL contains github.com", func(t *testing.T) {
		logger := &sshLaunchLogger{}
		url, err := resolveGitHubReleaseAssetURL("v1.0.0", "asset.tar.gz", logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(url, "https://github.com/") {
			t.Errorf("expected https://github.com/ prefix, got: %s", url)
		}
	})
}
