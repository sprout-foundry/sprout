package embedding

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// DefaultModelDir returns the directory holding the embedding model weights and
// the ONNX Runtime shared library.
//
// This resolver is deliberately NOT per-workspace and deliberately has no
// build-tag variants. Both properties were violated, and each violation cost
// real disk:
//
//  1. It used to resolve under the *config* root. `--isolated-config` (which
//     SP-116 turns on automatically for any git repo) points SPROUT_CONFIG at
//     <workspace>/.sprout, so every repo the user opened downloaded its own
//     copy of the 196MB weights blob plus the 35MB runtime dylib — ~222MB per
//     workspace, found duplicated across six of them. SP-133 moved the cgo
//     build to the data root but left the wasm and non-cgo builds resolving off
//     the config root and off ~/.cache respectively, so the bug stayed live and
//     the three builds disagreed about where the model even was.
//
//  2. Three copies of this function existed behind //go:build cgo, !cgo, and
//     wasm, with three different env var names (SPROUT_MODELS_DIR vs
//     SPROUT_MODEL_DIR) and three different fallbacks. Divergence was the root
//     cause; one definition is the fix. Keep it that way — if a platform needs
//     different behavior, branch inside this function, not with a build tag.
//
// The contents are large, immutable, and content-addressed (the downloader
// verifies sha256), so one shared copy per user is always correct.
//
// Resolution: $SPROUT_MODELS_DIR → $SPROUT_MODEL_DIR (legacy alias) →
// $SPROUT_DATA_DIR/models/embedding → $XDG_DATA_HOME/sprout/models/embedding
// → $HOME/.local/share/sprout/models/embedding.
//
// Nested under models/embedding (not directly under models/) as of the
// sibling pkg/localmodel/DefaultModelsDir landing: LLM chat models moved
// in as models/llm alongside this, one shared "models" root with a
// subdirectory per kind, rather than two unrelated top-level directories
// under DataDir with no indication either exists. Falls back to the flat
// models/ directory (this function's own pre-split default) when that
// already has content and models/embedding doesn't, so installations that
// downloaded the embedding model before this split keep working without
// re-downloading.
func DefaultModelDir() string {
	if dir := strings.TrimSpace(os.Getenv("SPROUT_MODELS_DIR")); dir != "" {
		return dir
	}
	// Legacy singular spelling, previously honored only by the non-cgo build.
	if dir := strings.TrimSpace(os.Getenv("SPROUT_MODEL_DIR")); dir != "" {
		return dir
	}
	dataDir, err := envutil.DataDir()
	if err != nil {
		// No home and no override (WASM, minimal sandboxes). Stay off the
		// config root — a workspace-scoped fallback is what caused the
		// duplication this function exists to prevent.
		home, homeErr := os.UserHomeDir()
		if homeErr != nil || strings.TrimSpace(home) == "" {
			return filepath.Join(os.TempDir(), "sprout-models", "embedding")
		}
		return legacyOrNested(filepath.Join(home, ".local", "share", "sprout", "models"))
	}
	return legacyOrNested(filepath.Join(dataDir, "models"))
}

// legacyOrNested picks between modelsRoot/embedding (the current layout)
// and modelsRoot itself (the flat, pre-split layout) — see DefaultModelDir.
func legacyOrNested(modelsRoot string) string {
	nested := filepath.Join(modelsRoot, "embedding")
	if hasEntries(nested) {
		return nested
	}
	if hasFlatLegacyContent(modelsRoot) {
		return modelsRoot
	}
	return nested
}

func hasEntries(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// hasFlatLegacyContent reports whether modelsRoot has real pre-split
// content sitting directly in it, ignoring the "embedding"/"llm"
// subdirectories the current (post-split) layout itself creates — without
// excluding those, this would see pkg/localmodel's models/llm subdirectory
// and misread it as flat legacy embedding content, permanently pinning
// resolution to the wrong (flat) directory the moment any LLM model was
// ever downloaded.
func hasFlatLegacyContent(modelsRoot string) bool {
	entries, err := os.ReadDir(modelsRoot)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if name := e.Name(); name != "embedding" && name != "llm" {
			return true
		}
	}
	return false
}
