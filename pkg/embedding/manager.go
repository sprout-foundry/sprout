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

	// ONNX provider (lazy-initialized)
	onnxRuntime  *ONNXRuntime
	onnxProvider *ONNXEmbeddingProvider
	onnxStore    *JSONLFileStore
	onnxReady    bool
	onnxError    error // cached error from failed ONNX init
}// NewEmbeddingManager creates a new manager with the given config.
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
		IndexFileLevel: true,  // Enable file-level indexing by default
	})

	m.provider = provider
	m.store = store
	m.indexMgr = indexMgr
	m.initialized = true
	return nil
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

// GetIndexDir returns the resolved index directory path.
// Returns empty string if the manager has not been initialized.
// This is the same directory that stores conversation_turns.jsonl.
func (m *EmbeddingManager) GetIndexDir() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.indexDir
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
// Routes to ONNX provider when available and provider mode is "onnx" or "auto".
func (m *EmbeddingManager) QuerySimilar(ctx context.Context, query string, topK int, threshold float32) ([]QueryResult, error) {
	// If ONNX provider is available and config says to use it, use ONNX store.
	if m.config != nil && (m.config.Provider == "onnx" || m.config.Provider == "auto") {
		onnx := m.getONNXProviderUnlocked()
		if onnx != nil {
			store := m.getONNXStoreUnlocked()
			if store != nil {
				vec, err := onnx.Embed(ctx, query)
				if err == nil {
					return store.Query(vec, topK, threshold)
				}
				// ONNX failed — fall through to static
			}
		}
	}

	// Fall back to static provider.
	if err := m.Init(ctx); err != nil {
		return nil, err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}
	return idx.QuerySimilar(ctx, query, topK, threshold)
}

// ActivateONNX downloads and initializes the ONNX embedding provider.
// Returns nil if already activated, error if activation fails.
// This is a blocking operation: it downloads the model if not present,
// then loads the tokenizer and creates an inference session.
func (m *EmbeddingManager) ActivateONNX(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Already active or already tried and failed.
	if m.onnxReady {
		return nil
	}
	if m.onnxError != nil {
		return m.onnxError
	}

	// Ensure config is present.
	if m.config == nil || m.config.ONNX == nil {
		m.onnxError = fmt.Errorf("embedding: ONNX config not set")
		return m.onnxError
	}
	onnxCfg := m.config.ONNX

	// Ensure base init is done (need indexDir for store).
	if !m.initialized {
		if err := m.initLocked(ctx); err != nil {
			m.onnxError = fmt.Errorf("embedding: init before ONNX: %w", err)
			return m.onnxError
		}
	}

	// Determine model paths.
	modelDir := DefaultModelDir()
	modelName := "embeddinggemma-300m-q8"
	modelPath := filepath.Join(modelDir, modelName, "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, modelName, "tokenizer.json")

	// Download if not present.
	downloader := NewModelDownloaderWithDir(modelDir)
	if !downloader.IsDownloaded(modelName) {
		modelConfig := ModelConfig{
			Name:          modelName,
			ModelURL:      onnxCfg.ModelURL,
			ModelDataURL:  onnxCfg.ModelDataURL,
			TokenizerURL:  onnxCfg.TokenizerURL,
			ModelHash:     onnxCfg.ModelHash,
			TokenizerHash: onnxCfg.TokenizerHash,
		}
		if err := downloader.Download(ctx, modelConfig, nil); err != nil {
			m.onnxError = fmt.Errorf("embedding: download ONNX model: %w", err)
			return m.onnxError
		}
	}

	// Create runtime and provider.
	runtime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		m.onnxError = fmt.Errorf("embedding: create ONNX runtime: %w", err)
		return m.onnxError
	}

	dims := onnxCfg.Dimensions
	if dims <= 0 {
		dims = 256 // default MRL truncation
	}

	provider, err := NewONNXEmbeddingProvider(ctx, runtime, modelPath, tokenizerPath, dims)
	if err != nil {
		runtime.Close()
		m.onnxError = fmt.Errorf("embedding: create ONNX provider: %w", err)
		return m.onnxError
	}

	// Create separate store for ONNX vectors.
	onnxIndexPath := filepath.Join(m.indexDir, "index_onnx.jsonl")
	onnxStore, err := NewJSONLFileStore(onnxIndexPath, provider.ModelHash())
	if err != nil {
		provider.Close()
		runtime.Close()
		m.onnxError = fmt.Errorf("embedding: open ONNX store: %w", err)
		return m.onnxError
	}

	m.onnxRuntime = runtime
	m.onnxProvider = provider
	m.onnxStore = onnxStore
	m.onnxReady = true
	return nil
}

// getONNXProviderUnlocked returns the ONNX provider if active. Caller must hold m.mu.
func (m *EmbeddingManager) getONNXProviderUnlocked() *ONNXEmbeddingProvider {
	if m.onnxReady {
		return m.onnxProvider
	}
	return nil
}

// getONNXStoreUnlocked returns the ONNX store if active. Caller must hold m.mu.
func (m *EmbeddingManager) getONNXStoreUnlocked() *JSONLFileStore {
	if m.onnxReady {
		return m.onnxStore
	}
	return nil
}

// GetConversationStore returns the conversation store, creating it lazily on first use.
// When ONNX provider is active and configured, uses ONNX-backed store at
// {indexDir}/conversation_turns_onnx.jsonl. Otherwise falls back to static provider
// at {indexDir}/conversation_turns.jsonl.
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

	// Try ONNX provider first if available and configured
	if m.config != nil && (m.config.Provider == "onnx" || m.config.Provider == "auto") {
		if m.onnxProvider != nil {
			convoPath := filepath.Join(m.indexDir, "conversation_turns_onnx.jsonl")
			store, err := NewConversationStore(m.onnxProvider, convoPath, m.onnxProvider.ModelHash())
			if err == nil {
				m.convoStore = store
				return store, nil
			}
			// ONNX store creation failed — fall through to static
		}
	}

	// Fall back to static provider
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
	if !m.initialized && !m.onnxReady {
		return nil
	}
	var firstErr error
	if m.convoStore != nil {
		if err := m.convoStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.convoStore = nil
	}
	if m.onnxRuntime != nil {
		if err := m.onnxRuntime.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.onnxRuntime = nil
	}
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
	m.initialized = false
	m.onnxReady = false
	m.initError = nil
	return firstErr
}
