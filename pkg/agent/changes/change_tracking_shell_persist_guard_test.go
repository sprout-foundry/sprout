package changes

import (
	"path/filepath"
	"testing"
)

func TestShellSkipDirsFilePath_RefusesRealConfigInTests(t *testing.T) {
	t.Setenv("SPROUT_CONFIG_DIR", "")
	t.Setenv("SPROUT_CONFIG", "")
	if p, err := shellSkipDirsFilePath(); err == nil {
		t.Fatalf("tests without an isolated config dir must not get the real skip file, got %s", p)
	}

	dir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", dir)
	p, err := shellSkipDirsFilePath()
	if err != nil || filepath.Dir(p) != dir {
		t.Fatalf("isolated config dir not used: %s, %v", p, err)
	}
}
