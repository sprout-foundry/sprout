// Note: Tests in this file must not call t.Parallel() because
// resetDeprecatedVars() replaces the package-level deprecatedVars
// sync.Map, which is not safe for concurrent access.
package envutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// resetDeprecatedVars clears the package-level deprecation tracking state.
func resetDeprecatedVars(t *testing.T) {
	t.Helper()
	deprecatedVars = sync.Map{}
}

// unsetEnv cleans up both SPROUT_ and LEDIT_ prefixed env vars for a suffix,
// restoring their original values after the test.
func unsetEnv(t *testing.T, suffix string) {
	t.Helper()
	sproutKey := "SPROUT_" + suffix
	leditKey := "LEDIT_" + suffix
	origSprout, hadSprout := os.LookupEnv(sproutKey)
	origLedit, hadLedit := os.LookupEnv(leditKey)
	os.Unsetenv(sproutKey)
	os.Unsetenv(leditKey)
	t.Cleanup(func() {
		if hadSprout {
			os.Setenv(sproutKey, origSprout)
		} else {
			os.Unsetenv(sproutKey)
		}
		if hadLedit {
			os.Setenv(leditKey, origLedit)
		} else {
			os.Unsetenv(leditKey)
		}
	})
}

// TestGetEnv_PrimaryKeySet verifies that when the primary key is set,
// it returns that value without triggering deprecation tracking.
func TestGetEnv_PrimaryKeySet(t *testing.T) {
	resetDeprecatedVars(t)

	t.Setenv("SPROUT_CONFIG", "/primary/config")
	t.Setenv("LEDIT_CONFIG", "/legacy/config")

	result := GetEnv("SPROUT_CONFIG", "LEDIT_CONFIG")

	if result != "/primary/config" {
		t.Errorf("expected /primary/config, got %s", result)
	}

	// Verify deprecation was NOT recorded when primary key is set
	_, loaded := deprecatedVars.Load("LEDIT_CONFIG")
	if loaded {
		t.Error("deprecation should NOT be recorded when primary key is set")
	}
}

// TestGetEnv_OnlyLegacySet verifies that when only the legacy key is set,
// it returns the legacy value and records the deprecation.
func TestGetEnv_OnlyLegacySet(t *testing.T) {
	resetDeprecatedVars(t)

	// Ensure primary is NOT set
	unsetEnv(t, "CONFIG")
	t.Setenv("LEDIT_CONFIG", "/legacy/config")

	result := GetEnv("SPROUT_CONFIG", "LEDIT_CONFIG")

	if result != "/legacy/config" {
		t.Errorf("expected /legacy/config, got %s", result)
	}

	// Verify deprecation was tracked
	_, loaded := deprecatedVars.Load("LEDIT_CONFIG")
	if !loaded {
		t.Error("deprecation warning should have been recorded")
	}
}

// TestGetEnv_LegacyDeprecationPrintedOnce verifies that the deprecation
// warning is only printed once, not on repeated calls.
func TestGetEnv_LegacyDeprecationPrintedOnce(t *testing.T) {
	resetDeprecatedVars(t)

	unsetEnv(t, "UNIQUE_KEY")
	t.Setenv("LEDIT_UNIQUE_KEY", "/legacy/config")

	// Verify deprecation is NOT recorded yet
	_, loadedBefore := deprecatedVars.Load("LEDIT_UNIQUE_KEY")
	if loadedBefore {
		t.Error("deprecation should not be recorded before first call")
	}

	// First call - deprecation warning should be printed
	result1 := GetEnv("SPROUT_UNIQUE_KEY", "LEDIT_UNIQUE_KEY")
	if result1 != "/legacy/config" {
		t.Errorf("expected /legacy/config, got %s", result1)
	}

	// After first call, deprecation should now be recorded
	_, loadedAfterFirst := deprecatedVars.Load("LEDIT_UNIQUE_KEY")
	if !loadedAfterFirst {
		t.Error("deprecation should be recorded after first call")
	}

	// Second call - deprecation warning should NOT be printed again
	result2 := GetEnv("SPROUT_UNIQUE_KEY", "LEDIT_UNIQUE_KEY")
	if result2 != "/legacy/config" {
		t.Errorf("expected /legacy/config, got %s", result2)
	}

	// Still recorded (not a new entry)
	_, loadedAfterSecond := deprecatedVars.Load("LEDIT_UNIQUE_KEY")
	if !loadedAfterSecond {
		t.Error("deprecation should still be recorded after second call")
	}
}

// TestGetEnv_NeitherSet verifies that when neither key is set,
// it returns an empty string.
func TestGetEnv_NeitherSet(t *testing.T) {
	resetDeprecatedVars(t)

	unsetEnv(t, "CONFIG")

	result := GetEnv("SPROUT_CONFIG", "LEDIT_CONFIG")

	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

// TestGetEnv_BothSet verifies that when both are set,
// the primary key takes precedence.
func TestGetEnv_BothSet(t *testing.T) {
	resetDeprecatedVars(t)

	t.Setenv("SPROUT_CONFIG", "/primary/config")
	t.Setenv("LEDIT_CONFIG", "/legacy/config")

	result := GetEnv("SPROUT_CONFIG", "LEDIT_CONFIG")

	if result != "/primary/config" {
		t.Errorf("expected /primary/config (primary should win), got %s", result)
	}
}

// TestGetEnvSimple_SproutSuffixSet verifies that GetEnvSimple returns
// the SPROUT_ value when it's set.
func TestGetEnvSimple_SproutSuffixSet(t *testing.T) {
	resetDeprecatedVars(t)

	t.Setenv("SPROUT_CONFIG", "/sprout/config")
	t.Setenv("LEDIT_CONFIG", "/edit/config")

	result := GetEnvSimple("CONFIG")

	if result != "/sprout/config" {
		t.Errorf("expected /sprout/config, got %s", result)
	}
}

// TestGetEnvSimple_OnlyLeditSet verifies that GetEnvSimple returns
// the LEDIT_ value when only that's set.
func TestGetEnvSimple_OnlyLeditSet(t *testing.T) {
	resetDeprecatedVars(t)

	unsetEnv(t, "CONFIG")
	t.Setenv("LEDIT_CONFIG", "/edit/config")

	result := GetEnvSimple("CONFIG")

	if result != "/edit/config" {
		t.Errorf("expected /edit/config, got %s", result)
	}
}

// TestGetEnvSimple_NeitherSet verifies that GetEnvSimple returns
// empty string when neither is set.
func TestGetEnvSimple_NeitherSet(t *testing.T) {
	resetDeprecatedVars(t)

	unsetEnv(t, "CONFIG")

	result := GetEnvSimple("CONFIG")

	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

// TestGetEnvSimple_BothSet verifies that GetEnvSimple returns
// the SPROUT_ value when both are set.
func TestGetEnvSimple_BothSet(t *testing.T) {
	resetDeprecatedVars(t)

	t.Setenv("SPROUT_CONFIG", "/sprout/config")
	t.Setenv("LEDIT_CONFIG", "/edit/config")

	result := GetEnvSimple("CONFIG")

	if result != "/sprout/config" {
		t.Errorf("expected /sprout/config (SPROUT_ should win), got %s", result)
	}
}

// TestSetEnv_SetsBoth verifies that SetEnv sets both SPROUT_ and LEDIT_
// variants of the environment variable.
func TestSetEnv_SetsBoth(t *testing.T) {
	resetDeprecatedVars(t)

	unsetEnv(t, "TEST_VAR")

	err := SetEnv("TEST_VAR", "test_value")
	if err != nil {
		t.Fatalf("SetEnv failed: %v", err)
	}

	sproutValue := os.Getenv("SPROUT_TEST_VAR")
	leditValue := os.Getenv("LEDIT_TEST_VAR")

	if sproutValue != "test_value" {
		t.Errorf("expected SPROUT_TEST_VAR=test_value, got %s", sproutValue)
	}
	if leditValue != "test_value" {
		t.Errorf("expected LEDIT_TEST_VAR=test_value, got %s", leditValue)
	}
}

// TestLookupEnv_SproutSet verifies that LookupEnv returns the SPROUT_
// value when it's set.
func TestLookupEnv_SproutSet(t *testing.T) {
	resetDeprecatedVars(t)

	t.Setenv("SPROUT_CONFIG", "/sprout/config")
	t.Setenv("LEDIT_CONFIG", "/edit/config")

	result, found := LookupEnv("CONFIG")

	if !found {
		t.Error("expected found=true, got false")
	}
	if result != "/sprout/config" {
		t.Errorf("expected /sprout/config, got %s", result)
	}
}

// TestLookupEnv_LeditSet verifies that LookupEnv returns the LEDIT_
// value when only that's set.
func TestLookupEnv_LeditSet(t *testing.T) {
	resetDeprecatedVars(t)

	unsetEnv(t, "CONFIG")
	t.Setenv("LEDIT_CONFIG", "/edit/config")

	result, found := LookupEnv("CONFIG")

	if !found {
		t.Error("expected found=true, got false")
	}
	if result != "/edit/config" {
		t.Errorf("expected /edit/config, got %s", result)
	}
}

// TestLookupEnv_NeitherSet verifies that LookupEnv returns ("", false)
// when neither is set.
func TestLookupEnv_NeitherSet(t *testing.T) {
	resetDeprecatedVars(t)

	unsetEnv(t, "CONFIG")

	result, found := LookupEnv("CONFIG")

	if found {
		t.Error("expected found=false, got true")
	}
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

// TestLookupEnv_BothSet verifies that LookupEnv returns the SPROUT_
// value when both are set (SPROUT_ takes precedence).
func TestLookupEnv_BothSet(t *testing.T) {
	resetDeprecatedVars(t)

	t.Setenv("SPROUT_CONFIG", "/sprout/config")
	t.Setenv("LEDIT_CONFIG", "/edit/config")

	result, found := LookupEnv("CONFIG")

	if !found {
		t.Error("expected found=true, got false")
	}
	if result != "/sprout/config" {
		t.Errorf("expected /sprout/config (SPROUT_ should win), got %s", result)
	}
}

// TestUnsetEnv_RemovesBoth verifies that UnsetEnv removes both SPROUT_
// and LEDIT_ variants.
func TestUnsetEnv_RemovesBoth(t *testing.T) {
	resetDeprecatedVars(t)

	// Set both vars
	t.Setenv("SPROUT_TEST_VAR", "value1")
	t.Setenv("LEDIT_TEST_VAR", "value2")

	// Verify they're set
	if os.Getenv("SPROUT_TEST_VAR") != "value1" {
		t.Fatal("SPROUT_TEST_VAR not set properly before test")
	}
	if os.Getenv("LEDIT_TEST_VAR") != "value2" {
		t.Fatal("LEDIT_TEST_VAR not set properly before test")
	}

	// Unset both
	UnsetEnv("TEST_VAR")

	// Verify both are unset
	sproutValue := os.Getenv("SPROUT_TEST_VAR")
	leditValue := os.Getenv("LEDIT_TEST_VAR")

	if sproutValue != "" {
		t.Errorf("expected SPROUT_TEST_VAR to be unset, got %s", sproutValue)
	}
	if leditValue != "" {
		t.Errorf("expected LEDIT_TEST_VAR to be unset, got %s", leditValue)
	}
}

// TestUnsetEnv_AlreadyUnset verifies that calling UnsetEnv on
// already-unset vars doesn't cause any issues.
func TestUnsetEnv_AlreadyUnset(t *testing.T) {
	resetDeprecatedVars(t)

	unsetEnv(t, "TEST_VAR")

	// This should not panic or error
	UnsetEnv("TEST_VAR")

	sproutValue := os.Getenv("SPROUT_TEST_VAR")
	leditValue := os.Getenv("LEDIT_TEST_VAR")

	if sproutValue != "" {
		t.Errorf("expected empty string, got %s", sproutValue)
	}
	if leditValue != "" {
		t.Errorf("expected empty string, got %s", leditValue)
	}
}

// TestHasPrefix_SproutPrefix verifies that HasPrefix returns true
// for SPROUT_ prefixed names.
func TestHasPrefix_SproutPrefix(t *testing.T) {
	if !HasPrefix("SPROUT_FOO") {
		t.Error("expected HasPrefix(SPROUT_FOO) = true")
	}
}

// TestHasPrefix_LeditPrefix verifies that HasPrefix returns true
// for LEDIT_ prefixed names.
func TestHasPrefix_LeditPrefix(t *testing.T) {
	if !HasPrefix("LEDIT_FOO") {
		t.Error("expected HasPrefix(LEDIT_FOO) = true")
	}
}

// TestHasPrefix_OtherPrefix verifies that HasPrefix returns false
// for names with other prefixes.
func TestHasPrefix_OtherPrefix(t *testing.T) {
	if HasPrefix("OTHER_FOO") {
		t.Error("expected HasPrefix(OTHER_FOO) = false")
	}
}

// TestHasPrefix_EmptyString verifies that HasPrefix returns false
// for empty strings.
func TestHasPrefix_EmptyString(t *testing.T) {
	if HasPrefix("") {
		t.Error("expected HasPrefix('') = false")
	}
}

// TestHasPrefix_Lowercase verifies that HasPrefix is case-sensitive
// and returns false for lowercase prefixes.
func TestHasPrefix_Lowercase(t *testing.T) {
	if HasPrefix("sprout_foo") {
		t.Error("expected HasPrefix(sprout_foo) = false (case sensitive)")
	}
	if HasPrefix("ledit_foo") {
		t.Error("expected HasPrefix(ledit_foo) = false (case sensitive)")
	}
}

// TestSproutKey_LeditConfig verifies that SproutKey converts LEDIT_
// prefix to SPROUT_.
func TestSproutKey_LeditConfig(t *testing.T) {
	result := SproutKey("LEDIT_CONFIG")
	if result != "SPROUT_CONFIG" {
		t.Errorf("expected SPROUT_CONFIG, got %s", result)
	}
}

// TestSproutKey_LeditPrefixOnly verifies that SproutKey handles
// the prefix by itself.
func TestSproutKey_LeditPrefixOnly(t *testing.T) {
	result := SproutKey("LEDIT_")
	if result != "SPROUT_" {
		t.Errorf("expected SPROUT_, got %s", result)
	}
}

// TestSproutKey_OtherPrefix verifies that SproutKey leaves names
// without LEDIT_ prefix unchanged.
func TestSproutKey_OtherPrefix(t *testing.T) {
	result := SproutKey("OTHER_CONFIG")
	if result != "OTHER_CONFIG" {
		t.Errorf("expected OTHER_CONFIG (unchanged), got %s", result)
	}
}

// TestSproutKey_EmptyString verifies that SproutKey handles
// empty strings.
func TestSproutKey_EmptyString(t *testing.T) {
	result := SproutKey("")
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

// TestGetConfigDir_SproutConfigSet verifies that GetConfigDir uses
// SPROUT_CONFIG directly when set.
func TestGetConfigDir_SproutConfigSet(t *testing.T) {
	resetDeprecatedVars(t)

	tmpDir := t.TempDir()

	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CONFIG", "")

	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	if configDir != tmpDir {
		t.Errorf("expected %s, got %s", tmpDir, configDir)
	}
}

// TestGetConfigDir_LeditConfigSet verifies that GetConfigDir uses
// LEDIT_CONFIG when SPROUT_CONFIG is not set.
func TestGetConfigDir_LeditConfigSet(t *testing.T) {
	resetDeprecatedVars(t)

	tmpDir := t.TempDir()

	unsetEnv(t, "CONFIG")
	t.Setenv("LEDIT_CONFIG", tmpDir)

	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	if configDir != tmpDir {
		t.Errorf("expected %s, got %s", tmpDir, configDir)
	}
}

// TestGetConfigDir_XdgConfigHomeSet verifies that GetConfigDir uses
// XDG_CONFIG_HOME/sprout when neither SPROUT_CONFIG nor LEDIT_CONFIG
// is set, but XDG_CONFIG_HOME is.
func TestGetConfigDir_XdgConfigHomeSet(t *testing.T) {
	resetDeprecatedVars(t)

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

	// Verify directory was created
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("config directory was not created: %s", configDir)
	}
}

// TestGetConfigDir_HomeSet verifies that GetConfigDir uses
// HOME/.config/sprout when SPROUT_CONFIG, LEDIT_CONFIG, and
// XDG_CONFIG_HOME are not set, but HOME is.
func TestGetConfigDir_HomeSet(t *testing.T) {
	resetDeprecatedVars(t)

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

	// Verify directory was created
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("config directory was not created: %s", configDir)
	}
}

// TestGetConfigDir_FallbackToUserHomeDir verifies that GetConfigDir
// falls back to os.UserHomeDir()/.config/sprout when none of the
// environment variables are set.
func TestGetConfigDir_FallbackToUserHomeDir(t *testing.T) {
	resetDeprecatedVars(t)

	unsetEnv(t, "CONFIG")

	// Save HOME so we can unset it for the fallback test but restore afterward.
	origHome, hadHome := os.LookupEnv("HOME")
	origXDG, hadXDG := os.LookupEnv("XDG_CONFIG_HOME")
	os.Unsetenv("HOME")
	os.Unsetenv("XDG_CONFIG_HOME")
	t.Cleanup(func() {
		if hadHome {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
		if hadXDG {
			os.Setenv("XDG_CONFIG_HOME", origXDG)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	})

	configDir, err := GetConfigDir()
	if err != nil {
		// os.UserHomeDir() may fail when HOME is unset; skip in that case.
		t.Skipf("os.UserHomeDir() not available in test environment: %v", err)
	}

	if !strings.Contains(configDir, ".config"+string(filepath.Separator)+"sprout") &&
		!strings.Contains(configDir, ".config/sprout") {
		t.Errorf("expected path to contain .config/sprout, got %s", configDir)
	}

	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("config directory was not created: %s", configDir)
	}
}

// TestGetConfigDir_CreatesDirectory verifies that GetConfigDir
// creates the directory if it doesn't exist.
func TestGetConfigDir_CreatesDirectory(t *testing.T) {
	resetDeprecatedVars(t)

	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "nonexistent", "nested", "sprout")

	t.Setenv("SPROUT_CONFIG", nonExistentPath)
	t.Setenv("LEDIT_CONFIG", "")

	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	if configDir != nonExistentPath {
		t.Errorf("expected %s, got %s", nonExistentPath, configDir)
	}

	// Verify directory was created
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("config directory was not created: %s", configDir)
	}
}

// TestGetConfigDir_ReturnsPath verifies that GetConfigDir returns
// the directory path.
func TestGetConfigDir_ReturnsPath(t *testing.T) {
	resetDeprecatedVars(t)

	tmpDir := t.TempDir()

	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CONFIG", "")

	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	if configDir == "" {
		t.Error("expected non-empty config dir path")
	}

	if !filepath.IsAbs(configDir) {
		t.Errorf("expected absolute path, got %s", configDir)
	}
}

// TestGetConfigDir_WhitespaceTrimmed verifies that leading/trailing
// whitespace in SPROUT_CONFIG is trimmed before use.
func TestGetConfigDir_WhitespaceTrimmed(t *testing.T) {
	resetDeprecatedVars(t)

	tmpDir := t.TempDir()

	t.Setenv("SPROUT_CONFIG", "  "+tmpDir+"  ")
	t.Setenv("LEDIT_CONFIG", "")

	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	if configDir != tmpDir {
		t.Errorf("expected %s (trimmed), got %s", tmpDir, configDir)
	}

	// Verify directory exists
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("config directory was not created: %s", configDir)
	}
}

// TestGetConfigDir_WhitespaceOnlyFallsThrough verifies that a whitespace-only
// SPROUT_CONFIG is treated as unset and falls through to XDG_CONFIG_HOME.
func TestGetConfigDir_WhitespaceOnlyFallsThrough(t *testing.T) {
	resetDeprecatedVars(t)

	xdgDir := t.TempDir()

	unsetEnv(t, "CONFIG")
	t.Setenv("SPROUT_CONFIG", "   ")
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	expectedDir := filepath.Join(xdgDir, "sprout")
	if configDir != expectedDir {
		t.Errorf("expected %s (XDG fallback), got %s", expectedDir, configDir)
	}

	// Verify directory was created
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("config directory was not created: %s", configDir)
	}
}

// TestGetConfigDir_TabNewlineFallsThrough verifies that SPROUT_CONFIG
// containing only tabs and newlines is treated as unset and falls
// through to HOME.
func TestGetConfigDir_TabNewlineFallsThrough(t *testing.T) {
	resetDeprecatedVars(t)

	homeDir := t.TempDir()

	unsetEnv(t, "CONFIG")
	t.Setenv("SPROUT_CONFIG", "\t\n")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", homeDir)

	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	expectedDir := filepath.Join(homeDir, ".config", "sprout")
	if configDir != expectedDir {
		t.Errorf("expected %s (HOME fallback), got %s", expectedDir, configDir)
	}

	// Verify directory was created
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Errorf("config directory was not created: %s", configDir)
	}
}

// TestGetConfigDir_MkdirAllError verifies that GetConfigDir returns
// an error when os.MkdirAll fails (e.g., read-only parent directory).
func TestGetConfigDir_MkdirAllError(t *testing.T) {
	resetDeprecatedVars(t)

	if os.Geteuid() == 0 {
		t.Skip("skipping: permission-based test requires non-root user")
	}
	if runtime.GOOS == "darwin" {
		t.Skip("skipping: permission-based test unreliable on macOS")
	}

	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")

	// Create a read-only parent directory.
	err := os.MkdirAll(readOnlyDir, 0700)
	if err != nil {
		t.Fatalf("failed to create temp read-only dir: %v", err)
	}
	err = os.Chmod(readOnlyDir, 0o444)
	if err != nil {
		t.Fatalf("failed to chmod read-only: %v", err)
	}
	t.Cleanup(func() {
		// Restore write permissions so t.TempDir() cleanup can remove it.
		_ = os.Chmod(readOnlyDir, 0700)
	})

	targetDir := filepath.Join(readOnlyDir, "child", "sprout")

	unsetEnv(t, "CONFIG")
	t.Setenv("SPROUT_CONFIG", targetDir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	configDir, err := GetConfigDir()
	if err == nil {
		t.Fatal("expected error from MkdirAll failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create config directory") {
		t.Errorf("expected 'failed to create config directory' in error, got: %v", err)
	}
	if configDir != "" {
		t.Errorf("expected empty configDir on error, got %s", configDir)
	}
}

// TestSetEnv_InvalidKey verifies that SetEnv returns an error when
// given an invalid environment variable key (containing NUL byte).
func TestSetEnv_InvalidKey(t *testing.T) {
	resetDeprecatedVars(t)

	// Cleanup even if os.Setenv partially succeeds on some platforms.
	t.Cleanup(func() {
		os.Unsetenv("SPROUT_IN\x00VALID")
		os.Unsetenv("LEDIT_IN\x00VALID")
	})

	err := SetEnv("IN\x00VALID", "value")
	if err == nil {
		t.Fatal("expected error from SetEnv with NUL byte in key, got nil")
	}
}
