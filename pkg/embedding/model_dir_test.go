package embedding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The model weights (196MB) plus the ONNX Runtime dylib (35MB) are immutable,
// content-addressed, and identical for every workspace. Resolving them off the
// config root meant --isolated-config — which SP-116 enables automatically for
// any git repo — gave each repo its own ~222MB copy. Six workspaces on one
// machine held 1.3GB of byte-identical duplicates.
func TestDefaultModelDirIgnoresWorkspaceConfigRoot(t *testing.T) {
	dataDir := t.TempDir()
	workspace := t.TempDir()
	workspaceConfig := filepath.Join(workspace, ".sprout")

	t.Setenv("SPROUT_MODELS_DIR", "")
	t.Setenv("SPROUT_MODEL_DIR", "")
	t.Setenv("SPROUT_DATA_DIR", dataDir)
	// Exactly what cmd/root.go sets for an isolated (git-repo) workspace.
	t.Setenv("SPROUT_CONFIG", workspaceConfig)
	t.Setenv("SPROUT_CONFIG_DIR", workspaceConfig)

	got := DefaultModelDir()

	if strings.HasPrefix(got, workspace) {
		t.Errorf("model dir %q sits inside the workspace — every repo gets its own 222MB copy", got)
	}
	if want := filepath.Join(dataDir, "models"); got != want {
		t.Errorf("DefaultModelDir() = %q, want %q", got, want)
	}
}

// Two workspaces must resolve to one shared directory. This is the property
// that actually prevents duplication; the assertion above only covers the
// specific path that broke.
func TestDefaultModelDirIsWorkspaceIndependent(t *testing.T) {
	t.Setenv("SPROUT_MODELS_DIR", "")
	t.Setenv("SPROUT_MODEL_DIR", "")
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())

	t.Setenv("SPROUT_CONFIG", filepath.Join(t.TempDir(), ".sprout"))
	first := DefaultModelDir()

	t.Setenv("SPROUT_CONFIG", filepath.Join(t.TempDir(), ".sprout"))
	second := DefaultModelDir()

	if first != second {
		t.Errorf("two workspaces resolved to different model dirs:\n  %s\n  %s", first, second)
	}
}

func TestDefaultModelDirHonorsExplicitOverride(t *testing.T) {
	explicit := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	t.Setenv("SPROUT_MODEL_DIR", "")
	t.Setenv("SPROUT_MODELS_DIR", explicit)

	if got := DefaultModelDir(); got != explicit {
		t.Errorf("SPROUT_MODELS_DIR: got %q, want %q", got, explicit)
	}
}

// SPROUT_MODEL_DIR (singular) was honored only by the non-cgo build. Keeping it
// as an alias means anyone who set it does not silently get a second download
// location now that the resolvers are unified.
func TestDefaultModelDirHonorsLegacySingularOverride(t *testing.T) {
	legacy := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	t.Setenv("SPROUT_MODELS_DIR", "")
	t.Setenv("SPROUT_MODEL_DIR", legacy)

	if got := DefaultModelDir(); got != legacy {
		t.Errorf("SPROUT_MODEL_DIR: got %q, want %q", got, legacy)
	}
}

func TestDefaultModelDirPrefersPluralOverLegacy(t *testing.T) {
	plural := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	t.Setenv("SPROUT_MODELS_DIR", plural)
	t.Setenv("SPROUT_MODEL_DIR", t.TempDir())

	if got := DefaultModelDir(); got != plural {
		t.Errorf("SPROUT_MODELS_DIR should win: got %q, want %q", got, plural)
	}
}

// The runtime dylib lives under the model dir, so it inherits the same sharing
// — it was duplicated per workspace by the same bug.
func TestONNXRuntimeDirIsUnderSharedModelDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SPROUT_MODELS_DIR", "")
	t.Setenv("SPROUT_MODEL_DIR", "")
	t.Setenv("SPROUT_DATA_DIR", dataDir)
	t.Setenv("SPROUT_CONFIG", filepath.Join(t.TempDir(), ".sprout"))

	runtimeDir := filepath.Join(DefaultModelDir(), "onnxruntime")
	if !strings.HasPrefix(runtimeDir, dataDir) {
		t.Errorf("runtime dir %q is not under the shared data root %q", runtimeDir, dataDir)
	}
}

// A single definition is the fix; build-tag variants were the root cause.
// Guard the invariant structurally so a future platform tweak re-adds behavior
// inside the function rather than behind a new //go:build.
func TestDefaultModelDirHasExactlyOneDefinition(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	var defining []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(src), "func DefaultModelDir(") {
			defining = append(defining, name)
		}
	}

	if len(defining) != 1 {
		t.Errorf("DefaultModelDir defined in %d files (%v); it must have exactly one build-tag-free definition — divergent per-build copies are what duplicated 1.3GB of model weights", len(defining), defining)
	}
}
