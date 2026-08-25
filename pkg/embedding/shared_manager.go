package embedding

import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// A manager owns an in-memory HNSW store and drives index builds against one
// on-disk index. Two managers over the same index are not a cache miss — they
// are two writers: each loads its own copy of every record and vector, each
// runs its own build, and each persists its own view over the other's (see the
// staleIDs sweep in IndexManager.BuildIndex).
//
// Nothing about agent construction made that unlikely. The daemon builds a
// fresh agent per chat session, per client context, and on every workspace
// switch, and each agent called NewEmbeddingManager directly — so N agents on
// one workspace meant N stores and N concurrent builds of the same files.
//
// This registry hands every caller on the same index the same manager, with a
// refcount so the last releaser closes it. It mirrors acquireSharedONNXProvider
// in shared_runtime.go, which solved the identical problem for the model
// weights, except that managers are closable and so must be counted rather than
// held for the life of the process.

type sharedManagerEntry struct {
	mgr  *EmbeddingManager
	refs int
}

var (
	sharedManagerMu sync.Mutex
	sharedManagers  = map[string]*sharedManagerEntry{}
)

// AcquireManager returns the process-wide manager for the index that (cfg,
// workspaceRoot) resolves to, creating it on first use. Every successful call
// must be paired with exactly one ReleaseManager.
//
// The key includes both the index directory and the workspace root. The index
// directory alone is not enough: two workspaces that explicitly configure the
// same embedding_index.index_dir must not silently share a build whose
// stale-record sweep is scoped to one of them. The default index directory is
// already workspace-derived, so ordinary callers key on it uniquely.
func AcquireManager(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) *EmbeddingManager {
	key := resolveIndexDirFromConfig(cfg, workspaceRoot) + "\x00" + workspaceRoot

	sharedManagerMu.Lock()
	defer sharedManagerMu.Unlock()

	if e, ok := sharedManagers[key]; ok {
		e.refs++
		return e.mgr
	}

	mgr := NewEmbeddingManager(cfg, workspaceRoot)
	mgr.sharedKey = key
	sharedManagers[key] = &sharedManagerEntry{mgr: mgr, refs: 1}
	return mgr
}

// ReleaseManager drops one reference taken by AcquireManager, closing the
// manager once the last holder releases it. Managers not obtained from
// AcquireManager are closed directly, so callers can release unconditionally.
//
// Closing on the last release rather than never is deliberate: unlike the ONNX
// weights, a manager pins a full copy of the workspace's vectors, and a daemon
// that opens many workspaces over a long session should not accumulate them.
func ReleaseManager(m *EmbeddingManager) {
	if m == nil {
		return
	}

	if m.sharedKey == "" {
		_ = m.Close()
		return
	}

	sharedManagerMu.Lock()
	e, ok := sharedManagers[m.sharedKey]
	if !ok {
		sharedManagerMu.Unlock()
		return
	}
	e.refs--
	last := e.refs <= 0
	if last {
		delete(sharedManagers, m.sharedKey)
	}
	sharedManagerMu.Unlock()

	// Close outside the registry lock — it flushes the store to disk.
	if last {
		_ = e.mgr.Close()
	}
}

// sharedManagerRefsForTest reports the live refcount for a manager, or 0 when
// it is not registered.
func sharedManagerRefsForTest(m *EmbeddingManager) int {
	if m == nil || m.sharedKey == "" {
		return 0
	}
	sharedManagerMu.Lock()
	defer sharedManagerMu.Unlock()
	if e, ok := sharedManagers[m.sharedKey]; ok {
		return e.refs
	}
	return 0
}
