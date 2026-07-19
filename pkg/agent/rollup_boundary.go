package agent

import (
	"context"
	"math"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// SP-066 Phase 3d: embedding-driven rollup boundary detection.
//
// The default rollup picks the first rollupSourceCount contiguous entries
// at the over-budget level. This produces fine results when those N
// entries are about a single topic; it produces a fragmented summary when
// the user shifted topics partway through the span (e.g. "fix the auth
// bug" → "now help me set up CI").
//
// This file adds an opt-in refinement: if embeddings are available for
// the candidate range, look for the largest pairwise-similarity drop
// between consecutive entries. If the drop exceeds a threshold, treat
// it as a natural topic boundary and shrink the rollup to stop before
// it, so each rollup stays topically coherent.
//
// Behavior:
//   - When embeddings aren't available (no manager, no records for these
//     checkpoints, or an embed failure), fall back to the default first-N
//     window. The worker never blocks on this.
//   - When the largest drop is below the threshold, fall back to first-N.
//     A small drop isn't a topic shift; it's normal turn-to-turn drift.
//   - When the boundary would leave fewer than rollupBoundaryMin source
//     items, fall back to first-N. Don't produce trivially-small rollups.

const (
	// rollupBoundarySimilarityDrop is the minimum drop in cosine
	// similarity (vs. the prior pairwise similarity in the candidate
	// window) that counts as a topic shift. Tuned conservatively to
	// avoid splitting on normal drift; we'd rather miss a soft boundary
	// than over-segment.
	rollupBoundarySimilarityDrop = 0.20

	// rollupBoundaryMin is the minimum number of source items a refined
	// rollup must cover. Below this, we'd be paying an LLM call for
	// almost no compression; better to wait for more candidates.
	rollupBoundaryMin = 5
)

// refineRollupEnd returns an adjusted endIdx ≤ defaultEnd, narrowing the
// rollup range to the largest topic boundary in [startIdx, defaultEnd]
// when one is present. Returns defaultEnd unchanged when no manager,
// no usable embeddings, or no significant drop is found.
//
// The function is best-effort: any retrieval failure causes a fall-back
// to the default range. This keeps the rollup worker on its critical
// path even when the embedding store is misbehaving.
func (a *Agent) refineRollupEnd(ctx context.Context, checkpoints []TurnCheckpoint, startIdx, defaultEnd int) int {
	if a == nil {
		return defaultEnd
	}
	if defaultEnd-startIdx+1 < rollupBoundaryMin*2 {
		// Too few candidates to bother — no meaningful split possible
		// while still leaving rollupBoundaryMin items on each side.
		return defaultEnd
	}
	mgr := a.GetEmbeddingManager()
	if mgr == nil {
		return defaultEnd
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil || store == nil {
		return defaultEnd
	}

	vectors, ok := collectCheckpointVectors(store, checkpoints[startIdx:defaultEnd+1])
	if !ok {
		return defaultEnd
	}

	boundary, drop := largestSimilarityDrop(vectors)
	if drop < rollupBoundarySimilarityDrop {
		return defaultEnd
	}

	candidateEnd := startIdx + boundary
	if candidateEnd < startIdx+rollupBoundaryMin-1 {
		// The split would leave the rollup smaller than rollupBoundaryMin.
		return defaultEnd
	}
	if defaultEnd-candidateEnd < rollupBoundaryMin {
		// Splitting here would leave fewer than rollupBoundaryMin items
		// behind — pointless; the next pass would just retrigger.
		return defaultEnd
	}
	return candidateEnd
}

// collectCheckpointVectors looks up the embedding for each candidate
// checkpoint, returning the vector slice in candidate order. Returns
// ok=false when any vector is missing or when checkpoints lack the IDs
// we need to look them up — boundary detection is opt-in.
//
// ID resolution contract:
//   - Per-turn records (Type="conversation_turn"): metadata["checkpoint_id"]
//     holds the TurnCheckpoint.ID ("cp-<uuid>"). r.ID is a 32-char hex turn
//     ID and is ignored.
//   - Rollup records (Type=checkpointRollupRecordType): metadata["checkpoint_id"]
//     holds the RollupCheckpoint.ID (same as its source checkpoint IDs).
//     r.ID has the form "rollup:<checkpoint_id>" so we strip the prefix as a
//     fallback key.
//
// This dual-key strategy lets callers pass cp.ID ("cp-...") directly for
// both per-turn and rollup candidates without needing to know which type
// each one is.
func collectCheckpointVectors(store *embedding.ConversationStore, cps []TurnCheckpoint) ([][]float32, bool) {
	if store == nil {
		return nil, false
	}
	all, err := store.LoadAll()
	if err != nil || len(all) == 0 {
		return nil, false
	}

	// Index by checkpoint ID. See ID resolution contract above.
	byID := make(map[string][]float32, len(all))
	for _, r := range all {
		if r.Type != checkpointRollupRecordType && r.Type != "conversation_turn" {
			continue
		}
		// Primary key: checkpoint_id in metadata (works for both per-turn and rollup).
		var cid string
		if v, ok := r.Metadata["checkpoint_id"].(string); ok && v != "" {
			cid = v
		}
		if cid == "" {
			// Fallback: r.ID stripped of "rollup:" prefix. This handles legacy
			// per-turn records that have no checkpoint_id metadata, and also
			// rollup records where only r.ID was written.
			cid = strings.TrimPrefix(r.ID, "rollup:")
		}
		if cid == "" || len(r.Embedding) == 0 {
			continue
		}
		// Make a defensive copy so the worker can't mutate stored vectors.
		v := make([]float32, len(r.Embedding))
		copy(v, r.Embedding)
		byID[cid] = v
	}

	out := make([][]float32, 0, len(cps))
	for _, cp := range cps {
		if cp.ID == "" {
			return nil, false
		}
		v, ok := byID[cp.ID]
		if !ok {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}

// largestSimilarityDrop returns the (boundary_index, drop_size) of the
// largest similarity drop between consecutive vector pairs. boundary_index
// is the index of the LAST item BEFORE the drop — i.e., a rollup using
// items [0..boundary_index] inclusive ends right before the topic shift.
//
// Returns (0, 0) when fewer than 3 vectors are supplied (can't have a
// drop with fewer than two pairs).
func largestSimilarityDrop(vectors [][]float32) (boundary int, drop float64) {
	if len(vectors) < 3 {
		return 0, 0
	}

	sims := make([]float64, len(vectors)-1)
	for i := 0; i < len(vectors)-1; i++ {
		sims[i] = cosineSimilarity(vectors[i], vectors[i+1])
	}

	for i := 1; i < len(sims); i++ {
		d := sims[i-1] - sims[i]
		if d > drop {
			drop = d
			boundary = i
		}
	}
	return boundary, drop
}

// cosineSimilarity computes ⟨a,b⟩ / (‖a‖·‖b‖). Returns 0 for zero-norm
// vectors. Kept local to avoid pulling in the embedding package's
// CosineSimilarity (which has a slightly different signature in some
// versions); this implementation is straightforward and easy to test.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		da, db := float64(a[i]), float64(b[i])
		dot += da * db
		na += da * da
		nb += db * db
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
