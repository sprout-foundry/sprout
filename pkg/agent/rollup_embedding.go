// Package agent: rollup embedding — writes rollup summaries into the conversation store.
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

// checkpointRollupRecordType is the VectorRecord.Type tag for rollup embeddings.
const checkpointRollupRecordType = "checkpoint_rollup"

// embedRollupCheckpoint writes the rollup's summary into the conversation store.
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

	// Redact secrets before embedding.
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
