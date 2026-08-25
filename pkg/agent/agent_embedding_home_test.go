package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// writeWorkspaceEmbeddingOptIn writes the workspace config shape that opts a
// workspace into embedding auto-indexing.
func writeWorkspaceEmbeddingOptIn(t *testing.T, workspaceRoot string) {
	t.Helper()
	dir := filepath.Join(workspaceRoot, ".sprout")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := `{"embedding_index":{"enabled":true,"experimental":true,"auto_index":true}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
}

// The installed service runs with WorkingDirectory=$HOME, so the workspace root
// is the home directory. GetWorkspaceConfigPath($HOME) then aliases
// ~/.sprout/config.json — the daemon's own per-user state directory, which
// sprout itself creates — and reading it as a per-workspace opt-in made the
// daemon index the user's entire home at startup with no client connected.
func TestRestoreEmbeddingIndexSkipsHomeWorkspace(t *testing.T) {
	a := newTestAgent(t)

	// TestMain hard-disables auto-indexing for the whole suite; clear it so this
	// test actually reaches the home guard instead of short-circuiting.
	t.Setenv("SPROUT_DISABLE_EMBEDDING_AUTOINDEX", "0")

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeWorkspaceEmbeddingOptIn(t, home)

	a.SetWorkspaceRoot(home)
	a.RestoreEmbeddingIndex()

	if a.GetEmbeddingManager() != nil {
		t.Error("embedding index must not auto-enable when the workspace is $HOME")
	}
}

// The home guard must not disable indexing for genuine workspaces that opted in.
func TestRestoreEmbeddingIndexHonorsRealWorkspaceOptIn(t *testing.T) {
	a := newTestAgent(t)

	t.Setenv("SPROUT_DISABLE_EMBEDDING_AUTOINDEX", "0")

	home := t.TempDir()
	t.Setenv("HOME", home)

	project := filepath.Join(home, "dev", "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceEmbeddingOptIn(t, project)

	a.SetWorkspaceRoot(project)
	a.RestoreEmbeddingIndex()

	if a.GetEmbeddingManager() == nil {
		t.Error("a real workspace opting in must still restore its index")
	}
}

// A workspace config persisted before the Experimental gate existed has
// "enabled":true but no "experimental" key at all. That must not silently
// keep auto-indexing after upgrading — full-workspace auto-indexing was
// found to cause severe, unbounded native-memory growth (see
// pkg/embedding/index.go), so existing users must explicitly re-opt-in via
// /index or the UI toggle (which sets both fields together — see
// EnableEmbeddingIndex) rather than have old state carry the same weight.
func TestRestoreEmbeddingIndexRequiresReoptInWithoutExperimentalFlag(t *testing.T) {
	a := newTestAgent(t)

	t.Setenv("SPROUT_DISABLE_EMBEDDING_AUTOINDEX", "0")
	t.Setenv("SPROUT_EXPERIMENTAL_EMBEDDINGS", "0")

	home := t.TempDir()
	t.Setenv("HOME", home)

	project := filepath.Join(home, "dev", "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(project, ".sprout")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	// Pre-Experimental-gate config shape: enabled, but no "experimental" key.
	cfg := `{"embedding_index":{"enabled":true,"auto_index":true}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}

	a.SetWorkspaceRoot(project)
	a.RestoreEmbeddingIndex()

	if a.GetEmbeddingManager() != nil {
		t.Error("a pre-existing enabled:true config without experimental:true must not auto-restore")
	}
}

func TestIsHomeDirPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if !isHomeDirPath(home) {
		t.Error("the home directory itself should match")
	}
	if isHomeDirPath(filepath.Join(home, "dev")) {
		t.Error("a subdirectory of home is a legitimate workspace and must not match")
	}
	if isHomeDirPath("") {
		t.Error("empty path must not match")
	}
}
