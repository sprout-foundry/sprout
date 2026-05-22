package embedding

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

	// Parallel ONNX conversation store (lazy-initialized, nil until first use).
	// Maintained alongside convoStore so proactive context retrieval can benefit
	// from higher-quality embeddings when ONNX is available. Writes are best-effort:
	// if ONNX isn't ready at write time, only the static store is updated.
	onnxConvoStore *ConversationStore

	// Resolved index directory path (stored during init)
	indexDir string

	// ONNX provider (lazy-initialized, higher quality embeddings).
	// Only available when the native ONNX runtime is present (!wasm builds
	// with CGO). On WASM or when the model is not downloaded, these remain
	// nil and the manager falls back to the static provider.
	onnxRuntime  *ONNXRuntime
	onnxProvider *ONNXEmbeddingProvider
	onnxStore    VectorStore
	onnxReady    bool
	onnxError    error // cached error from failed ONNX init

	// ONNX background-build coordination. ONNX indexing is too slow for the
	// 2-minute auto-build budget on any non-trivial workspace, so it runs
	// in its own goroutine with a longer timeout. These fields let Close()
	// cancel and wait for the build to drain before tearing down resources.
	onnxBuilding    bool
	onnxBuildCancel context.CancelFunc
	onnxBuildWG     sync.WaitGroup

	// ONNX init is also kicked off in a goroutine from initLocked because
	// CreateSessionFromFile blocks for hundreds of milliseconds. Tracked so
	// Close can wait for the goroutine to finish — without that, init
	// goroutines outlive the EmbeddingManager that started them and race
	// inside yalue's global CGO state when other tests/instances spin up.
	onnxInitWG sync.WaitGroup
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
	indexPath := filepath.Join(indexDir, "index.hnsw")
	store, err := NewHNSWStore(indexPath, provider.ModelHash())
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
		ManifestPath:   filepath.Join(indexDir, ".index.hnsw.manifest.json"),
	})

	m.provider = provider
	m.store = store
	m.indexMgr = indexMgr
	m.initialized = true

	// Try to initialize ONNX provider in background (non-blocking).
	// Errors are cached in m.onnxError and will be logged on first access.
	// Tracked in onnxInitWG so Close can wait for completion — otherwise a
	// long-running CreateSessionFromFile call can outlive the manager and
	// crash inside yalue's global CGO state on shutdown.
	m.onnxInitWG.Add(1)
	go func() {
		defer m.onnxInitWG.Done()
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

	// Load the pre-registered EmbeddingGemma-300M config
	modelConfig := EmbeddingGemma300MConfig()
	modelName := modelConfig.Name
	modelPath := filepath.Join(modelDir, modelName, "model_q4.onnx")
	tokenizerPath := filepath.Join(modelDir, modelName, "tokenizer.json")

	// Native builds load .onnx from disk; download it if missing.
	// The WASM build delegates to a JS-side provider that owns its own model
	// loading, so we skip the on-disk file check there — see
	// onnxRequiresModelFiles in onnx_runtime.go / onnx_wasm_stub.go.
	if onnxRequiresModelFiles() {
		if _, err := os.Stat(modelPath); err != nil {
			log.Printf("embedding: downloading ONNX model %s...", modelName)
			if err := DownloadModel(ctx, modelDir, modelConfig); err != nil {
				runtime.Close()
				return fmt.Errorf("onnx: download model: %w", err)
			}
			log.Printf("embedding: ONNX model %s downloaded", modelName)
		}
	}

	// Create ONNX embedding provider (EmbeddingGemma-300M outputs 768-dim vectors).
	provider, err := NewONNXEmbeddingProvider(ctx, runtime, modelPath, tokenizerPath, 768)
	if err != nil {
		runtime.Close()
		return fmt.Errorf("onnx: create provider: %w", err)
	}

	// Open ONNX HNSW store
	onnxStore, err := NewHNSWStore(
		filepath.Join(m.indexDir, "embedding_index_onnx.hnsw"),
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

	// Also build the ONNX index if available — but in its own background
	// goroutine with a separate timeout. The static auto-build path has a
	// 2-minute budget that ONNX cannot meet on non-trivial workspaces, so
	// decoupling them lets the static index return quickly while ONNX
	// continues building.
	if m.isONNXReady() {
		m.startONNXBuildBackground()
	}

	return stats, err
}

// startONNXBuildBackground kicks off an ONNX index build in its own goroutine
// with a 30-minute timeout, independent of any caller context. Concurrent
// calls while a build is already in progress are dropped with a log message.
// Close() cancels the build and waits for the goroutine to drain.
func (m *EmbeddingManager) startONNXBuildBackground() {
	m.mu.Lock()
	if m.onnxBuilding {
		m.mu.Unlock()
		log.Printf("embedding: ONNX index build already in progress, skipping")
		return
	}
	if !m.onnxReady || m.onnxProvider == nil || m.onnxStore == nil {
		m.mu.Unlock()
		return
	}
	provider := m.onnxProvider
	store := m.onnxStore
	root := m.workspaceRoot
	m.onnxBuilding = true
	bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	m.onnxBuildCancel = cancel
	m.onnxBuildWG.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.onnxBuildWG.Done()
		defer func() {
			m.mu.Lock()
			m.onnxBuilding = false
			if m.onnxBuildCancel != nil {
				m.onnxBuildCancel()
				m.onnxBuildCancel = nil
			}
			m.mu.Unlock()
		}()

		log.Printf("embedding: building ONNX index in background (this may take many minutes)...")
		start := time.Now()
		onnxMgr := NewIndexManager(provider, store, IndexOptions{
			BatchSize:      32,
			MaxBodyLen:     2000,
			IndexFileLevel: true,
			ManifestPath:   filepath.Join(m.indexDir, ".embedding_index_onnx.hnsw.manifest.json"),
		})
		if _, err := onnxMgr.BuildIndex(bgCtx, root); err != nil {
			log.Printf("embedding: ONNX index build failed (static index OK): %v", err)
			return
		}
		log.Printf("embedding: ONNX index build complete in %s", time.Since(start))
	}()
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

// UpdateFile incrementally updates the index for a single file. Both the
// static and (when ready) ONNX indexes are updated so they stay aligned;
// without this, the ONNX side would only catch new code via the full
// background build, which never completes on large workspaces.
func (m *EmbeddingManager) UpdateFile(ctx context.Context, filePath string) error {
	if err := m.Init(ctx); err != nil {
		return err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return err
	}
	if err := idx.UpdateFile(ctx, filePath); err != nil {
		return err
	}

	// Best-effort ONNX update. Failure here doesn't surface — the static
	// index is the source of truth; ONNX is an enrichment layer that
	// gracefully degrades.
	if onnxIdx := m.snapshotONNXIndexMgr(); onnxIdx != nil {
		if err := onnxIdx.UpdateFile(ctx, filePath); err != nil {
			log.Printf("embedding: onnx UpdateFile failed for %s (static index OK): %v", filePath, err)
		}
	}
	return nil
}

// UpdateFromGitDiff incrementally updates the index by examining git-tracked
// files that have changed, been added, or been created since the last build.
// Both static and ONNX indexes are updated when ONNX is ready; the ONNX pass
// can be much slower (transformer inference per file) but is bounded by the
// diff size and is the only way to keep the ONNX index converging on a
// workspace too large for the full background build to finish.
func (m *EmbeddingManager) UpdateFromGitDiff(ctx context.Context) (*IndexStats, error) {
	if err := m.Init(ctx); err != nil {
		return nil, err
	}
	idx, err := m.snapshotIndexMgr()
	if err != nil {
		return nil, err
	}
	stats, err := idx.UpdateFromGitDiff(ctx, m.workspaceRoot)
	if err != nil {
		return nil, err
	}

	if onnxIdx := m.snapshotONNXIndexMgr(); onnxIdx != nil {
		if _, oerr := onnxIdx.UpdateFromGitDiff(ctx, m.workspaceRoot); oerr != nil {
			log.Printf("embedding: onnx UpdateFromGitDiff failed (static stats OK): %v", oerr)
		}
	}
	return stats, nil
}

// snapshotONNXIndexMgr returns an IndexManager wrapped around the ONNX
// provider+store, or nil if ONNX isn't ready. Callers use this to issue
// incremental updates against the ONNX index without duplicating the
// IndexOptions wiring.
func (m *EmbeddingManager) snapshotONNXIndexMgr() *IndexManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.onnxReady || m.onnxProvider == nil || m.onnxStore == nil {
		return nil
	}
	return NewIndexManager(m.onnxProvider, m.onnxStore, IndexOptions{
		BatchSize:      32,
		MaxBodyLen:     2000,
		IndexFileLevel: true,
		ManifestPath:   filepath.Join(m.indexDir, ".embedding_index_onnx.hnsw.manifest.json"),
	})
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
			ManifestPath:   filepath.Join(m.indexDir, ".embedding_index_onnx.hnsw.manifest.json"),
		})
		onnxResults, err := onnxMgr.QuerySimilar(ctx, query, topK, threshold)
		if err != nil {
			// ONNX search failed, fall back to static only.
			return staticResults, nil
		}
		return RRFMergeResults(staticResults, onnxResults, topK), nil
	}

	return staticResults, nil
}

// RRFMergeResults combines two ranked result lists using Reciprocal Rank
// Fusion. Each document's fused score is sum_p 1/(k + rank_p), where rank_p
// is its 1-based position in provider p's ranked list (absent providers
// contribute nothing). Documents are deduped by file path + start line.
//
// RRF replaces the older max-cosine merge because cosine similarities from
// the static and ONNX providers live in different vector spaces and are not
// comparable on the same scale. Rank-based fusion gives a sound combination
// without requiring score calibration.
//
// Each returned QueryResult retains its source-provider .Similarity (taking
// the higher-similarity copy when both providers found the same doc) so
// callers that display the value as a percentage still see a meaningful
// per-provider score. Only the result ORDER reflects fusion.
//
// k=60 is the value introduced in the original RRF paper (Cormack et al. 2009)
// and is the de-facto standard.
func RRFMergeResults(a, b []QueryResult, topK int) []QueryResult {
	const rrfK = 60.0

	type entry struct {
		result QueryResult
		score  float64
		order  int // insertion order, used as deterministic tiebreaker
	}
	// Dedupe by Record.ID — universally unique across record types. Code units
	// use "<file>:<symbol>", memories use "memory:<name>", turns use "turn:<id>".
	// File+StartLine collapsed all memories together (both are zero-valued).
	keyOf := func(r QueryResult) string {
		if r.Record.ID != "" {
			return r.Record.ID
		}
		// Defensive fallback for records that somehow lack an ID — should be
		// vanishingly rare since extractors always set one.
		return r.Record.File + ":" + strconv.Itoa(r.Record.StartLine)
	}
	by := make(map[string]*entry, len(a)+len(b))
	next := 0
	contribute := func(list []QueryResult) {
		for rank, r := range list {
			key := keyOf(r)
			inv := 1.0 / (rrfK + float64(rank+1))
			if e, ok := by[key]; ok {
				e.score += inv
				if r.Similarity > e.result.Similarity {
					e.result = r
				}
			} else {
				by[key] = &entry{result: r, score: inv, order: next}
				next++
			}
		}
	}
	// `a` is contributed first so its entries get lower insertion-order indices,
	// which means ties resolve in favor of provider `a` (callers pass static
	// first, ONNX second — see EmbeddingManager.QuerySimilar). The intent:
	// when both providers agree on rank, the higher-precision provider wins.
	contribute(a)
	contribute(b)

	entries := make([]*entry, 0, len(by))
	for _, e := range by {
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].score != entries[j].score {
			return entries[i].score > entries[j].score
		}
		return entries[i].order < entries[j].order
	})
	if topK > 0 && len(entries) > topK {
		entries = entries[:topK]
	}
	out := make([]QueryResult, len(entries))
	for i, e := range entries {
		out[i] = e.result
	}
	return out
}

// GetConversationStore returns the conversation store, creating it lazily on first use.
// The store is user-scoped and lives at {indexDir}/conversation_turns.hnsw.
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
	convoPath := filepath.Join(m.indexDir, "conversation_turns.hnsw")
	convoStore, err := NewConversationStore(m.provider, convoPath, m.provider.ModelHash())
	if err != nil {
		return nil, fmt.Errorf("embedding: create conversation store: %w", err)
	}

	m.convoStore = convoStore
	return convoStore, nil
}

// GetONNXConversationStore returns the parallel ONNX-backed conversation store,
// creating it lazily on first use. Returns (nil, nil) if the ONNX provider is
// not ready — callers should treat this as "feature unavailable" and continue
// with the static store only. The file lives at
// {indexDir}/conversation_turns_onnx.hnsw.
//
// Like the code-index ONNX path, this is best-effort and never required for
// correctness: proactive context retrieval falls back to static-only when this
// returns nil.
func (m *EmbeddingManager) GetONNXConversationStore(ctx context.Context) (*ConversationStore, error) {
	if !m.isONNXReady() {
		return nil, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.onnxConvoStore != nil {
		return m.onnxConvoStore, nil
	}

	if m.onnxProvider == nil {
		return nil, nil
	}

	convoPath := filepath.Join(m.indexDir, "conversation_turns_onnx.hnsw")
	store, err := NewConversationStore(m.onnxProvider, convoPath, m.onnxProvider.ModelHash())
	if err != nil {
		return nil, fmt.Errorf("embedding: create onnx conversation store: %w", err)
	}

	m.onnxConvoStore = store
	return store, nil
}

// Close releases all resources.
func (m *EmbeddingManager) Close() error {
	// Cancel any in-progress ONNX background build and wait for both the
	// build goroutine and the lazy init goroutine to drain BEFORE we acquire
	// m.mu for the teardown — those goroutines' terminating defers also need
	// the lock, so holding it across the Wait() would deadlock. Draining
	// init prevents a CreateSessionFromFile call from running concurrently
	// with our ONNX resource teardown (the previous behavior segfaulted
	// inside yalue's global CGO state on busy test suites).
	m.mu.Lock()
	if m.onnxBuildCancel != nil {
		m.onnxBuildCancel()
		m.onnxBuildCancel = nil
	}
	m.mu.Unlock()
	m.onnxBuildWG.Wait()
	m.onnxInitWG.Wait()

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
	if m.onnxConvoStore != nil {
		if err := m.onnxConvoStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.onnxConvoStore = nil
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
		filepath.Join(indexDir, "embedding_index_onnx.hnsw"),
		filepath.Join(indexDir, "embedding_index_onnx.hnsw.meta"),
		filepath.Join(indexDir, "embedding_index_onnx.hnsw.records.json"),
	}
	return removeFilesSilently(files)
}

func clearConversationEmbeddingFiles(indexDir string) (int, error) {
	files := []string{
		filepath.Join(indexDir, "conversation_turns.hnsw"),
		filepath.Join(indexDir, "conversation_turns.hnsw.meta"),
		filepath.Join(indexDir, "conversation_turns.hnsw.records.json"),
		filepath.Join(indexDir, "conversation_turns_onnx.hnsw"),
		filepath.Join(indexDir, "conversation_turns_onnx.hnsw.meta"),
		filepath.Join(indexDir, "conversation_turns_onnx.hnsw.records.json"),
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
