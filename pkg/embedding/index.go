package embedding

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IndexStats reports the results of an indexing operation.
type IndexStats struct {
	FilesProcessed int
	UnitsExtracted int
	UnitsEmbedded  int
	Duration       time.Duration
}

// IndexOptions configures the behavior of IndexManager.
type IndexOptions struct {
	// IncludeTests controls whether test functions are indexed.
	IncludeTests bool
	// BatchSize controls how many code units are embedded per batch.
	BatchSize int
	// MaxBodyLen truncates CodeUnit.Body to this many bytes before embedding (0 = no limit).
	MaxBodyLen int
}

// IndexManager orchestrates code extraction, embedding, and storage.
type IndexManager struct {
	provider EmbeddingProvider
	store    VectorStore
	opts     IndexOptions
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

// BuildIndex walks rootDir, extracts code units, embeds them, and stores them.
func (m *IndexManager) BuildIndex(ctx context.Context, rootDir string) (*IndexStats, error) {
	start := time.Now()
	stats := &IndexStats{}

	files, err := WalkCodeFiles(ctx, rootDir)
	if err != nil {
		return nil, fmt.Errorf("index: walk %s: %w", rootDir, err)
	}

	var allUnits []CodeUnit
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: cancelled during file extraction")
		}

		units, err := ExtractFromFile(path, WithIncludeTests(m.opts.IncludeTests))
		if err != nil {
			log.Printf("index: skipping %s: %v", path, err)
			continue
		}
		stats.FilesProcessed++
		allUnits = append(allUnits, units...)

		// Log progress every ProgressInterval files processed.
		if stats.FilesProcessed%ProgressInterval == 0 {
			log.Printf("index: extraction progress: %d files, %d units", stats.FilesProcessed, len(allUnits))
		}
	}

	stats.UnitsExtracted = len(allUnits)
	if len(allUnits) == 0 {
		stats.Duration = time.Since(start)
		return stats, nil
	}

	records, err := m.embedUnits(ctx, allUnits)
	if err != nil {
		stats.Duration = time.Since(start)
		return stats, fmt.Errorf("index: embed units: %w", err)
	}

	// Store in batches to avoid overwhelming the store.
	// On context cancellation, store what we can and return partial results.
	const storeBatch = 128
	stored := 0
	for i := 0; i < len(records); i += storeBatch {
		if err := ctx.Err(); err != nil {
			// Partial store — log and return what we have so far.
			log.Printf("index: store interrupted after %d/%d records: %v", stored, len(records), err)
			break
		}
		end := i + storeBatch
		if end > len(records) {
			end = len(records)
		}
		if err := m.store.Store(records[i:end]); err != nil {
			return stats, fmt.Errorf("index: store batch %d-%d: %w", i, end, err)
		}
		stored += end - i
	}

	stats.UnitsEmbedded = stored

	stats.Duration = time.Since(start)
	return stats, nil
}

// UpdateFile re-indexes a single file: deletes old records, extracts, embeds, and stores.
func (m *IndexManager) UpdateFile(ctx context.Context, filePath string) error {
	// Always delete old records first (handles deleted files too).
	if err := m.store.DeleteByFile(filePath); err != nil {
		return fmt.Errorf("index: delete file %s: %w", filePath, err)
	}

	units, err := ExtractFromFile(filePath, WithIncludeTests(m.opts.IncludeTests))
	if err != nil {
		return fmt.Errorf("index: extract %s: %w", filePath, err)
	}

	if len(units) == 0 {
		return nil
	}

	records, err := m.embedUnits(ctx, units)
	if err != nil {
		return fmt.Errorf("index: embed %s: %w", filePath, err)
	}

	if err := m.store.Store(records); err != nil {
		return fmt.Errorf("index: store %s: %w", filePath, err)
	}

	return nil
}

// QuerySimilar embeds query text and returns the top-K most similar records above threshold.
func (m *IndexManager) QuerySimilar(ctx context.Context, query string, topK int, threshold float32) ([]QueryResult, error) {
	vec, err := m.provider.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("index: embed query: %w", err)
	}
	results, err := m.store.Query(vec, topK, threshold)
	if err != nil {
		return nil, fmt.Errorf("index: query store: %w", err)
	}
	return results, nil
}

// CheckDuplicates is like QuerySimilar but uses a default threshold of 0.90.
func (m *IndexManager) CheckDuplicates(ctx context.Context, codeText string, topK int, threshold float32) ([]QueryResult, error) {
	if threshold == 0 {
		threshold = 0.90
	}
	return m.QuerySimilar(ctx, codeText, topK, threshold)
}

// embedUnits converts CodeUnits to text, batch-embeds, and returns VectorRecords.
// On context cancellation (timeout), it returns partial results instead of an error
// so that the caller can store whatever was processed so far.
func (m *IndexManager) embedUnits(ctx context.Context, units []CodeUnit) ([]VectorRecord, error) {
	now := time.Now()
	var records []VectorRecord

	for i := 0; i < len(units); i += m.opts.BatchSize {
		if err := ctx.Err(); err != nil {
			// Graceful degradation: return partial results on timeout/cancellation.
			log.Printf("index: embedding interrupted after %d records (%d total units): %v",
				len(records), len(units), err)
			return records, nil
		}

		end := i + m.opts.BatchSize
		if end > len(units) {
			end = len(units)
		}

		batch := units[i:end]
		texts := make([]string, len(batch))
		for j, u := range batch {
			texts[j] = embeddingText(u, m.opts.MaxBodyLen)
		}

		vecs, err := m.provider.EmbedBatch(ctx, texts)
		if err != nil {
			return records, fmt.Errorf("index: embed batch [%d:%d]: %w", i, end, err)
		}

		for j, u := range batch {
			records = append(records, codeUnitToRecord(u, vecs[j], now))
		}

		// Log progress every ProgressInterval records embedded.
		if len(records)%ProgressInterval == 0 {
			log.Printf("index: embedding progress: %d/%d records", len(records), len(units))
		}
	}

	return records, nil
}

// embeddingText builds the text to embed from a CodeUnit, with optional body truncation.
func embeddingText(u CodeUnit, maxBodyLen int) string {
	body := u.Body
	if maxBodyLen > 0 && len(body) > maxBodyLen {
		// Truncate at the last valid UTF-8 boundary before the limit.
		// Converting to runes and back ensures we don't split multi-byte characters.
		runes := []rune(body)
		if len(runes) > maxBodyLen {
			runes = runes[:maxBodyLen]
		}
		body = string(runes)
	}
	return u.Signature + "\n" + body
}

// codeUnitToRecord converts a CodeUnit and its embedding into a VectorRecord.
func codeUnitToRecord(u CodeUnit, embedding []float32, indexedAt time.Time) VectorRecord {
	return VectorRecord{
		ID:        u.ID,
		File:      u.File,
		Name:      u.Name,
		Signature: strings.TrimSpace(u.Signature),
		StartLine: u.StartLine,
		EndLine:   u.EndLine,
		Language:  u.Language,
		Embedding: embedding,
		Hash:      u.Hash,
		IndexedAt: indexedAt,
	}
}

// UpdateFromGitDiff incrementally updates the index by examining files changed
// since the last index build. It uses git diff to detect modified, added,
// and deleted files. Deleted files have their records removed from the store,
// while changed/new files are re-indexed.
func (m *IndexManager) UpdateFromGitDiff(ctx context.Context, repoRoot string) (*IndexStats, error) {
	start := time.Now()
	stats := &IndexStats{}

	// Collect deleted files from both staged and unstaged diffs (SHOULD_FIX #8).
	var deletedFiles []string
	if files, err := runGit(repoRoot, "diff", "--name-only", "--diff-filter=D", "--cached"); err == nil {
		deletedFiles = append(deletedFiles, files...)
	}
	if files, err := runGit(repoRoot, "diff", "--name-only", "--diff-filter=D"); err == nil {
		deletedFiles = append(deletedFiles, files...)
	}

	// Filter deleted files to supported extensions only.
	toDelete := make(map[string]bool)
	for _, f := range deletedFiles {
		f = filepath.Clean(f)
		if f == "" || !isSupportedFile(f) {
			continue
		}
		toDelete[f] = true
	}

	// Delete records for removed files.
	for f := range toDelete {
		if err := ctx.Err(); err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: cancelled")
		}
		if err := m.store.DeleteByFile(f); err != nil {
			log.Printf("index: skipping delete %s: %v", f, err)
			continue
		}
		stats.FilesProcessed++
	}

	// Collect changed files from three git sources.
	var changedFiles []string

	// 1. Staged (cached) changes
	files, err := runGit(repoRoot, "diff", "--name-only", "--cached")
	if err != nil {
		return nil, fmt.Errorf("index: git diff --cached: %w", err)
	}
	changedFiles = append(changedFiles, files...)

	// 2. Working tree (unstaged) changes
	files, err = runGit(repoRoot, "diff", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("index: git diff: %w", err)
	}
	changedFiles = append(changedFiles, files...)

	// 3. Untracked (new) files
	files, err = runGit(repoRoot, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("index: git ls-files: %w", err)
	}
	changedFiles = append(changedFiles, files...)

	// Deduplicate and filter to supported extensions.
	// Skip files that are in the delete list (they've already been handled).
	fileSet := make(map[string]bool)
	for _, f := range changedFiles {
		if f == "" {
			continue
		}
		cleanPath := filepath.Clean(f)
		if !isSupportedFile(f) {
			continue
		}
		if toDelete[cleanPath] {
			continue // already deleted
		}
		fileSet[cleanPath] = true
	}

	if len(fileSet) == 0 && len(toDelete) == 0 {
		stats.Duration = time.Since(start)
		return stats, nil
	}

	var errs []string
	for f := range fileSet {
		if err := ctx.Err(); err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: cancelled")
		}

		if err := m.UpdateFile(ctx, f); err != nil {
			log.Printf("index: skipping %s: %v", f, err)
			errs = append(errs, f)
			continue
		}
		stats.FilesProcessed++
	}

	if len(errs) > 0 {
		return stats, fmt.Errorf("index: failed to update %d files: %v", len(errs), errs)
	}

	stats.Duration = time.Since(start)
	return stats, nil
}

// runGit executes a git command in the given directory and returns the output
// split into non-empty lines.
func runGit(dir string, args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// isSupportedFile returns true if the file path has a supported source-code extension.
func isSupportedFile(path string) bool {
	switch ext := filepath.Ext(path); ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".py":
		return true
	default:
		return false
	}
}
