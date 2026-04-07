package credentials

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestGetConfigDir_HomeError covers the os.UserHomeDir() error path (line 33).
// When LEDIT_CONFIG and XDG_CONFIG_HOME are both empty and HOME is unset,
// os.UserHomeDir() returns an error.
func TestGetConfigDir_HomeError(t *testing.T) {
	// Save and restore HOME
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", "")
	defer func() {
		if origHome != "" {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
	}()

	// Also clear LEDIT_CONFIG and XDG_CONFIG_HOME to force the home dir path
	t.Setenv("LEDIT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	_, err := GetConfigDir()
	if err == nil {
		t.Fatal("expected error when HOME is unset and UserHomeDir fails")
	}
}

// TestLoad_GetAPIKeysPathError covers lines 55-57: Load() propagates the error
// from GetAPIKeysPath() when GetConfigDir() fails.
func TestLoad_GetAPIKeysPathError(t *testing.T) {
	// Save and restore HOME
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", "")
	defer func() {
		if origHome != "" {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
	}()
	t.Setenv("LEDIT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error from Load when GetAPIKeysPath fails")
	}
}

// TestSave_GetAPIKeysPathError covers the error path where GetAPIKeysPath
// fails during Save (GetConfigDir → MkdirAll error or HomeDir error).
func TestSave_GetAPIKeysPathError(t *testing.T) {
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", "")
	defer func() {
		if origHome != "" {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
	}()
	t.Setenv("LEDIT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	err := Save(Store{"test": "val"})
	if err == nil {
		t.Fatal("expected error from Save when GetAPIKeysPath fails")
	}
}

// TestResolve_LoadError covers lines 103-105: Resolve returns an error when
// Load() fails (no env var set, and Load cannot find/access the keys file).
func TestResolve_LoadError(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", "")
	defer func() {
		if origHome != "" {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
	}()
	t.Setenv("LEDIT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	// No env var set, so Resolve falls through to Load()
	_, err := resolve("some-provider", "")
	if err == nil {
		t.Fatal("expected error from Resolve when Load fails")
	}
}

// TestResolve_LoadErrorWithEnvVarUnsetButNamed covers Resolve → Load error
// when env var name is provided but the variable is not set.
func TestResolve_LoadErrorWithEnvVarUnsetButNamed(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", "")
	defer func() {
		if origHome != "" {
			os.Setenv("HOME", origHome)
		} else {
			os.Unsetenv("HOME")
		}
	}()
	t.Setenv("LEDIT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	// Explicitly unset the named env var so os.Getenv returns empty
	t.Setenv("SOME_UNSET_VAR", "")

	_, err := resolve("some-provider", "SOME_UNSET_VAR")
	if err == nil {
		t.Fatal("expected error from Resolve when Load fails after empty env var check")
	}
}

// TestSave_WriteFileError covers lines 84-86: os.WriteFile fails when
// the target directory is read-only.
func TestSave_WriteFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root user can write to read-only directories")
	}

	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(readOnlyDir, 0500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Point LEDIT_CONFIG to a sub-path inside the read-only directory.
	// GetConfigDir will MkdirAll (which works because owner can create dirs with 0500),
	// but the configDir itself may be writable by owner. To force WriteFile failure,
	// remove write permission after the directory is created.
	t.Setenv("LEDIT_CONFIG", readOnlyDir)
	// Pre-create the directory so GetConfigDir's MkdirAll succeeds
	// then make it read-only for files
	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir: %v", err)
	}
	// Remove write permission from the directory to prevent file creation
	if err := os.Chmod(configDir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(configDir, 0700)

	err = Save(Store{"test": "value"})
	if err == nil {
		t.Fatal("expected error from Save when WriteFile fails on read-only directory")
	}
}

// TestSaveAndLoad_LargeStore verifies Save and Load work with hundreds of keys.
func TestSaveAndLoad_LargeStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	store := make(Store, 500)
	for i := 0; i < 500; i++ {
		store[fmt.Sprintf("provider-%04d", i)] = fmt.Sprintf("api-key-value-%04d-secret", i)
	}

	if err := Save(store); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 500 {
		t.Fatalf("expected 500 keys, got %d", len(loaded))
	}
	// Spot-check a few keys
	for _, key := range []string{"provider-0000", "provider-0250", "provider-0499"} {
		if loaded[key] != store[key] {
			t.Fatalf("key %q: expected %q, got %q", key, store[key], loaded[key])
		}
	}
}

// TestResolve_SpecialCharProviderNames verifies Resolve handles special
// characters in provider names and env var values.
func TestResolve_SpecialCharProviderNames(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	store := Store{
		"provider/with/slashes":  "key-slashes",
		"provider.with.dots":     "key-dots",
		"provider-with-dashes":   "key-dashes",
		"provider_with_underscores": "key-underscores",
		"UPPERCASE-PROVIDER":     "key-upper",
		"provider with spaces":   "key-spaces",
	}
	if err := Save(store); err != nil {
		t.Fatalf("save: %v", err)
	}

	for provider, expected := range store {
		resolved, err := resolve(provider, "")
		if err != nil {
			t.Fatalf("resolve %q: %v", provider, err)
		}
		if resolved.Value != expected {
			t.Fatalf("provider %q: expected %q, got %q", provider, expected, resolved.Value)
		}
		if resolved.Source != "stored" {
			t.Fatalf("provider %q: expected source 'stored', got %q", provider, resolved.Source)
		}
	}
}

// TestResolve_EnvVarSpecialChars verifies env var values with special characters.
func TestResolve_EnvVarSpecialChars(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	cases := []struct {
		envVal string
		desc   string
	}{
		{"sk-proj-abc123DEF456ghi789", "alphanumeric with dashes"},
		{"key=with=equals", "equals signs"},
		{"key\nwith\nnewlines", "newlines"},
		{"key\twith\ttabs", "tabs"},
		{"path/to/file:value", "colons and slashes"},
		{"aGVsbG8gd29ybGQ=", "base64 value"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			envName := "SPECIAL_CHAR_KEY"
			t.Setenv(envName, tc.envVal)

			// With spaces around the value to test trimming
			t.Setenv(envName, "  "+tc.envVal+"  ")

			resolved, err := resolve("provider", envName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resolved.Value != tc.envVal {
				t.Fatalf("expected %q, got %q", tc.envVal, resolved.Value)
			}
			if resolved.Source != "environment" {
				t.Fatalf("expected source 'environment', got %q", resolved.Source)
			}
		})
	}
}

// TestConcurrentSaveLoad verifies Save and Load don't panic under concurrent use.
// Note: concurrent writes to the same file will naturally produce interleaved data,
// so Load errors during races are expected and acceptable. This test verifies
// no panics or deadlocks occur.
func TestConcurrentSaveLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	// Pre-populate the store
	initialStore := make(Store, 50)
	for i := 0; i < 50; i++ {
		initialStore[fmt.Sprintf("key-%d", i)] = fmt.Sprintf("val-%d", i)
	}
	if err := Save(initialStore); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	var wg sync.WaitGroup

	// Concurrent goroutines doing Save and Load — errors from races are expected
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			store := make(Store, 10)
			for j := 0; j < 10; j++ {
				store[fmt.Sprintf("goroutine-%d-key-%d", n, j)] = fmt.Sprintf("val-%d", j)
			}
			_ = Save(store) // may fail due to concurrent writes — acceptable
		}(i)
		go func() {
			defer wg.Done()
			_, _ = Load() // may fail due to concurrent writes — acceptable
		}()
	}

	wg.Wait()
}

// TestResolve_NilLikeEmptyProvider verifies Resolve with empty provider string.
func TestResolve_NilLikeEmptyProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	resolved, err := resolve("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Provider != "" {
		t.Fatalf("expected empty provider, got %q", resolved.Provider)
	}
	if resolved.Value != "" {
		t.Fatalf("expected empty value, got %q", resolved.Value)
	}
	if resolved.Source != "" {
		t.Fatalf("expected empty source, got %q", resolved.Source)
	}
}

// TestLoad_GetAPIKeysPathErrorViaUnreadableConfigDir covers the Load error path
// where the config directory's parent is not creatable.
func TestLoad_GetAPIKeysPathErrorViaReadOnlyParent(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping: root user bypasses permission checks")
	}

	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "ro")
	if err := os.Mkdir(readOnlyDir, 0500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("LEDIT_CONFIG", filepath.Join(readOnlyDir, "sub", "deep"))
	t.Setenv("XDG_CONFIG_HOME", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when GetConfigDir's MkdirAll fails in Load")
	}
}

// TestResolve_EnvVarSetButProvidedNameNotInEnv covers the path where
// an env var name is given but os.Getenv returns empty, falling through to Load.
func TestResolve_EnvVarSetButProvidedNameNotInEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	store := Store{"fallback-provider": "stored-value"}
	if err := Save(store); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Intentionally don't set NONEXISTENT_ENV_VAR
	resolved, err := resolve("fallback-provider", "NONEXISTENT_ENV_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Value != "stored-value" {
		t.Fatalf("expected stored value, got %q", resolved.Value)
	}
	if resolved.Source != "stored" {
		t.Fatalf("expected source 'stored', got %q", resolved.Source)
	}
}
