package localmodel

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Registers the real local-model listing with pkg/agent_api's
// GetModelsForProvider dispatch — see api.LocalModelsProvider's doc comment
// for why this is a runtime hook rather than a direct import.
//
// Serves TieredModelInfos directly rather than through GetLocalProvider:
// in a cgo-less build the provider singleton is the stub whose ListModels
// returns (nil, nil) — which GetModelsForProviderCtx treats as a valid
// empty list, leaving the WebUI model picker empty while the settings
// tab (which reads the catalog independently) shows every model. The
// catalog matrix is pure Go — no cgo, no MLX — so listing works on every
// platform; whether a listed model can actually load is gated separately
// in LocalProvider.SetModel and detectBackend.
func init() {
	api.LocalModelsProvider = func(ctx context.Context) ([]api.ModelInfo, error) {
		return TieredModelInfos(TotalSystemRAM()), nil
	}
}

// DefaultPort is the port the local LLM server listens on (for the
// standalone HTTP server mode). The in-process provider doesn't use it.
const DefaultPort = 18081

// localBackendMLX identifies the MLX server backend. Declared here (no
// build tag) rather than in local_provider.go (darwin && arm64 && cgo) so
// that lifecycle.go, which builds on every platform, can reference it.
const localBackendMLX = "mlx"

// DefaultModelsDir is where downloaded LLM (chat) model weights are
// stored — ~/.sprout-local/models. $SPROUT_LLM_MODELS_DIR overrides for
// tests and custom layouts.
//
// History: ~/dev/llm-models was the original hardcoded default (a personal
// dev-machine convention that leaked into the product), superseded by an
// XDG data-dir layout, and finally by ~/.sprout-local/models — a dedicated,
// predictable root users can find and size at a glance, with one
// subdirectory per model. Installations with weights at a previous
// location are migrated once, at first resolve: each model directory is
// renamed into the new root (same-volume renames are instant); a
// cross-volume move falls back to copy+delete; anything that can't move
// is left in place and keeps resolving to the old root until it can be
// migrated (partial migration is safe — remaining dirs migrate on a later
// attempt).
var DefaultModelsDir = resolveDefaultModelsDir()

func resolveDefaultModelsDir() string {
	if dir := strings.TrimSpace(os.Getenv("SPROUT_LLM_MODELS_DIR")); dir != "" {
		return dir
	}
	newDir := sproutLocalModelsDir()
	migrateLegacyModels(newDir)
	return newDir
}

// sproutLocalModelsDir returns ~/.sprout-local/models, or a temp-dir
// fallback when $HOME can't be resolved (tests, exotic sandboxes).
func sproutLocalModelsDir() string {
	h, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(h) == "" {
		return filepath.Join(os.TempDir(), "sprout-local", "models")
	}
	return filepath.Join(h, ".sprout-local", "models")
}

// legacyModelsDirs lists the previous default locations, oldest scheme
// first. The first one holding any model content is the migration source.
func legacyModelsDirs() []string {
	var dirs []string
	if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(h, "dev", "llm-models")) // original hardcoded path
		if dataDir, err := envutilDataDir(); err == nil {
			dirs = append(dirs, filepath.Join(dataDir, "models", "llm")) // XDG-era path
		}
	}
	return dirs
}

// envutilDataDir mirrors pkg/envutil.DataDir ($SPROUT_DATA_DIR →
// $XDG_DATA_HOME/sprout → ~/.local/share/sprout) without an import cycle.
func envutilDataDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("SPROUT_DATA_DIR")); dir != "" {
		return dir, nil
	}
	h, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(h) == "" {
		return "", os.ErrNotExist
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		return filepath.Join(xdg, "sprout"), nil
	}
	return filepath.Join(h, ".local", "share", "sprout"), nil
}

// migrateLegacyModels moves model directories from the first legacy
// location that has content into newDir. Best-effort and idempotent: a
// failed move (cross-device link, permissions) leaves the directory in
// place for a later attempt; entries that already exist under newDir are
// skipped (never overwritten). Run before DefaultModelsDir is first used
// — resolveDefaultModelsDir calls it, so the migration happens once per
// process at package init.
func migrateLegacyModels(newDir string) {
	if hasEntries(newDir) {
		return // new location already in use — nothing to migrate
	}
	for _, legacy := range legacyModelsDirs() {
		if legacy == newDir || !hasEntries(legacy) {
			continue
		}
		entries, err := os.ReadDir(legacy)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(newDir, 0o755); err != nil {
			return
		}
		moved := 0
		for _, e := range entries {
			if !e.IsDir() {
				continue // migrate model directories only
			}
			src := filepath.Join(legacy, e.Name())
			dst := filepath.Join(newDir, e.Name())
			if _, err := os.Stat(dst); err == nil {
				continue // already migrated
			}
			if err := moveDir(src, dst); err != nil {
				continue // leave for a later attempt
			}
			moved++
		}
		// One legacy source is enough — don't merge multiple old roots in
		// a single pass (keeps failure半ways recoverable and the operation
		// easy to reason about).
		_ = moved
		return
	}
}

// moveDir relocates a directory tree: a plain rename when the source and
// destination share a device, copy+delete otherwise.
func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device fallback: copy the tree, then remove the source only
	// when the copy was complete.
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		os.RemoveAll(dst) // incomplete copy — don't leave a partial model
		return err
	}
	return os.RemoveAll(src)
}

func hasEntries(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}
