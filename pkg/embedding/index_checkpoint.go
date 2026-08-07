package embedding

import "fmt"

// checkpointManifest incrementally records files as they are embedded for
// crash-safe manifests. recordBatcher aligns store flushes with the same
// interval.
type checkpointManifest struct {
	path     string
	manifest *BuildManifest
	pending  int
}

// manifestCheckpointInterval: flush checkpoint every N completed files.
const manifestCheckpointInterval = 50

// newCheckpointManifest loads or creates the checkpoint manifest. A nil path
// or load error disables checkpointing.
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
	// Stamp current model hash so a checkpoint never claims the old model.
	manifest.ModelHash = modelHash
	return &checkpointManifest{path: path, manifest: manifest}, nil
}

// add marks a file as indexed; flushes every manifestCheckpointInterval files.
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

// recordBatcher accumulates records and flushes to the store in batches
// aligned with manifest checkpoints.
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

// flush writes accumulated records then marks files in the checkpoint manifest.
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
