package embedding

import (
	"context"
	"fmt"
	"log"
	"os"
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
	// IndexFileLevel controls whether non-code files (markdown, configs, etc.)
	// are indexed at the file level. When true, files like README.md, package.json,
	// Dockerfile, etc. are indexed as single records with Type="file".
	IndexFileLevel bool
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
// Uses incremental rebuild: loads existing records, compares content hashes,
// and only re-embeds changed or new files. Deleted files have their records
// removed from the store.
// When IndexFileLevel is enabled, also indexes non-code files at the file level.
func (m *IndexManager) BuildIndex(ctx context.Context, rootDir string) (*IndexStats, error) {
	start := time.Now()
	stats := &IndexStats{}

	// Load existing records for incremental comparison.
	existingRecords, err := m.store.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("index: load existing: %w", err)
	}

	// Choose the appropriate walk function based on file-level indexing flag
	var files []string
	if m.opts.IndexFileLevel {
		files, err = WalkAllIndexableFiles(ctx, rootDir)
	} else {
		files, err = WalkCodeFiles(ctx, rootDir)
	}

	if err != nil {
		return nil, fmt.Errorf("index: walk %s: %w", rootDir, err)
	}

	var allUnits []CodeUnit
	var fileExtractor *FileExtractor
	if m.opts.IndexFileLevel {
		// Initialize file extractor for non-code files
		fileExtractor = NewFileExtractor(8000)
	}

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: cancelled during file extraction")
		}

		// Determine if this is a code file or non-code file
		isCodeFile := hasCodeExtension(path)
		isIndexableFile := IsSupportedIndexableFile(path)

		var units []CodeUnit
		if isCodeFile {
			// Use existing code extractor for code files
			units, err = ExtractFromFile(path, WithIncludeTests(m.opts.IncludeTests))
			if err != nil {
				log.Printf("index: skipping %s: %v", path, err)
				continue
			}
		} else if isIndexableFile {
			// Use file extractor for non-code files
			content, err := os.ReadFile(path)
			if err != nil {
				log.Printf("index: skipping %s: %v", path, err)
				continue
			}
			units, err = fileExtractor.Extract(path, content)
			if err != nil {
				log.Printf("index: skipping %s: %v", path, err)
				continue
			}
		} else {
			// Should not happen if walk functions are correct, but skip anyway
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

	// --- Incremental rebuild logic ---

	// Build a map of file → unit ID → hash from existing records.
	existingFileUnits := make(map[string]map[string]string)
	for _, rec := range existingRecords {
		if existingFileUnits[rec.File] == nil {
			existingFileUnits[rec.File] = make(map[string]string)
		}
		existingFileUnits[rec.File][rec.ID] = rec.Hash
	}

	// Build a set of current files from extracted units.
	currentFiles := make(map[string]bool)
	for _, unit := range allUnits {
		currentFiles[unit.File] = true
	}

	// Filter existing records to only include files that still exist.
	// This handles the case where files were deleted since the last index build.
	// Defer all disk writes to a single Store call at the end.
	var baseRecords []VectorRecord
	for _, rec := range existingRecords {
		if currentFiles[rec.File] {
			baseRecords = append(baseRecords, rec)
		}
	}
	if len(baseRecords) < len(existingRecords) {
		log.Printf("index: dropping %d records for deleted files",
			len(existingRecords)-len(baseRecords))
	}

	// Build a map of file → unit ID → hash from extracted units.
	currentFileUnits := make(map[string]map[string]string)
	for _, unit := range allUnits {
		if currentFileUnits[unit.File] == nil {
			currentFileUnits[unit.File] = make(map[string]string)
		}
		currentFileUnits[unit.File][unit.ID] = unit.Hash
	}

	// Determine which files have changed by comparing hashes.
	var filesToReembed []string
	for file, unitHashes := range currentFileUnits {
		existingHashes := existingFileUnits[file]
		// File is new or has different unit count → re-embed.
		if len(existingHashes) != len(unitHashes) {
			filesToReembed = append(filesToReembed, file)
			continue
		}
		// Compare hashes unit-by-unit.
		for id, hash := range unitHashes {
			if existingHashes[id] != hash {
				filesToReembed = append(filesToReembed, file)
				break
			}
		}
	}

	// Collect units from changed/new files for embedding.
	var unitsToEmbed []CodeUnit
	reembedSet := make(map[string]bool)
	for _, f := range filesToReembed {
		reembedSet[f] = true
	}
	for _, unit := range allUnits {
		if reembedSet[unit.File] {
			unitsToEmbed = append(unitsToEmbed, unit)
		}
	}

	// Embed only changed units.
	var newRecords []VectorRecord
	if len(unitsToEmbed) > 0 {
		log.Printf("index: re-embedding %d units from %d changed/new files...",
			len(unitsToEmbed), len(filesToReembed))
		embedStart := time.Now()
		newRecords, err = m.embedUnits(ctx, unitsToEmbed)
		if err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: embed units: %w", err)
		}
		log.Printf("index: re-embedded %d units in %s", len(newRecords), time.Since(embedStart))
	}

	// Store all records in a single call.
	// Store() merges by ID: existing records that weren't re-embedded are preserved,
	// and re-embedded records replace their old versions.
	if len(newRecords) > 0 || len(baseRecords) < len(existingRecords) {
		// Combine filtered existing records with new records.
		allStoreRecords := make([]VectorRecord, 0, len(baseRecords)+len(newRecords))
		allStoreRecords = append(allStoreRecords, baseRecords...)
		allStoreRecords = append(allStoreRecords, newRecords...)

		log.Printf("index: storing %d records (%d existing + %d new)...",
			len(allStoreRecords), len(baseRecords), len(newRecords))
		storeStart := time.Now()
		if err := m.store.Store(allStoreRecords); err != nil {
			return stats, fmt.Errorf("index: store: %w", err)
		}
		log.Printf("index: stored %d records in %s", len(allStoreRecords), time.Since(storeStart))
		stats.UnitsEmbedded = len(newRecords)
	} else {
		log.Printf("index: no changes detected, skipping store")
	}
	stats.Duration = time.Since(start)
	return stats, nil
}

// UpdateFile re-indexes a single file: deletes old records, extracts, embeds, and stores.
// Handles both code files (symbol extraction) and non-code files (file-level embedding)
// when IndexFileLevel is enabled.
// If the file does not exist, it deletes any existing records for that file and returns nil
// (graceful handling of deleted files).
func (m *IndexManager) UpdateFile(ctx context.Context, filePath string) error {
	// Always delete old records first (handles deleted files too).
	if err := m.store.DeleteByFile(filePath); err != nil {
		return fmt.Errorf("index: delete file %s: %w", filePath, err)
	}

	// If the file doesn't exist, we're done — records were already deleted.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil
	}

	// Determine which extractor to use
	isCodeFile := hasCodeExtension(filePath)
	var units []CodeUnit
	var err error

	if isCodeFile {
		// Use code extractor for code files
		units, err = ExtractFromFile(filePath, WithIncludeTests(m.opts.IncludeTests))
		if err != nil {
			return fmt.Errorf("index: extract %s: %w", filePath, err)
		}
	} else if m.opts.IndexFileLevel && IsSupportedIndexableFile(filePath) {
		// Use file extractor for non-code files when file-level indexing is enabled
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("index: read %s: %w", filePath, err)
		}
		ext := NewFileExtractor(8000)
		units, err = ext.Extract(filePath, content)
		if err != nil {
			return fmt.Errorf("index: extract %s: %w", filePath, err)
		}
	} else {
		// Not a supported file type for current indexing mode
		return nil
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
// Detects file-level units (ID == file path) vs code units (ID == file:path) and
// uses the appropriate converter to set the Type field correctly.
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
			// Check if this is a file-level unit (ID == file path) or code unit (ID contains :)
			// File-level units from FileExtractor have ID == File
			// Code units from ExtractFromFile have ID == "file:functionName"
			if u.ID == u.File {
				// File-level unit
				records = append(records, fileCodeUnitToRecord(u, vecs[j], now))
			} else {
				// Code unit
				records = append(records, codeUnitToRecord(u, vecs[j], now))
			}
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
		Type:      "code_unit", // All code unit records are type "code_unit"
	}
}

// fileCodeUnitToRecord converts a file-level CodeUnit and its embedding into a VectorRecord.
// Sets Type to "file" to distinguish it from code_unit records.
func fileCodeUnitToRecord(u CodeUnit, embedding []float32, indexedAt time.Time) VectorRecord {
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
		Type:      "file", // File-level records have type "file"
	}
}

// hasCodeExtension checks if a file path has a code extension (.go, .py, .ts, etc.).
func hasCodeExtension(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".py":
		return true
	default:
		return false
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
		if f == "" || !isSupportedFile(f, m.opts.IndexFileLevel) {
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
		if !isSupportedFile(f, m.opts.IndexFileLevel) {
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
// When fileLevel is true, also includes non-code file extensions.
func isSupportedFile(path string, fileLevel bool) bool {
	ext := filepath.Ext(path)

	// Always support code extensions
	codeExts := map[string]bool{
		".go": true, ".ts": true, ".tsx": true,
		".js": true, ".jsx": true, ".mjs": true, ".py": true,
	}
	if codeExts[ext] {
		return true
	}

	// When file-level indexing is enabled, also support non-code extensions
	if fileLevel {
		if IsSupportedIndexableFile(path) {
			return true
		}
	}

	return false
}
