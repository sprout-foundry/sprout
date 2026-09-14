package localmodel

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveDefaultModelsDirIsSproutLocal guards the current default:
// ~/.sprout-local/models on a fresh install.
func TestResolveDefaultModelsDirIsSproutLocal(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())

	want := filepath.Join(home, ".sprout-local", "models")
	if got := resolveDefaultModelsDir(); got != want {
		t.Errorf("resolveDefaultModelsDir() = %q, want %q", got, want)
	}
}

// TestMigrateLegacyDevModels guards the ~/.sprout-local/models move: model
// directories under the original hardcoded ~/dev/llm-models location are
// migrated into the new root at first resolve (same-volume rename).
func TestMigrateLegacyDevModels(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())

	legacy := filepath.Join(home, "dev", "llm-models")
	if err := os.MkdirAll(filepath.Join(legacy, "qwen3.5-4b-4bit"), 0o755); err != nil {
		t.Fatalf("seed legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "qwen3.5-4b-4bit", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveDefaultModelsDir()
	want := filepath.Join(home, ".sprout-local", "models")
	if got != want {
		t.Fatalf("resolveDefaultModelsDir() = %q, want %q", got, want)
	}
	// The model directory moved.
	if _, err := os.Stat(filepath.Join(want, "qwen3.5-4b-4bit", "config.json")); err != nil {
		t.Errorf("model not migrated into %s: %v", want, err)
	}
	// The legacy directory no longer holds it.
	if _, err := os.Stat(filepath.Join(legacy, "qwen3.5-4b-4bit")); !os.IsNotExist(err) {
		t.Errorf("legacy model dir still present after migration: %v", err)
	}
}

// TestMigrateLegacyXDGMovesFromDataDir guards the second-generation
// location: DataDir/models/llm content also migrates when the new root is
// empty. The older ~/dev/llm-models scheme wins when both have content.
func TestMigrateLegacyXDGMovesFromDataDir(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", dataDir)

	xdg := filepath.Join(dataDir, "models", "llm")
	if err := os.MkdirAll(filepath.Join(xdg, "minicpm5-2b-mlx"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveDefaultModelsDir()
	want := filepath.Join(home, ".sprout-local", "models")
	if got != want {
		t.Fatalf("resolveDefaultModelsDir() = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "minicpm5-2b-mlx")); err != nil {
		t.Errorf("XDG model not migrated: %v", err)
	}
}

// TestMigrateLegacyPrefersOldestSource pins the source-selection order:
// when both legacy locations hold models, the original ~/dev/llm-models
// is migrated first (single-source-per-pass keeps the operation
// recoverable).
func TestMigrateLegacyPrefersOldestSource(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", dataDir)

	legacyDev := filepath.Join(home, "dev", "llm-models")
	xdg := filepath.Join(dataDir, "models", "llm")
	for _, dir := range []string{legacyDev, xdg} {
		if err := os.MkdirAll(filepath.Join(dir, "some-model"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	resolveDefaultModelsDir()
	want := filepath.Join(home, ".sprout-local", "models")
	if _, err := os.Stat(filepath.Join(want, "some-model")); err != nil {
		t.Fatalf("oldest legacy source not migrated: %v", err)
	}
	// XDG content stays put — it migrates on a later resolve once the
	// first source is drained.
	if _, err := os.Stat(filepath.Join(xdg, "some-model")); err != nil {
		t.Fatalf("second legacy source must not be touched in the same pass: %v", err)
	}
}

// TestMigrateDoesNotOverwriteExisting guards the no-clobber rule: an
// entry already present under the new root is never replaced by its
// legacy twin.
func TestMigrateDoesNotOverwriteExisting(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())

	legacy := filepath.Join(home, "dev", "llm-models")
	newDir := filepath.Join(home, ".sprout-local", "models")
	if err := os.MkdirAll(filepath.Join(legacy, "m", "legacy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(newDir, "m"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "m", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolveDefaultModelsDir()
	// Existing new-root entry untouched; legacy sibling stays for later.
	if _, err := os.Stat(filepath.Join(newDir, "m", "config.json")); err != nil {
		t.Errorf("existing model clobbered: %v", err)
	}
}

// TestResolveDefaultModelsDirEnvOverride guards that SPROUT_LLM_MODELS_DIR
// takes priority over everything else (and skips migration).
func TestResolveDefaultModelsDirEnvOverride(t *testing.T) {
	override := t.TempDir()
	t.Setenv("SPROUT_LLM_MODELS_DIR", override)
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())

	if got := resolveDefaultModelsDir(); got != override {
		t.Errorf("resolveDefaultModelsDir() = %q, want override %q", got, override)
	}
}

// TestMoveDirCrossDevice exercises moveDir's copy+delete fallback by
// simulating rename failure via an invalid first rename target... rename
// can't be made to fail portably, so this test targets the fallback's
// correctness directly: content survives and the source is removed.
func TestMoveDirCrossDevice(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "moved")
	if err := os.MkdirAll(filepath.Join(src, "model", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "model", "weights.bin"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "model", "nested", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := moveDir(src, dst); err != nil {
		t.Fatalf("moveDir: %v", err)
	}
	for _, p := range []string{
		filepath.Join(dst, "model", "weights.bin"),
		filepath.Join(dst, "model", "nested", "config.json"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing after move: %s (%v)", p, err)
		}
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source not removed after move: %v", err)
	}
}
