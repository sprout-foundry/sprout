package credentials

import "testing"

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
