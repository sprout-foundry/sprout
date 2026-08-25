package embedding

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestSearchBackendRecall compares the HNSW index against exhaustive cosine
// over the identical record set, on the held-out query set in value_eval_test.go.
//
// This exists because a full-repository evaluation showed semantic search
// finding the right code for only 2 of 14 conceptual queries — a result that
// reads as "the embeddings are useless" and is not what is happening. The same
// embeddings scored by exhaustive cosine rank the answer in the top 5 for 10 of
// 14. The loss is entirely in the approximate-nearest-neighbour layer.
//
// Two separable defects show up here:
//
//  1. The graph as built by HNSWStore.Store (delete-then-add per record)
//     retrieves worse than the same records reloaded through ReplaceAll into a
//     fresh graph — roughly half the recall, from an identical record set.
//  2. Even a cleanly rebuilt HNSW loses about half the recall of exhaustive
//     search at this corpus size.
//
// And the trade buys almost nothing: exhaustive scan over ~12k records costs
// ~14ms, against ~145ms to embed the query in the first place. HNSW saves under
// 10% of end-to-end query latency in exchange for most of the accuracy.
//
// Opt-in: SPROUT_VALUE_INDEX_DIR=<dir built by TestBuildFullIndexForValueEval>
func TestSearchBackendRecall(t *testing.T) {
	dir := valueEvalIndexDir(t)

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{IndexDir: dir}, repoRoot)
	t.Cleanup(func() { _ = mgr.Close() })
	if err := mgr.Init(ctx); err != nil {
		t.Skipf("embedding init unavailable: %v", err)
	}
	if n := mgr.IndexSize(); n < 1000 {
		t.Skipf("index holds only %d records — run TestBuildFullIndexForValueEval first", n)
	}
	idx, err := mgr.snapshotIndexMgr()
	if err != nil {
		t.Fatalf("index manager: %v", err)
	}
	all, err := idx.store.LoadAll()
	if err != nil {
		t.Fatalf("load records: %v", err)
	}

	matches := func(c valueCase, file, id string) bool {
		rel := relativeTo(repoRoot, file)
		if !strings.HasPrefix(rel, c.wantFile) {
			return false
		}
		return c.wantSymbol == "" || strings.Contains(id, c.wantSymbol)
	}

	type recall struct{ at1, at5, at10 int }
	score := func(rank int, r *recall) {
		if rank == 1 {
			r.at1++
		}
		if rank >= 1 && rank <= 5 {
			r.at5++
		}
		if rank >= 1 && rank <= 10 {
			r.at10++
		}
	}

	var hnsw, brute recall
	for _, c := range valueCases {
		hits, err := idx.QuerySimilar(ctx, c.query, 10, 0.0)
		if err != nil {
			t.Fatalf("%s: hnsw query: %v", c.name, err)
		}
		for i, h := range hits {
			if matches(c, h.Record.File, h.Record.ID) {
				score(i+1, &hnsw)
				break
			}
		}

		vec, err := idx.provider.EmbedWithPrefix(ctx, c.query, codeQueryPrefix)
		if err != nil {
			t.Fatalf("%s: embed: %v", c.name, err)
		}
		order := make([]int, len(all))
		sims := make([]float32, len(all))
		for i := range all {
			order[i] = i
			sims[i] = CosineSimilarity(vec, all[i].Embedding)
		}
		sort.Slice(order, func(a, b int) bool { return sims[order[a]] > sims[order[b]] })
		for i, oi := range order {
			if matches(c, all[oi].File, all[oi].ID) {
				score(i+1, &brute)
				break
			}
		}
	}

	n := len(valueCases)
	t.Logf("records: %d", len(all))
	t.Logf("HNSW       recall@1=%2d/%d  recall@5=%2d/%d  recall@10=%2d/%d", hnsw.at1, n, hnsw.at5, n, hnsw.at10, n)
	t.Logf("exhaustive recall@1=%2d/%d  recall@5=%2d/%d  recall@10=%2d/%d", brute.at1, n, brute.at5, n, brute.at10, n)

	if brute.at5 > hnsw.at5 {
		t.Logf("APPROXIMATION LOSS: exhaustive finds %d more answers in the top 5", brute.at5-hnsw.at5)
	}
	// Guard the property that matters: the ANN layer must not be throwing away
	// most of what the embeddings actually know.
	if hnsw.at10*2 < brute.at5 {
		t.Errorf("HNSW recall@10 (%d) is less than half exhaustive recall@5 (%d) — "+
			"the vector index, not the embedding model, is the limiting factor",
			hnsw.at10, brute.at5)
	}
}
