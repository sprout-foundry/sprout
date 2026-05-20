package embedding

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// EmbeddingManager manages the embedding index lifecycle.
// It lazily initializes the static embedding provider and IndexManager
// on first use, and caches them for subsequent calls.
type EmbeddingManager struct {
	mu            sync.Mutex
	provider      *StaticProvider
	store         *JSONLFileStore
	indexMgr      *IndexManager
	initialized   bool
	building      bool // true while BuildIndex is running
	initError     error // cached error from failed Init()
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

	// ONNX provider (lazy-initialized, higher quality embeddings).
	// Only available when the native ONNX runtime is present (!wasm builds
	// with CGO). On WASM or when the model is not downloaded, these remain
	// nil and the manager falls back to the static provider.
	onnxRuntime  *ONNXRuntime
	onnxProvider *ONNXEmbeddingProvider
	onnxStore    *JSONLFileStore
	onnxReady    bool
	onnxError    error // cached error from failed ONNX init
}

// NewEmbeddingManager creates a new manager with the given config.
// The manager is NOT initialized until Init() or a query method is called.
func NewEmbeddingManager(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) *EmbeddingManager {
	return &EmbeddingManager{
		config:        cfg,
		workspaceRoot: workspaceRoot,
	}
}

// Init initializes the static embedding provider and opens the vector store.
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
	if m.initialized {
		return nil
	}

	// Handle nil config gracefully
	if m.config == nil {
		m.config = &configuration.EmbeddingIndexConfig{}
	}

	// Initialize static embedding provider
	provider, err := NewStaticProvider()
	if err != nil {
		m.initError = fmt.Errorf("embedding: init provider: %w", err)
		return m.initError
	}

	// Resolve index directory
	indexDir := m.config.IndexDir
	if indexDir == "" {
		configDir := os.Getenv("SPROUT_CONFIG")
		if configDir == "" {
			configDir = os.Getenv("LEDIT_CONFIG")
		}
		if configDir == "" {
			home, _ := os.UserHomeDir()
			configDir = filepath.Join(home, ".config", "sprout")
		}
		indexDir = filepath.Join(configDir, "embeddings")
	}

	// Store the resolved index directory for reuse
	m.indexDir = indexDir

	// Create workspace-specific index file
	indexFile := filepath.Join(indexDir, "index.jsonl")
	store, err := NewJSONLFileStore(indexFile, provider.ModelHash())
	if err != nil {
		provider.Close()
		m.initError = fmt.Errorf("embedding: open store: %w", err)
		return m.initError
	}

	// Store resolved threshold and maxResults as fields (SHOULD_FIX #7).
	m.threshold = m.config.SimilarityThreshold
	if m.threshold == 0 {
		m.threshold = 0.90
	}

	m.maxResults = m.config.MaxResults
	if m.maxResults == 0 {
		m.maxResults = 3
	}

	indexMgr := NewIndexManager(provider, store, IndexOptions{
		BatchSize:      32,
		MaxBodyLen:     2000,
		IndexFileLevel: true, // Enable file-level indexing by default
	})

	m.provider = provider
	m.store = store
	m.indexMgr = indexMgr
	m.initialized = true

	// Try to initialize ONNX provider in background (non-blocking).
	// Errors are cached in m.onnxError and will be logged on first access.
	go func() {
		if err := m.initONNX(ctx); err != nil {
			m.mu.Lock()
			m.onnxError = err
			m.mu.Unlock()
			log.Printf("embedding: ONNX provider init failed (will use static): %v", err)
		}
	}()

	return nil
}

// initONNX lazily initializes the ONNX embedding provider and its index.
// Must be called with m.mu NOT held (it acquires the lock internally).
// Errors are cached in m.onnxError to avoid repeated failures.
// This is safe to call multiple times and from multiple goroutines.
func (m *EmbeddingManager) initONNX(ctx context.Context) error {
	m.mu.Lock()
	// Fast path: already initialized or already failed
	if m.onnxReady {
		m.mu.Unlock()
		return nil
	}
	if m.onnxError != nil {
		m.mu.Unlock()
		return m.onnxError
	}
	m.mu.Unlock()

	// Get the default model directory
	modelDir := DefaultModelDir()

	// Create ONNX runtime
	runtime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		return fmt.Errorf("onnx: create runtime: %w", err)
	}

	// Load the pre-registered EmbeddingGemma-2-925M config
	modelConfig := EmbeddingGemma2925MConfig()

	// Check if model file exists; if not, download it
	modelName := modelConfig.Name
	modelPath := filepath.Join(modelDir, modelName, "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, modelName, "tokenizer.json")

	if _, err := os.Stat(modelPath); err != nil {
		log.Printf("embedding: downloading ONNX model %s...", modelName)
		if err := DownloadModel(ctx, modelDir, modelConfig); err != nil {
			runtime.Close()
			return fmt.Errorf("onnx: download model: %w", err)
		}
		log.Printf("embedding: ONNX model %s downloaded", modelName)
	}

	// Create ONNX embedding provider (EmbeddingGemma-2-925M outputs 768-dim vectors).
	provider, err := NewONNXEmbeddingProvider(ctx, runtime, modelPath, tokenizerPath, 768)
	if err != nil {
		runtime.Close()
		return fmt.Errorf("onnx: create provider: %w", err)
	}

	// Open ONNX JSONL store
	onnxStore, err := NewJSONLFileStore(
		filepath.Join(m.indexDir, "embedding_index_onnx.jsonl"),
		provider.ModelHash(),
	)
	if err != nil {
		provider.Close()
		runtime.Close()
		return fmt.Errorf("onnx: open store: %w", err)
	}

	// All successful — store references under lock
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onnxRuntime = runtime
	m.onnxProvider = provider
	m.onnxStore = onnxStore
	m.onnxReady = true
	m.onnxError = nil

	return nil
}

// ensureONNX calls initONNX and returns the cached error (nil if ready).
func (m *EmbeddingManager) ensureONNX(ctx context.Context) error {
	if err := m.initONNX(ctx); err != nil {
		return err
	}
	return nil
}

// isONNXReady returns true if the ONNX provider is initialized and ready.
func (m *EmbeddingManager) isONNXReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.onnxReady
}

// snapshotIndexMgr returns a reference to the IndexManager under lock.
// This avoids holding the mutex during slow operations (MUST_FIX #1).
func (m *EmbeddingManager) snapshotIndexMgr() (*IndexManager, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
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

// IsInitialized returns whether the manager has been initialized.
func (m *EmbeddingManager) IsInitialized() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initialized
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
	stats, err := idx.BuildIndex(ctx, m.workspaceRoot)

	// Also build the ONNX index if available.
	// This uses higher-quality embeddings but is slower.
	if m.isONNXReady() {
		log.Printf("embedding: building ONNX index...")
		if err := m.buildONNXIndex(ctx); err != nil {
			log.Printf("embedding: ONNX index build failed (static index OK): %v", err)
			// Don't fail the whole build for ONNX issues.
		}
	}

	return stats, err
}

// buildONNXIndex builds the ONNX embedding index for the workspace.
// Requires the ONNX provider to be initialized.
func (m *EmbeddingManager) buildONNXIndex(ctx context.Context) error {
	if err := m.ensureONNX(ctx); err != nil {
		return fmt.Errorf("onnx: not ready: %w", err)
	}

	onnxMgr := NewIndexManager(m.onnxProvider, m.onnxStore, IndexOptions{
		BatchSize:      32,
		MaxBodyLen:     2000,
		IndexFileLevel: true,
	})

	_, err := onnxMgr.BuildIndex(ctx, m.workspaceRoot)
	if err != nil {
		return fmt.Errorf("onnx: build index: %w", err)
	}
	return nil
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

		ctx, cancel := context.WithTimeout(ctx, WalkTimeout)
		defer cancel()

		if err := m.Init(ctx); err != nil {
			ch <- &BuildResult{Err: err}
			return
		}

		stats, err := m.buildIndexLocked(ctx)
		ch <- &BuildResult{
			Stats: stats,
			Err:  err,
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
func (m *EmbeddingManager) AutoBuildWhenReady() {
	// Wait a few seconds so we don't compete with startup I/O.
	time.Sleep(3 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stats, err := m.BuildIndex(ctx)
	if err != nil {
		log.Printf("embedding: auto-build failed: %v", err)
		return
	}
	log.Printf("embedding: auto-build complete: %d files, %d units in %s",
		stats.FilesProcessed, stats.UnitsExtracted, stats.Duration)
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
// Uses the static provider only (ONNX not needed for duplicate detection).
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
// Uses the static provider. If the ONNX provider is also available,
// it searches both indexes and merges results (deduplicated by path).
func (m *EmbeddingManager) QuerySimilar(ctx context.Context, query string, topK int, threshold float32) ([]QueryResult, error) {
	if err := m.Init(ctx); err != nil {
		return nil, err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}

	// Search static index
	staticResults, err := idx.QuerySimilar(ctx, query, topK, threshold)
	if err != nil {
		return nil, err
	}

	// If ONNX is available, search it too and merge results.
	if m.isONNXReady() {
		onnxMgr := NewIndexManager(m.onnxProvider, m.onnxStore, IndexOptions{
			BatchSize:      32,
			MaxBodyLen:     2000,
			IndexFileLevel: true,
		})
		onnxResults, err := onnxMgr.QuerySimilar(ctx, query, topK, threshold)
		if err != nil {
			// ONNX search failed, fall back to static only.
			return staticResults, nil
		}
		// Merge: return union of both, deduplicated by file path + line range.
		return mergeQueryResults(staticResults, onnxResults), nil
	}

	return staticResults, nil
}

// mergeQueryResults merges two result sets, deduplicating by path+line and
// keeping the highest-scoring entry for each unique match.
func mergeQueryResults(a, b []QueryResult) []QueryResult {
	seen := make(map[string]*QueryResult)
	for _, r := range a {
		key := r.Record.File + ":" + fmt.Sprintf("%d", r.Record.StartLine)
		if _, exists := seen[key]; !exists {
			cp := r
			seen[key] = &cp
		} else if r.Similarity > seen[key].Similarity {
			seen[key] = &r
		}
	}
	for _, r := range b {
		key := r.Record.File + ":" + fmt.Sprintf("%d", r.Record.StartLine)
		if existing, exists := seen[key]; !exists {
			cp := r
			seen[key] = &cp
		} else if r.Similarity > existing.Similarity {
			seen[key] = &r
		}
	}
	results := make([]QueryResult, 0, len(seen))
	for _, r := range seen {
		results = append(results, *r)
	}
	// Sort by similarity descending.
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Similarity > results[j-1].Similarity; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
	return results
}

// GetConversationStore returns the conversation store, creating it lazily on first use.
// The store is user-scoped and lives at {indexDir}/conversation_turns.jsonl.
// Multiple calls return the same instance.
// Uses the static provider only (no ONNX conversation indexing for now).
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

	// Create conversation store in the same directory as the main index
	convoPath := filepath.Join(m.indexDir, "conversation_turns.jsonl")
	convoStore, err := NewConversationStore(m.provider, convoPath, m.provider.ModelHash())
	if err != nil {
		return nil, fmt.Errorf("embedding: create conversation store: %w", err)
	}

	m.convoStore = convoStore
	return convoStore, nil
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

	// Close static provider and store
	if m.provider != nil {
		if err := m.provider.Close(); err != nil && firstErr == nil {
			firstErr = err
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

	// Close ONNX resources
	if m.onnxProvider != nil {
		if err := m.onnxProvider.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.onnxProvider = nil
	}
	if m.onnxStore != nil {
		if err := m.onnxStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.onnxStore = nil
	}
	if m.onnxRuntime != nil {
		if err := m.onnxRuntime.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.onnxRuntime = nil
	}

	m.initialized = false
	m.onnxReady = false
	m.onnxError = nil
	m.initError = nil

	return firstErr
}
