package agent

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// TestRecalledItemFromRecord_PopulatesFields covers the happy path: a
// rollup record with all expected metadata fields lands as a populated
// RecalledItem and the recency decay knocks the score below the raw
// similarity.
func TestRecalledItemFromRecord_PopulatesFields(t *testing.T) {
	now := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	rec := embedding.QueryResult{
		Similarity: 0.80,
		Record: embedding.VectorRecord{
			ID:        "rollup:cp-abc",
			Signature: "rolled-up summary text",
			IndexedAt: now.Add(-14 * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"sessionId":          "sess-1",
				"checkpoint_id":      "cp-abc",
				"level":              float64(1),
				"start_index":        float64(0),
				"end_index":          float64(99),
				"actionable_summary": "do the thing",
				"workingDir":         "/repo/x",
			},
		},
	}

	item, ok := recalledItemFromRecord(rec, "sess-1", now, math.Ln2/semanticRecallHalfLifeDays)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if item.CheckpointID != "cp-abc" {
		t.Errorf("CheckpointID = %q, want cp-abc", item.CheckpointID)
	}
	if item.Level != 1 {
		t.Errorf("Level = %d, want 1", item.Level)
	}
	if item.StartIndex != 0 || item.EndIndex != 99 {
		t.Errorf("range = (%d,%d), want (0,99)", item.StartIndex, item.EndIndex)
	}
	if item.Actionable != "do the thing" {
		t.Errorf("Actionable = %q, want 'do the thing'", item.Actionable)
	}
	if item.AgeDays < 13 || item.AgeDays > 15 {
		t.Errorf("AgeDays = %f, want ~14", item.AgeDays)
	}
	// With a 7-day half-life and 14-day age, score ≈ similarity × 2^(-2) ≈ 0.20.
	if item.Score > 0.25 || item.Score < 0.15 {
		t.Errorf("score = %f, want ~0.20 (similarity 0.8 decayed by 14 days at 7-day half-life)", item.Score)
	}
}

// TestRecalledItemFromRecord_RejectsWrongSession enforces the in-session
// scope. Cross-session retrieval is explicitly out-of-scope for SP-066.
func TestRecalledItemFromRecord_RejectsWrongSession(t *testing.T) {
	now := time.Now().UTC()
	rec := embedding.QueryResult{
		Similarity: 0.9,
		Record: embedding.VectorRecord{
			Signature: "from another session",
			Metadata:  map[string]interface{}{"sessionId": "other-session", "checkpoint_id": "cp-x"},
		},
	}
	if _, ok := recalledItemFromRecord(rec, "this-session", now, 0.1); ok {
		t.Fatalf("expected cross-session record to be rejected")
	}
}

// TestRecalledItemFromRecord_EmptySignatureRejected guards against
// injecting a blank recall block. Legacy records with an empty Signature
// would otherwise produce an empty header in the prompt.
func TestRecalledItemFromRecord_EmptySignatureRejected(t *testing.T) {
	now := time.Now().UTC()
	rec := embedding.QueryResult{
		Similarity: 0.95,
		Record: embedding.VectorRecord{
			Signature: "   ",
			Metadata:  map[string]interface{}{"sessionId": "sess-1", "checkpoint_id": "cp-x"},
		},
	}
	if _, ok := recalledItemFromRecord(rec, "sess-1", now, 0.1); ok {
		t.Fatalf("expected empty-signature record to be rejected")
	}
}

// TestFormatSemanticRecall_Empty returns an empty string when there's
// nothing to inject; the caller short-circuits on this so we don't append
// a stray header to the system supplement.
func TestFormatSemanticRecall_Empty(t *testing.T) {
	if got := FormatSemanticRecall(nil, 0); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

// TestFormatSemanticRecall_RendersBlocks verifies the markdown shape and
// the level-vs-turn header switch. The model needs distinct headers so
// it can tell apart "we covered this 3 turns ago" from "this is from a
// rollup spanning 20 historical turns."
func TestFormatSemanticRecall_RendersBlocks(t *testing.T) {
	items := []RecalledItem{
		{Level: 1, Similarity: 0.7, AgeDays: 5, Summary: "first item"},
		{Level: 0, Similarity: 0.6, AgeDays: 1, Summary: "second item", Actionable: "do X"},
	}
	got := FormatSemanticRecall(items, semanticRecallMaxInjectedChars)
	if !strings.Contains(got, "Recalled From Session History") {
		t.Errorf("missing section header: %q", got)
	}
	if !strings.Contains(got, "level-1 rollup") {
		t.Errorf("missing rollup level annotation: %q", got)
	}
	if !strings.Contains(got, "turn, similarity") {
		t.Errorf("missing per-turn header: %q", got)
	}
	if !strings.Contains(got, "first item") || !strings.Contains(got, "second item") {
		t.Errorf("missing summary bodies: %q", got)
	}
	if !strings.Contains(got, "Actionable: do X") {
		t.Errorf("missing actionable line: %q", got)
	}
}

// TestFormatSemanticRecall_RespectsCharBudget caps the total size so a
// pathological run (many large recalled blocks) can't undo the savings
// substitution earned us.
func TestFormatSemanticRecall_RespectsCharBudget(t *testing.T) {
	big := strings.Repeat("x", semanticRecallMaxInjectedChars)
	items := []RecalledItem{
		{Level: 0, Summary: big},
		{Level: 0, Summary: big},
		{Level: 0, Summary: big},
	}
	got := FormatSemanticRecall(items, semanticRecallMaxInjectedChars)
	// Total should be bounded near the budget plus the section header.
	if len(got) > semanticRecallMaxInjectedChars+500 {
		t.Fatalf("output exceeds budget by too much: got %d, budget %d", len(got), semanticRecallMaxInjectedChars)
	}
}

// TestRecall_NilAgent confirms the nil-receiver guard. We need this so
// CLI callers can short-circuit before constructing an Agent.
func TestRecall_NilAgent(t *testing.T) {
	var a *Agent
	items, err := a.Recall(context.Background(), "anything", 5)
	if err != nil {
		t.Fatalf("nil-agent Recall must not error, got: %v", err)
	}
	if items != nil {
		t.Fatalf("nil-agent Recall must return nil items, got: %v", items)
	}
}

// TestRecall_NoEmbeddingManager confirms that an Agent without an
// embedding manager returns (nil, nil) gracefully, without touching
// the store. The CLI path (--limit N for an offline Agent) hits this.
func TestRecall_NoEmbeddingManager(t *testing.T) {
	a := &Agent{}
	items, err := a.Recall(context.Background(), "anything", 5)
	if err != nil {
		t.Fatalf("no-manager Recall must not error, got: %v", err)
	}
	if items != nil {
		t.Fatalf("no-manager Recall must return nil items, got: %v", items)
	}
}

// TestRecall_NonPositiveLimit confirms the limit<=0 short-circuit
// fires for every non-positive value the CLI might pass via --limit.
func TestRecall_NonPositiveLimit(t *testing.T) {
	cases := []struct {
		name  string
		limit int
	}{
		{"negative", -1},
		{"zero", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{}
			items, err := a.Recall(context.Background(), "anything", tc.limit)
			if err != nil {
				t.Fatalf("Recall(limit=%d) must not error, got: %v", tc.limit, err)
			}
			if items != nil {
				t.Fatalf("Recall(limit=%d) must return nil items, got: %v", tc.limit, items)
			}
		})
	}
}

// TestRecall_LimitTruncatesAndFiltersBySession exercises the full Recall
// path with a mock embedding manager. It verifies that:
//   - limit truncates the returned slice to the requested size
//   - cross-session records are excluded (SP-066 in-session scope)
func TestRecall_LimitTruncatesAndFiltersBySession(t *testing.T) {
	ctx := context.Background()

	// Build an EmbeddingManager with a mock provider.
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// Populate the conversation store with test records:
	// 4 records from "this-session" + 1 from "other-session".
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	now := time.Now().UTC()
	provider := store.Provider()

	// Create embeddings for each record text.
	embed1, _ := provider.Embed(ctx, "first recalled summary about testing")
	embed2, _ := provider.Embed(ctx, "second recalled summary about auth")
	embed3, _ := provider.Embed(ctx, "third recalled summary about config")
	embed4, _ := provider.Embed(ctx, "fourth recalled summary about deploy")
	embedOther, _ := provider.Embed(ctx, "cross session summary about something")

	records := []embedding.VectorRecord{
		{
			ID:        "rollup:cp-1",
			Signature: "first recalled summary about testing",
			Embedding: embed1,
			IndexedAt: now.Add(-1 * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"sessionId":     "this-session",
				"checkpoint_id": "cp-1",
				"level":         float64(0),
			},
		},
		{
			ID:        "rollup:cp-2",
			Signature: "second recalled summary about auth",
			Embedding: embed2,
			IndexedAt: now.Add(-2 * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"sessionId":     "this-session",
				"checkpoint_id": "cp-2",
				"level":         float64(0),
			},
		},
		{
			ID:        "rollup:cp-3",
			Signature: "third recalled summary about config",
			Embedding: embed3,
			IndexedAt: now.Add(-3 * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"sessionId":     "this-session",
				"checkpoint_id": "cp-3",
				"level":         float64(0),
			},
		},
		{
			ID:        "rollup:cp-4",
			Signature: "fourth recalled summary about deploy",
			Embedding: embed4,
			IndexedAt: now.Add(-4 * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"sessionId":     "this-session",
				"checkpoint_id": "cp-4",
				"level":         float64(0),
			},
		},
		{
			ID:        "rollup:cp-other",
			Signature: "cross session summary about something",
			Embedding: embedOther,
			IndexedAt: now.Add(-1 * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"sessionId":     "other-session",
				"checkpoint_id": "cp-other",
				"level":         float64(0),
			},
		},
	}

	if err := store.Store(records); err != nil {
		t.Fatalf("failed to store test records: %v", err)
	}

	// Wire the manager into an Agent and set the session ID.
	a := &Agent{}
	a.embeddingMgr = mgr
	a.SetSessionID("this-session")

	// Query with limit=2; expect at most 2 items, all from "this-session".
	items, err := a.Recall(ctx, "testing auth config", 2)
	if err != nil {
		t.Fatalf("Recall returned error: %v", err)
	}

	// Verify limit truncation.
	if len(items) > 2 {
		t.Fatalf("Recall returned %d items, want at most 2", len(items))
	}
	if len(items) == 0 {
		t.Fatal("Recall returned no items, expected at least 1")
	}

	// Verify no cross-session leakage.
	for _, item := range items {
		// The items themselves don't carry sessionId directly, but
		// retrieveSemanticRecall filters by session before returning.
		// We verify indirectly: the cross-session record's checkpoint_id
		// is "cp-other" — if it leaked through, we'd see it.
		if item.CheckpointID == "cp-other" {
			t.Errorf("cross-session record leaked through: checkpoint_id=%q", item.CheckpointID)
		}
	}
}
