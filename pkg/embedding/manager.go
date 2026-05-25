package embedding

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

	// ONNX runtime (held so Close() can release it)
	onnxRuntime *ONNXRuntime
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
	if m.initialized {
		return nil
	}

	// Handle nil config gracefully
	if m.config == nil {
		m.config = &configuration.EmbeddingIndexConfig{}
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

	// Store resolved threshold and maxResults as fields (SHOULD_FIX #7).
	m.threshold = m.config.SimilarityThreshold
	if m.threshold == 0 {
		m.threshold = 0.90
	}

	m.maxResults = m.config.MaxResults
	if m.maxResults == 0 {
		m.maxResults = 3
	}

	// Create ONNX embedding provider as the sole provider.
	provider, runtime, err := m.createONNXProvider(ctx, indexDir)
	if err != nil {
		m.initError = fmt.Errorf("embedding: init provider: %w", err)
		return m.initError
	}

	// Open vector store with the ONNX provider's model hash
	store, err := NewHNSWStore(filepath.Join(indexDir, "index.hnsw"), provider.ModelHash())
	if err != nil {
		provider.Close()
		runtime.Close()
		m.initError = fmt.Errorf("embedding: open store: %w", err)
		return m.initError
	}

	indexMgr := NewIndexManager(provider, store, IndexOptions{
		BatchSize:      32,
		MaxBodyLen:     2000,
		IndexFileLevel: true, // Enable file-level indexing by default
		ManifestPath:   filepath.Join(indexDir, ".index.hnsw.manifest.json"),
	})

	m.provider = provider
	m.onnxRuntime = runtime
	m.store = store
	m.indexMgr = indexMgr
	m.initialized = true

	return nil
}

// createONNXProvider creates an ONNX embedding provider, downloading the
// model if needed. Returns the provider and its underlying runtime (so the
// caller can Close the runtime on failure).
//
// On non-WASM builds the model is loaded from disk and downloaded if
// missing. On WASM the JS bridge (__sproutONNX) handles model loading
// internally — if the bridge is absent, the call fails with a clear error.
func (m *EmbeddingManager) createONNXProvider(ctx context.Context, indexDir string) (EmbeddingProvider, *ONNXRuntime, error) {
	// If no ONNX backend is available at all (stub build), fail fast.
	if !onnxAvailable {
		return nil, nil, fmt.Errorf("ONNX runtime not available in this build (requires CGO or WASM bridge)")
	}

	modelDir := DefaultModelDir()

	// Create ONNX runtime
	runtime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		return nil, nil, fmt.Errorf("onnx: create runtime: %w", err)
	}

	// Load the pre-registered EmbeddingGemma-300M config
	modelConfig := EmbeddingGemma300MConfig()
	modelName := modelConfig.Name
	modelPath := filepath.Join(modelDir, modelName, "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, modelName, "tokenizer.json")

	// Native builds load .onnx from disk; download it if missing.
	// The WASM build delegates to a JS-side provider that owns its own model
	// loading, so we skip the on-disk file check there — see
	// onnxRequiresModelFiles in onnx_runtime.go / onnx_wasm.go.
	if onnxRequiresModelFiles() {
		if _, err := os.Stat(modelPath); err != nil {
			log.Printf("embedding: downloading ONNX model %s...", modelName)
			if err := DownloadModel(ctx, modelDir, modelConfig); err != nil {
				runtime.Close()
				return nil, nil, fmt.Errorf("onnx: download model: %w", err)
			}
			log.Printf("embedding: ONNX model %s downloaded", modelName)
		}
	}

	// Create ONNX embedding provider (EmbeddingGemma-300M outputs 768-dim vectors).
	provider, err := NewONNXEmbeddingProvider(ctx, runtime, modelPath, tokenizerPath, 768)
	if err != nil {
		runtime.Close()
		return nil, nil, fmt.Errorf("onnx: create provider: %w", err)
	}

	return provider, runtime, nil
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

// SetForTesting injects mock provider, store, and indexManager for testing.
// This bypasses Init() so tests can run without an ONNX runtime.
// NOT for production use.
func (m *EmbeddingManager) SetForTesting(provider EmbeddingProvider, store VectorStore, indexMgr *IndexManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provider = provider
	m.store = store
	m.indexMgr = indexMgr
	m.initialized = true
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
		debugLogf("embedding: auto-build failed: %v", err)
		return
	}
	debugLogf("embedding: auto-build complete: %d files, %d units in %s",
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

	// Create conversation store in the same directory as the main index
	convoPath := filepath.Join(m.indexDir, "conversation_turns.hnsw")
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

	// Close provider and store
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

	// Close ONNX runtime
	if m.onnxRuntime != nil {
		if err := m.onnxRuntime.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.onnxRuntime = nil
	}

	m.initialized = false
	m.initError = nil // cleared to allow re-initialization after Close()

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

func isDebugEnabled() bool {
	value := configuration.GetEnvSimple("DEBUG")
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
