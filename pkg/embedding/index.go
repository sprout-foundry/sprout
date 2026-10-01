package embedding

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync/atomic"
	"time"
)

// index.go — the IndexManager core: the manager type, constructor, the
// build-lock, memory checkpointing, and the query/read surface
// (QuerySimilar, CheckDuplicates). Build, embedding, and git-diff
// incremental paths live in index_build.go / index_embed.go /
// index_gitdiff.go.

// Task prefixes for embedding queries and documents.
const (
	// documentPrefix is prepended to code/text before embedding for indexing.
	documentPrefix = "title: none | text: "

	queryPrefix     = "task: search result | query: "
	codeQueryPrefix = "task: code retrieval | query: "
)

// IndexStats reports the results of an indexing operation.
type IndexStats struct {
	FilesProcessed int
	UnitsExtracted int
	UnitsEmbedded  int
	Duration       time.Duration
}

// IndexOptions configures IndexManager behavior.
type IndexOptions struct {
	// IncludeTests controls whether test functions are indexed.
	IncludeTests bool
	// BatchSize controls how many code units are embedded per batch.
	BatchSize int
	// MaxBodyLen truncates CodeUnit.Body to this many bytes before embedding (0 = no limit).
	MaxBodyLen int
	// IndexFileLevel indexes non-code files (markdown, configs, etc.) at file level.
	IndexFileLevel bool
	// ManifestPath is the path to the build manifest file for incremental rebuilds.
	ManifestPath string
	// IndexDir is the directory containing the HNSW index files. Used to place
	// the cross-process .build.lock file. Empty disables locking.
	IndexDir string
}

// IndexManager orchestrates code extraction, embedding, and storage.
type IndexManager struct {
	provider      EmbeddingProvider
	store         VectorStore
	opts          IndexOptions
	buildLockHeld atomic.Bool // true when this manager acquired the flock (for re-entrant calls)
}

// NewIndexManager creates an IndexManager with the given provider, store, and options.
// Default BatchSize is 32, default MaxBodyLen is 2000.
func NewIndexManager(provider EmbeddingProvider, store VectorStore, opts IndexOptions) *IndexManager {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 32
	}
	if opts.MaxBodyLen <= 0 {
		opts.MaxBodyLen = 2000
	}
	return &IndexManager{
		provider: provider,
		store:    store,
		opts:     opts,
	}
}

// lockForBuild acquires the cross-process build lock with re-entrant behavior.
//
// If this IndexManager already holds the lock (e.g. UpdateFromGitDiff called
// UpdateFile), it returns immediately without re-acquiring — avoiding the
// deadlock that occurs when flock(2) is used on a different open file
// description for the same lock file.
//
// Returns (nil, nil) when no lock is needed (IndexDir empty or flock unavailable).
// Returns (release, nil) when the lock was acquired.
// Returns (nil, errBuildLocked) when another process holds the lock.
func (m *IndexManager) lockForBuild() (func(), error) {
	// Re-entrant fast path: this manager already holds the lock.
	if m.buildLockHeld.Load() {
		return nil, nil
	}

	release, err := acquireBuildLock(m.opts.IndexDir)
	if release != nil {
		// Wrap the release to clear the re-entrant flag on unlock.
		// Note: use a LOCAL variable, not a named return — a closure capturing
		// a named return would recurse into itself after the return assigns it.
		m.buildLockHeld.Store(true)
		return func() {
			m.buildLockHeld.Store(false)
			release()
		}, nil
	}
	// errBuildLocked or (nil, nil) from acquireBuildLock — pass through as-is.
	return nil, err
}

// buildIndexMemoryLimit bounds Go heap growth for the duration of a build.
// This is a bursty, allocation-heavy batch pass — tokenizing and embedding
// every code unit across the whole repo, with all units for the current
// batch of changed files held in memory (allUnits below) before anything
// gets written out. Without an explicit limit, Go's GC waits for the heap
// to roughly double before collecting; measured on a real 2,363-file repo,
// that let RSS swing widely up to 7+ GB before reclaiming back down to
// ~2GB — and this runs in the same process as, and concurrently with,
// whatever else sprout is doing (e.g. a loaded local model holding its own
// several-GB unified-memory footprint outside the Go heap entirely).
// SetMemoryLimit makes the GC work harder as usage approaches the limit
// instead of waiting for the heap to double.
const buildIndexMemoryLimit = 3 * 1024 * 1024 * 1024

// logMemCheckpoint reports Go's own runtime.MemStats at a named point in
// the build. HeapAlloc is Go-managed live objects; Sys is total memory
// obtained from the OS for the Go runtime itself (heap + stacks + GC
// metadata). Comparing Sys against observed process RSS is what separates
// "Go's own accounting is growing" from "something outside Go entirely is
// growing" — on a real 2,363-file repo this showed Sys pinned flat at the
// configured buildIndexMemoryLimit throughout the whole embedding loop
// while RSS still swung up to 5+ GB above it, meaning the excess is native
// (cgo) memory the ONNX runtime holds that neither this limit nor Go's GC
// can see or reclaim — worth knowing precisely if this needs
// re-investigating later rather than re-deriving it from scratch.
func logMemCheckpoint(tag string) {
	if !debugLogEnabled.Load() {
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	log.Printf("index: MEMCHECK %s heapAlloc=%.1fMB sys=%.1fMB numGC=%d",
		tag, float64(ms.HeapAlloc)/1048576, float64(ms.Sys)/1048576, ms.NumGC)
}

// queryWithPrefix embeds text under a task prefix and returns the top-K records above threshold.
// EmbeddingGemma uses different subspaces for queries vs documents, so the prefix matters.
func (m *IndexManager) queryWithPrefix(ctx context.Context, text, prefix string, topK int, threshold float32) ([]QueryResult, error) {
	vec, err := m.provider.EmbedWithPrefix(ctx, text, prefix)
	if err != nil {
		return nil, fmt.Errorf("index: embed query: %w", err)
	}
	results, err := m.store.Query(vec, topK, threshold)
	if err != nil {
		return nil, fmt.Errorf("index: query store: %w", err)
	}
	return results, nil
}

// QuerySimilar embeds a natural-language query and returns the top-K most
// similar records above threshold. Use CheckDuplicates when the input is code
// rather than a question.
func (m *IndexManager) QuerySimilar(ctx context.Context, query string, topK int, threshold float32) ([]QueryResult, error) {
	return m.queryWithPrefix(ctx, query, codeQueryPrefix, topK, threshold)
}

// CheckDuplicates finds indexed code similar to codeText.
//
// It embeds with documentPrefix, NOT the query prefix: this compares code
// against code, and the index stores code as documents. Routing this through
// QuerySimilar (which is for natural-language questions) put the two sides in
// different subspaces and cost roughly 0.10 of similarity — enough that, on
// top of an already unreachable 0.90 gate, duplicate detection could not fire
// at all. See TestPrefixSymmetryAffectsDuplicateThresholds.
func (m *IndexManager) CheckDuplicates(ctx context.Context, codeText string, topK int, threshold float32) ([]QueryResult, error) {
	if threshold == 0 {
		threshold = DefaultDuplicateThreshold
	}
	return m.queryWithPrefix(ctx, codeText, documentPrefix, topK, threshold)
}
