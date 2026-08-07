package embedding

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

	// Embed only changed units, checkpointing each file as it completes so an
	// interrupted build persists whatever it finished instead of discarding
	// everything accumulated up to the timeout.
	//
	// Store writes are batched because HNSWStore.Store rewrites the whole
	// records JSON plus HNSW graph on every call: flushing per file made a
	// build O(N²) in store I/O. The recordBatcher flushes every
	// manifestCheckpointInterval files plus a final flush after embedUnits
	// returns (including on cancellation, so the timeout path loses nothing).
	// A hard process death between flushes loses at most the in-memory batch;
	// Store is an upsert by ID, so the next build simply re-embeds those
	// files. The 50× write-amplification reduction is worth that trade.
	var newRecords []VectorRecord
	if len(unitsToEmbed) > 0 {
		debugLogf("index: re-embedding %d units...", len(unitsToEmbed))
		embedStart := time.Now()

		var checkpoint *checkpointManifest
		if m.opts.ManifestPath != "" {
			checkpoint, err = newCheckpointManifest(m.opts.ManifestPath, m.provider.ModelHash())
			if err != nil {
				debugLogf("index: manifest checkpoint disabled (continuing without): %v", err)
				checkpoint = nil
			}
		}

		batcher := newRecordBatcher(m.store, checkpoint, manifestCheckpointInterval)

		newRecords, err = m.embedUnits(ctx, unitsToEmbed, rootDir, batcher.add)
		// Flush whatever the callback left pending even when embedding
		// aborted, so the store on disk matches the records embedUnits
		// reports (and the manifest matches the store).
		flushErr := batcher.flush()
		if checkpoint != nil {
			// Persist whatever the manifest checkpoint left pending even when
			// embedding aborted, so the manifest on disk matches the store.
			if cerr := checkpoint.flush(); cerr != nil {
				debugLogf("index: manifest checkpoint flush failed (non-fatal): %v", cerr)
			}
		}
		if err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: embed units: %w", err)
		}
		if flushErr != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: flush pending records: %w", flushErr)
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

		stats.UnitsEmbedded = len(newRecords)
		if len(newRecords) > 0 {
			// Records were already persisted per-file by the checkpoint
			// callback during embedding; there is no end-of-build flush.
			debugLogf("index: stored %d records via per-file checkpoints", len(newRecords))
		} else if len(staleIDs) == 0 {
			debugLogf("index: no changes detected, skipping store")
		}
	}

	// Save the manifest covering only files this build actually finished.
	//
	// embedUnits degrades gracefully on timeout: it returns the records it got
	// and a nil error. Recording every walked file as indexed on top of that
	// used to freeze the index permanently — the next build's mtime diff saw
	// nothing changed, re-embedded nothing, and the partial record count stood
	// forever. A full build of this repository is projected well past the
	// 15-minute auto-build budget, so the interrupted case is the normal case,
	// not an edge case. Excluding partially-embedded files is what makes a
	// build resumable across runs.
	if m.opts.ManifestPath != "" {
		wantByFile := make(map[string]int, len(unitsToEmbed))
		for _, u := range unitsToEmbed {
			wantByFile[u.File]++
		}
		gotByFile := make(map[string]int, len(newRecords))
		for _, r := range newRecords {
			gotByFile[r.File]++
		}

		allFiles := make([]string, 0, len(changedFiles)+len(unchangedFiles))
		var incomplete int
		for _, f := range changedFiles {
			if gotByFile[f] < wantByFile[f] {
				incomplete++
				continue
			}
			allFiles = append(allFiles, f)
		}
		// Manifest-skipped files were never re-examined, so they remain as
		// indexed as they were.
		allFiles = append(allFiles, unchangedFiles...)

		if incomplete > 0 {
			debugLogf("index: %d file(s) only partially embedded; excluded from manifest so the next build resumes them", incomplete)
		}

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

	records, err := m.embedUnits(ctx, units, "", nil)
	if err != nil {
		return fmt.Errorf("index: embed %s: %w", filePath, err)
	}

	if err := m.store.Store(records); err != nil {
		return fmt.Errorf("index: store %s: %w", filePath, err)
	}

	return nil
}

// queryWithPrefix embeds text under the given EmbeddingGemma task prefix and
// returns the top-K records above threshold.
//
// The prefix is not decoration: EmbeddingGemma embeds queries and documents
// into deliberately different subspaces, so the same pair of texts scores very
// differently depending on which prefix each side carried. Picking the wrong
// one silently shifts the whole similarity distribution out from under
// whatever threshold the caller applies.
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

// embedUnits converts CodeUnits to text, batch-embeds, and returns VectorRecords.
// On context cancellation (timeout), it returns partial results instead of an error
// so that the caller can store whatever was processed so far.
// Detects file-level units (ID == file path) vs code units (ID == file:path) and
// uses the appropriate converter to set the Type field correctly.
//
// When onFileComplete is non-nil, it is invoked the moment every unit belonging
// to a file has been embedded — in batch completion order, not input order —
// with that file's records. BuildIndex feeds these to a recordBatcher that
// persists completed files in batches, so an interrupted build keeps the files
// it finished instead of discarding them (the single end-of-build Store call
// never ran on devices where the timeout fired mid-embed).
func (m *IndexManager) embedUnits(ctx context.Context, units []CodeUnit, repoRoot string, onFileComplete func(file string, records []VectorRecord) error) ([]VectorRecord, error) {
	now := time.Now()
	var records []VectorRecord
	var embedded int

	// Embed short units alongside short ones. Every row in an ORT chunk pads up
	// to that chunk's longest row, so batching in extraction order makes a
	// one-line function pay for the 2000-byte file-level unit that happens to
	// sit beside it. Measured on this repository: median unit is 122 tokens but
	// p90 is 540, and extraction-order batching does 3.4x the token-positions
	// actually needed. Grouping by length cuts that to 1.1x — 2.4x faster
	// end-to-end (2.4 -> 5.9 units/s).
	//
	// Sorting a permutation rather than `units` keeps the caller's slice
	// untouched and lets the output stay in input order.
	order := make([]int, len(units))
	textOf := make([]string, len(units))
	for i := range units {
		order[i] = i
		textOf[i] = embeddingText(units[i], m.opts.MaxBodyLen)
	}

	// Embed recently-touched files first so the partial index becomes
	// semantically useful within ~1 min instead of ~17 min on a full build.
	// The store is flushed and queryable during embedding, so ordering
	// determines when useful results appear to the user.
	priority := buildFilePriority(repoRoot, uniqueFiles(units))
	if len(priority) > 0 {
		var t0, t1, t2 int
		for _, u := range units {
			switch priority[u.File] {
			case 0:
				t0++
			case 1:
				t1++
			default:
				t2++
			}
		}
		debugLogf("index: priority tiers — recent: %d, 30d: %d, older: %d units", t0, t1, t2)
	}

	sort.SliceStable(order, func(a, b int) bool {
		pa := priority[units[order[a]].File]
		pb := priority[units[order[b]].File]
		if pa != pb {
			return pa < pb
		}
		return len(textOf[order[a]]) < len(textOf[order[b]])
	})

	// Vectors land here by original index so a partial (cancelled) run still
	// reports which specific units completed.
	vecByIndex := make([][]float32, len(units))

	// Group unit indices by file so a file's completion can be detected the
	// moment its last unit embeds, no matter which batch that lands in.
	unitsByFile := make(map[string][]int, len(units))
	for i, u := range units {
		unitsByFile[u.File] = append(unitsByFile[u.File], i)
	}
	embeddedByFile := make(map[string]int, len(unitsByFile))
	completedFiles := make(map[string]bool, len(unitsByFile))

	// recordsForFile assembles one file's records from the vectors that have
	// landed. Only called once every unit of the file has embedded, so every
	// index is non-nil.
	recordsForFile := func(file string) []VectorRecord {
		idxs := unitsByFile[file]
		out := make([]VectorRecord, 0, len(idxs))
		for _, idx := range idxs {
			u := units[idx]
			if u.ID == u.File {
				out = append(out, fileCodeUnitToRecord(u, vecByIndex[idx], now))
			} else {
				out = append(out, codeUnitToRecord(u, vecByIndex[idx], now))
			}
		}
		return out
	}

	// markEmbedded advances per-file progress for one batch and fires
	// onFileComplete for any file whose last unit just landed.
	markEmbedded := func(idxs []int) error {
		if onFileComplete == nil {
			return nil
		}
		for _, idx := range idxs {
			file := units[idx].File
			if completedFiles[file] {
				continue
			}
			embeddedByFile[file]++
			if embeddedByFile[file] < len(unitsByFile[file]) {
				continue
			}
			completedFiles[file] = true
			if err := onFileComplete(file, recordsForFile(file)); err != nil {
				return err
			}
		}
		return nil
	}

	for i := 0; i < len(order); i += m.opts.BatchSize {
		if err := ctx.Err(); err != nil {
			// Graceful degradation: return partial results on timeout/cancellation.
			// Completed files were already handed to onFileComplete (and the
			// caller flushes them), so `embedded` is the honest progress figure —
			// len(records) is still empty here.
			log.Printf("index: embedding interrupted after %d/%d units: %v",
				embedded, len(units), err)
			break
		}

		end := i + m.opts.BatchSize
		if end > len(order) {
			end = len(order)
		}

		idxs := order[i:end]
		texts := make([]string, len(idxs))
		for j, idx := range idxs {
			texts[j] = textOf[idx]
		}

		vecs, err := m.provider.EmbedBatchWithPrefix(ctx, texts, documentPrefix)
		if err != nil {
			return records, fmt.Errorf("index: embed batch [%d:%d]: %w", i, end, err)
		}

		for j, idx := range idxs {
			vecByIndex[idx] = vecs[j]
		}
		embedded += len(idxs)

		if err := markEmbedded(idxs); err != nil {
			return records, err
		}

		// Log progress every ProgressInterval records embedded.
		if embedded%ProgressInterval < m.opts.BatchSize {
			debugLogf("index: embedding progress: %d/%d records", embedded, len(units))
		}
	}

	// Emit in input order so records stay grouped by file, which is what the
	// caller's per-file completeness check reads.
	for i, u := range units {
		if vecByIndex[i] == nil {
			continue // not reached before cancellation
		}
		// Check if this is a file-level unit (ID == file path) or code unit (ID contains :)
		// File-level units from FileExtractor have ID == File
		// Code units from ExtractFromFile have ID == "file:functionName"
		if u.ID == u.File {
			// File-level unit
			records = append(records, fileCodeUnitToRecord(u, vecByIndex[i], now))
		} else {
			// Code unit
			records = append(records, codeUnitToRecord(u, vecByIndex[i], now))
		}
	}

	return records, nil
}

// embeddingText builds the text to embed from a CodeUnit, with optional body truncation.
func embeddingText(u CodeUnit, maxBodyLen int) string {
	body := u.Body
	if maxBodyLen > 0 && len(body) > maxBodyLen {
		// Truncate to maxBodyLen bytes, snapping back to the last valid
		// UTF-8 character boundary so we don't produce invalid runes.
		// This avoids the cost of converting the entire body to []rune
		// just to truncate it.
		body = truncateUTF8Safe(body, maxBodyLen)
	}
	return u.Signature + "\n" + body
}

// truncateUTF8Safe truncates s to at most maxBytes bytes, snapping back to
// the last valid UTF-8 character boundary if the cut falls mid-character.
func truncateUTF8Safe(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// A UTF-8 continuation byte has the top two bits as 10 (0x80 set, 0x40 clear).
	// If the byte at maxBytes is a continuation byte, we're mid-character — walk
	// backward to find the start of that character.
	for maxBytes > 0 && s[maxBytes]&0xC0 == 0x80 {
		maxBytes--
	}
	return s[:maxBytes]
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

// buildFilePriority assigns each file a priority tier using git recency.
// Tier 0 = modified within 7 days, Tier 1 = modified within 30 days,
// Tier 2 = older or not found in git history.
// Returns a map of file path → tier. If git fails, returns an empty map
// so the caller falls back to pure length-based ordering.
func buildFilePriority(repoRoot string, files []string) map[string]int {
	if repoRoot == "" {
		return nil
	}

	recent7, err := runGit(repoRoot, "log", "--name-only", "--format=", "--since=7 days ago")
	if err != nil {
		return nil
	}
	recent30, err := runGit(repoRoot, "log", "--name-only", "--format=", "--since=30 days ago")
	if err != nil {
		return nil
	}

	set7 := make(map[string]bool)
	for _, f := range recent7 {
		set7[filepath.Clean(f)] = true
	}
	set30 := make(map[string]bool)
	for _, f := range recent30 {
		set30[filepath.Clean(f)] = true
	}

	result := make(map[string]int, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(repoRoot, f)
		if err != nil {
			result[f] = 2
			continue
		}
		clean := filepath.Clean(rel)
		switch {
		case set7[clean]:
			result[f] = 0
		case set30[clean]:
			result[f] = 1
		default:
			result[f] = 2
		}
	}
	return result
}

// uniqueFiles extracts the distinct file paths from a slice of CodeUnits.
func uniqueFiles(units []CodeUnit) []string {
	seen := make(map[string]bool)
	var files []string
	for _, u := range units {
		if !seen[u.File] {
			seen[u.File] = true
			files = append(files, u.File)
		}
	}
	return files
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
