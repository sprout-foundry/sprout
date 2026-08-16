package localmodel

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveDefaultModelsDirFallsBackToLegacy guards the ~/dev/llm-models
// -> DataDir/models/llm move: an installation that already downloaded
// chat models under the old hardcoded ~/dev/llm-models directory must
// keep resolving there (no re-download) until it also has content under
// the new location.
func TestResolveDefaultModelsDirFallsBackToLegacy(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", dataDir)

	legacy := filepath.Join(home, "dev", "llm-models")
	if err := os.MkdirAll(filepath.Join(legacy, "qwen3.5-4b-4bit"), 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}

	if got := resolveDefaultModelsDir(); got != legacy {
		t.Errorf("resolveDefaultModelsDir() = %q, want legacy %q", got, legacy)
	}
}

// TestResolveDefaultModelsDirPrefersNewLocation guards the other half: once
// the new location has content, it wins even if the legacy directory also
// still has content (e.g. after a manual migration, or a fresh download
// landing in the right place while stale files remain at the old path).
func TestResolveDefaultModelsDirPrefersNewLocation(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", dataDir)

	legacy := filepath.Join(home, "dev", "llm-models")
	if err := os.MkdirAll(filepath.Join(legacy, "qwen3.5-4b-4bit"), 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	newDir := filepath.Join(dataDir, "models", "llm")
	if err := os.MkdirAll(filepath.Join(newDir, "qwen3.5-4b-4bit"), 0o755); err != nil {
		t.Fatalf("seed new dir: %v", err)
	}

	if got := resolveDefaultModelsDir(); got != newDir {
		t.Errorf("resolveDefaultModelsDir() = %q, want new location %q", got, newDir)
	}
}

// TestResolveDefaultModelsDirFreshInstall guards the common case: neither
// location has content yet, so a fresh install goes straight to the new
// XDG-style location, not the legacy one.
func TestResolveDefaultModelsDirFreshInstall(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", dataDir)

	want := filepath.Join(dataDir, "models", "llm")
	if got := resolveDefaultModelsDir(); got != want {
		t.Errorf("resolveDefaultModelsDir() = %q, want %q", got, want)
	}
}

// TestResolveDefaultModelsDirEnvOverride guards that SPROUT_LLM_MODELS_DIR
// takes priority over everything else.
func TestResolveDefaultModelsDirEnvOverride(t *testing.T) {
	override := t.TempDir()
	t.Setenv("SPROUT_LLM_MODELS_DIR", override)
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())

	if got := resolveDefaultModelsDir(); got != override {
		t.Errorf("resolveDefaultModelsDir() = %q, want override %q", got, override)
	}
}
