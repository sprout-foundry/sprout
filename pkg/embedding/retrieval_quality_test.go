package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// Production thresholds, gathered here because they are scattered and
// inconsistent. Each gates a different consumer of the same index.
const (
	thresholdManagerDefault = DefaultSemanticSearchThreshold
	thresholdFileDupCheck   = DefaultDuplicateThreshold
	thresholdSemanticRecall = 0.45 // semantic_recall.go:34
)

type retrievalCase struct {
	name     string
	query    string
	expected []string // case-insensitive substrings; a hit matches any
}

// TestRetrievalQuality builds a real index over pkg/embedding and measures
// whether semantic search returns the right code AND whether the thresholds
// the product actually ships would let those results through.
//
// The pre-existing eval harness (retrieval_eval.go, //go:build ignore) queried
// with threshold 0.0, so it could report "good retrieval" while every shipping
// consumer returned nothing.
//
// Opt-in: SPROUT_RETRIEVAL_EVAL=1 (loads the real model, ~2 min).
func TestRetrievalQuality(t *testing.T) {
	if os.Getenv("SPROUT_RETRIEVAL_EVAL") != "1" {
		t.Skip("SPROUT_RETRIEVAL_EVAL unset")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	indexDir := t.TempDir()
	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{IndexDir: indexDir}, "..")
	t.Cleanup(func() { _ = mgr.Close() })

	if err := mgr.Init(ctx); err != nil {
		t.Skipf("embedding init unavailable: %v", err)
	}

	idx, err := mgr.snapshotIndexMgr()
	if err != nil {
		t.Fatalf("index manager: %v", err)
	}

	buildStart := time.Now()
	stats, err := idx.BuildIndex(ctx, "../embedding")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	t.Logf("indexed %d files / %d units in %s", stats.FilesProcessed, stats.UnitsEmbedded, buildStart.Sub(buildStart)+time.Since(buildStart).Round(time.Second))

	cases := []retrievalCase{
		{"cosine similarity", "compute cosine similarity between two vectors", []string{"cosine", "similarity"}},
		{"provider interface", "interface for producing vector embeddings from text", []string{"embeddingprovider", "provider.go", "embed"}},
		{"model hash", "compute SHA-256 hash of the model file for change detection", []string{"modelhash", "sha256", "filesha256"}},
		{"delete by file", "remove embedding records when a source file is deleted", []string{"deletebyfile"}},
		{"hnsw store", "thread-safe vector store backed by an HNSW graph index", []string{"hnswstore", "store_hnsw"}},
		{"tokenizer", "tokenize text into subword token ids", []string{"tokeniz", "encode"}},
		{"code extraction", "extract functions and methods from a source file", []string{"extractfromfile", "codeunit", "extract"}},
		{"index build", "walk a directory and build the embedding index incrementally", []string{"buildindex", "index.go"}},
		{"batch chunking", "split a batch into chunks bounded by attention cost", []string{"embedbatch", "chunk", "batch"}},
		{"manifest diff", "compare file modification times against a build manifest", []string{"manifest", "diff"}},
	}

	type outcome struct {
		name    string
		topSim  float32
		hitRank int // 1-based rank of first expected match; 0 = miss
		topID   string
	}

	var results []outcome
	for _, c := range cases {
		// threshold 0 so we observe the true score distribution, then judge
		// the shipping thresholds against it.
		got, err := idx.QuerySimilar(ctx, c.query, 10, 0.0)
		if err != nil {
			t.Fatalf("%s: query: %v", c.name, err)
		}
		o := outcome{name: c.name}
		if len(got) > 0 {
			o.topSim = got[0].Similarity
			o.topID = got[0].Record.ID
		}
		for rank, r := range got {
			hay := strings.ToLower(r.Record.ID + " " + r.Record.Name + " " + r.Record.Signature + " " + r.Record.File)
			matched := false
			for _, want := range c.expected {
				if strings.Contains(hay, strings.ToLower(want)) {
					matched = true
					break
				}
			}
			if matched {
				o.hitRank = rank + 1
				break
			}
		}
		results = append(results, o)
	}

	t.Log("")
	t.Log("query                    top-sim  hit@rank  top result")
	t.Log("----------------------------------------------------------------")
	var hits, hitsTop3 int
	var trueHitSims []float32
	for _, o := range results {
		rank := "MISS"
		if o.hitRank > 0 {
			rank = fmt.Sprintf("#%d", o.hitRank)
			hits++
			if o.hitRank <= 3 {
				hitsTop3++
			}
		}
		t.Logf("%-24s %7.3f  %-8s  %s", o.name, o.topSim, rank, shortID(o.topID))
		if o.hitRank == 1 {
			trueHitSims = append(trueHitSims, o.topSim)
		}
	}

	t.Log("")
	t.Logf("recall@10 = %d/%d,  recall@3 = %d/%d", hits, len(cases), hitsTop3, len(cases))

	// The decisive question: would the shipping thresholds admit these?
	sort.Slice(trueHitSims, func(i, j int) bool { return trueHitSims[i] < trueHitSims[j] })
	if len(trueHitSims) > 0 {
		lo, hi := trueHitSims[0], trueHitSims[len(trueHitSims)-1]
		med := trueHitSims[len(trueHitSims)/2]
		t.Logf("correct top-1 similarities: min=%.3f median=%.3f max=%.3f", lo, med, hi)

		for _, th := range []struct {
			name string
			v    float32
		}{
			{"manager default (QuerySimilar/CheckDuplicates)", thresholdManagerDefault},
			{"file duplicate check", thresholdFileDupCheck},
			{"semantic recall", thresholdSemanticRecall},
		} {
			var admitted int
			for _, s := range trueHitSims {
				if s >= th.v {
					admitted++
				}
			}
			t.Logf("  threshold %.2f (%s): admits %d/%d correct results",
				th.v, th.name, admitted, len(trueHitSims))
		}
	}

	if hits == 0 {
		t.Error("semantic search returned no correct result for any query — the index has no retrieval value")
	}
	if hitsTop3*2 < len(cases) {
		t.Errorf("recall@3 = %d/%d; fewer than half the queries surface the right code in the top 3", hitsTop3, len(cases))
	}
}

func shortID(id string) string {
	if id == "" {
		return "(none)"
	}
	return filepath.Base(id)
}
