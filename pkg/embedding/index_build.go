package embedding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"time"
)

// index_build.go — the full index build and single-file update path:
// BuildIndex, UpdateFile. Split out of index.go.

// BuildIndex walks rootDir, extracts code units, embeds them, and stores them.
// Uses incremental rebuild with mtime-based manifest to skip unchanged files.
func (m *IndexManager) BuildIndex(ctx context.Context, rootDir string) (*IndexStats, error) {
	prevLimit := debug.SetMemoryLimit(buildIndexMemoryLimit)
	defer debug.SetMemoryLimit(prevLimit)
	logMemCheckpoint("start")

	start := time.Now()
	stats := &IndexStats{}

	// Cross-process lock to prevent concurrent builds from corrupting the index.
	release, err := m.lockForBuild()
	if release != nil {
		defer release()
	}
	if err == errBuildLocked {
		debugLogf("index: build skipped — lock held by another process")
		stats.Duration = time.Since(start)
		return stats, nil
	}
	if err != nil {
		return nil, fmt.Errorf("index: acquire build lock: %w", err)
	}

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
			// Skip oversized files before any read: file-level indexing
			// truncates to 8 KB anyway, and a multi-GB corpus would OOM.
			fi, err := os.Stat(path)
			if err != nil {
				debugLogf("index: skipping %s: %v", path, err)
				continue
			}
			if fi.Size() > MaxIndexableFileBytes {
				debugLogf("index: skipping %s: %d bytes exceeds %d-byte indexable limit",
					path, fi.Size(), MaxIndexableFileBytes)
				continue
			}
			// Defense-in-depth: LimitReader bounds the read even if the file
			// grew between Stat and Open (or is a symlink to something huge).
			f, err := os.Open(path)
			if err != nil {
				debugLogf("index: skipping %s: %v", path, err)
				continue
			}
			content, err := io.ReadAll(io.LimitReader(f, MaxIndexableFileBytes))
			if cerr := f.Close(); cerr != nil && err == nil {
				err = cerr
			}
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
	logMemCheckpoint(fmt.Sprintf("after-extraction (%d units)", len(allUnits)))

	// Note: we don't early-return when allUnits is empty — deleted file records still need cleanup below.

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

	// Embed only changed units, checkpointing each file as it completes.
	// Batch store writes to avoid O(N²) I/O — HNSWStore.Store rewrites
	// the whole records JSON plus HNSW graph on every call.
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
			if errors.Is(err, errMemFloor) && len(newRecords) > 0 {
				// The floor is a system-wide condition, not a per-file failure,
				// and partial records were already checkpointed per-file. Treat
				// it as a clean stop: keep the partial index and fall through
				// to the normal completion path (stale cleanup, manifest save).
				// Zero-progress floor trips stay hard errors — an empty build
				// result would be indistinguishable from "nothing to index".
				log.Printf("index: embedding halted at memory floor; keeping partial results")
			} else {
				stats.Duration = time.Since(start)
				return stats, fmt.Errorf("index: embed units: %w", err)
			}
		}
		if flushErr != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("index: flush pending records: %w", flushErr)
		}
		debugLogf("index: re-embedded %d units in %s", len(newRecords), time.Since(embedStart))
		logMemCheckpoint(fmt.Sprintf("after-embedUnits (%d records)", len(newRecords)))
	}

	// Model changed: replace all records with fresh embeddings.
	if manifestInvalidated && len(newRecords) > 0 {
		debugLogf("index: replacing all records with %d re-embedded records (model changed)", len(newRecords))
		storeStart := time.Now()
		if err := m.store.ReplaceAll(newRecords); err != nil {
			return stats, fmt.Errorf("index: store: %w", err)
		}
		debugLogf("index: stored %d records in %s", len(newRecords), time.Since(storeStart))
		stats.UnitsEmbedded = len(newRecords)
	} else {
		// Compute stale record IDs: removed symbols and deleted files.
		// Manifest-skipped files are left alone.
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

	// Save manifest covering only files this build actually finished.
	// Partially-embedded files are excluded so the next build resumes them.
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
	// Cross-process lock (re-entrant: safe when called from UpdateFromGitDiff).
	release, err := m.lockForBuild()
	if release != nil {
		defer release()
	}
	if err == errBuildLocked {
		return fmt.Errorf("index: update %s: %w", filePath, errBuildLocked)
	}
	if err != nil {
		return fmt.Errorf("index: acquire build lock: %w", err)
	}

	// Always delete old records first (handles deleted files too).
	if err := m.store.DeleteByFile(filePath); err != nil {
		return fmt.Errorf("index: delete file %s: %w", filePath, err)
	}

	// Determine which extractor to use
	isCodeFile := hasCodeExtension(filePath)
	var units []CodeUnit

	if isCodeFile {
		// Use code extractor for code files
		units, err = ExtractFromFile(filePath, WithIncludeTests(m.opts.IncludeTests))
		if err != nil {
			return fmt.Errorf("index: extract %s: %w", filePath, err)
		}
	} else if m.opts.IndexFileLevel && IsSupportedIndexableFile(filePath) {
		// Skip oversized files entirely: file-level indexing truncates to
		// 8 KB anyway, and a multi-GB corpus would OOM the read path.
		fi, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("index: stat %s: %w", filePath, err)
		}
		if fi.Size() > MaxIndexableFileBytes {
			debugLogf("index: skipping %s: %d bytes exceeds %d-byte indexable limit",
				filePath, fi.Size(), MaxIndexableFileBytes)
			return nil
		}
		// Defense-in-depth: LimitReader bounds the read even if the file
		// grew between Stat and Open (or is a symlink to something huge).
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("index: open %s: %w", filePath, err)
		}
		content, err := io.ReadAll(io.LimitReader(f, MaxIndexableFileBytes))
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
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
