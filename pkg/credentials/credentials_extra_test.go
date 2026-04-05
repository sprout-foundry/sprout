package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolve_EnvVarAndStoredBothPresent verifies env var wins over stored key
// with non-trivial values (spaces in provider name, special chars in values).
func TestResolve_EnvVarAndStoredBothPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	store := Store{
		"my-provider": "stored-secret-key-12345",
	}
	if err := Save(store); err != nil {
		t.Fatalf("save: %v", err)
	}

	t.Setenv("MY_PROVIDER_KEY", "env-secret-key-67890")

	resolved, err := Resolve("my-provider", "MY_PROVIDER_KEY")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Value != "env-secret-key-67890" {
		t.Fatalf("expected env value to win, got %q", resolved.Value)
	}
	if resolved.Source != "environment" {
		t.Fatalf("expected source 'environment', got %q", resolved.Source)
	}
}

// TestResolve_EnvVarHasLeadingTrailingSpaces verifies precedence when env var
// trim produces a non-empty value — stored key should still be ignored.
func TestResolve_EnvVarHasLeadingTrailingSpaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	store := Store{
		"spaced-provider": "stored-key",
	}
	if err := Save(store); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Set env var with surrounding spaces; trim should yield a valid value
	t.Setenv("SPACED_PROVIDER_KEY", "  env-key-with-spaces  ")

	resolved, err := Resolve("spaced-provider", "SPACED_PROVIDER_KEY")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Value != "env-key-with-spaces" {
		t.Fatalf("expected trimmed env value %q, got %q", "env-key-with-spaces", resolved.Value)
	}
	if resolved.Source != "environment" {
		t.Fatalf("expected source 'environment', got %q", resolved.Source)
	}
}

// TestLoad_UnicodeKeysAndValues verifies Load correctly parses JSON containing
// Unicode keys and values (CJK characters, emoji, accented chars).
func TestLoad_UnicodeKeysAndValues(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	path := filepath.Join(dir, "api_keys.json")
	unicodeJSON := `{
		"日本語プロバイダー": "sk-にほんご-αβγ",
		"provider-🚀": "key-with-é-ñ-ü-ö-ß",
		"中文": "密钥值🔑"
	}`
	if err := os.WriteFile(path, []byte(unicodeJSON), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(store) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(store))
	}
	if store["日本語プロバイダー"] != "sk-にほんご-αβγ" {
		t.Fatalf("unexpected value for Japanese key: %q", store["日本語プロバイダー"])
	}
	if store["provider-🚀"] != "key-with-é-ñ-ü-ö-ß" {
		t.Fatalf("unexpected value for emoji key: %q", store["provider-🚀"])
	}
	if store["中文"] != "密钥值🔑" {
		t.Fatalf("unexpected value for Chinese key: %q", store["中文"])
	}
}

// TestSaveLoadRoundTrip_UnicodeValues verifies Save followed by Load preserves
// Unicode values exactly.
func TestSaveLoadRoundTrip_UnicodeValues(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	original := Store{
		"日本語プロバイダー": "sk-にほんご-αβγ",
		"provider-🚀":       "key-with-é-ñ-ü-ö-ß",
		"中文":              "密钥值🔑",
		"mixed":             "Hello世界🌍Cyber sécurité",
	}
	if err := Save(original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 4 {
		t.Fatalf("expected 4 keys, got %d", len(loaded))
	}
	for k, v := range original {
		if loaded[k] != v {
			t.Fatalf("key %q: expected %q, got %q", k, v, loaded[k])
		}
	}
}

// TestGetConfigDir_WhitespaceOnlyXDG duplicates coverage for the whitespace
// fallthrough path, explicitly testing that TrimSpace on XDG causes home fallback.
func TestGetConfigDir_WhitespaceOnlyXDG(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "   ")

	got, err := GetConfigDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".ledit")
	if got != expected {
		t.Fatalf("expected fallthrough to home %q, got %q", expected, got)
	}
}

// TestResolve_WhitespaceOnlyProvider verifies Resolve correctly trims a
// whitespace-only provider name to empty string and returns no match from store.
func TestResolve_WhitespaceOnlyProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	resolved, err := Resolve("   \t  ", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Provider != "" {
		t.Fatalf("expected empty provider after trim, got %q", resolved.Provider)
	}
	if resolved.Value != "" {
		t.Fatalf("expected empty value, got %q", resolved.Value)
	}
	if resolved.Source != "" {
		t.Fatalf("expected empty source, got %q", resolved.Source)
	}
}

// TestResolve_WhitespaceProviderWithEnvVar verifies that even with a whitespace-
// only provider, a valid env var still resolves correctly.
func TestResolve_WhitespaceProviderWithEnvVar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("MY_KEY", "env-value-42")

	resolved, err := Resolve("   \t  ", "MY_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Provider != "" {
		t.Fatalf("expected empty provider after trim, got %q", resolved.Provider)
	}
	if resolved.Value != "env-value-42" {
		t.Fatalf("expected %q, got %q", "env-value-42", resolved.Value)
	}
	if resolved.Source != "environment" {
		t.Fatalf("expected source 'environment', got %q", resolved.Source)
	}
}

// TestLoad_EmptyJSONObject verifies Load returns a non-nil empty Store for
// an empty JSON object file.
func TestLoad_EmptyJSONObject(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)

	path := filepath.Join(dir, "api_keys.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	store, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if len(store) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(store))
	}
}
