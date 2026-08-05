package embedding

import "testing"

// planChunks mirrors the chunking arithmetic in embedBatchInternal so the
// bound can be asserted without a live ONNX session.
func planChunks(lens []int) (rows []int, seqs []int) {
	work := make([]int, 0, len(lens))
	for i, n := range lens {
		if n > 0 {
			work = append(work, i)
		}
	}
	for s := 0; s < len(work); {
		maxLen, e := 0, s
		for e < len(work) && e-s < defaultBatchChunkSize {
			candLen := maxLen
			if n := lens[work[e]]; n > candLen {
				candLen = n
			}
			r := int64(e - s + 1)
			if r > 1 && r*int64(candLen)*int64(candLen) > defaultBatchAttentionBudget {
				break
			}
			maxLen = candLen
			e++
		}
		rows = append(rows, e-s)
		seqs = append(seqs, maxLen)
		s = e
	}
	return rows, seqs
}

// The regression: one 2048-token unit in a 32-row chunk forced all 32 rows to
// pad to 2048, so attention cost hit 32 × 2048² and a routine index build held
// ~18 GB in a single ORT call.
func TestChunkingBoundsAttentionCost(t *testing.T) {
	lens := make([]int, 32)
	for i := range lens {
		lens[i] = 64
	}
	lens[10] = 2048 // one long unit poisons the whole chunk under the old scheme

	rows, seqs := planChunks(lens)
	for i := range rows {
		cost := int64(rows[i]) * int64(seqs[i]) * int64(seqs[i])
		if rows[i] > 1 && cost > defaultBatchAttentionBudget {
			t.Errorf("chunk %d: rows=%d seq=%d cost=%d exceeds budget %d",
				i, rows[i], seqs[i], cost, defaultBatchAttentionBudget)
		}
	}
	worst := int64(0)
	for i := range rows {
		if c := int64(rows[i]) * int64(seqs[i]) * int64(seqs[i]); c > worst {
			worst = c
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
	lens := make([]int, 128)
	for i := range lens {
		lens[i] = 256
	}
	rows, _ := planChunks(lens)
	for i, r := range rows {
		if r != defaultBatchChunkSize {
			t.Errorf("chunk %d: rows=%d, want full %d for short units", i, r, defaultBatchChunkSize)
		}
	}
}

// A single unit longer than the budget must still be admitted, not skipped.
func TestChunkingAlwaysAdmitsOneRow(t *testing.T) {
	rows, seqs := planChunks([]int{2048, 2048, 2048})
	for i, r := range rows {
		if r < 1 {
			t.Fatalf("chunk %d admitted %d rows", i, r)
		}
		t.Logf("chunk %d: rows=%d seq=%d", i, r, seqs[i])
	}
	total := 0
	for _, r := range rows {
		total += r
	}
	if total != 3 {
		t.Errorf("processed %d rows, want all 3", total)
	}
}

// Empty inputs must not consume a batch slot.
func TestChunkingSkipsEmptyRows(t *testing.T) {
	rows, _ := planChunks([]int{0, 0, 128, 0, 128})
	total := 0
	for _, r := range rows {
		total += r
	}
	if total != 2 {
		t.Errorf("processed %d rows, want 2 non-empty", total)
	}
}
