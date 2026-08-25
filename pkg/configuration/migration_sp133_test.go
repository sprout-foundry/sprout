package configuration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

func TestNeedsMigration_NoLegacyDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_STATE_DIR", filepath.Join(home, ".local", "state", "sprout"))

	if NeedsMigration() {
		t.Error("NeedsMigration should be false when no legacy dir exists")
	}
}

func TestNeedsMigration_LegacyDirExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_STATE_DIR", filepath.Join(home, ".local", "state", "sprout"))

	// Create legacy ~/.sprout
	legacy := filepath.Join(home, ".sprout")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}

	if !NeedsMigration() {
		t.Error("NeedsMigration should be true when legacy dir exists")
	}
}

func TestNeedsMigration_AfterMigration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stateDir := filepath.Join(home, ".local", "state", "sprout")
	t.Setenv("SPROUT_STATE_DIR", stateDir)

	// Create legacy dir
	legacy := filepath.Join(home, ".sprout")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	// Create marker
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, migrationMarker), []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if NeedsMigration() {
		t.Error("NeedsMigration should be false after migration marker exists")
	}
}

func TestRunMigration_MovesStateFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_STATE_DIR", filepath.Join(home, ".local", "state", "sprout"))
	t.Setenv("SPROUT_CACHE_DIR", filepath.Join(home, ".cache", "sprout"))
	t.Setenv("SPROUT_CONFIG_DIR", filepath.Join(home, ".config", "sprout"))
	t.Setenv("SPROUT_DATA_DIR", filepath.Join(home, ".local", "share", "sprout"))

	// Create legacy dir with a state file
	legacy := filepath.Join(home, ".sprout")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "recent_workspaces.json"), []byte(`{"workspaces":[]}`), 0600); err != nil {
		t.Fatal(err)
	}

	if err := RunMigration(); err != nil {
		t.Fatalf("RunMigration: %v", err)
	}

	// File should be in state dir
	stateDir, _ := envutil.StateDir()
	moved, err := os.ReadFile(filepath.Join(stateDir, "recent_workspaces.json"))
	if err != nil {
		t.Fatalf("file not in state dir: %v", err)
	}
	if string(moved) != `{"workspaces":[]}` {
		t.Errorf("unexpected content: %q", moved)
	}

	// Source should be gone
	if _, err := os.Stat(filepath.Join(legacy, "recent_workspaces.json")); !os.IsNotExist(err) {
		t.Error("source file should be removed")
	}

	// Marker should exist
	if _, err := os.Stat(filepath.Join(stateDir, migrationMarker)); err != nil {
		t.Error("migration marker should exist")
	}
}

func TestRunMigration_MovesStateDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_STATE_DIR", filepath.Join(home, ".local", "state", "sprout"))
	t.Setenv("SPROUT_CACHE_DIR", filepath.Join(home, ".cache", "sprout"))
	t.Setenv("SPROUT_CONFIG_DIR", filepath.Join(home, ".config", "sprout"))
	t.Setenv("SPROUT_DATA_DIR", filepath.Join(home, ".local", "share", "sprout"))

	legacy := filepath.Join(home, ".sprout")
	sessionsDir := filepath.Join(legacy, "sessions")
	if err := os.MkdirAll(sessionsDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "session1.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := RunMigration(); err != nil {
		t.Fatalf("RunMigration: %v", err)
	}

	stateDir, _ := envutil.StateDir()
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", "session1.json")); err != nil {
		t.Errorf("session file not in state dir: %v", err)
	}
}

func TestRunMigration_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_STATE_DIR", filepath.Join(home, ".local", "state", "sprout"))
	t.Setenv("SPROUT_CACHE_DIR", filepath.Join(home, ".cache", "sprout"))
	t.Setenv("SPROUT_CONFIG_DIR", filepath.Join(home, ".config", "sprout"))
	t.Setenv("SPROUT_DATA_DIR", filepath.Join(home, ".local", "share", "sprout"))

	// Create legacy dir
	legacy := filepath.Join(home, ".sprout")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "recent_workspaces.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	// Run twice
	if err := RunMigration(); err != nil {
		t.Fatalf("first RunMigration: %v", err)
	}
	if err := RunMigration(); err != nil {
		t.Fatalf("second RunMigration: %v", err)
	}

	// File should still be in state dir
	stateDir, _ := envutil.StateDir()
	if _, err := os.Stat(filepath.Join(stateDir, "recent_workspaces.json")); err != nil {
		t.Error("file should exist in state dir after second run")
	}
}

func TestRunMigration_MovesCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_STATE_DIR", filepath.Join(home, ".local", "state", "sprout"))
	t.Setenv("SPROUT_CACHE_DIR", filepath.Join(home, ".cache", "sprout"))
	t.Setenv("SPROUT_CONFIG_DIR", filepath.Join(home, ".config", "sprout"))
	t.Setenv("SPROUT_DATA_DIR", filepath.Join(home, ".local", "share", "sprout"))

	legacy := filepath.Join(home, ".sprout")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "api_keys.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := RunMigration(); err != nil {
		t.Fatalf("RunMigration: %v", err)
	}

	// Credential should be in config/credentials/
	configDir, _ := envutil.ConfigDir()
	credPath := filepath.Join(configDir, "credentials", "api_keys.json")
	if _, err := os.Stat(credPath); err != nil {
		t.Errorf("credential file not in config/credentials/: %v", err)
	}
}
