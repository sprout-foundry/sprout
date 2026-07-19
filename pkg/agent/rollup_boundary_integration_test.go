package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// TestRefineRollupEnd_FindsTopicShift verifies that refineRollupEnd detects
// a topic boundary when per-turn embeddings show a large similarity drop
// between two clusters. This is the end-to-end integration test for SP-066
// Phase 3d: per-turn records written with checkpoint_id metadata are
// successfully looked up and used for boundary detection.
func TestRefineRollupEnd_FindsTopicShift(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	// Build 20 checkpoints with IDs "cp-0" through "cp-19".
	const total = 20
	checkpoints := make([]TurnCheckpoint, total)
	for i := 0; i < total; i++ {
		checkpoints[i] = TurnCheckpoint{
			ID:         "cp-" + string(rune('0'+i%10)) + string(rune('0'+i/10)),
			Summary:    "summary",
			ActionableSummary: "actionable summary",
		}
	}

	// Create embeddings: turns 0-9 are in the "auth" cluster (x-axis),
	// turns 10-19 are in the "CI" cluster (y-axis). The similarity drop
	// at the boundary (index 9→10) should exceed the detection threshold.
	//
	// Dimensions must match what the mock provider uses (128).
	const dims = 128
	records := make([]embedding.VectorRecord, total)
	for i := 0; i < total; i++ {
		vec := make([]float32, dims)
		if i < 10 {
			// Auth cluster: unit vector along dimension 0
			vec[0] = 1.0
		} else {
			// CI cluster: unit vector along dimension 1
			vec[1] = 1.0
		}
		records[i] = embedding.VectorRecord{
			ID:        "hex-" + checkpoints[i].ID,
			Signature: "turn " + checkpoints[i].ID,
			Embedding: vec,
			IndexedAt: time.Now().UTC(),
			Type:      "conversation_turn",
			Metadata: map[string]interface{}{
				"checkpoint_id": checkpoints[i].ID,
			},
		}
	}

	// Store the records directly (bypassing EmbedAndStoreTurn to avoid
	// the mock provider's deterministic embedding logic — we want controlled
	// orthogonal vectors for boundary detection).
	if err := store.Store(records); err != nil {
		t.Fatalf("failed to store records: %v", err)
	}

	// Build a minimal Agent with the embedding manager attached.
	// We use a newAgent stub so refineRollupEnd has access to the manager.
	a := newTestAgentForEmbedding()
	setEmbeddingManager(a, mgr)

	// Request refinement for the full range. We expect the boundary
	// detector to find a large similarity drop at index 9→10 and return
	// a value in [9, 11] rather than the default 19.
	got := a.refineRollupEnd(ctx, checkpoints, 0, 19)

	// The cosine similarity between x-axis and y-axis unit vectors is 0.
	// With rollupBoundarySimilarityDrop = 0.20, this should be detected.
	// The boundary should be at index 9 (last item before the drop).
	if got < 8 || got > 12 {
		t.Errorf("refineRollupEnd returned %d, expected boundary in [8, 12]", got)
	}
}

// TestRefineRollupEnd_FallsBackWhenNoEmbeddings verifies that when the
// embedding manager is nil, refineRollupEnd returns defaultEnd unchanged
// (graceful degradation).
func TestRefineRollupEnd_FallsBackWhenNoEmbeddings(t *testing.T) {
	ctx := context.Background()

	a := newTestAgentForEmbedding()
	// Do not set embedding manager — a.GetEmbeddingManager() returns nil.

	checkpoints := []TurnCheckpoint{
		{ID: "cp-a", Summary: "a"}, {ID: "cp-b", Summary: "b"},
		{ID: "cp-c", Summary: "c"}, {ID: "cp-d", Summary: "d"},
		{ID: "cp-e", Summary: "e"},
	}

	const defaultEnd = 4
	got := a.refineRollupEnd(ctx, checkpoints, 0, defaultEnd)

	if got != defaultEnd {
		t.Errorf("refineRollupEnd returned %d, want %d (no manager)", got, defaultEnd)
	}
}

// TestRefineRollupEnd_FallsBackWhenTooFewCandidates verifies that when
// the candidate window is too small for meaningful boundary detection,
// the function returns defaultEnd (SP-066 Phase 3d guards this).
func TestRefineRollupEnd_FallsBackWhenTooFewCandidates(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	a := newTestAgentForEmbedding()
	setEmbeddingManager(a, mgr)

	// Fewer than rollupBoundaryMin*2 candidates → no split possible.
	checkpoints := []TurnCheckpoint{
		{ID: "cp-1", Summary: "s1"},
		{ID: "cp-2", Summary: "s2"},
		{ID: "cp-3", Summary: "s3"},
		{ID: "cp-4", Summary: "s4"},
	}

	const defaultEnd = 3
	got := a.refineRollupEnd(ctx, checkpoints, 0, defaultEnd)

	if got != defaultEnd {
		t.Errorf("refineRollupEnd returned %d for small window, want %d", got, defaultEnd)
	}
}

// TestRefineRollupEnd_EndToEnd_ThroughEmbedAndStoreTurn exercises the FULL
// production plumbing chain: EmbedAndStoreTurn → store → collectCheckpointVectors
// → refineRollupEnd. This is the regression test for the SP-066 Phase 3d bug
// where the metadata checkpoint_id was not stamped, causing boundary detection
// to always fall back to default range on real workloads.
//
// The test stores 15 records through EmbedAndStoreTurn (real production path:
// embedding → VectorRecord with checkpoint_id → store), then adds 5 hand-crafted
// records with orthogonal vectors (to guarantee a boundary). This proves both
// the stamping path and the boundary detection path work together.
func TestRefineRollupEnd_EndToEnd_ThroughEmbedAndStoreTurn(t *testing.T) {
	ctx := context.Background()
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	// Store 15 records through EmbedAndStoreTurn (real production path).
	// This proves checkpoint_id stamping works end-to-end.
	const embedCount = 15
	for i := 0; i < embedCount; i++ {
		turn, err := NewConversationTurn("e2e-session", i+1,
			"Implement authentication using JWT tokens", "/tmp/workspace")
		if err != nil {
			t.Fatalf("failed to create turn %d: %v", i, err)
		}
		turn.ActionableSummary = "JWT auth implementation"

		cpID := "cp-" + formatInt(i)
		if err := EmbedAndStoreTurn(ctx, mgr, turn, cpID); err != nil {
			t.Fatalf("EmbedAndStoreTurn failed at turn %d: %v", i, err)
		}
	}

	// Add 5 records with orthogonal vectors (dim 1) to create a topic boundary.
	// These are stored directly to get guaranteed orthogonal vectors.
	// They still use checkpoint_id metadata (simulating what EmbedAndStoreTurn would set).
	const handCraftedCount = 5
	for i := 0; i < handCraftedCount; i++ {
		vec := make([]float32, 128)
		vec[1] = 1.0 // orthogonal to dim-0 cluster
		cpID := "cp-" + formatInt(embedCount+i)
		record := embedding.VectorRecord{
			ID:        "hex-" + cpID,
			Signature: "turn " + cpID,
			Embedding: vec,
			IndexedAt: time.Now().UTC(),
			Type:      "conversation_turn",
			Metadata: map[string]interface{}{
				"checkpoint_id": cpID,
			},
		}
		if err := store.Store([]embedding.VectorRecord{record}); err != nil {
			t.Fatalf("failed to store record %d: %v", i, err)
		}
	}

	// Now verify all 20 records landed, and that the 15 from the production
	// path carry checkpoint_id metadata. If EmbedAndStoreTurn didn't stamp it,
	// the lookup in refineRollupEnd can't find them and falls back to default.
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("failed to load records: %v", err)
	}
	const total = embedCount + handCraftedCount
	if len(all) != total {
		t.Fatalf("expected %d records, got %d", total, len(all))
	}
	embedRecordsFound := 0
	for _, rec := range all {
		cpID, ok := rec.Metadata["checkpoint_id"].(string)
		if !ok || cpID == "" {
			t.Errorf("record %s missing checkpoint_id metadata — EmbedAndStoreTurn failed to stamp it", rec.ID)
			continue
		}
		var id int
		fmt.Sscanf(cpID, "cp-%d", &id)
		if id >= 0 && id < embedCount {
			embedRecordsFound++
		}
	}
	if embedRecordsFound != embedCount {
		t.Errorf("expected %d records with checkpoint_id from EmbedAndStoreTurn, got %d", embedCount, embedRecordsFound)
	}

	// Build checkpoints for all 20 records.
	checkpoints := make([]TurnCheckpoint, 20)
	for i := 0; i < 20; i++ {
		checkpoints[i] = TurnCheckpoint{
			ID:                 "cp-" + formatInt(i),
			Summary:            "summary",
			ActionableSummary:  "actionable summary",
		}
	}

	a := newTestAgentForEmbedding()
	setEmbeddingManager(a, mgr)

	// Boundary should be detected at the dim-0/dim-1 boundary (index 14).
	// dim-0/dim-1 cosim = 0.0, drop = 1.0 > 0.20 threshold.
	// Split at 14 leaves 15 items first, 5 last → 5 >= rollupBoundaryMin.
	got := a.refineRollupEnd(ctx, checkpoints, 0, 19)
	if got < 12 || got > 16 {
		t.Errorf("refineRollupEnd=%d, want ~14 (boundary between dim-0 and dim-1 clusters)", got)
	}
}

// setEmbeddingManager is already declared in memory_integration_test.go.

// newTestAgentForEmbedding creates a minimal Agent for testing with
// no LLM client, no config, and an optional embedding manager.
// The returned agent satisfies the minimum requirements for refineRollupEnd.
func newTestAgentForEmbedding() *Agent {
	return &Agent{
		debug: false,
	}
}

// formatInt converts an integer to string without importing strconv.
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + formatInt(-n)
	}
	digits := "0123456789"
	result := ""
	for n > 0 {
		result = string(digits[n%10]) + result
		n /= 10
	}
	return result
}
