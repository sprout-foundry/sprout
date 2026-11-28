package configuration

import (
	"os"
	"path/filepath"
	"testing"
)

// A fresh runner's first Load() writes the migration marker into the real
// providers dir mid-run; the detector must not fail the package over it.
// A genuinely test-written provider file is still reported.
func TestProviderLeaks_IgnoresMigrationMarker(t *testing.T) {
	dir := t.TempDir()
	before := snapshotProvidersDir(dir)

	if err := os.WriteFile(filepath.Join(dir, apiKeysMigratedMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, configApiKeysMigratedMarker), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "some-provider.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	leaked := providerLeaks(dir, before)
	if len(leaked) != 1 || leaked[0] != "some-provider.json" {
		t.Fatalf("expected only some-provider.json reported, got %v", leaked)
	}
}
