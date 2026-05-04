package webui

import (
	"testing"
)

func TestNormalizeReleaseTagCandidate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.0.0", "v1.0.0"},
		{"v1.2.3", "v1.2.3"},
		{"1.0.0", ""},                // missing v prefix
		{"v1.0.0-0.beta1", ""},      // pre-release
		{"v1.0.0+dirty", ""},        // dirty
		{"v1.0.0 (devel)", ""},      // devel
		{"", ""},
		{"  v1.0.0  ", "v1.0.0"},    // whitespace trimmed
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
		_, err := fingerprintFile("/nonexistent/path")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("same file returns same fingerprint", func(t *testing.T) {
		// Use the package's own source file - it always exists
		f1, err1 := fingerprintFile("ssh_binary.go")
		f2, err2 := fingerprintFile("ssh_binary.go")
		if err1 != nil || err2 != nil {
			t.Skip("source file not accessible in test environment")
		}
		if f1 != f2 {
			t.Errorf("expected same fingerprint, got %q and %q", f1, f2)
		}
		if len(f1) != 16 {
			t.Errorf("expected 16-char fingerprint, got %d", len(f1))
		}
	})
}

func TestResolveGitHubReleaseAssetURLLatest(t *testing.T) {
	logger := &sshLaunchLogger{}
	url, err := resolveGitHubReleaseAssetURL("latest", "sprout-linux-amd64.tar.gz", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
}

func TestResolveGitHubReleaseAssetURLTagged(t *testing.T) {
	logger := &sshLaunchLogger{}
	url, err := resolveGitHubReleaseAssetURL("v1.0.0", "sprout-linux-amd64.tar.gz", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url == "" {
		t.Fatal("expected non-empty URL")
	}
}

func TestResolveGitHubReleaseAssetURLEmptyName(t *testing.T) {
	logger := &sshLaunchLogger{}
	_, err := resolveGitHubReleaseAssetURL("v1.0.0", "", logger)
	if err == nil {
		t.Fatal("expected error for empty asset name")
	}
}

func TestResolveGitHubReleaseAssetURLEmptyTag(t *testing.T) {
	logger := &sshLaunchLogger{}
	url, err := resolveGitHubReleaseAssetURL("", "sprout-linux-amd64.tar.gz", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty tag should resolve to "latest" URL
	if url == "" {
		t.Fatal("expected non-empty URL for empty tag (should use latest)")
	}
}
