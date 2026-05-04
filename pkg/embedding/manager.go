package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// EmbeddingManager manages the embedding index lifecycle.
// It lazily initializes the ONNX Runtime provider and IndexManager
// on first use, and caches them for subsequent calls.
type EmbeddingManager struct {
	mu            sync.Mutex
	provider      *BundledProvider
	store         *JSONLFileStore
	indexMgr      *IndexManager
	initialized   bool
	config        *configuration.EmbeddingIndexConfig
	workspaceRoot string

	// Resolved config values stored during init to avoid re-reading config
	// under lock on every query call (SHOULD_FIX #7).
	threshold float32
	maxResults int
}

// NewEmbeddingManager creates a new manager with the given config.
// The manager is NOT initialized until Init() or a query method is called.
func NewEmbeddingManager(cfg *configuration.EmbeddingIndexConfig, workspaceRoot string) *EmbeddingManager {
	return &EmbeddingManager{
		config:        cfg,
		workspaceRoot: workspaceRoot,
	}
}

// Init initializes the ONNX Runtime provider and opens the vector store.
// This is idempotent — calling it multiple times is safe.
func (m *EmbeddingManager) Init(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

	// Resolve config defaults
	ortLibPath := m.config.ORTLibraryPath
	if ortLibPath == "" {
		ortLibPath = os.Getenv("ONNXRUNTIME_LIB")
	}
	if ortLibPath == "" {
		return fmt.Errorf("embedding: ONNX Runtime library path not configured. Set embedding_index.ort_library_path in config or ONNXRUNTIME_LIB env var")
	}

	modelDir := m.config.ModelDir
	if modelDir == "" {
		// Use the embedded model directory relative to the binary
		exe, _ := os.Executable()
		modelDir = filepath.Join(filepath.Dir(exe), "embedding_models")
		// Also check relative to working directory
		if _, err := os.Stat(filepath.Join(modelDir, "tokenizer.json")); err != nil {
			modelDir = "pkg/embedding/models"
		}
	}

	// Initialize provider
	provider, err := NewBundledProvider(modelDir, ortLibPath)
	if err != nil {
		return fmt.Errorf("embedding: init provider: %w", err)
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

	// Create workspace-specific index file
	indexFile := filepath.Join(indexDir, "index.jsonl")
	store, err := NewJSONLFileStore(indexFile)
	if err != nil {
		provider.Close()
		return fmt.Errorf("embedding: open store: %w", err)
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
		BatchSize:  32,
		MaxBodyLen: 2000,
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
func (m *EmbeddingManager) BuildIndex(ctx context.Context) (*IndexStats, error) {
	if err := m.Init(ctx); err != nil {
		return nil, err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}
	return idx.BuildIndex(ctx, m.workspaceRoot)
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
	return CheckFileForDuplicates(ctx, idx, filePath, content, threshold, topK)
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

// Close releases all resources.
func (m *EmbeddingManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
		return nil
	}
	var firstErr error
	if m.provider != nil {
		if err := m.provider.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if m.store != nil {
		if err := m.store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.initialized = false
	return firstErr
}
