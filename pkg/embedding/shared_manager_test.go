package embedding

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// Two managers over one index are two writers, not a redundant cache. The
// daemon builds an agent per chat session and per workspace switch, so callers
// on the same workspace have to land on one manager.
func TestAcquireManagerSharesPerWorkspace(t *testing.T) {
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	cfg := &configuration.EmbeddingIndexConfig{}
	root := t.TempDir()

	first := AcquireManager(cfg, root)
	second := AcquireManager(cfg, root)
	t.Cleanup(func() { ReleaseManager(first); ReleaseManager(second) })

	if first != second {
		t.Fatal("two acquisitions for one workspace returned different managers")
	}
	if got := sharedManagerRefsForTest(first); got != 2 {
		t.Errorf("refs = %d, want 2", got)
	}
}

func TestAcquireManagerSeparatesWorkspaces(t *testing.T) {
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	cfg := &configuration.EmbeddingIndexConfig{}

	a := AcquireManager(cfg, t.TempDir())
	b := AcquireManager(cfg, t.TempDir())
	t.Cleanup(func() { ReleaseManager(a); ReleaseManager(b) })

	if a == b {
		t.Fatal("distinct workspaces shared one manager")
	}
}

// The manager must outlive any single holder — one agent shutting down while
// another is mid-build must not close the store out from under it.
func TestReleaseManagerClosesOnlyOnLastRelease(t *testing.T) {
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	cfg := &configuration.EmbeddingIndexConfig{}
	root := t.TempDir()

	mgr := AcquireManager(cfg, root)
	_ = AcquireManager(cfg, root)

	ReleaseManager(mgr)
	if got := sharedManagerRefsForTest(mgr); got != 1 {
		t.Fatalf("refs after first release = %d, want 1", got)
	}

	ReleaseManager(mgr)
	if got := sharedManagerRefsForTest(mgr); got != 0 {
		t.Errorf("refs after last release = %d, want 0", got)
	}

	// Dropping to zero must also drop the registry entry, so the next acquire
	// builds a fresh manager rather than handing out a closed one.
	next := AcquireManager(cfg, root)
	t.Cleanup(func() { ReleaseManager(next) })
	if next == mgr {
		t.Error("acquire after full release returned the closed manager")
	}
}

// Release must tolerate managers built directly (tests, WASM paths) and nils,
// so callers can release unconditionally.
func TestReleaseManagerHandlesUnregistered(t *testing.T) {
	ReleaseManager(nil)
	ReleaseManager(NewEmbeddingManager(&configuration.EmbeddingIndexConfig{}, t.TempDir()))
}

func TestAcquireManagerIsConcurrencySafe(t *testing.T) {
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	cfg := &configuration.EmbeddingIndexConfig{}
	root := t.TempDir()

	const n = 16
	got := make([]*EmbeddingManager, n)
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = AcquireManager(cfg, root)
		}()
	}
	wg.Wait()

	for _, m := range got {
		if m != got[0] {
			t.Fatal("concurrent acquisitions produced more than one manager")
		}
	}
	if refs := sharedManagerRefsForTest(got[0]); refs != n {
		t.Errorf("refs = %d, want %d", refs, n)
	}
	for _, m := range got {
		ReleaseManager(m)
	}
	if refs := sharedManagerRefsForTest(got[0]); refs != 0 {
		t.Errorf("refs after releasing all = %d, want 0", refs)
	}
}

// An explicit index_dir is honored verbatim, so two workspaces pointed at one
// directory must still not share a manager — their builds have different
// stale-record scopes.
func TestAcquireManagerKeysOnWorkspaceNotJustIndexDir(t *testing.T) {
	t.Setenv("SPROUT_DATA_DIR", t.TempDir())
	shared := filepath.Join(t.TempDir(), "shared-index")
	cfg := &configuration.EmbeddingIndexConfig{IndexDir: shared}

	a := AcquireManager(cfg, t.TempDir())
	b := AcquireManager(cfg, t.TempDir())
	t.Cleanup(func() { ReleaseManager(a); ReleaseManager(b) })

	if a == b {
		t.Error("workspaces sharing an explicit index_dir collapsed into one manager")
	}
}
