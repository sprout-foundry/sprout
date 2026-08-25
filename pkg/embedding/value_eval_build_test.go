package embedding

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// valueEvalIndexDir is where the full-repository index for the value
// evaluation is built and cached, so the ~30 minute build happens once and any
// number of eval runs reuse it.
func valueEvalIndexDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("SPROUT_VALUE_INDEX_DIR")
	if dir == "" {
		t.Skip("SPROUT_VALUE_INDEX_DIR unset")
	}
	return dir
}

// TestBuildFullIndexForValueEval builds a real index over the whole repository.
// Separate from the eval so the expensive build is not repeated.
//
// Opt-in: SPROUT_VALUE_INDEX_DIR=<dir> SPROUT_VALUE_BUILD=1
func TestBuildFullIndexForValueEval(t *testing.T) {
	if os.Getenv("SPROUT_VALUE_BUILD") != "1" {
		t.Skip("SPROUT_VALUE_BUILD unset")
	}
	dir := valueEvalIndexDir(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()

	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{IndexDir: dir}, "../..")
	t.Cleanup(func() { _ = mgr.Close() })
	if err := mgr.Init(ctx); err != nil {
		t.Skipf("embedding init unavailable: %v", err)
	}
	idx, err := mgr.snapshotIndexMgr()
	if err != nil {
		t.Fatalf("index manager: %v", err)
	}

	start := time.Now()
	stats, err := idx.BuildIndex(ctx, "../..")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("indexed %d files / %d units embedded / %d extracted in %s",
		stats.FilesProcessed, stats.UnitsEmbedded, stats.UnitsExtracted, time.Since(start).Round(time.Second))
	t.Logf("store size: %d records", mgr.IndexSize())
}
