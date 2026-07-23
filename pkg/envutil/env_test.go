// Note: Tests in this file must not call t.Parallel() because
// tests set environment variables, which are a process-global
// resource, so parallel execution would cause cross-test interference.
package envutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func unsetEnv(t *testing.T, suffix string) {
	t.Helper()
	sproutKey := "SPROUT_" + suffix
	origSprout, hadSprout := os.LookupEnv(sproutKey)
	os.Unsetenv(sproutKey)
	t.Cleanup(func() {
		if hadSprout {
			os.Setenv(sproutKey, origSprout)
		} else {
			os.Unsetenv(sproutKey)
		}
	})
}

func TestGetEnvSimple_SproutSet(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", "/sprout/config")
	result := GetEnvSimple("CONFIG")
	if result != "/sprout/config" {
		t.Errorf("expected /sprout/config, got %s", result)
	}
}

func TestGetEnvSimple_NotSet(t *testing.T) {
	unsetEnv(t, "CONFIG")
	result := GetEnvSimple("CONFIG")
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

func TestSetEnv(t *testing.T) {
	unsetEnv(t, "TEST_VAR")
	err := SetEnv("TEST_VAR", "test_value")
	if err != nil {
		t.Fatalf("SetEnv failed: %v", err)
	}
	if v := os.Getenv("SPROUT_TEST_VAR"); v != "test_value" {
		t.Errorf("expected SPROUT_TEST_VAR=test_value, got %s", v)
	}
}

func TestLookupEnv_SproutSet(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", "/sprout/config")
	result, found := LookupEnv("CONFIG")
	if !found {
		t.Error("expected found=true")
	}
	if result != "/sprout/config" {
		t.Errorf("expected /sprout/config, got %s", result)
	}
}

func TestLookupEnv_NotSet(t *testing.T) {
	unsetEnv(t, "NONEXISTENT_LOOKUP")
	result, found := LookupEnv("NONEXISTENT_LOOKUP")
	if found {
		t.Error("expected found=false")
	}
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

func TestUnsetEnv(t *testing.T) {
	t.Setenv("SPROUT_TEST_VAR", "value1")
	UnsetEnv("TEST_VAR")
	if v := os.Getenv("SPROUT_TEST_VAR"); v != "" {
		t.Errorf("expected SPROUT_TEST_VAR unset, got %s", v)
	}
}

func TestHasPrefix_Sprout(t *testing.T) {
	if !HasPrefix("SPROUT_FOO") {
		t.Error("expected HasPrefix(SPROUT_FOO) = true")
	}
}

func TestHasPrefix_Other(t *testing.T) {
	if HasPrefix("OTHER_FOO") {
		t.Error("expected HasPrefix(OTHER_FOO) = false")
	}
}

func TestHasPrefix_Empty(t *testing.T) {
	if HasPrefix("") {
		t.Error("expected HasPrefix('') = false")
	}
}

func TestGetConfigDir_SproutConfigSet(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}
	if configDir != tmpDir {
		t.Errorf("expected %s, got %s", tmpDir, configDir)
	}
}

func TestGetConfigDir_XdgConfigHomeSet(t *testing.T) {
	tmpDir := t.TempDir()
	unsetEnv(t, "CONFIG")
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}
	expectedDir := filepath.Join(tmpDir, "sprout")
	if configDir != expectedDir {
		t.Errorf("expected %s, got %s", expectedDir, configDir)
	}
}

func TestGetConfigDir_HomeSet(t *testing.T) {
	tmpDir := t.TempDir()
	unsetEnv(t, "CONFIG")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", tmpDir)
	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}
	expectedDir := filepath.Join(tmpDir, ".config", "sprout")
	if configDir != expectedDir {
		t.Errorf("expected %s, got %s", expectedDir, configDir)
	}
}

func TestGetConfigDir_FallbackToUserHomeDir(t *testing.T) {
	unsetEnv(t, "CONFIG")
	origHome, hadHome := os.LookupEnv("HOME")
	origXDG, hadXDG := os.LookupEnv("XDG_CONFIG_HOME")
	os.Unsetenv("HOME")
	os.Unsetenv("XDG_CONFIG_HOME")
	t.Cleanup(func() {
		if hadHome {
			os.Setenv("HOME", origHome)
		}
		if hadXDG {
			os.Setenv("XDG_CONFIG_HOME", origXDG)
		}
	})
	configDir, err := GetConfigDir()
	if err != nil {
		t.Skipf("os.UserHomeDir() not available: %v", err)
	}
	if !strings.Contains(configDir, ".config") {
		t.Errorf("expected path to contain .config, got %s", configDir)
	}
}

func TestGetConfigDir_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "nonexistent", "nested", "sprout")
	t.Setenv("SPROUT_CONFIG", nonExistentPath)
	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}
	if configDir != nonExistentPath {
		t.Errorf("expected %s, got %s", nonExistentPath, configDir)
	}
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("config directory was not created: %s", configDir)
	}
}

func TestGetConfigDir_WhitespaceTrimmed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", "  "+tmpDir+"  ")
	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}
	if configDir != tmpDir {
		t.Errorf("expected %s (trimmed), got %s", tmpDir, configDir)
	}
}

func TestGetConfigDir_MkdirAllError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping: permission-based test requires non-root user")
	}
	if runtime.GOOS == "darwin" {
		t.Skip("skipping: permission-based test unreliable on macOS")
	}
	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(readOnlyDir, 0700); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	if err := os.Chmod(readOnlyDir, 0o444); err != nil {
		t.Fatalf("failed to chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnlyDir, 0700) })
	targetDir := filepath.Join(readOnlyDir, "child", "sprout")
	unsetEnv(t, "CONFIG")
	t.Setenv("SPROUT_CONFIG", targetDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	_, err := GetConfigDir()
	if err == nil {
		t.Fatal("expected error from MkdirAll failure")
	}
	if !strings.Contains(err.Error(), "failed to create config directory") {
		t.Errorf("unexpected error: %v", err)
	}
}
