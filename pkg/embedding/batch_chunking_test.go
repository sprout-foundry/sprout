package embedding

import "testing"

// The regression: one 2048-token unit in a 32-row chunk forced all 32 rows to
// pad to 2048, so attention cost hit 32 × 2048² — the ONNX backend held
// ~18 GB in a single call, and the MLX backend pooled those transient score
// tensors indefinitely in its freed-buffer cache (observed as a multi-GB
// daemon footprint with a flat Go heap).
func TestChunkingBoundsAttentionCost(t *testing.T) {
	lens := make([]int32, 32)
	for i := range lens {
		lens[i] = 64
	}
	lens[10] = 2048 // one long unit poisons the whole chunk under the old scheme

	chunks := planInferenceChunks(lens)
	for i, c := range chunks {
		cost := int64(len(c.Rows)) * int64(c.SeqLen) * int64(c.SeqLen)
		if len(c.Rows) > 1 && cost > defaultBatchAttentionBudget {
			t.Errorf("chunk %d: rows=%d seq=%d cost=%d exceeds budget %d",
				i, len(c.Rows), c.SeqLen, cost, defaultBatchAttentionBudget)
		}
	}
	worst := int64(0)
	for _, c := range chunks {
		if cost := int64(len(c.Rows)) * int64(c.SeqLen) * int64(c.SeqLen); cost > worst {
			worst = cost
		}
	}
	old := int64(32) * 2048 * 2048
	if worst >= old {
		t.Errorf("no improvement: worst cost %d vs old scheme %d", worst, old)
	}
	t.Logf("worst chunk cost %d cells vs %d under the old fixed-32 scheme (%.0fx better)",
		worst, old, float64(old)/float64(worst))
}

// Short units must still batch at the full row cap — the budget must not
// pessimize the common case.
func TestChunkingKeepsShortUnitsBatched(t *testing.T) {
	lens := make([]int32, 128)
	for i := range lens {
		lens[i] = 256
	}
	for i, c := range planInferenceChunks(lens) {
		if len(c.Rows) != defaultBatchChunkSize {
			t.Errorf("chunk %d: rows=%d, want full %d for short units", i, len(c.Rows), defaultBatchChunkSize)
		}
	}
}

// A single unit longer than the budget must still be admitted, not skipped.
func TestChunkingAlwaysAdmitsOneRow(t *testing.T) {
	chunks := planInferenceChunks([]int32{2048, 2048, 2048})
	total := 0
	for i, c := range chunks {
		if len(c.Rows) < 1 {
			t.Fatalf("chunk %d admitted %d rows", i, len(c.Rows))
		}
		t.Logf("chunk %d: rows=%d seq=%d", i, len(c.Rows), c.SeqLen)
		total += len(c.Rows)
	}
	if total != 3 {
		t.Errorf("processed %d rows, want all 3", total)
	}
}

// Empty inputs must not consume a batch slot.
func TestChunkingSkipsEmptyRows(t *testing.T) {
	chunks := planInferenceChunks([]int32{0, 0, 128, 0, 128})
	total := 0
	for _, c := range chunks {
		total += len(c.Rows)
	}
	if total != 2 {
		t.Errorf("processed %d rows, want 2 non-empty", total)
	}
}

// Row ORDER is preserved — providers write results back by original index,
// so the planner must never reorder rows across chunks.
func TestChunkingPreservesRowOrder(t *testing.T) {
	lens := []int32{10, 2048, 5, 2048, 7, 300, 300}
	var got []int
	for _, c := range planInferenceChunks(lens) {
		got = append(got, c.Rows...)
	}
	want := []int{0, 1, 2, 3, 4, 5, 6}
	if len(got) != len(want) {
		t.Fatalf("planner dropped rows: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row order changed: got %v want %v", got, want)
		}
	}
}
