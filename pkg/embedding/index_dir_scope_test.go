package embedding

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// The daemon builds one EmbeddingManager per agent, per chat session, across
// every workspace the user opens, and envutil.DataDir() reads process-global
// env vars. If two workspaces resolve to the same index directory they share
// one index.hnsw and one manifest, and each build deletes the other's records
// as stale — an index that never converges plus concurrent full rebuilds.
func TestResolveIndexDirIsolatesWorkspaces(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", dataDir)

	a := resolveIndexDir(filepath.Join(t.TempDir(), "alpha"))
	b := resolveIndexDir(filepath.Join(t.TempDir(), "beta"))

	if a == b {
		t.Fatalf("distinct workspaces resolved to the same index dir: %s", a)
	}
	for _, dir := range []string{a, b} {
		if !strings.HasPrefix(dir, dataDir) {
			t.Errorf("index dir %q escaped the data root %q", dir, dataDir)
		}
	}
}

// Same-named directories in different parents are the common daemon case (two
// checkouts of one repo). The basename alone must not decide the path.
func TestResolveIndexDirSeparatesSameNamedWorkspaces(t *testing.T) {
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())

	a := resolveIndexDir(filepath.Join(t.TempDir(), "sprout"))
	b := resolveIndexDir(filepath.Join(t.TempDir(), "sprout"))

	if a == b {
		t.Fatalf("same-named workspaces in different parents collided: %s", a)
	}
}

func TestResolveIndexDirIsStable(t *testing.T) {
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	root := t.TempDir()

	if first, second := resolveIndexDir(root), resolveIndexDir(root); first != second {
		t.Fatalf("resolution not stable: %q then %q", first, second)
	}
}

// An empty workspace root keeps the previous unscoped layout so callers with
// no workspace are unaffected.
func TestResolveIndexDirUnscopedWithoutWorkspace(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SPROUT_DATA_DIR", dataDir)

	if got, want := resolveIndexDir(""), filepath.Join(dataDir, "embeddings"); got != want {
		t.Fatalf("resolveIndexDir(\"\") = %q, want %q", got, want)
	}
}

// An explicit config IndexDir is per-workspace overridable, so it is honored
// verbatim rather than scoped a second time.
func TestResolveIndexDirFromConfigHonorsExplicitDir(t *testing.T) {
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	explicit := filepath.Join(t.TempDir(), "custom-index")

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: explicit}
	if got := resolveIndexDirFromConfig(cfg, t.TempDir()); got != explicit {
		t.Fatalf("explicit IndexDir = %q, want %q", got, explicit)
	}
}

func TestWorkspaceSlugIsPathSafe(t *testing.T) {
	for _, root := range []string{
		filepath.Join(t.TempDir(), "my repo (v2)"),
		filepath.Join(t.TempDir(), "..weird.."),
		filepath.Join(t.TempDir(), strings.Repeat("x", 200)),
	} {
		slug := workspaceSlug(root)
		if slug == "" {
			t.Fatalf("empty slug for %q", root)
		}
		if strings.ContainsAny(slug, `/\:`) {
			t.Errorf("slug %q for %q contains a path separator", slug, root)
		}
		if len(slug) > 64 {
			t.Errorf("slug %q for %q is %d chars, want <= 64", slug, root, len(slug))
		}
	}
}
