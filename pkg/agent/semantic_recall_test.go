package agent

import (
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
	if got := FormatSemanticRecall(nil); got != "" {
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
	got := FormatSemanticRecall(items)
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
	got := FormatSemanticRecall(items)
	// Total should be bounded near the budget plus the section header.
	if len(got) > semanticRecallMaxInjectedChars+500 {
		t.Fatalf("output exceeds budget by too much: got %d, budget %d", len(got), semanticRecallMaxInjectedChars)
	}
}
