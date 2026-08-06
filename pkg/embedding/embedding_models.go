// Types, constructor, and accessor methods for the embedding manager

package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// EmbeddingManager manages the embedding index lifecycle.
// It lazily initializes the ONNX embedding provider and IndexManager
// on first use, and caches them for subsequent calls.
type EmbeddingManager struct {
	mu            sync.Mutex
	provider      EmbeddingProvider
	store         VectorStore
	indexMgr      *IndexManager
	initialized   atomic.Bool // set true only after all fields are written; read lock-free by IsInitialized
	building      bool        // true while BuildIndex is running; guarded by mu
	initError     error       // cached error from failed Init(); guarded by mu
	config        *configuration.EmbeddingIndexConfig
	workspaceRoot string

	// Resolved config values stored during init to avoid re-reading config
	// under lock on every query call (SHOULD_FIX #7).
	threshold  float32
	maxResults int

	// Conversation store (lazy-initialized)
	convoStore *ConversationStore

	// Resolved index directory path (stored during init)
	indexDir string

	// ONNX runtime (held so Close() can release it). When providerShared is
	// true, provider+onnxRuntime came from the process-wide shared cache
	// (acquireSharedONNXProvider) and MUST NOT be closed by this manager —
	// other managers/agents in the same process reference the same instances.
	onnxRuntime    *ONNXRuntime
	providerShared bool

	// closeChan is closed by Close() to signal long-running goroutines
	// (e.g., AutoBuildWhenReady) to abort early.
	closeChan chan struct{}

	// cachedProvider wraps the raw provider with an LRU content-hash cache.
	// This is the provider exposed via GetConversationStore().Provider().
	cachedProvider *cachedProvider

	// Code-specific provider (Jina Code v2) for the dual-model architecture
	// (SP-135). When available, QuerySimilarCode and CheckDuplicates route
	// here instead of the Gemma-backed indexMgr. Both stores are separate:
	// the code index uses the code provider's ModelHash, the Gemma index
	// keeps its own. Falls back to indexMgr (Gemma) when the code model is
	// unavailable or not initialized.
	codeProvider   EmbeddingProvider
	codeStore      VectorStore
	codeIndexMgr   *IndexManager
	codeAvailable  bool

	// sharedKey identifies this manager in the process-wide registry when it
	// came from AcquireManager. Empty for directly-constructed managers, which
	// ReleaseManager then closes outright. Written once at acquisition, before
	// the manager is published to any other goroutine.
	sharedKey string

	// autoBuildOnce keeps AutoBuildWhenReady to one build per manager. A shared
	// manager is handed to every agent on the workspace and each would
	// otherwise kick off its own startup build of the same files.
	autoBuildOnce sync.Once
}

// BuildResult carries the result of a background index build.
type BuildResult struct {
	Stats *IndexStats
	Err   error
}

// NewEmbeddingManager creates a new manager with the given config.
// The manager is NOT initialized until Init() or a query method is called.
func NewEmbeddingManager(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) *EmbeddingManager {
	return &EmbeddingManager{
		config:        cfg,
		workspaceRoot: workspaceRoot,
	}
}

// SetForTesting injects mock provider, store, and indexManager for testing.
// This bypasses Init() so tests can run without an ONNX runtime.
// NOT for production use.
//
// It also resolves indexDir (mirroring the logic in initLocked) so that
// GetConversationStore creates the conversation store in the expected
// location rather than leaking a file into the process working directory.
func (m *EmbeddingManager) SetForTesting(provider EmbeddingProvider, store VectorStore, indexMgr *IndexManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = provider
	m.cachedProvider = newCachedProvider(provider)
	m.store = store
	m.indexMgr = indexMgr
	m.initialized.Store(true)

	// Resolve indexDir using the same logic as initLocked so that
	// GetConversationStore can create the conversation store in the right place.
	m.indexDir = resolveIndexDirFromConfig(m.config, m.workspaceRoot)
}

// IsInitialized returns whether the manager has been initialized.
// Safe to call without holding m.mu — initialized is an atomic so this never
// blocks, even while Init() is running and holding m.mu during ONNX loading.
func (m *EmbeddingManager) IsInitialized() bool {
	return m.initialized.Load()
}

// IndexReadiness is a consistent snapshot of whether the index can answer a
// query, and how completely.
//
// Taken under one lock acquisition on purpose. Composing IsInitialized() +
// IsBuilding() + IndexSize() reads three separate snapshots, so a caller can
// observe "not building" and "0 records" from different instants and conclude
// the index is empty and idle while a build is in fact running.
type IndexReadiness struct {
	Initialized bool
	Building    bool
	Records     int
}

// CanAnswerQueries reports whether a search against this index is meaningful.
// An index with no records cannot distinguish "no such code exists" from
// "nothing has been indexed", and reporting the former is a false negative the
// caller will act on.
func (r IndexReadiness) CanAnswerQueries() bool {
	return r.Initialized && r.Records > 0
}

// Readiness returns a consistent snapshot of the index's ability to serve
// queries.
func (m *EmbeddingManager) Readiness() IndexReadiness {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := IndexReadiness{
		Initialized: m.initialized.Load(),
		Building:    m.building,
	}
	if m.store != nil {
		r.Records = m.store.Size()
	}
	return r
}

// IsBuilding returns true if an index build is currently in progress.
func (m *EmbeddingManager) IsBuilding() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.building
}

// InitError returns the error from a previous failed Init() call, or nil if
// initialization succeeded or has never been attempted.
func (m *EmbeddingManager) InitError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initError
}

// IndexSize returns the number of records in the vector store.
// Returns 0 and a nil error if the manager is not yet initialized.
func (m *EmbeddingManager) IndexSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return 0
	}
	return m.store.Size()
}

// ModelHash returns the active embedding provider's model hash, or "" if no
// provider is currently initialized. Used by tests to re-open persisted stores
// with the same hash so the model-change invalidation logic doesn't wipe them.
func (m *EmbeddingManager) ModelHash() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.provider == nil {
		return ""
	}
	return m.provider.ModelHash()
}

// closeCh returns a channel that is closed when the manager is closed.
// Used by AutoBuildWhenReady to abort the startup sleep if Close() is called.
func (m *EmbeddingManager) closeCh() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeChan == nil {
		m.closeChan = make(chan struct{})
	}
	return m.closeChan
}

// CloseNotify returns a channel that is closed when the manager is closed.
// Long-running goroutines owned by other packages (e.g. agent.MigrateMemories)
// select on this channel so they can abort when DisableEmbeddingIndex tears
// the manager down. The returned channel is the same one internal goroutines
// see, so a single Close() wakes every waiter.
func (m *EmbeddingManager) CloseNotify() <-chan struct{} {
	return m.closeCh()
}

// snapshotIndexMgr returns a reference to the IndexManager under lock.
// This avoids holding the mutex during slow operations (MUST_FIX #1).
func (m *EmbeddingManager) snapshotIndexMgr() (*IndexManager, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized.Load() {
		return nil, fmt.Errorf("embedding: manager not initialized")
	}
	return m.indexMgr, nil
}

// snapshotCodeIndexMgr returns the code-specific IndexManager when the dual-
// model architecture is active (codeProvider available), or falls back to the
// Gemma-backed indexMgr otherwise. The caller (QuerySimilarCode /
// CheckDuplicates) routes to whichever provider is ready.
func (m *EmbeddingManager) snapshotCodeIndexMgr() (*IndexManager, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized.Load() {
		return nil, fmt.Errorf("embedding: manager not initialized")
	}
	if m.codeAvailable && m.codeIndexMgr != nil {
		return m.codeIndexMgr, nil
	}
	return m.indexMgr, nil
}

// snapshotQueryParams returns the resolved threshold and maxResults under lock.
func (m *EmbeddingManager) snapshotQueryParams() (threshold float32, topK int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.threshold, m.maxResults
}

// resolveIndexDirFromConfig resolves the embedding index directory using the
// same precedence as initLocked: explicit config value first, then the
// workspace-scoped default under the data root.
//
// An explicit config IndexDir is honored verbatim — it is per-workspace
// overridable (see configuration.Config merge layering), so scoping it again
// here would move an index the user deliberately placed.
func resolveIndexDirFromConfig(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) string {
	indexDir := ""
	if cfg != nil {
		indexDir = cfg.IndexDir
	}
	if indexDir == "" {
		indexDir = resolveIndexDir(workspaceRoot)
	}
	return indexDir
}

// DefaultIndexDir returns the directory the embedding index for workspaceRoot
// lives in when no explicit IndexDir is configured. Exported so callers outside
// this package (the embedding_index tool, the `sprout embeddings` CLI) resolve
// the same location the manager writes to — resolving it independently off the
// config root produced a second, stale index.
func DefaultIndexDir(workspaceRoot string) string {
	return resolveIndexDir(workspaceRoot)
}

// resolveIndexDir resolves the embedding index directory from the
// $SPROUT_DATA_DIR → XDG → HOME chain, scoped to workspaceRoot.
//
// The per-workspace segment is load-bearing, not cosmetic. envutil.DataDir()
// reads process-global env vars, so without it every EmbeddingManager in a
// process shares one index.hnsw — and the daemon builds one manager per agent,
// per chat session, across every workspace the user opens. Sharing the file
// makes each build treat the other workspaces' records as stale (see the
// staleIDs sweep in IndexManager.BuildIndex) and clobber the shared manifest,
// so every build re-embeds its whole workspace from scratch and deletes its
// neighbor's work. Observed as an index that pinned at a few hundred records
// and never converged, with multi-GB inference spikes from the concurrent
// rebuilds. CLI runs were immune only because SP-116 already points
// SPROUT_DATA_DIR at the workspace's own .sprout/.
//
// An empty workspaceRoot returns the unscoped base, preserving the previous
// layout for callers that genuinely have no workspace.
func resolveIndexDir(workspaceRoot string) string {
	dataDir, err := envutil.DataDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share", "sprout")
	}
	base := filepath.Join(dataDir, "embeddings")
	if slug := workspaceSlug(workspaceRoot); slug != "" {
		return filepath.Join(base, slug)
	}
	return base
}

// workspaceSlug builds a filesystem-safe, collision-free directory name for a
// workspace root: a readable basename plus a hash of the full resolved path, so
// two checkouts of the same-named repo never share an index. Returns "" for an
// empty root.
func workspaceSlug(workspaceRoot string) string {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return ""
	}

	// Resolve symlinks so /var/folders vs /private/var/folders (macOS) and
	// similar aliases map to one index rather than two.
	resolved := workspaceRoot
	if abs, err := filepath.Abs(resolved); err == nil {
		resolved = abs
	}
	if evaled, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = evaled
	}
	resolved = filepath.Clean(resolved)

	sum := sha256.Sum256([]byte(resolved))
	name := sanitizeSlugName(filepath.Base(resolved))
	return name + "-" + hex.EncodeToString(sum[:4])
}

// sanitizeSlugName reduces a path basename to [A-Za-z0-9._-], bounded in
// length, so it is safe as a single path segment on every supported platform.
func sanitizeSlugName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "workspace"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

// Measured alternatives to the shipped q4f16 export, on this repository's own
// code units (M1 Pro, 4 intra-op threads, after length-sorted batching):
//
//	model_q4 (shipped)     ~6.5 units/s   (fp32 activations suit the ORT CPU
//	                                       backend, which has no native fp16
//	                                       kernels)
//	model_q4f16            ~4.4-5.3 units/s
//	model_fp16             ~3.9 units/s
//	model_quantized (int8) ~1.8 units/s
//
// Changing this is not free for users: ModelHash keys the vector store, so a
// swap clears every index and forces a full rebuild, plus a fresh download.
//
// CoreML is NOT a shortcut here despite being available in the Go binding
// (AppendExecutionProviderCoreMLV2). The export uses dynamic sequence length,
// which MIL cannot bound, so the EP shatters the graph — 291 partitions out of
// 1767 nodes — and compiling them exhausted memory before producing a single
// embedding. GPU/ANE would need a fixed-shape (seq-length-bucketed) export.
//
// createONNXProvider returns the process-wide shared ONNX embedding provider
// and its runtime, creating (and downloading the model, if needed) on first
// use. The returned instances are owned by the shared cache — the manager must
// NOT close them (see providerShared and acquireSharedONNXProvider). Sharing
// avoids loading a fresh ~180MB model copy per agent, which matters most for
// the WebUI daemon that builds one agent per chat session.
//
// On WASM the JS bridge (__sproutONNX) handles model loading internally.
func (m *EmbeddingManager) createONNXProvider(ctx context.Context) (EmbeddingProvider, *ONNXRuntime, error) {
	return acquireSharedONNXProvider(ctx, DefaultModelDir(), EmbeddingGemma300MConfig())
}
