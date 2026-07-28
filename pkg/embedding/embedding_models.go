// Types, constructor, and accessor methods for the embedding manager

package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/sprout-foundry/sprout/pkg/configuration"
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
	m.indexDir = resolveIndexDirFromConfig(m.config)
}

// IsInitialized returns whether the manager has been initialized.
// Safe to call without holding m.mu — initialized is an atomic so this never
// blocks, even while Init() is running and holding m.mu during ONNX loading.
func (m *EmbeddingManager) IsInitialized() bool {
	return m.initialized.Load()
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

// snapshotQueryParams returns the resolved threshold and maxResults under lock.
func (m *EmbeddingManager) snapshotQueryParams() (threshold float32, topK int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.threshold, m.maxResults
}

// resolveIndexDirFromConfig resolves the embedding index directory using the
// same precedence as initLocked: explicit config value first, then the
// SPROUT_CONFIG / SPROUT_CONFIG env vars, then the user's default config dir.
func resolveIndexDirFromConfig(cfg *configuration.EmbeddingIndexConfig) string {
	indexDir := ""
	if cfg != nil {
		indexDir = cfg.IndexDir
	}
	if indexDir == "" {
		indexDir = resolveIndexDir()
	}
	return indexDir
}

// resolveIndexDir resolves the embedding index directory from the SPROUT_CONFIG
// or SPROUT_CONFIG environment variables, falling back to the user's default
// config directory. Used by both initLocked and SetForTesting.
func resolveIndexDir() string {
	configDir := os.Getenv("SPROUT_CONFIG")
	if configDir == "" {
		configDir = os.Getenv("XDG_CONFIG_HOME")
	}
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config", "sprout")
	}
	return filepath.Join(configDir, "embeddings")
}

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
