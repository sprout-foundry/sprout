package agent

// proactive_context_retrieve.go — the proactive-context retrieval pipeline,
// split out of proactive_context.go. RetrieveProactiveContext embeds the query,
// runs HNSW top-K (retrieveProactiveViaHNSW) with a brute-force fallback for
// small stores, and post-filters/re-scores with time decay
// (filterAndScoreProactive). proactiveRawSimilarityFloor bounds the raw
// pre-filter so decayed-but-relevant matches survive.
import (
	"context"
	"sort"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// RetrieveProactiveContext retrieves relevant conversation turns from the
// conversation store based on semantic similarity with time-decay scoring.
//
// Pipeline: embed query → HNSW top-K → filter type/workspace → re-score with decay → cap results.
// Falls back to brute-force LoadAll for stores under 2000 records if HNSW returns no matches.
// Graceful degradation: all errors are logged and nil/empty is returned.
func RetrieveProactiveContext(
	ctx context.Context,
	mgr *embedding.EmbeddingManager,
	config ProactiveContextConfig,
	query string,
	workingDir string,
	now time.Time,
) ([]ProactiveContextResult, error) {
	config = config.resolve()

	if mgr == nil {
		debugLogf("[proactive-context] skipping: embedding manager is nil")
		return nil, nil
	}
	if ctx == nil {
		debugLogf("[proactive-context] skipping: context is nil")
		return nil, nil
	}
	if query == "" {
		debugLogf("[proactive-context] skipping: empty query")
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	if err := mgr.Init(ctx); err != nil {
		debugLogf("[proactive-context] init failed: %v", err)
		return nil, nil
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		debugLogf("[proactive-context] conversation store unavailable: %v", err)
		return nil, nil
	}

	provider := store.Provider()
	if provider == nil {
		debugLogf("[proactive-context] provider unexpectedly nil")
		return nil, nil
	}

	queryEmb, err := provider.Embed(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			debugLogf("[proactive-context] embedding cancelled: %v", ctx.Err())
		} else {
			debugLogf("[proactive-context] query embedding failed: %v", err)
		}
		return nil, nil
	}
	if len(queryEmb) == 0 {
		debugLogf("[proactive-context] query embedding returned empty vector")
		return nil, nil
	}

	scored := retrieveProactiveViaHNSW(store, queryEmb, config, workingDir, now)
	if len(scored) == 0 {
		return nil, nil
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if len(scored) > config.MaxContextualResults {
		scored = scored[:config.MaxContextualResults]
	}

	debugLogf("[proactive-context] retrieved %d candidates above decayed threshold %.2f",
		len(scored), config.MinRelevanceScore)

	return scored, nil
}

// proactiveRawSimilarityFloor is the minimum RAW cosine similarity an HNSW
// candidate must have. It is deliberately lower than MinRelevanceScore (which
// refers to the DECAYED score) so decayed-but-relevant matches survive.
const proactiveRawSimilarityFloor = 0.30

// retrieveProactiveViaHNSW runs the HNSW query + post-filter pipeline.
// Falls back to brute-force LoadAll for small stores if HNSW returns no matches.
func retrieveProactiveViaHNSW(
	store *embedding.ConversationStore,
	queryEmb []float32,
	config ProactiveContextConfig,
	workingDir string,
	now time.Time,
) []ProactiveContextResult {
	topK := config.MaxContextualResults * 4
	if topK < 4 {
		topK = 4
	}

	// Use lower raw-similarity floor for HNSW pre-filter so decayed matches survive.
	rawThreshold := float32(proactiveRawSimilarityFloor)
	if float64(rawThreshold) > config.MinRelevanceScore {
		rawThreshold = float32(config.MinRelevanceScore)
	}

	rawResults, err := store.Query(queryEmb, topK, rawThreshold)
	if err != nil {
		debugLogf("[proactive-context] HNSW query failed: %v", err)
		return nil
	}

	scored := filterAndScoreProactive(rawResults, config, workingDir, now)
	if len(scored) > 0 {
		return scored
	}

	// Fallback: brute-force for small stores where O(N) cost is negligible.
	if store.Size() > 2000 {
		return nil
	}
	allRecords, err := store.LoadAll()
	if err != nil {
		debugLogf("[proactive-context] fallback LoadAll failed: %v", err)
		return nil
	}
	bruteResults := make([]embedding.QueryResult, 0, len(allRecords))
	for _, rec := range allRecords {
		if len(rec.Embedding) == 0 {
			continue
		}
		sim := embedding.CosineSimilarity(queryEmb, rec.Embedding)
		if sim >= rawThreshold {
			bruteResults = append(bruteResults, embedding.QueryResult{Record: rec, Similarity: sim})
		}
	}
	return filterAndScoreProactive(bruteResults, config, workingDir, now)
}

// filterAndScoreProactive applies type filter, workspace filter, and
// time-decay re-scoring. Shared between HNSW and brute-force fallback.
func filterAndScoreProactive(
	rawResults []embedding.QueryResult,
	config ProactiveContextConfig,
	workingDir string,
	now time.Time,
) []ProactiveContextResult {
	scored := make([]ProactiveContextResult, 0, len(rawResults))
	for _, r := range rawResults {
		if r.Record.Type != "conversation_turn" {
			continue
		}

		if config.WorkspaceScoped && workingDir != "" {
			recWD, ok := r.Record.Metadata["workingDir"].(string)
			if !ok || recWD != workingDir {
				continue
			}
		}

		decayedScore := embedding.ScoreWithDecay(float64(r.Similarity), r.Record.IndexedAt, now)

		if decayedScore >= config.MinRelevanceScore {
			scored = append(scored, ProactiveContextResult{
				Record: r.Record,
				Score:  decayedScore,
			})
		}
	}
	return scored
}
