package configuration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// loadConfigFromTempDir is a test helper that points the config layer at
// an isolated temp dir for the test lifetime, writes the given JSON as
// the initial config, then calls Load. Returns the loaded Config and
// the resolved on-disk path so the test can mutate it.
func loadConfigFromTempDir(t *testing.T, body string) (*Config, string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("SPROUT_CONFIG", tmp)

	configPath := filepath.Join(tmp, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(body), 0600); err != nil {
		t.Fatalf("seed config file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	return cfg, configPath
}

// TestConfigConflictError_Detected proves Save fails with the typed
// error when the on-disk file's mtime has changed since Load.
func TestConfigConflictError_Detected(t *testing.T) {
	cfg, path := loadConfigFromTempDir(t, `{"version":"2.0","last_used_provider":"openai"}`)

	// Simulate an external writer bumping the file. WriteFile keeps the
	// file open just long enough to change mtime; we then advance the
	// clock by adding a future mtime via os.Chtimes so the comparison is
	// stable on systems with low-resolution mtimes.
	future := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(path, []byte(`{"version":"2.0","last_used_provider":"anthropic"}`), 0600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	err := cfg.Save()
	if err == nil {
		t.Fatal("expected ConfigConflictError when file changed under us, got nil")
	}

	var ccErr *ConfigConflictError
	if !errors.As(err, &ccErr) {
		t.Fatalf("expected *ConfigConflictError, got %T: %v", err, err)
	}
	if !IsConfigConflict(err) {
		t.Error("IsConfigConflict should report true for the returned error")
	}
	if ccErr.Path != path {
		t.Errorf("ConfigConflictError.Path = %q, want %q", ccErr.Path, path)
	}
	if ccErr.LoadedSize == ccErr.CurrentSize && ccErr.LoadedModTime.Equal(ccErr.CurrentModTime) {
		t.Error("ConfigConflictError fields suggest no actual change — test setup wrong")
	}

	if !strings.Contains(ccErr.Error(), "config file changed on disk") {
		t.Errorf("error message should reference the cause, got: %s", ccErr.Error())
	}
}

// TestConfigConflictError_NoFalsePositiveOnSequentialSaves proves that
// after a successful Save, the in-memory snapshot is refreshed so the
// NEXT save doesn't trip the conflict check.
func TestConfigConflictError_NoFalsePositiveOnSequentialSaves(t *testing.T) {
	cfg, _ := loadConfigFromTempDir(t, `{"version":"2.0","last_used_provider":"openai"}`)

	if err := cfg.Save(); err != nil {
		t.Fatalf("first Save unexpectedly failed: %v", err)
	}
	// Sleep just enough for fs mtime resolution to advance — most
	// filesystems are ns-resolution but ext4 on some kernels is 1s.
	time.Sleep(10 * time.Millisecond)

	cfg.LastUsedProvider = "anthropic"
	if err := cfg.Save(); err != nil {
		t.Errorf("second Save (no external writer) should succeed, got: %v", err)
	}
}

// TestConfigConflictError_FreshConfigBypassesCheck proves that a
// Config constructed via NewConfig (never loaded from disk) can Save
// without tripping the conflict check — that path is intentional for
// "first ever save" or "reset to defaults" flows.
func TestConfigConflictError_FreshConfigBypassesCheck(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmp)
	t.Setenv("SPROUT_CONFIG", tmp)

	// Pre-populate the file so a load-then-save would see a conflict;
	// but we deliberately don't load — we construct from scratch.
	configPath := filepath.Join(tmp, ConfigFileName)
	if err := os.WriteFile(configPath, []byte(`{"version":"2.0"}`), 0600); err != nil {
		t.Fatalf("seed config file: %v", err)
	}

	cfg := NewConfig()
	if err := cfg.Save(); err != nil {
		t.Errorf("fresh-from-NewConfig save should bypass conflict check, got: %v", err)
	}
}

// TestIsConfigConflict_Nil ensures the convenience predicate handles
// nil errors without panicking.
func TestIsConfigConflict_Nil(t *testing.T) {
	if IsConfigConflict(nil) {
		t.Error("IsConfigConflict(nil) = true, want false")
	}
}
