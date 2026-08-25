package envutil

import (
	"path/filepath"
	"testing"
)

func TestConfigDir_HonorsEnvOverride(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_CONFIG_DIR", filepath.Join(base, "test-config"))
	t.Setenv("SPROUT_CONFIG", filepath.Join(base, "test-config-legacy"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "test-xdg"))
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}
	if dir != filepath.Join(base, "test-config") {
		t.Errorf("ConfigDir() = %q, want %q (SPROUT_CONFIG_DIR should win)", dir, filepath.Join(base, "test-config"))
	}
}

func TestConfigDir_LegacyAlias(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_CONFIG_DIR", "")
	t.Setenv("SPROUT_CONFIG", filepath.Join(base, "test-legacy"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "test-xdg"))
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}
	if dir != filepath.Join(base, "test-legacy") {
		t.Errorf("ConfigDir() = %q, want %q (SPROUT_CONFIG legacy alias)", dir, filepath.Join(base, "test-legacy"))
	}
}

func TestConfigDir_FallsBackToXDG(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_CONFIG_DIR", "")
	t.Setenv("SPROUT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "test-xdg"))
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}
	want := filepath.Join(filepath.Join(base, "test-xdg"), "sprout")
	if dir != want {
		t.Errorf("ConfigDir() = %q, want %q", dir, want)
	}
}

func TestConfigDir_FallsBackToHome(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_CONFIG_DIR", "")
	t.Setenv("SPROUT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	dir, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir() error: %v", err)
	}
	want := filepath.Join(filepath.Join(base, "test-home"), ".config", "sprout")
	if dir != want {
		t.Errorf("ConfigDir() = %q, want %q", dir, want)
	}
}

func TestStateDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", filepath.Join(base, "test-state"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "test-xdg-state"))
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	dir, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir() error: %v", err)
	}
	if dir != filepath.Join(base, "test-state") {
		t.Errorf("StateDir() = %q, want %q", dir, filepath.Join(base, "test-state"))
	}
}

func TestStateDir_XDG(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(base, "test-xdg-state"))
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	dir, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir() error: %v", err)
	}
	want := filepath.Join(filepath.Join(base, "test-xdg-state"), "sprout")
	if dir != want {
		t.Errorf("StateDir() = %q, want %q", dir, want)
	}
}

func TestStateDir_Home(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	dir, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir() error: %v", err)
	}
	want := filepath.Join(filepath.Join(base, "test-home"), ".local", "state", "sprout")
	if dir != want {
		t.Errorf("StateDir() = %q, want %q", dir, want)
	}
}

func TestDataDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error: %v", err)
	}
	want := filepath.Join(filepath.Join(base, "test-home"), ".local", "share", "sprout")
	if dir != want {
		t.Errorf("DataDir() = %q, want %q", dir, want)
	}
}

func TestCacheDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	dir, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir() error: %v", err)
	}
	want := filepath.Join(filepath.Join(base, "test-home"), ".cache", "sprout")
	if dir != want {
		t.Errorf("CacheDir() = %q, want %q", dir, want)
	}
}

func TestResolvers_NoPanicWithoutHome(t *testing.T) {
	// Unset all env vars so that os.UserHomeDir() is the only fallback.
	// We can't easily force os.UserHomeDir to fail in a unit test, but
	// we can at least verify the resolvers don't panic when HOME is unset.
	t.Setenv("SPROUT_CONFIG_DIR", "")
	t.Setenv("SPROUT_CONFIG", "")
	t.Setenv("SPROUT_STATE_DIR", "")
	t.Setenv("SPROUT_DATA_DIR", "")
	t.Setenv("SPROUT_CACHE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")

	// These may error (no HOME) but must not panic.
	_, _ = ConfigDir()
	_, _ = StateDir()
	_, _ = DataDir()
	_, _ = CacheDir()
}

func TestGetConfigDir_BackwardCompat(t *testing.T) {
	base := t.TempDir()
	t.Setenv("SPROUT_CONFIG", filepath.Join(base, "compat-test"))
	t.Setenv("SPROUT_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", filepath.Join(base, "test-home"))

	// GetConfigDir (deprecated) must return the same as ConfigDir
	dir1, err1 := GetConfigDir()
	dir2, err2 := ConfigDir()
	if err1 != nil || err2 != nil {
		t.Fatalf("errors: %v %v", err1, err2)
	}
	if dir1 != dir2 {
		t.Errorf("GetConfigDir() = %q != ConfigDir() = %q", dir1, dir2)
	}
}
