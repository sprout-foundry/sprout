package agent

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// EmbedAndStoreTurn computes embeddings for a conversation turn's prompt and
// actionable summary using the static embedding provider, then stores the result
// as a VectorRecord in the ConversationStore.
//
// The checkpointID is stamped into the record's metadata so that
// collectCheckpointVectors can look it up during SP-066 Phase 3d rollup boundary
// detection. Pass "" when no checkpoint ID is available (e.g. in tests).
//
// Graceful failure: Errors are logged but not returned. The caller (checkpoint
// recording) should always succeed regardless of embedding failures.
func EmbedAndStoreTurn(ctx context.Context, mgr *embedding.EmbeddingManager, turn *ConversationTurn, checkpointID string) error {
	// Validate inputs. These are routine "feature unavailable" paths that
	// fire on every turn when embedding is unconfigured — demoted to debug
	// so they don't spam the default log.
	if mgr == nil {
		debugLogf("[turn-embedding] skipping embedding: embedding manager is nil")
		return nil
	}
	if turn == nil {
		debugLogf("[turn-embedding] skipping embedding: turn is nil")
		return nil
	}
	if ctx == nil {
		debugLogf("[turn-embedding] skipping embedding: context is nil")
		return nil
	}
	if turn.UserPrompt == "" {
		debugLogf("[turn-embedding] skipping embedding: empty user prompt")
		return nil
	}

	// Get the conversation store from the manager.
	// Errors here are real failures (disk full, permission denied, schema
	// mismatch) and stay at info level so operators can see them.
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		packageLogErrorf("[turn-embedding] failed to get conversation store: %v", err)
		return nil
	}

	// Get the provider from the store
	provider := store.Provider()
	if provider == nil {
		packageLogErrorf("[turn-embedding] ERROR: provider unexpectedly nil after successful store init")
		return nil
	}

	// Prepare texts for embedding
	var texts []string
	hasSummary := turn.ActionableSummary != ""
	if hasSummary {
		texts = []string{turn.UserPrompt, turn.ActionableSummary}
	} else {
		texts = []string{turn.UserPrompt}
	}

	// Embed both prompt and summary in one call
	embeddings, err := provider.EmbedBatch(ctx, texts)
	if err != nil {
		if ctx.Err() != nil {
			packageLogErrorf("[turn-embedding] embedding cancelled: %v", ctx.Err())
		} else {
			packageLogErrorf("[turn-embedding] failed to embed texts: %v", err)
		}
		return nil
	}

	// Extract embeddings
	var promptEmb []float32
	if len(embeddings) > 0 {
		promptEmb = embeddings[0]
	}

	// Compute mean of prompt and summary embeddings, or use just prompt
	var meanEmb []float32
	if hasSummary && len(embeddings) > 1 {
		summaryEmb := embeddings[1]
		meanEmb = meanEmbedding(promptEmb, summaryEmb)
	} else {
		// Only prompt was embedded, use it directly
		if promptEmb != nil {
			meanEmb = make([]float32, len(promptEmb))
			copy(meanEmb, promptEmb)
		}
	}

	// Set turn.PromptEmbedding to the mean embedding
	turn.PromptEmbedding = meanEmb

	// Convert to VectorRecord
	record := turn.ToVectorRecord()

	// Stamp checkpoint_id into metadata so collectCheckpointVectors can look it
	// up during rollup boundary detection (SP-066 Phase 3d). The lookup uses
	// this field as the primary key; empty string is intentionally skipped.
	if checkpointID != "" {
		record.Metadata["checkpoint_id"] = checkpointID
	}

	// Store in conversation store
	if err := store.Store([]embedding.VectorRecord{record}); err != nil {
		packageLogErrorf("[turn-embedding] failed to store vector record: %v", err)
		return nil
	}

	// Routine per-turn success — debug-only to avoid flooding the log on
	// busy sessions.
	debugLogf("[turn-embedding] successfully stored turn %s in conversation store", turn.ID)
	return nil
}

// meanEmbedding computes the element-wise mean of two embedding vectors.
// Both must have the same length. Returns the mean vector.
func meanEmbedding(a, b []float32) []float32 {
	if len(a) != len(b) {
		// If lengths differ, return a copy of the longer one
		if len(a) > len(b) {
			result := make([]float32, len(a))
			copy(result, a)
			return result
		}
		result := make([]float32, len(b))
		copy(result, b)
		return result
	}
	result := make([]float32, len(a))
	for i := range a {
		result[i] = (a[i] + b[i]) / 2.0
	}
	return result
}
