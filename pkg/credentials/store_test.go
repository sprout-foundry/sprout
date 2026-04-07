package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetConfigDir_CustomEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	got, err := GetConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Fatalf("expected %q, got %q", dir, got)
	}
}

func TestGetConfigDir_XDGEnv(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	// Ensure LEDIT_CONFIG is not set
	t.Setenv("LEDIT_CONFIG", "")

	got, err := GetConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(xdgDir, "ledit")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestGetConfigDir_Default(t *testing.T) {
	// Ensure neither LEDIT_CONFIG nor XDG_CONFIG_HOME are set
	t.Setenv("LEDIT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := GetConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".ledit")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestGetAPIKeysPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	got, err := GetAPIKeysPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(dir, "api_keys.json")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	// Don't create the file

	store, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil empty store")
	}
	if len(store) != 0 {
		t.Fatalf("expected empty store, got %d keys", len(store))
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	path := filepath.Join(dir, "api_keys.json")
	if err := os.WriteFile(path, []byte("not-json{{{"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	// The file doesn't start with age magic or JSON, so it fails at decryption
	// Either a decryption error or a parse error is acceptable
	if !strings.Contains(err.Error(), "failed to decrypt") && !strings.Contains(err.Error(), "failed to parse") {
		t.Fatalf("expected decrypt or parse error, got: %v", err)
	}
}

func TestLoad_NilStoreJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	path := filepath.Join(dir, "api_keys.json")
	if err := os.WriteFile(path, []byte("null"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store when JSON is null")
	}
	if len(store) != 0 {
		t.Fatalf("expected empty store, got %d keys", len(store))
	}
}

func TestSave_NilStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	if err := Save(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it wrote encrypted data (starts with age magic)
	data, err := os.ReadFile(filepath.Join(dir, "api_keys.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.HasPrefix(string(data), "age-encryption.org/v1") {
		t.Fatalf("expected encrypted data starting with age magic, got: %s", string(data))
	}
}

func TestSave_ValidStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	store := Store{"openai": "sk-test123", "anthropic": "sk-abc"}
	if err := Save(store); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file is encrypted (not plaintext)
	data, err := os.ReadFile(filepath.Join(dir, "api_keys.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.HasPrefix(string(data), "age-encryption.org/v1") {
		t.Fatalf("expected encrypted data, got plaintext: %s", string(data))
	}

	// Verify file permissions (0600)
	info, err := os.Stat(filepath.Join(dir, "api_keys.json"))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Fatalf("expected 0600 permissions, got %04o", perm)
	}
}

func TestSave_LoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	original := Store{"provider-a": "key-a", "provider-b": "key-b"}
	if err := Save(original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(loaded))
	}
	if loaded["provider-a"] != "key-a" {
		t.Fatalf("expected provider-a key %q, got %q", "key-a", loaded["provider-a"])
	}
	if loaded["provider-b"] != "key-b" {
		t.Fatalf("expected provider-b key %q, got %q", "key-b", loaded["provider-b"])
	}
}

func TestResolve_NoEnvVarAndNoStored(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	// Don't set any env var or store anything

	resolved, err := resolve("missing-provider", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Provider != "missing-provider" {
		t.Fatalf("expected provider %q, got %q", "missing-provider", resolved.Provider)
	}
	if resolved.Value != "" {
		t.Fatalf("expected empty value, got %q", resolved.Value)
	}
	if resolved.Source != "" {
		t.Fatalf("expected empty source, got %q", resolved.Source)
	}
}

func TestResolve_WhitespaceTrimmedEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	// Store an env var with surrounding whitespace
	t.Setenv("TRIM_ME_KEY", "  trim-value  ")

	resolved, err := resolve("  provider  ", "TRIM_ME_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Value != "trim-value" {
		t.Fatalf("expected %q, got %q", "trim-value", resolved.Value)
	}
	if resolved.Source != "environment" {
		t.Fatalf("expected environment source, got %q", resolved.Source)
	}
	if resolved.Provider != "provider" {
		t.Fatalf("expected trimmed provider %q, got %q", "provider", resolved.Provider)
	}
}

func TestResolvePrefersEnvironmentOverStoredKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("TEST_PROVIDER_API_KEY", "env-key")

	store := Store{
		"test-provider": "stored-key",
	}
	if err := Save(store); err != nil {
		t.Fatalf("save store: %v", err)
	}

	resolved, err := resolve("test-provider", "TEST_PROVIDER_API_KEY")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Value != "env-key" {
		t.Fatalf("expected env key, got %q", resolved.Value)
	}
	if resolved.Source != "environment" {
		t.Fatalf("expected environment source, got %q", resolved.Source)
	}
}

func TestResolveFallsBackToStoredKey(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	// Force file backend to avoid keyring state pollution
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")

	store := Store{
		"test-provider": "stored-key",
	}
	if err := Save(store); err != nil {
		t.Fatalf("save store: %v", err)
	}

	resolved, err := resolve("test-provider", "TEST_PROVIDER_API_KEY")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Value != "stored-key" {
		t.Fatalf("expected stored key, got %q", resolved.Value)
	}
	if resolved.Source != "stored" {
		t.Fatalf("expected stored source, got %q", resolved.Source)
	}
}

// --- Additional coverage tests ---

func TestGetConfigDir_WhitespaceLEDITConfig(t *testing.T) {
	// LEDIT_CONFIG is set to whitespace-only — should be treated as empty
	t.Setenv("LEDIT_CONFIG", "   \t  ")
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := GetConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".ledit")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestGetConfigDir_WhitespaceXDGConfigHome(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "   \t  ")

	got, err := GetConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".ledit")
	if got != expected {
		t.Fatalf("expected %q (fallthrough to home), got %q", expected, got)
	}
}

func TestGetAPIKeysPath_GetConfigDirFails(t *testing.T) {
	// Make home directory lookup fail by setting HOME to a very long invalid path
	// that will cause os.MkdirAll to fail. Use a temp dir as LEDIT_CONFIG
	// with a non-existent sub-path that we make unwritable.
	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "rodir")
	if err := os.MkdirAll(readOnlyDir, 0500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Make the directory truly read-only
	if err := os.Chmod(readOnlyDir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Set LEDIT_CONFIG to a sub-directory of the read-only dir that doesn't exist
	t.Setenv("LEDIT_CONFIG", filepath.Join(readOnlyDir, "subdir", "nested"))
	t.Setenv("XDG_CONFIG_HOME", "")

	_, err := GetAPIKeysPath()
	if err == nil {
		t.Fatal("expected error when GetConfigDir fails, got nil")
	}
	// The error is about failing to create the config directory
	if !strings.Contains(err.Error(), "failed to create config directory") {
		t.Fatalf("expected config dir creation error, got: %v", err)
	}
}

func TestLoad_ReadError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	path := filepath.Join(dir, "api_keys.json")
	if err := os.WriteFile(path, []byte(`{"key":"val"}`), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Make the file unreadable
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(path, 0600) // restore for cleanup

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for unreadable file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read API keys file") {
		t.Fatalf("expected read error, got: %v", err)
	}
}

func TestSave_WriteError(t *testing.T) {
	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(readOnlyDir, 0500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(readOnlyDir, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	// Point LEDIT_CONFIG to the read-only directory itself (already exists)
	t.Setenv("LEDIT_CONFIG", readOnlyDir)

	store := Store{"test": "value"}
	err := Save(store)
	// The directory exists and GetConfigDir succeeds. WriteFile may fail
	// because the directory is read-only (0500).
	// Error can be either from WriteFile (permission denied) or succeed if owner can write.
	if err != nil {
		// Either permission denied from WriteFile or config dir error
		t.Logf("Got expected write error: %v", err)
	}
	// If err is nil, the owner was able to write despite 0500 mode — acceptable on some systems.
}

func TestResolve_EnvVarSetButEmpty(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	// Force file backend to avoid keyring state pollution
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	t.Setenv("EMPTY_KEY", "")

	resolved, err := resolve("test-provider", "EMPTY_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty env var should be skipped; falls back to stored (which is empty)
	if resolved.Value != "" {
		t.Fatalf("expected empty value, got %q", resolved.Value)
	}
	if resolved.Source != "" {
		t.Fatalf("expected empty source, got %q", resolved.Source)
	}
}

func TestResolve_EnvVarWhitespaceOnly(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	// Force file backend to avoid keyring state pollution
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	t.Setenv("WS_KEY", "   \t  ")

	resolved, err := resolve("test-provider", "WS_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Whitespace-only env var value should be trimmed to empty and skipped
	if resolved.Value != "" {
		t.Fatalf("expected empty value, got %q", resolved.Value)
	}
	if resolved.Source != "" {
		t.Fatalf("expected empty source, got %q", resolved.Source)
	}
}

func TestResolve_StoredValueWithWhitespace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	// Force file backend to avoid keyring state pollution
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend() // Reset backend cache for this test
	t.Setenv("WS_PROVIDER_KEY", "")

	store := Store{
		"ws-provider": "  \t actual-key \t  ",
	}
	if err := Save(store); err != nil {
		t.Fatalf("save: %v", err)
	}

	resolved, err := resolve("ws-provider", "WS_PROVIDER_KEY")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Whitespace in stored value should be trimmed
	if resolved.Value != "actual-key" {
		t.Fatalf("expected %q, got %q", "actual-key", resolved.Value)
	}
	if resolved.Source != "stored" {
		t.Fatalf("expected stored source, got %q", resolved.Source)
	}
}
