package embedding

import "fmt"

// checkpointManifest incrementally records files as they are embedded so a
// crash mid-build leaves a manifest that matches the records already on disk.
// BuildIndex's final manifest save covers files that made it to the end of a
// build; this type covers the ones that completed before an interruption.
//
// Writes are batched: reloading and rewriting the whole manifest per file is
// O(n²) on large workspaces. recordBatcher persists the store in batches of
// the same size, so a crash between flushes only costs a re-embed of the
// files whose manifest entries were still in memory — never a lost record.
type checkpointManifest struct {
	path     string
	manifest *BuildManifest
	pending  int
}

// manifestCheckpointInterval is how many completed files accumulate before the
// checkpoint manifest is written to disk. recordBatcher uses the same
// interval for its store flushes so the two stay aligned.
const manifestCheckpointInterval = 50

// newCheckpointManifest loads (or creates) the manifest that per-file
// checkpoints accumulate into. A nil path disables checkpointing, as does a
// load error — a corrupt manifest must not abort an otherwise healthy build.
func newCheckpointManifest(path, modelHash string) (*checkpointManifest, error) {
	if path == "" {
		return nil, nil
	}
	manifest, err := LoadManifest(path)
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		manifest = &BuildManifest{Files: make(map[string]int64)}
	}
	// Stamp the current model hash unconditionally. add() only marks a file
	// after its records were persisted to the store (Store runs before the
	// manifest is touched), so a checkpoint written mid-build must never
	// claim the old model — a stale hash made an interrupted model-change
	// build force the next run to re-embed every file (correct but wasteful).
	manifest.ModelHash = modelHash
	return &checkpointManifest{path: path, manifest: manifest}, nil
}

// add marks a file as indexed, flushing to disk every
// manifestCheckpointInterval files. A nil checkpoint is a no-op.
func (c *checkpointManifest) add(file string) error {
	if c == nil {
		return nil
	}
	if mtime, err := fileModTime(file); err == nil {
		c.manifest.Files[file] = mtime
	}
	c.pending++
	if c.pending >= manifestCheckpointInterval {
		return c.flush()
	}
	return nil
}

// flush writes the checkpoint manifest to disk unconditionally. Safe to call
// when nothing is pending; a nil checkpoint is a no-op.
func (c *checkpointManifest) flush() error {
	if c == nil {
		return nil
	}
	c.pending = 0
	return SaveManifest(c.path, c.manifest)
}

// recordBatcher accumulates completed files' records and flushes them to the
// store in batches aligned with the manifest checkpoint interval.
//
// The batches exist because HNSWStore.Store rewrites every record and the
// whole HNSW graph on each call: writing once per file made a build O(N²) in
// store I/O. Flushing every manifestCheckpointInterval files cuts that 50×.
//
// Trade-off: a hard process death between flushes loses at most the in-memory
// batch. Store is an upsert by ID, so the next build simply re-embeds those
// files. Context cancellation is not process death — the caller's final flush
// runs, so the normal timeout path loses nothing.
type recordBatcher struct {
	store         VectorStore
	checkpoint    *checkpointManifest
	flushInterval int
	pending       []VectorRecord
	pendingFiles  []string
}

// newRecordBatcher creates a batcher that flushes to the store every
// flushInterval completed files. A non-positive flushInterval defaults to the
// manifest checkpoint interval.
func newRecordBatcher(store VectorStore, checkpoint *checkpointManifest, flushInterval int) *recordBatcher {
	if flushInterval <= 0 {
		flushInterval = manifestCheckpointInterval
	}
	return &recordBatcher{
		store:         store,
		checkpoint:    checkpoint,
		flushInterval: flushInterval,
	}
}

// add accumulates one completed file's records, flushing to the store once
// flushInterval files have accumulated. It matches the onFileComplete
// callback signature consumed by embedUnits.
func (b *recordBatcher) add(file string, recs []VectorRecord) error {
	b.pending = append(b.pending, recs...)
	b.pendingFiles = append(b.pendingFiles, file)
	if len(b.pendingFiles) < b.flushInterval {
		return nil
	}
	return b.flush()
}

// flush writes all accumulated records to the store in a single call, then
// marks each file indexed in the checkpoint manifest. Safe to call with
// nothing pending; on failure the accumulator is left intact for a retry.
func (b *recordBatcher) flush() error {
	if len(b.pendingFiles) == 0 {
		return nil
	}
	if err := b.store.Store(b.pending); err != nil {
		return fmt.Errorf("index: store %d files: %w", len(b.pendingFiles), err)
	}
	debugLogf("index: store flush: %d files, %d records", len(b.pendingFiles), len(b.pending))
	for _, f := range b.pendingFiles {
		if b.checkpoint != nil {
			if err := b.checkpoint.add(f); err != nil {
				return err
			}
		}
	}
	b.pending = nil
	b.pendingFiles = nil
	return nil
}
