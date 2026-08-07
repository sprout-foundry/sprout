package embedding

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// Init initializes the ONNX embedding provider and opens the vector store. Idempotent.
func (m *EmbeddingManager) Init(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized.Load() {
		return nil
	}

	m.initError = nil
	return m.initLocked(ctx)
}

// initLocked performs the actual initialization. The caller must hold m.mu.
func (m *EmbeddingManager) initLocked(ctx context.Context) error {
	if m.initialized.Load() {
		return nil
	}

	if m.config == nil {
		m.config = &configuration.EmbeddingIndexConfig{}
	}

	m.indexDir = resolveIndexDirFromConfig(m.config, m.workspaceRoot)

	m.threshold = DefaultDuplicateThreshold
	m.maxResults = m.config.MaxResults
	if m.maxResults == 0 {
		m.maxResults = 3
	}

	provider, runtime, err := m.createProvider(ctx)
	if err != nil {
		m.initError = fmt.Errorf("embedding: init provider: %w", err)
		return m.initError
	}

	store, err := NewHNSWStore(filepath.Join(m.indexDir, "index.hnsw"), provider.ModelHash())
	if err != nil {
		m.initError = fmt.Errorf("embedding: open store: %w", err)
		return m.initError
	}

	indexMgr := NewIndexManager(provider, store, IndexOptions{
		BatchSize:      32,
		MaxBodyLen:     2000,
		IndexFileLevel: true,
		ManifestPath:   filepath.Join(m.indexDir, ".index.hnsw.manifest.json"),
	})

	m.provider = provider
	m.cachedProvider = newCachedProvider(provider)
	m.onnxRuntime = runtime
	m.providerShared = true
	m.store = store
	m.indexMgr = indexMgr

	m.initialized.Store(true)
	return nil
}

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

// buildIndexLocked performs the actual build. The caller must hold the building lock.
func (m *EmbeddingManager) buildIndexLocked(ctx context.Context) (*IndexStats, error) {
	if err := m.Init(ctx); err != nil {
		return nil, err
	}

	if filesystem.IsHomeDir(m.workspaceRoot) {
		return nil, fmt.Errorf("embedding: refusing to index home directory %q", m.workspaceRoot)
	}

	files, err := WalkCodeFiles(ctx, m.workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("embedding: scan workspace: %w", err)
	}
	if len(files) > MaxFileCount {
		return nil, fmt.Errorf("embedding: workspace has %d files (max %d)", len(files), MaxFileCount)
	}

	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}
	return idx.BuildIndex(ctx, m.workspaceRoot)
}

func (m *EmbeddingManager) BuildIndexBackground(ctx context.Context) <-chan *BuildResult {
	ch := make(chan *BuildResult, 1)

	m.mu.Lock()
	if m.building {
		m.mu.Unlock()
		ch <- &BuildResult{Err: fmt.Errorf("embedding: build already in progress")}
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

		select {
		case <-m.closeCh():
			ch <- &BuildResult{Err: ErrStoreClosed}
			return
		default:
		}

		ctx, cancel := context.WithTimeout(ctx, BuildTimeout)
		defer cancel()

		if err := m.Init(ctx); err != nil {
			ch <- &BuildResult{Err: err}
			return
		}

		stats, err := m.buildIndexLocked(ctx)
		ch <- &BuildResult{Stats: stats, Err: err}
	}()

	return ch
}

// AutoBuildWhenReady runs a background index build after a short startup delay.
func (m *EmbeddingManager) AutoBuildWhenReady() {
	m.autoBuildOnce.Do(m.autoBuildWhenReady)
}

func (m *EmbeddingManager) autoBuildWhenReady() {
	select {
	case <-time.After(3 * time.Second):
	case <-m.closeCh():
		return
	}

	select {
	case <-m.closeCh():
		return
	default:
	}

	walkCtx, walkCancel := context.WithTimeout(context.Background(), WalkTimeout)
	files, _ := WalkCodeFiles(walkCtx, m.workspaceRoot)
	walkCancel()

	timeout := autoBuildTimeout(len(files))
	debugLogf("embedding: auto-build budget for %d files: %v", len(files), timeout)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stats, err := m.BuildIndex(ctx)
	if err != nil {
		log.Printf("embedding: auto-build failed: %v", err)
		return
	}
	log.Printf("embedding: auto-build complete: %d files, %d units in %s",
		stats.FilesProcessed, stats.UnitsExtracted, stats.Duration)
}

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

func (m *EmbeddingManager) UpdateFromGitDiffBackground(ctx context.Context) <-chan *BuildResult {
	ch := make(chan *BuildResult, 1)

	m.mu.Lock()
	if m.building {
		m.mu.Unlock()
		ch <- &BuildResult{Err: fmt.Errorf("embedding: build already in progress")}
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

		select {
		case <-m.closeCh():
			ch <- &BuildResult{Err: ErrStoreClosed}
			return
		default:
		}

		ctx, cancel := context.WithTimeout(ctx, BuildTimeout)
		defer cancel()

		stats, err := m.UpdateFromGitDiff(ctx)
		ch <- &BuildResult{Stats: stats, Err: err}
	}()

	return ch
}

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

// QuerySimilarCode searches using source code as the input rather than NL.
func (m *EmbeddingManager) QuerySimilarCode(ctx context.Context, codeText string, topK int, threshold float32) ([]QueryResult, error) {
	if err := m.Init(ctx); err != nil {
		return nil, err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}
	return idx.CheckDuplicates(ctx, codeText, topK, threshold)
}

func (m *EmbeddingManager) GetConversationStore(ctx context.Context) (*ConversationStore, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.convoStore != nil {
		return m.convoStore, nil
	}

	m.initError = nil
	if err := m.initLocked(ctx); err != nil {
		return nil, err
	}

	convoPath := filepath.Join(m.indexDir, "conversation_turns.hnsw")
	convoStore, err := NewConversationStore(m.cachedProvider, convoPath, m.provider.ModelHash())
	if err != nil {
		return nil, fmt.Errorf("embedding: create conversation store: %w", err)
	}

	m.convoStore = convoStore
	return convoStore, nil
}

func (m *EmbeddingManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error

	if m.convoStore != nil {
		if err := m.convoStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.convoStore = nil
	}

	// Provider and runtime are shared; don't close when shared.
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
	m.initError = nil

	if m.closeChan == nil {
		m.closeChan = make(chan struct{})
	}
	select {
	case <-m.closeChan:
	default:
		close(m.closeChan)
	}

	return firstErr
}

func ClearEmbeddingFiles(indexDir string, fileType string) (int, error) {
	switch fileType {
	case "code":
		return clearCodeEmbeddingFiles(indexDir)
	case "conversation_turn", "memory":
		return clearConversationEmbeddingFiles(indexDir)
	case "all":
		codeCount, err := clearCodeEmbeddingFiles(indexDir)
		if err != nil {
			return codeCount, err
		}
		convCount, err := clearConversationEmbeddingFiles(indexDir)
		return codeCount + convCount, err
	default:
		return 0, fmt.Errorf("invalid file type %q: valid options are code, conversation_turn, memory, all", fileType)
	}
}

func clearCodeEmbeddingFiles(indexDir string) (int, error) {
	files := []string{
		filepath.Join(indexDir, "index.hnsw"),
		filepath.Join(indexDir, "index.hnsw.meta"),
		filepath.Join(indexDir, "index.hnsw.records.json"),
		filepath.Join(indexDir, ".index.hnsw.manifest.json"),
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
