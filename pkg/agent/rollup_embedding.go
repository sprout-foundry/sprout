package agent

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/redact"
)

// SP-066 Phase 3: rollup summaries get embedded into the same conversation
// store that holds per-turn embeddings, so semantic recall can surface them
// when a future user turn asks about a span that's already been rolled up.
//
// Per-turn embeddings (Type="conversation_turn", written by EmbedAndStoreTurn)
// continue to live alongside rollup embeddings (Type="checkpoint_rollup",
// written here). Both survive `/compact` wipes — the active TurnCheckpoint
// list is just the substitution window; the embedding store is the
// permanent memory layer.

// checkpointRollupRecordType is the VectorRecord.Type tag for rollup
// embeddings. Distinct from "conversation_turn" (per-turn) and "memory"
// (cross-session) so recall queries can filter by resolution level.
const checkpointRollupRecordType = "checkpoint_rollup"

// embedRollupCheckpoint writes the rollup's summary into the conversation
// store as a VectorRecord. Called from the rollup worker after the LLM
// returns a non-empty summary; errors are logged and swallowed so a flaky
// embedding provider never blocks rollup completion.
func (a *Agent) embedRollupCheckpoint(ctx context.Context, sessionID string, rollup TurnCheckpoint) {
	if a == nil {
		return
	}
	mgr := a.GetEmbeddingManager()
	if mgr == nil {
		return
	}
	if strings.TrimSpace(rollup.Summary) == "" {
		return
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		packageLogErrorf("[rollup-embed] get store failed: %v", err)
		return
	}
	provider := store.Provider()
	if provider == nil {
		packageLogErrorf("[rollup-embed] provider unexpectedly nil")
		return
	}

	// Redact secrets before embedding — the conversation store is
	// long-lived persistent memory and survives /compact wipes.
	safeSummary := redact.String(rollup.Summary)
	safeActionable := redact.String(rollup.ActionableSummary)

	emb, err := provider.Embed(ctx, safeSummary)
	if err != nil {
		if ctx.Err() != nil {
			packageLogErrorf("[rollup-embed] embed cancelled: %v", ctx.Err())
		} else {
			packageLogErrorf("[rollup-embed] embed failed: %v", err)
		}
		return
	}
	if len(emb) == 0 {
		return
	}

	signature := safeSummary
	if len(signature) > maxSignatureLen {
		signature = signature[:maxSignatureLen]
	}

	metadata := map[string]interface{}{
		"sessionId":     sessionID,
		"checkpoint_id": rollup.ID,
		"level":         rollup.Level,
		"covered_turns": rollup.CoveredTurns,
		"start_index":   rollup.StartIndex,
		"end_index":     rollup.EndIndex,
	}
	if safeActionable != "" {
		metadata["actionable_summary"] = safeActionable
	}
	if len(rollup.SourceCheckpointIDs) > 0 {
		metadata["source_checkpoint_ids"] = append([]string(nil), rollup.SourceCheckpointIDs...)
	}
	if len(rollup.FileChanges) > 0 {
		paths := make([]string, len(rollup.FileChanges))
		for i, fc := range rollup.FileChanges {
			paths[i] = fc.Op + " " + fc.Path
		}
		metadata["files_touched"] = paths
	}

	record := embedding.VectorRecord{
		ID:        "rollup:" + rollup.ID,
		File:      fmt.Sprintf("session_%s.json", sessionID),
		Name:      fmt.Sprintf("rollup-level-%d", rollup.Level),
		Signature: signature,
		Embedding: emb,
		Hash:      fmt.Sprintf("%x", md5.Sum([]byte(safeSummary))),
		IndexedAt: time.Now().UTC(),
		Type:      checkpointRollupRecordType,
		Metadata:  metadata,
	}

	if err := store.Store([]embedding.VectorRecord{record}); err != nil {
		packageLogErrorf("[rollup-embed] store failed: %v", err)
		return
	}
	debugLogf("[rollup-embed] stored level-%d rollup %s (%d covered turns)", rollup.Level, rollup.ID, rollup.CoveredTurns)
}
