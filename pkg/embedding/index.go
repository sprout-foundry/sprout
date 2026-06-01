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

// EmbeddingGemma task prefixes for task-specific embedding.
// These match the prefixes defined in the ONNX provider to ensure queries
// and documents are embedded into the correct semantic space.
const (
	// documentPrefix is prepended to code/text before embedding for indexing.
	documentPrefix = "title: none | text: "

	// queryPrefix is prepended to search queries before embedding.
	queryPrefix = "task: search result | query: "

	// codeQueryPrefix is prepended to code-specific search queries.
	codeQueryPrefix = "task: code retrieval | query: "
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
	// ManifestPath is the path to the build manifest file that tracks file
	// modification times from the last successful build. When set, BuildIndex
	// uses the manifest to skip parsing unchanged files.
	ManifestPath string
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
// When ManifestPath is set, uses an mtime-based manifest to skip parsing
// unchanged files entirely, turning a multi-minute full parse into a
// ~2-second stat sweep on warm indexes.
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

	// Attempt mtime-based manifest optimization to skip parsing unchanged files.
	var (
		changedFiles        []string
		unchangedFiles      []string
		manifest            *BuildManifest
		manifestInvalidated bool // true when model hash changed, forces full re-embed
	)

	if m.opts.ManifestPath != "" {
		manifest, err = LoadManifest(m.opts.ManifestPath)
		if err != nil {
			debugLogf("index: manifest load failed (falling back): %v", err)
		}
	}

	if manifest != nil {
		diff, err := DiffManifest(ctx, manifest, m.provider.ModelHash(), rootDir, m.opts.IndexFileLevel)
		if err != nil {
			debugLogf("index: manifest diff failed (falling back): %v", err)
		} else {
			changedFiles = diff.ChangedFiles
			unchangedFiles = diff.UnchangedFiles
			manifestInvalidated = diff.ManifestInvalidated

			debugLogf("index: manifest: %d changed, %d unchanged, %d deleted (out of %d walked)",
				len(changedFiles), len(unchangedFiles), len(diff.DeletedFiles), len(files))
		}
	}

	// If manifest didn't provide a filtered list, parse all files.
	if changedFiles == nil && unchangedFiles == nil {
		changedFiles = files
	}

	var allUnits []CodeUnit
	var fileExtractor *FileExtractor
	if m.opts.IndexFileLevel {
		fileExtractor = NewFileExtractor(8000)
	}

	for _, path := range changedFiles {
		if err := ctx.Err(); err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: cancelled during file extraction")
		}

		isCodeFile := hasCodeExtension(path)
		isIndexableFile := IsSupportedIndexableFile(path)

		var units []CodeUnit
		if isCodeFile {
			units, err = ExtractFromFile(path, WithIncludeTests(m.opts.IncludeTests))
			if err != nil {
				debugLogf("index: skipping %s: %v", path, err)
				continue
			}
		} else if isIndexableFile {
			content, err := os.ReadFile(path)
			if err != nil {
				debugLogf("index: skipping %s: %v", path, err)
				continue
			}
			units, err = fileExtractor.Extract(path, content)
			if err != nil {
				debugLogf("index: skipping %s: %v", path, err)
				continue
			}
		} else {
			continue
		}

		stats.FilesProcessed++
		allUnits = append(allUnits, units...)

		if stats.FilesProcessed%ProgressInterval == 0 {
			debugLogf("index: extraction progress: %d files, %d units", stats.FilesProcessed, len(allUnits))
		}
	}

	stats.UnitsExtracted = len(allUnits)

	// Note: we no longer early-return when allUnits is empty. Even if no
	// files changed, existing records for files that were deleted from
	// the workspace must still be cleaned up below.

	// --- Incremental rebuild logic ---

	// Build a map of file → unit ID → hash from existing records.
	existingFileUnits := make(map[string]map[string]string)
	for _, rec := range existingRecords {
		if existingFileUnits[rec.File] == nil {
			existingFileUnits[rec.File] = make(map[string]string)
		}
		existingFileUnits[rec.File][rec.ID] = rec.Hash
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
	// When the manifest is invalidated (model hash changed), skip hash comparison
	// and re-embed everything with the new model.
	var unitsToEmbed []CodeUnit
	if manifestInvalidated {
		debugLogf("index: model hash changed (manifest invalidated), re-embedding all %d units", len(allUnits))
		unitsToEmbed = allUnits
	} else {
		var filesToReembed []string
		for file, unitHashes := range currentFileUnits {
			existingHashes := existingFileUnits[file]
			if len(existingHashes) != len(unitHashes) {
				filesToReembed = append(filesToReembed, file)
				continue
			}
			for id, hash := range unitHashes {
				if existingHashes[id] != hash {
					filesToReembed = append(filesToReembed, file)
					break
				}
			}
		}

		reembedSet := make(map[string]bool)
		for _, f := range filesToReembed {
			reembedSet[f] = true
		}
		for _, unit := range allUnits {
			if reembedSet[unit.File] {
				unitsToEmbed = append(unitsToEmbed, unit)
			}
		}
	}

	// Embed only changed units.
	var newRecords []VectorRecord
	if len(unitsToEmbed) > 0 {
		debugLogf("index: re-embedding %d units...", len(unitsToEmbed))
		embedStart := time.Now()
		newRecords, err = m.embedUnits(ctx, unitsToEmbed)
		if err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: embed units: %w", err)
		}
		debugLogf("index: re-embedded %d units in %s", len(newRecords), time.Since(embedStart))
	}

	// Manifest-invalidated path: model changed, every embedding is stale.
	// ReplaceAll wipes the store and writes the freshly-embedded records.
	if manifestInvalidated && len(newRecords) > 0 {
		debugLogf("index: replacing all records with %d re-embedded records (model changed)", len(newRecords))
		storeStart := time.Now()
		if err := m.store.ReplaceAll(newRecords); err != nil {
			return stats, fmt.Errorf("index: store: %w", err)
		}
		debugLogf("index: stored %d records in %s", len(newRecords), time.Since(storeStart))
		stats.UnitsEmbedded = len(newRecords)
	} else {
		// Compute stale record IDs: records whose owning file or symbol no
		// longer exists in the workspace. Two cases:
		//   1. The file was re-walked (in currentFileUnits) and the symbol
		//      ID is missing from the new extraction → symbol was removed.
		//   2. The file is absent from both changedFiles (walked) and
		//      unchangedFiles (manifest-skipped) → file was deleted.
		// Records for manifest-skipped files are left alone; we have no
		// evidence they're stale.
		unchangedSet := make(map[string]bool, len(unchangedFiles))
		for _, f := range unchangedFiles {
			unchangedSet[f] = true
		}

		var staleIDs []string
		for _, rec := range existingRecords {
			if walked, ok := currentFileUnits[rec.File]; ok {
				if _, stillExists := walked[rec.ID]; !stillExists {
					staleIDs = append(staleIDs, rec.ID)
				}
				continue
			}
			if !unchangedSet[rec.File] {
				staleIDs = append(staleIDs, rec.ID)
			}
		}

		if len(staleIDs) > 0 {
			debugLogf("index: removing %d stale records (deleted files + removed symbols)", len(staleIDs))
			if err := m.store.DeleteByIDs(staleIDs); err != nil {
				return stats, fmt.Errorf("index: delete stale records: %w", err)
			}
		}

		if len(newRecords) > 0 {
			debugLogf("index: storing %d new records...", len(newRecords))
			storeStart := time.Now()
			if err := m.store.Store(newRecords); err != nil {
				return stats, fmt.Errorf("index: store: %w", err)
			}
			debugLogf("index: stored %d records in %s", len(newRecords), time.Since(storeStart))
			stats.UnitsEmbedded = len(newRecords)
		} else if len(staleIDs) == 0 {
			debugLogf("index: no changes detected, skipping store")
		}
	}

	// Save manifest after successful store (always, when ManifestPath is set,
	// so the manifest covers the full workspace including unchanged files).
	if m.opts.ManifestPath != "" {
		// Build manifest from all files in the workspace (changed + unchanged).
		allFiles := make([]string, 0, len(changedFiles)+len(unchangedFiles))
		allFiles = append(allFiles, changedFiles...)
		allFiles = append(allFiles, unchangedFiles...)
		manifest = BuildManifestFromFiles(allFiles, m.provider.ModelHash())
		if err := SaveManifest(m.opts.ManifestPath, manifest); err != nil {
			debugLogf("index: manifest save failed (non-fatal): %v", err)
		}
	}

	stats.Duration = time.Since(start)
	return stats, nil
}

// UpdateFile re-indexes a single file: deletes old records, extracts, embeds, and stores.
// Handles both code files (symbol extraction) and non-code files (file-level embedding)
// when IndexFileLevel is enabled.
func (m *IndexManager) UpdateFile(ctx context.Context, filePath string) error {
	// Always delete old records first (handles deleted files too).
	if err := m.store.DeleteByFile(filePath); err != nil {
		return fmt.Errorf("index: delete file %s: %w", filePath, err)
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
	vec, err := m.provider.EmbedWithPrefix(ctx, query, queryPrefix)
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

		vecs, err := m.provider.EmbedBatchWithPrefix(ctx, texts, documentPrefix)
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
			debugLogf("index: embedding progress: %d/%d records", len(records), len(units))
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
			debugLogf("index: skipping delete %s: %v", f, err)
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
			debugLogf("index: skipping %s: %v", f, err)
			errs = append(errs, f)
			continue
		}
		stats.FilesProcessed++
	}

	if len(errs) > 0 {
		return stats, fmt.Errorf("index: failed to update %d files: %v", len(errs), errs)
	}

	// Update manifest for changed/deleted files.
	if m.opts.ManifestPath != "" {
		m.updateManifestForDiff(fileSet, toDelete)
	}

	stats.Duration = time.Since(start)
	return stats, nil
}

// updateManifestForDiff updates the manifest entries for files that changed
// in a git diff update. It updates mtimes for changed files and removes
// entries for deleted files.
func (m *IndexManager) updateManifestForDiff(updatedFiles map[string]bool, deletedFiles map[string]bool) {
	manifest, err := LoadManifest(m.opts.ManifestPath)
	if err != nil || manifest == nil {
		manifest = &BuildManifest{
			Files:     make(map[string]int64),
			ModelHash: m.provider.ModelHash(),
		}
	}

	for f := range updatedFiles {
		mtime, e := fileModTime(f)
		if e == nil {
			manifest.Files[f] = mtime
		}
	}
	for f := range deletedFiles {
		delete(manifest.Files, f)
	}

	if err := SaveManifest(m.opts.ManifestPath, manifest); err != nil {
		debugLogf("index: update manifest after diff failed (non-fatal): %v", err)
	}
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
