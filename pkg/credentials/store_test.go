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
	if !strings.Contains(err.Error(), "failed to parse API keys file") {
		t.Fatalf("expected parse error, got: %v", err)
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

	// Verify it wrote an empty object
	data, err := os.ReadFile(filepath.Join(dir, "api_keys.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("expected '{}', got %s", string(data))
	}
}

func TestSave_ValidStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	store := Store{"openai": "sk-test123", "anthropic": "sk-abc"}
	if err := Save(store); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file contents
	data, err := os.ReadFile(filepath.Join(dir, "api_keys.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "sk-test123") {
		t.Fatalf("expected file to contain openai key, got: %s", string(data))
	}
	if !strings.Contains(string(data), "sk-abc") {
		t.Fatalf("expected file to contain anthropic key, got: %s", string(data))
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

	resolved, err := Resolve("missing-provider", "")
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

	resolved, err := Resolve("  provider  ", "TRIM_ME_KEY")
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

	resolved, err := Resolve("test-provider", "TEST_PROVIDER_API_KEY")
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
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	store := Store{
		"test-provider": "stored-key",
	}
	if err := Save(store); err != nil {
		t.Fatalf("save store: %v", err)
	}

	resolved, err := Resolve("test-provider", "TEST_PROVIDER_API_KEY")
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
