package cmd

import (
	"path/filepath"
	"testing"
)

func TestResolveGlobalConfigDir(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	explicit := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_CONFIG", filepath.Join(t.TempDir(), ".sprout"))
	t.Setenv("SPROUT_CONFIG_DIR", "")

	t.Setenv("XDG_CONFIG_HOME", "")
	if got, want := resolveGlobalConfigDir(), filepath.Join(home, ".config", "sprout"); got != want {
		t.Fatalf("default = %q, want %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got, want := resolveGlobalConfigDir(), filepath.Join(xdg, "sprout"); got != want {
		t.Fatalf("with XDG_CONFIG_HOME = %q, want %q", got, want)
	}

	t.Setenv("SPROUT_CONFIG_DIR", explicit)
	if got := resolveGlobalConfigDir(); got != explicit {
		t.Fatalf("with SPROUT_CONFIG_DIR = %q, want %q", got, explicit)
	}
}
