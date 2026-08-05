// Batch operations for the embedding index

package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

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
	m.indexDir = resolveIndexDirFromConfig(m.config, m.workspaceRoot)

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

		ctx, cancel := context.WithTimeout(ctx, BuildTimeout)
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

// AutoBuildWhenReady runs a background index build after a short delay.
// This is called at agent startup so the index is ready for duplicate
// detection and context enrichment without waiting for an explicit query.
// The timeout is adaptive — large workspaces get proportionally more time
// (see autoBuildTimeout) so the build doesn't get killed mid-way on
// resource-constrained devices like Termux/Android.
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
	m.autoBuildOnce.Do(m.autoBuildWhenReady)
}

func (m *EmbeddingManager) autoBuildWhenReady() {
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

	// Walk the workspace to count files, then derive an adaptive timeout.
	// The walk is fast (directory traversal, no embedding) and its own
	// WalkTimeout guards against pathological cases.
	walkCtx, walkCancel := context.WithTimeout(context.Background(), WalkTimeout)
	files, _ := WalkCodeFiles(walkCtx, m.workspaceRoot)
	walkCancel()

	timeout := autoBuildTimeout(len(files))
	debugLogf("embedding: auto-build budget for %d files: %v", len(files), timeout)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

// UpdateFromGitDiffBackground starts an incremental index update in a
// background goroutine. It reuses the build lock so update and build cannot
// run simultaneously. The returned channel receives exactly one result.
func (m *EmbeddingManager) UpdateFromGitDiffBackground(ctx context.Context) <-chan *BuildResult {
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

		select {
		case <-m.closeCh():
			ch <- &BuildResult{Err: ErrStoreClosed}
			return
		default:
		}

		ctx, cancel := context.WithTimeout(ctx, BuildTimeout)
		defer cancel()

		stats, err := m.UpdateFromGitDiff(ctx)
		ch <- &BuildResult{
			Stats: stats,
			Err:   err,
		}
	}()

	return ch
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
