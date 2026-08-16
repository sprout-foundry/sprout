package localmodel

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// Registers the real local-model listing with pkg/agent_api's
// GetModelsForProvider dispatch — see api.LocalModelsProvider's doc comment
// for why this is a runtime hook rather than a direct import.
func init() {
	api.LocalModelsProvider = func(ctx context.Context) ([]api.ModelInfo, error) {
		return GetLocalProvider().ListModels(ctx)
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
// stored — XDG-style, honoring $SPROUT_DATA_DIR/$XDG_DATA_HOME like
// sprout's other data, under the SAME models/ root pkg/embedding's
// DefaultModelDir uses (<DataDir>/models/embedding for embedding models),
// but its own "llm" subdirectory — one shared, discoverable "models" root
// with a subdirectory per kind, rather than two same-purpose-sounding but
// unrelated top-level directories under DataDir. $SPROUT_MODELS_DIR is
// already pkg/embedding's env var for its own directory, so this uses a
// distinct name.
//
// ~/dev/llm-models was the prior hardcoded default here: a personal
// dev-machine convention ("~/dev/...") baked in as if it were universal,
// not a real per-user default — most tools use a dot-folder or XDG path
// for downloaded model weights.
//
// Resolution: $SPROUT_LLM_MODELS_DIR → $SPROUT_DATA_DIR/models/llm →
// $XDG_DATA_HOME/sprout/models/llm → $HOME/.local/share/sprout/models/llm.
// Falls back to the legacy ~/dev/llm-models location only when it already
// has content and the new location doesn't, so installations that
// downloaded models before this fix keep working without needing to
// re-download or manually migrate anything. New installs go straight to
// the proper location.
var DefaultModelsDir = resolveDefaultModelsDir()

func resolveDefaultModelsDir() string {
	if dir := strings.TrimSpace(os.Getenv("SPROUT_LLM_MODELS_DIR")); dir != "" {
		return dir
	}
	newDir := legacyModelsDirFallback() // overwritten below if DataDir resolves
	if dataDir, err := envutil.DataDir(); err == nil {
		newDir = filepath.Join(dataDir, "models", "llm")
	}
	if legacy := legacyModelsDir(); legacy != "" && hasEntries(legacy) && !hasEntries(newDir) {
		return legacy
	}
	return newDir
}

// legacyModelsDir returns the pre-fix default (~/dev/llm-models), or ""
// if the home directory can't be resolved.
func legacyModelsDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, "dev", "llm-models")
}

// legacyModelsDirFallback mirrors the original hardcoded default, used
// only if envutil.DataDir() itself fails to resolve (no $HOME, no XDG
// vars) — envutil.DataDir failing at all is itself unusual, so this is a
// last-resort fallback, not the normal path.
func legacyModelsDirFallback() string {
	if dir := legacyModelsDir(); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), "llm-models")
}

func hasEntries(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}
