package embedding

import (
	"container/list"
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
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

const maxEmbedCacheEntries = 1024

// cachedProvider wraps an EmbeddingProvider with a content-hash cache.
// Identical text inputs return the cached vector without re-embedding.
//
// The eviction order is a FIFO backed by container/list (doubly-linked
// list), NOT a slice. A reslicing slice (`s = s[1:]`) leaks its backing
// array: the array's capacity only grows over the cache's lifetime, so a
// long-running daemon would accumulate a ~1M-entry string-header array
// (~15MB) holding only 1024 live entries. The list's nodes are GC'd on
// eviction, keeping the cache's memory footprint strictly bounded.
type cachedProvider struct {
	inner     EmbeddingProvider
	cache     map[string][]float32
	cacheMu   sync.Mutex
	cacheList *list.List // FIFO eviction order; *list.Element is the map value
}

func newCachedProvider(inner EmbeddingProvider) *cachedProvider {
	return &cachedProvider{
		inner:     inner,
		cache:     make(map[string][]float32),
		cacheList: list.New(),
	}
}

func contentHash(text string) string {
	// Non-cryptographic cache key. md5 is used purely for its uniform
	// 128-bit distribution as a map key; collision resistance is irrelevant
	// (worst case is a harmless cache miss on collision).
	return fmt.Sprintf("%x", md5.Sum([]byte(text)))
}

func (c *cachedProvider) evictIfFull() {
	for c.cacheList.Len() >= maxEmbedCacheEntries {
		oldest := c.cacheList.Front()
		if oldest == nil {
			break
		}
		h := oldest.Value.(string)
		c.cacheList.Remove(oldest)
		delete(c.cache, h)
	}
}

func (c *cachedProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	h := contentHash(text)
	c.cacheMu.Lock()
	if v, ok := c.cache[h]; ok {
		// Return a copy so the caller can't mutate the cached vector.
		res := make([]float32, len(v))
		copy(res, v)
		c.cacheMu.Unlock()
		return res, nil
	}
	c.cacheMu.Unlock()

	vec, err := c.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	// Double-check: another goroutine may have populated the cache while we
	// were embedding. Return a copy of the cached entry so callers can't
	// mutate the shared vector.
	if v, ok := c.cache[h]; ok {
		res := make([]float32, len(v))
		copy(res, v)
		return res, nil
	}
	c.evictIfFull()
	// Store a private copy in the cache; return the original to the caller.
	// If we cached vec itself, a mutating caller would corrupt the cache.
	cached := make([]float32, len(vec))
	copy(cached, vec)
	c.cache[h] = cached
	c.cacheList.PushBack(h)
	return vec, nil
}

func (c *cachedProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	// Check cache for each text; collect misses.
	type miss struct {
		index int
		text  string
	}
	hits := make([][]float32, len(texts))
	var misses []miss

	for i, text := range texts {
		h := contentHash(text)
		c.cacheMu.Lock()
		if v, ok := c.cache[h]; ok {
			res := make([]float32, len(v))
			copy(res, v)
			hits[i] = res
			c.cacheMu.Unlock()
			continue
		}
		c.cacheMu.Unlock()
		misses = append(misses, miss{index: i, text: text})
	}

	if len(misses) > 0 {
		batchTexts := make([]string, len(misses))
		for i, m := range misses {
			batchTexts[i] = m.text
		}
		results, err := c.inner.EmbedBatch(ctx, batchTexts)
		if err != nil {
			return nil, err
		}

		c.cacheMu.Lock()
		for i, m := range misses {
			h := contentHash(m.text)
			// Double-check on miss.
			if v, ok := c.cache[h]; ok {
				res := make([]float32, len(v))
				copy(res, v)
				hits[m.index] = res
			} else {
				vec := results[i]
				c.evictIfFull()
				// Cache a private copy; return the original to the caller.
				cached := make([]float32, len(vec))
				copy(cached, vec)
				c.cache[h] = cached
				c.cacheList.PushBack(h)
				hits[m.index] = vec
			}
		}
		c.cacheMu.Unlock()
	}

	return hits, nil
}

func (c *cachedProvider) Dimensions() int {
	return c.inner.Dimensions()
}

func (c *cachedProvider) Name() string {
	return c.inner.Name()
}

func (c *cachedProvider) ModelHash() string {
	return c.inner.ModelHash()
}

// EmbedWithPrefix is intentionally uncached: prefix embedding is used by the
// code-index search path (index.go QuerySimilar), which goes through the
// IndexManager's own provider reference, NOT this cachedProvider. Only the
// conversation store (EmbedAndStoreTurn, rollup embedding, semantic recall,
// proactive context) flows through the cached wrapper, and those paths call
// Embed/EmbedBatch without a prefix. Caching prefixed calls with a composite
// key would add complexity for a path that never re-embeds the same text.
func (c *cachedProvider) EmbedWithPrefix(ctx context.Context, text string, prefix string) ([]float32, error) {
	return c.inner.EmbedWithPrefix(ctx, text, prefix)
}

func (c *cachedProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	return c.inner.EmbedBatchWithPrefix(ctx, texts, prefix)
}

func (c *cachedProvider) Close() error {
	return nil // the inner provider is managed by the EmbeddingManager
}

// NewEmbeddingManager creates a new manager with the given config.
// The manager is NOT initialized until Init() or a query method is called.
func NewEmbeddingManager(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) *EmbeddingManager {
	return &EmbeddingManager{
		config:        cfg,
		workspaceRoot: workspaceRoot,
	}
}

// Init initializes the ONNX embedding provider and opens the vector store.
// This is idempotent — calling it multiple times is safe.
// If a previous Init() failed, the cached error is returned immediately.
func (m *EmbeddingManager) Init(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If we already tried and failed, return the cached error.
	if m.initError != nil {
		return m.initError
	}

	return m.initLocked(ctx)
}

// initLocked performs the actual initialization. The caller must hold m.mu.
func (m *EmbeddingManager) initLocked(ctx context.Context) error {
	if m.initialized.Load() {
		return nil
	}

	// Handle nil config gracefully
	if m.config == nil {
		m.config = &configuration.EmbeddingIndexConfig{}
	}

	// Resolve index directory
	m.indexDir = resolveIndexDirFromConfig(m.config)

	// Store resolved threshold and maxResults as fields (SHOULD_FIX #7).
	m.threshold = m.config.SimilarityThreshold
	if m.threshold == 0 {
		m.threshold = 0.90
	}

	m.maxResults = m.config.MaxResults
	if m.maxResults == 0 {
		m.maxResults = 3
	}

	// Create ONNX embedding provider as the sole provider. provider+runtime
	// are owned by the shared cache; do not Close them on any failure path.
	provider, runtime, err := m.createONNXProvider(ctx)
	if err != nil {
		m.initError = fmt.Errorf("embedding: init provider: %w", err)
		return m.initError
	}

	// Open vector store with the ONNX provider's model hash
	store, err := NewHNSWStore(filepath.Join(m.indexDir, "index.hnsw"), provider.ModelHash())
	if err != nil {
		m.initError = fmt.Errorf("embedding: open store: %w", err)
		return m.initError
	}

	indexMgr := NewIndexManager(provider, store, IndexOptions{
		BatchSize:      32,
		MaxBodyLen:     2000,
		IndexFileLevel: true, // Enable file-level indexing by default
		ManifestPath:   filepath.Join(m.indexDir, ".index.hnsw.manifest.json"),
	})

	m.provider = provider
	m.cachedProvider = newCachedProvider(provider)
	m.onnxRuntime = runtime
	m.providerShared = true
	m.store = store
	m.indexMgr = indexMgr
	// Store true last so concurrent IsInitialized() reads cannot observe a
	// partially-initialized manager (all other fields are written above under m.mu).
	m.initialized.Store(true)

	return nil
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
// SPROUT_CONFIG / LEDIT_CONFIG env vars, then the user's default config dir.
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
// or LEDIT_CONFIG environment variables, falling back to the user's default
// config directory. Used by both initLocked and SetForTesting.
func resolveIndexDir() string {
	configDir := os.Getenv("SPROUT_CONFIG")
	if configDir == "" {
		configDir = os.Getenv("LEDIT_CONFIG")
	}
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config", "sprout")
	}
	return filepath.Join(configDir, "embeddings")
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

// BuildIndex runs a full index build for the workspace.
// It acquires the building lock, validates workspace size, and delegates to
// buildIndexLocked for the actual work.
func (m *EmbeddingManager) BuildIndex(ctx context.Context) (*IndexStats, error) {
	m.mu.Lock()
	if m.building {
		m.mu.Unlock()
		return nil, fmt.Errorf("embedding: build already in progress")
	}
	m.building = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.building = false
		m.mu.Unlock()
	}()

	return m.buildIndexLocked(ctx)
}

// buildIndexLocked performs the actual index build. The caller must have
// already acquired the building lock. Used by both BuildIndex and
// BuildIndexBackground to avoid the deadlock of calling BuildIndex from
// a path that already set the building flag.
func (m *EmbeddingManager) buildIndexLocked(ctx context.Context) (*IndexStats, error) {
	if err := m.Init(ctx); err != nil {
		return nil, err
	}

	// Safety: refuse to index a user's home directory.
	// In daemon/service mode workspaceRoot may be set to the home dir,
	// and walking it would index private keys, credentials, media, etc.
	if filesystem.IsHomeDir(m.workspaceRoot) {
		return nil, fmt.Errorf("embedding: refusing to index home directory %q — set workspace_root to a project directory instead", m.workspaceRoot)
	}

	// Safety: skip if workspace is too large for auto-build.
	files, err := WalkCodeFiles(ctx, m.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("embedding: scan workspace: %w", err)
	}
	if len(files) > MaxFileCount {
		return nil, fmt.Errorf("embedding: workspace has %d files (max %d for auto-build)", len(files), MaxFileCount)
	}

	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}
	return idx.BuildIndex(ctx, m.workspaceRoot)
}

// BuildIndexBackground starts an index build in a background goroutine and
// returns a channel on which the result (or error) will be delivered. This
// must be used when called from HTTP handlers or other code paths where
// blocking would cause a timeout.
//
// The returned channel is non-buffered and the caller should read from it
// once to retrieve the result. The context passed to the caller is used for
// cancellation; if the context is cancelled, the build is interrupted
// gracefully (partial results may be stored).
func (m *EmbeddingManager) BuildIndexBackground(ctx context.Context) <-chan *BuildResult {
	ch := make(chan *BuildResult, 1)

	m.mu.Lock()
	if m.building {
		m.mu.Unlock()
		ch <- &BuildResult{
			Err: fmt.Errorf("embedding: build already in progress"),
		}
		return ch
	}
	m.building = true
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			m.building = false
			m.mu.Unlock()
		}()

		// Honor the manager's close signal before doing any work. Without
		// this check, a DisableEmbeddingIndex call arriving after the
		// goroutine is launched would not abort the 10-minute WalkTimeout
		// nor the Init/build work; the goroutine would either run to
		// completion against torn-down state or hit ErrStoreClosed deep
		// in the embedder. Surface the close as the result instead.
		select {
		case <-m.closeCh():
			ch <- &BuildResult{Err: ErrStoreClosed}
			return
		default:
		}

		ctx, cancel := context.WithTimeout(ctx, WalkTimeout)
		defer cancel()

		if err := m.Init(ctx); err != nil {
			ch <- &BuildResult{Err: err}
			return
		}

		stats, err := m.buildIndexLocked(ctx)
		ch <- &BuildResult{
			Stats: stats,
			Err:   err,
		}
	}()

	return ch
}

// BuildResult carries the result of a background index build.
type BuildResult struct {
	Stats *IndexStats
	Err   error
}

// AutoBuildWhenReady runs a background index build after a short delay.
// This is called at agent startup so the index is ready for duplicate
// detection and context enrichment without waiting for an explicit query.
// A 2-minute timeout prevents the build from hanging indefinitely.
//
// Two teardown paths are honored so a DisableEmbeddingIndex call arriving
// during the startup sleep (or during Init/Build) does not race into a
// closed store and panic:
//
//  1. The 3-second startup sleep selects on m.closeCh() so Close() can
//     wake it early.
//  2. After the sleep returns, m.closeCh() is re-checked *before* the
//     BuildIndex call. This catches the case where Close() ran while the
//     sleep was in flight (sleep saw the wake-up but the goroutine still
//     proceeded because the select picked the timer branch first).
//
// As a last line of defense, HNSWStore.Store/ReplaceAll/DeleteByFile/
// DeleteByIDs/Save return ErrStoreClosed instead of panicking on a nil
// records map if the goroutine still loses the race.
func (m *EmbeddingManager) AutoBuildWhenReady() {
	// Wait a few seconds so we don't compete with startup I/O.
	// Use a select-based timer so Close() can wake us early.
	select {
	case <-time.After(3 * time.Second):
	case <-m.closeCh():
		return
	}

	// Re-check the close signal before doing any work. The select above
	// can return through either branch when both fire concurrently; if
	// Close() ran while we were waking up, bail before reaching into
	// m.store (which Close() has already nulled out).
	select {
	case <-m.closeCh():
		return
	default:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stats, err := m.BuildIndex(ctx)
	if err != nil {
		debugLogf("embedding: auto-build failed: %v", err)
		return
	}
	debugLogf("embedding: auto-build complete: %d files, %d units in %s",
		stats.FilesProcessed, stats.UnitsExtracted, stats.Duration)
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

// UpdateFile incrementally updates the index for a single file.
func (m *EmbeddingManager) UpdateFile(ctx context.Context, filePath string) error {
	if err := m.Init(ctx); err != nil {
		return err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return err
	}
	return idx.UpdateFile(ctx, filePath)
}

// UpdateFromGitDiff incrementally updates the index by examining git-tracked
// files that have changed, been added, or been created since the last build.
func (m *EmbeddingManager) UpdateFromGitDiff(ctx context.Context) (*IndexStats, error) {
	if err := m.Init(ctx); err != nil {
		return nil, err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}
	return idx.UpdateFromGitDiff(ctx, m.workspaceRoot)
}

// CheckDuplicates checks if file content duplicates existing code.
func (m *EmbeddingManager) CheckDuplicates(ctx context.Context, filePath string, content string) (*CheckDuplicatesResult, error) {
	if err := m.Init(ctx); err != nil {
		return nil, err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}
	threshold, topK := m.snapshotQueryParams()
	return CheckFileForDuplicates(ctx, idx, filePath, content, m.workspaceRoot, threshold, topK)
}

// QuerySimilar searches for code similar to the given query text.
func (m *EmbeddingManager) QuerySimilar(ctx context.Context, query string, topK int, threshold float32) ([]QueryResult, error) {
	if err := m.Init(ctx); err != nil {
		return nil, err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}
	return idx.QuerySimilar(ctx, query, topK, threshold)
}

// GetConversationStore returns the conversation store, creating it lazily on first use.
// The store is user-scoped and lives at {indexDir}/conversation_turns.hnsw.
// Multiple calls return the same instance.
func (m *EmbeddingManager) GetConversationStore(ctx context.Context) (*ConversationStore, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Return cached instance if already created
	if m.convoStore != nil {
		return m.convoStore, nil
	}

	// Match Init() behavior: return cached error if a prior init failed
	if m.initError != nil {
		return nil, m.initError
	}

	// Ensure the manager itself is initialized
	if err := m.initLocked(ctx); err != nil {
		return nil, err
	}

	// Create conversation store with the cached provider so that all
	// Embed/EmbedBatch calls (turn embedding, rollup embedding, proactive
	// context, semantic recall) benefit from the content-hash cache.
	convoPath := filepath.Join(m.indexDir, "conversation_turns.hnsw")
	convoStore, err := NewConversationStore(m.cachedProvider, convoPath, m.provider.ModelHash())
	if err != nil {
		return nil, fmt.Errorf("embedding: create conversation store: %w", err)
	}

	m.convoStore = convoStore
	return convoStore, nil
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

// Close releases all resources.
func (m *EmbeddingManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error

	// Close conversation store
	if m.convoStore != nil {
		if err := m.convoStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.convoStore = nil
	}

	// Release provider/runtime references. When providerShared is true they are
	// owned by the process-wide shared cache (acquireSharedONNXProvider) and
	// other managers still reference them, so we drop our reference WITHOUT
	// closing — closing would tear down a session the rest of the process is
	// using. The shared instances intentionally live for the process lifetime.
	if m.provider != nil {
		if !m.providerShared {
			if err := m.provider.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		m.provider = nil
	}
	if m.store != nil {
		if err := m.store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.store = nil
	}
	m.indexMgr = nil

	// Drop the cachedProvider reference so the underlying provider (and
	// its internal state) can be GC'd. Without this, m.cachedProvider
	// outlives Close() and pins the (now-closed) provider alive for the
	// remainder of the manager's lifetime.
	m.cachedProvider = nil

	if m.onnxRuntime != nil {
		if !m.providerShared {
			if err := m.onnxRuntime.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		m.onnxRuntime = nil
	}
	m.providerShared = false

	m.initialized.Store(false)
	m.initError = nil // cleared to allow re-initialization after Close()

	// Signal long-running goroutines to abort.
	//
	// closeChan is lazily created by closeCh()/CloseNotify() on first read.
	// If a goroutine launched before Close() reached its first call to
	// closeCh() — and Close() acquired m.mu first — closeChan would be
	// nil at this point and the goroutine would sleep past its abort
	// signal. Eagerly create the channel here under the same lock so the
	// close is unconditional, even if no reader has materialized yet.
	if m.closeChan == nil {
		m.closeChan = make(chan struct{})
	}
	select {
	case <-m.closeChan:
		// Already closed
	default:
		close(m.closeChan)
	}

	return firstErr
}

// ClearEmbeddingFiles removes embedding index files from the given directory.
// fileType should be one of: "code", "conversation_turn", "memory", "all".
// For "memory", it clears the same files as "conversation_turn" since memories
// are stored in the conversation_turns index alongside conversation turns.
// Returns the number of files actually deleted.
func ClearEmbeddingFiles(indexDir string, fileType string) (int, error) {
	switch fileType {
	case "code":
		return clearCodeEmbeddingFiles(indexDir)
	case "conversation_turn":
		return clearConversationEmbeddingFiles(indexDir)
	case "memory":
		// Memories are stored in the same conversation_turns files
		return clearConversationEmbeddingFiles(indexDir)
	case "all":
		codeCount, err := clearCodeEmbeddingFiles(indexDir)
		if err != nil {
			return codeCount, err
		}
		convCount, err := clearConversationEmbeddingFiles(indexDir)
		if err != nil {
			return codeCount + convCount, err
		}
		return codeCount + convCount, nil
	default:
		return 0, fmt.Errorf("invalid file type %q: valid options are code, conversation_turn, memory, all", fileType)
	}
}

func clearCodeEmbeddingFiles(indexDir string) (int, error) {
	files := []string{
		filepath.Join(indexDir, "index.hnsw"),
		filepath.Join(indexDir, "index.hnsw.meta"),
		filepath.Join(indexDir, "index.hnsw.records.json"),
	}
	return removeFilesSilently(files)
}

func clearConversationEmbeddingFiles(indexDir string) (int, error) {
	files := []string{
		filepath.Join(indexDir, "conversation_turns.hnsw"),
		filepath.Join(indexDir, "conversation_turns.hnsw.meta"),
		filepath.Join(indexDir, "conversation_turns.hnsw.records.json"),
	}
	return removeFilesSilently(files)
}

func removeFilesSilently(files []string) (int, error) {
	deleted := 0
	for _, f := range files {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return deleted, fmt.Errorf("failed to remove %s: %w", f, err)
		} else if err == nil {
			deleted++
		}
	}
	return deleted, nil
}
