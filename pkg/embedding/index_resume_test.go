package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// cancellingProvider embeds normally until it has served maxBatches, then
// cancels the build context — standing in for the auto-build timeout, which on
// any real-sized workspace fires before the build finishes.
type cancellingProvider struct {
	*mockProvider
	cancel     context.CancelFunc
	maxBatches int
	batches    int
}

// The batch itself succeeds and only then does the deadline lapse, matching a
// real timeout: embedUnits notices at the top of the next iteration and returns
// what it has with a nil error, rather than failing the build outright.
func (p *cancellingProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	vecs, err := p.mockProvider.EmbedBatchWithPrefix(ctx, texts, prefix)
	p.batches++
	if p.batches >= p.maxBatches {
		p.cancel()
	}
	return vecs, err
}

// A build that runs out of time must leave the workspace resumable. Recording
// every walked file as indexed made the next build's mtime diff report "nothing
// changed", so the index froze at whatever partial count the first run reached
// and never grew again — indistinguishable from indexing being broken.
func TestBuildIndexResumesAfterInterruptedBuild(t *testing.T) {
	workspace := t.TempDir()
	const fileCount = 12
	for i := 0; i < fileCount; i++ {
		src := fmt.Sprintf("package p\n\nfunc F%d() int { return %d }\n\nfunc G%d() string { return \"%d\" }\n", i, i, i, i)
		writeFile(t, filepath.Join(workspace, fmt.Sprintf("f%d.go", i)), src)
	}

	indexDir := t.TempDir()
	manifestPath := filepath.Join(indexDir, "manifest.json")
	store := newCountingStore()

	// --- Build 1: cancelled after a couple of batches. ---
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	interrupted := &cancellingProvider{
		mockProvider: newMockProvider(8),
		cancel:       cancel,
		maxBatches:   1,
	}
	idx := NewIndexManager(interrupted, store, IndexOptions{
		BatchSize:    4,
		MaxBodyLen:   2000,
		ManifestPath: manifestPath,
	})
	if _, err := idx.BuildIndex(ctx, workspace); err != nil {
		t.Fatalf("interrupted build returned an error instead of partial progress: %v", err)
	}

	partial := store.Size()
	if partial == 0 {
		t.Fatal("interrupted build stored nothing; the probe cannot distinguish the bug")
	}
	if partial == fileCount*2 {
		t.Fatal("build was not actually interrupted; test proves nothing")
	}

	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if got := len(manifest.Files); got >= fileCount {
		t.Errorf("manifest claims %d/%d files indexed after a partial build — the next build will skip them", got, fileCount)
	}

	// --- Build 2: fresh context, working provider. Must make progress. ---
	idx2 := NewIndexManager(newMockProvider(8), store, IndexOptions{
		BatchSize:    4,
		MaxBodyLen:   2000,
		ManifestPath: manifestPath,
	})
	if _, err := idx2.BuildIndex(context.Background(), workspace); err != nil {
		t.Fatalf("resume build: %v", err)
	}

	if got := store.Size(); got != fileCount*2 {
		t.Errorf("after resume the index holds %d records, want %d — the build did not resume", got, fileCount*2)
	}

	// --- Build 3: everything is indexed; this one must be a no-op. ---
	before := store.writes.Load()
	if _, err := idx2.BuildIndex(context.Background(), workspace); err != nil {
		t.Fatalf("steady-state build: %v", err)
	}
	if store.writes.Load() != before {
		t.Errorf("a fully-indexed workspace still wrote to the store; incremental detection is broken")
	}
}

// A completed build must record every file, or every subsequent build re-embeds
// the whole workspace — the pathology that made indexing look like it never
// finished in the first place.
func TestBuildIndexRecordsAllFilesOnCompletion(t *testing.T) {
	workspace := t.TempDir()
	const fileCount = 5
	for i := 0; i < fileCount; i++ {
		writeFile(t, filepath.Join(workspace, fmt.Sprintf("f%d.go", i)),
			fmt.Sprintf("package p\n\nfunc F%d() int { return %d }\n", i, i))
	}

	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	idx := NewIndexManager(newMockProvider(8), newCountingStore(), IndexOptions{
		BatchSize:    4,
		MaxBodyLen:   2000,
		ManifestPath: manifestPath,
	})
	if _, err := idx.BuildIndex(context.Background(), workspace); err != nil {
		t.Fatalf("build: %v", err)
	}

	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(manifest.Files) != fileCount {
		t.Errorf("manifest holds %d files after a complete build, want %d", len(manifest.Files), fileCount)
	}
	for i := 0; i < fileCount; i++ {
		path := filepath.Join(workspace, fmt.Sprintf("f%d.go", i))
		if _, ok := manifest.Files[path]; !ok {
			abs, _ := filepath.Abs(path)
			if _, ok := manifest.Files[abs]; !ok {
				t.Errorf("manifest missing %s", path)
			}
		}
	}
	_ = os.Remove(manifestPath)
}
