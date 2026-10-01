//go:build windows

package filesystem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The scratch dir must be the directory Git Bash calls /tmp/sprout
// (%TEMP%\sprout), not C:\tmp\sprout, so file tools and shell commands agree.
func TestScratchDir_IsUnderOSTempOnWindows(t *testing.T) {
	want := filepath.Join(os.TempDir(), "sprout")
	if filepath.FromSlash(ScratchDir) != want {
		t.Fatalf("ScratchDir = %q, want %q", ScratchDir, want)
	}
	if strings.Contains(ScratchDir, `\`) {
		t.Fatalf("ScratchDir %q must use forward slashes so Git Bash accepts it unquoted", ScratchDir)
	}
	if !IsUnderTmpPath(filepath.Join(filepath.FromSlash(ScratchDir), "shot.png")) {
		t.Fatal("files in the scratch dir must be tmp-exempt")
	}
}

func TestLocalizeScratchDir_RewritesPromptPath(t *testing.T) {
	got := LocalizeScratchDir("use `/tmp/sprout/` for scratch")
	if strings.Contains(got, "/tmp/sprout") || !strings.Contains(got, ScratchDir+"/") {
		t.Fatalf("LocalizeScratchDir = %q", got)
	}
}
