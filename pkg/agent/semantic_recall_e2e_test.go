package agent

import (
	"context"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// TestSemanticRecall_E2E_FullPipeline exercises the entire SP-066 Phase 3
// recall chain against a mock embedding manager:
//
//  1. Five realistic conversation turns are checkpointed via
//     recordTurnCheckpointFromMessages → EmbedAndStoreTurn (production path).
//  2. The TurnCheckpoint list is mutated to simulate /compact removing
//     some checkpoints from the active substitution window while leaving
//     the embedding-store copies intact (the store is the permanent memory
//     layer — recall must work across /compact wipes per SP-066 §"Out of
//     Scope" / Phase 3 motivation).
//  3. Agent.Recall is invoked with a query that should surface the
//     "compacted-out" turn whose topic matches.
//  4. Agent.InjectSemanticRecall is invoked to verify the formatted
//     markdown lands in the pending system supplement.
//  5. A foreign-session record is inserted directly into the store to
//     verify the in-session scope filter.
//
// This is the regression net for the SP-066 Phase 3d wire-fix (d6094ec5):
// if recordTurnCheckpointFromMessages ever stops stamping checkpoint_id
// into the stored metadata, the lookup in retrieveSemanticRecall will
// find no records, the presentCheckpointIDs skip logic becomes
// untestable, and this test fails.
func TestSemanticRecall_E2E_FullPipeline(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// --- Step 1: checkpoint five realistic turns via the production path ---

	scenarios := []struct {
		userPrompt string
		topicTag   string
	}{
		{
			userPrompt: "Explain how to set up OAuth2 authentication in a Go web service.",
			topicTag:   "OAuth2",
		},
		{
			userPrompt: "How do I configure connection pooling for a Postgres database in Go?",
			topicTag:   "Postgres",
		},
		{
			userPrompt: "Walk me through the differences between context.WithCancel and context.WithTimeout.",
			topicTag:   "context.WithCancel",
		},
		{
			userPrompt: "What is the cleanest way to write a Kubernetes liveness probe in Go?",
			topicTag:   "Kubernetes",
		},
		{
			userPrompt: "How should I structure error handling across an HTTP API gateway?",
			topicTag:   "error handling",
		},
	}

	// Build the agent first so each checkpoint routes through
	// recordTurnCheckpointFromMessages → EmbedAndStoreTurn with the
	// session ID stamped onto each record.
	a := newTestAgentForEmbedding()
	a.debug = false
	setEmbeddingManager(a, mgr)
	a.SetSessionID("e2e-recall-session")
	a.effectiveContextCap = 200_000 // gates computeRecallMaxChars → 8000 (default ceiling)

	// Seed the agent's message slice with the five turns. 3 messages per
	// checkpoint (1 user + 1 assistant + 1 tool) — matches the structural
	// shape seed's chat loop emits.
	for i := range scenarios {
		base := i * 3
		msgs := []api.Message{
			{Role: "user", Content: scenarios[i].userPrompt},
			{Role: "assistant", Content: "Working on " + scenarios[i].topicTag + " task..."},
			{Role: "tool", Content: "Tool call result for shell: synthesized response for " + scenarios[i].topicTag},
		}
		for _, m := range msgs {
			a.state.AddMessage(m)
		}
		a.recordTurnCheckpointFromMessages(base, base+2, msgs)
	}

	// --- Step 2: assert the production path stamped checkpoint_id ---

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("get conversation store: %v", err)
	}
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("load all records: %v", err)
	}
	if len(all) != len(scenarios) {
		t.Fatalf("expected %d records in conversation store, got %d", len(scenarios), len(all))
	}
	stampedCount := 0
	for _, rec := range all {
		cpID, ok := rec.Metadata["checkpoint_id"].(string)
		if !ok || cpID == "" {
			t.Errorf("record %s missing checkpoint_id — production path did not stamp it (regression of d6094ec5 wire-fix)", rec.ID)
			continue
		}
		stampedCount++
	}
	if stampedCount != len(scenarios) {
		t.Errorf("expected all %d records stamped with checkpoint_id, got %d", len(scenarios), stampedCount)
	}

	// Snapshot the active checkpoint list so we can verify presentCheckpointIDs
	// skip logic in step 4.
	activeCheckpoints := a.copyTurnCheckpoints()
	if len(activeCheckpoints) != len(scenarios) {
		t.Fatalf("expected %d active checkpoints, got %d", len(scenarios), len(activeCheckpoints))
	}

	// --- Step 3: simulate /compact wiping 4 of the 5 checkpoints ---
	// The conversation store keeps its 5 records (permanent memory).
	// The active TurnCheckpoints list drops to just one — the most-recent
	// turn (Kubernetes, the 4th scenario). This mirrors what /compact
	// does today: replace the active list, leave the store intact.
	surviving := activeCheckpoints[3:4] // the Kubernetes one
	a.ReplaceTurnCheckpoints(surviving)

	if got := len(a.copyTurnCheckpoints()); got != 1 {
		t.Fatalf("after /compact simulation: expected 1 surviving checkpoint, got %d", got)
	}

	// --- Step 4: Recall a topic that's no longer in the active window ---
	//
	// Mock embedding is a byte-hash function — the similarity score is
	// dominated by byte-distribution overlap rather than semantic match.
	// To get a deterministic top hit, the recall query needs to share
	// significant literal byte content with the prompt we want recalled.
	// The OAuth2 prompt literally contains the words "OAuth2" and
	// "authentication", so this query hits it.
	items, err := a.Recall(ctx, "OAuth2 authentication Go web service", 3)
	if err != nil {
		t.Fatalf("Recall returned error: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("Recall returned no items; expected at least the OAuth2 checkpoint to surface from the store")
	}

	// Assert that the OAuth2 checkpoint is among the top-K. Byte-hash
	// embeddings don't guarantee top-1 ordering on semantically related
	// prompts, so we relax to top-K containment.
	const wantTag = "OAuth2"
	const wantPrompt = "OAuth2 authentication"
	foundOAuth2 := false
	for _, item := range items {
		if strings.Contains(item.Summary, wantPrompt) ||
			strings.Contains(item.Actionable, wantTag) ||
			strings.Contains(item.Summary, wantTag) {
			foundOAuth2 = true
			break
		}
	}
	if !foundOAuth2 {
		var dump []string
		for _, item := range items {
			dump = append(dump, item.Summary)
		}
		t.Errorf("OAuth2 checkpoint not in top-K recall results:\n  %s",
			strings.Join(dump, "\n  "))
	}

	// The surviving Kubernetes checkpoint must NOT appear: it's in the
	// present set, and recall explicitly skips present checkpoints so the
	// model doesn't see double-rendered summaries.
	for _, item := range items {
		for _, cp := range surviving {
			if item.CheckpointID == cp.ID {
				t.Errorf("recall surfaced surviving checkpoint %s — presentCheckpointIDs skip failed", cp.ID)
			}
		}
	}

	// Every recalled item must carry a populated checkpoint_id, similarity
	// above the gate threshold, and metadata extracted from the store.
	for i, item := range items {
		if item.CheckpointID == "" {
			t.Errorf("item[%d]: checkpoint_id empty", i)
		}
		if item.Similarity < semanticRecallSimilarityThreshold {
			t.Errorf("item[%d] (cp=%s): similarity %.3f below gate %.3f",
				i, item.CheckpointID, item.Similarity, semanticRecallSimilarityThreshold)
		}
		if item.Score <= 0 {
			t.Errorf("item[%d] (cp=%s): decayed score non-positive: %f",
				i, item.CheckpointID, item.Score)
		}
		if item.Summary == "" {
			t.Errorf("item[%d] (cp=%s): summary empty", i, item.CheckpointID)
		}
	}

	// --- Step 5: InjectSemanticRecall must update the pending system supplement ---

	a.state.SetPendingSystemSupplement("")
	a.InjectSemanticRecall(ctx, "OAuth2 authentication Go web service")

	got := a.state.GetPendingSystemSupplement()
	if got == "" {
		t.Fatal("InjectSemanticRecall produced no supplement; expected formatted markdown block")
	}
	if !strings.Contains(got, "Recalled From Session History") {
		t.Errorf("supplement missing section header: %q", got)
	}
	if !strings.Contains(got, "OAuth2") {
		t.Errorf("supplement didn't surface OAuth2 content: %q", got)
	}

	// --- Step 6: Verify cross-session recall is filtered out ---
	//
	// Inject a record that lives in another session directly into the
	// store. Recall (scoped to "e2e-recall-session") must NOT surface it.
	otherVec, err := store.Provider().Embed(ctx, "OAuth2 authentication from foreign session")
	if err != nil {
		t.Fatalf("embed for cross-session test: %v", err)
	}
	crossRec := embedding.VectorRecord{
		ID:        "cross-session-record",
		File:      "session_other.json",
		Name:      "cross-turn",
		Signature: "from another session",
		Embedding: otherVec,
		Type:      "conversation_turn",
		Metadata: map[string]interface{}{
			"sessionId":     "other-session-e2e",
			"checkpoint_id": "cp-cross",
		},
	}
	if err := store.Store([]embedding.VectorRecord{crossRec}); err != nil {
		t.Fatalf("store cross-session record: %v", err)
	}

	items2, err := a.Recall(ctx, "OAuth2 authentication interceptor", 5)
	if err != nil {
		t.Fatalf("Recall (cross-session check): %v", err)
	}
	for _, item := range items2 {
		if item.CheckpointID == "cp-cross" {
			t.Errorf("cross-session record leaked through: checkpoint_id=%q", item.CheckpointID)
		}
	}
}
