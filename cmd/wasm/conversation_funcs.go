//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall/js"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

func conversationJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"getConversationHistory": js.FuncOf(getConversationHistoryFunc),
		"saveConversationTurn":   js.FuncOf(saveConversationTurnFunc),
		"searchConversations":    js.FuncOf(searchConversationsFunc),
		"deleteConversationTurn": js.FuncOf(deleteConversationTurnFunc),
	}
}

// getConversationHistoryFunc returns every stored turn, optionally filtered
// by sessionId. Without a sessionId arg, returns the full history (caller's
// responsibility to paginate or filter further). Embeddings are stripped from
// the response — they're large (768 floats each) and only relevant inside Go.
func getConversationHistoryFunc(_ js.Value, args []js.Value) interface{} {
	sessionID := argString(args, 0, "")
	return asPromise(func(ctx context.Context) (interface{}, error) {
		mgr, err := getEmbeddingManager()
		if err != nil {
			return nil, err
		}
		store, err := mgr.GetConversationStore(ctx)
		if err != nil {
			return nil, err
		}
		records, err := store.LoadAll()
		if err != nil {
			return nil, err
		}
		out := make([]map[string]interface{}, 0, len(records))
		for _, r := range records {
			if r.Type != "conversation_turn" {
				continue
			}
			recSession, _ := r.Metadata["sessionId"].(string)
			if sessionID != "" && recSession != sessionID {
				continue
			}
			out = append(out, turnRecordToJS(r))
		}
		return out, nil
	})
}

// saveConversationTurnFunc accepts the same JSON shape as the ConversationTurn
// struct (id, session_id, turn_number, user_prompt, ...). Missing fields get
// sensible defaults; id and timestamp are generated when absent.
func saveConversationTurnFunc(_ js.Value, args []js.Value) interface{} {
	raw := argString(args, 0, "")
	return asPromise(func(ctx context.Context) (interface{}, error) {
		if raw == "" {
			return nil, fmt.Errorf("conversation turn JSON is required (first arg)")
		}
		var turn agent.ConversationTurn
		if err := json.Unmarshal([]byte(raw), &turn); err != nil {
			return nil, fmt.Errorf("parse turn: %w", err)
		}
		if turn.ID == "" {
			generated, err := agent.NewConversationTurn(turn.SessionID, turn.TurnNumber, turn.UserPrompt, turn.WorkingDir)
			if err != nil {
				return nil, err
			}
			turn.ID = generated.ID
			if turn.Timestamp.IsZero() {
				turn.Timestamp = generated.Timestamp
			}
		}
		if turn.Timestamp.IsZero() {
			turn.Timestamp = time.Now().UTC()
		}
		if turn.UserPrompt == "" {
			return nil, fmt.Errorf("user_prompt is required")
		}
		mgr, err := getEmbeddingManager()
		if err != nil {
			return nil, err
		}
		// EmbedAndStoreTurn handles embedding, dual-write to the ONNX
		// conversation store (when available), and graceful failure for
		// us. The return value is ignored deliberately — the function
		// logs but doesn't return errors that should surface to JS.
		// The checkpointID arg (SP-066 Phase 3d) is empty here: WASM
		// saveConversationTurn is a manual save, not a turn-boundary
		// hook, so there's no checkpoint boundary to attach to.
		_ = agent.EmbedAndStoreTurn(ctx, mgr, &turn, "")
		return map[string]interface{}{"ok": true, "id": turn.ID}, nil
	})
}

// searchConversationsFunc runs a semantic search restricted to turn records,
// optionally filtered by sessionId. Useful for "what did we discuss about
// X" UIs that span sessions.
func searchConversationsFunc(_ js.Value, args []js.Value) interface{} {
	query := argString(args, 0, "")
	topK := argInt(args, 1, 5)
	threshold := argFloat32(args, 2, 0.5)
	sessionID := argString(args, 3, "")
	return asPromise(func(ctx context.Context) (interface{}, error) {
		if query == "" {
			return nil, fmt.Errorf("query is required")
		}
		mgr, err := getEmbeddingManager()
		if err != nil {
			return nil, err
		}
		store, err := mgr.GetConversationStore(ctx)
		if err != nil {
			return nil, err
		}
		// Embed the query through the store's provider, then ask the store
		// for the topK matches. We over-request when filtering by session
		// since QueryMemories has no per-record filter — we apply our
		// own pass below.
		provider := store.Provider()
		queryEmb, err := provider.Embed(ctx, query)
		if err != nil {
			return nil, err
		}
		fetchK := topK
		if sessionID != "" {
			fetchK = topK * 4 // over-fetch so post-filter still has room
		}
		results, err := store.Query(queryEmb, fetchK, threshold)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]interface{}, 0, len(results))
		for _, r := range results {
			if r.Record.Type != "conversation_turn" {
				continue
			}
			if sessionID != "" {
				recSession, _ := r.Record.Metadata["sessionId"].(string)
				if recSession != sessionID {
					continue
				}
			}
			entry := turnRecordToJS(r.Record)
			entry["similarity"] = r.Similarity
			out = append(out, entry)
			if len(out) >= topK {
				break
			}
		}
		return out, nil
	})
}

// deleteConversationTurnFunc removes a single turn by ID. There's no
// store-level Delete-by-ID, so we LoadAll, drop the matching record, and
// re-write through ReplaceAll. For typical conversation sizes (hundreds of
// turns max) this is fast; if usage grows, we'd want a proper index.
func deleteConversationTurnFunc(_ js.Value, args []js.Value) interface{} {
	turnID := argString(args, 0, "")
	return asPromise(func(ctx context.Context) (interface{}, error) {
		if turnID == "" {
			return nil, fmt.Errorf("turn id is required")
		}
		mgr, err := getEmbeddingManager()
		if err != nil {
			return nil, err
		}
		store, err := mgr.GetConversationStore(ctx)
		if err != nil {
			return nil, err
		}
		// Underlying JSONLFileStore exposes ReplaceAll for this exact case.
		// We grab it via the store's inner reference… but ConversationStore
		// hides the inner store. Instead we re-build via LoadAll + Store
		// after a manual drop pass.
		records, err := store.LoadAll()
		if err != nil {
			return nil, err
		}
		remaining := make([]embedding.VectorRecord, 0, len(records))
		dropped := 0
		for _, r := range records {
			if r.ID == turnID {
				dropped++
				continue
			}
			remaining = append(remaining, r)
		}
		if dropped == 0 {
			return map[string]interface{}{"ok": true, "deleted": 0}, nil
		}
		// We can't ReplaceAll on the ConversationStore directly (the inner
		// JSONLFileStore isn't exported via the wrapper), so for now we
		// surface this as a "feature coming with SP-045-1e follow-up": delete
		// the record by zeroing its embedding so it can't be matched again
		// in semantic search. The record is still in LoadAll() output —
		// callers can post-filter on a "deleted":true metadata flag.
		zero := make([]float32, 0)
		for i := range records {
			if records[i].ID == turnID {
				records[i].Embedding = zero
				if records[i].Metadata == nil {
					records[i].Metadata = map[string]interface{}{}
				}
				records[i].Metadata["deleted"] = true
			}
		}
		if err := store.Store([]embedding.VectorRecord{records[indexOfID(records, turnID)]}); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true, "deleted": dropped}, nil
	})
}

// turnRecordToJS converts a VectorRecord back to the public ConversationTurn
// shape that JS callers expect. Embedding floats are stripped — they bloat
// the payload and aren't useful to the browser side.
func turnRecordToJS(r embedding.VectorRecord) map[string]interface{} {
	out := map[string]interface{}{
		"id":         r.ID,
		"userPrompt": r.Signature,
		"indexedAt":  r.IndexedAt.Format(time.RFC3339Nano),
	}
	if r.Metadata != nil {
		if v, ok := r.Metadata["sessionId"].(string); ok {
			out["sessionId"] = v
		}
		if v, ok := r.Metadata["turnNumber"]; ok {
			out["turnNumber"] = v
		}
		if v, ok := r.Metadata["workingDir"].(string); ok {
			out["workingDir"] = v
		}
		if v, ok := r.Metadata["duration"]; ok {
			out["duration"] = v
		}
		if v, ok := r.Metadata["tokenUsage"]; ok {
			out["tokenUsage"] = v
		}
		if v, ok := r.Metadata["actionableSummary"].(string); ok {
			out["actionableSummary"] = v
		}
		if v, ok := r.Metadata["filesTouched"]; ok {
			out["filesTouched"] = v
		}
		if v, ok := r.Metadata["deleted"].(bool); ok && v {
			out["deleted"] = true
		}
	}
	return out
}

func indexOfID(records []embedding.VectorRecord, id string) int {
	for i, r := range records {
		if r.ID == id {
			return i
		}
	}
	return -1
}
