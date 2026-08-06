package embedding

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"
)

// TestBatchPaddingProbe quantifies how much of an index build's inference cost
// is padding. Every row in a chunk pads up to that chunk's longest row, and
// embedUnits batches in extraction order — so a 2000-byte file-level unit
// sitting beside thirty one-line functions makes all thirty pay its length.
//
// Opt-in: SPROUT_PADDING_PROBE=1.
func TestBatchPaddingProbe(t *testing.T) {
	if os.Getenv("SPROUT_PADDING_PROBE") != "1" {
		t.Skip("SPROUT_PADDING_PROBE unset")
	}

	ctx := context.Background()
	units := sampleUnitsForProbe(t, 1024)

	provider, _, err := acquireSharedONNXProvider(ctx, DefaultModelDir(), EmbeddingGemma300MConfig())
	if err != nil {
		t.Skipf("provider unavailable: %v", err)
	}
	onnx, ok := provider.(*ONNXEmbeddingProvider)
	if !ok {
		t.Skip("not an ONNX provider")
	}

	tokens := make([]int, len(units))
	var realTotal int
	for i, u := range units {
		n := len(onnx.tokenizer.EncodeWithBOSAndEOS(documentPrefix + embeddingText(u, 2000)))
		if n > onnx.maxSeqLen {
			n = onnx.maxSeqLen
		}
		tokens[i] = n
		realTotal += n
	}

	paddedCost := func(order []int) int {
		var total int
		for s := 0; s < len(order); s += EmbedBatchSize {
			e := s + EmbedBatchSize
			if e > len(order) {
				e = len(order)
			}
			max := 0
			for _, idx := range order[s:e] {
				if tokens[idx] > max {
					max = tokens[idx]
				}
			}
			total += max * (e - s)
		}
		return total
	}

	natural := make([]int, len(units))
	for i := range natural {
		natural[i] = i
	}
	sorted := append([]int(nil), natural...)
	sort.Slice(sorted, func(a, b int) bool { return tokens[sorted[a]] < tokens[sorted[b]] })

	sortedTokens := append([]int(nil), tokens...)
	sort.Ints(sortedTokens)

	t.Logf("units=%d  tokens: min=%d p50=%d p90=%d p99=%d max=%d  mean=%.0f",
		len(units), sortedTokens[0], sortedTokens[len(sortedTokens)/2],
		sortedTokens[len(sortedTokens)*90/100], sortedTokens[len(sortedTokens)*99/100],
		sortedTokens[len(sortedTokens)-1], float64(realTotal)/float64(len(units)))

	naturalCost := paddedCost(natural)
	sortedCost := paddedCost(sorted)
	t.Logf("token-positions actually needed : %d", realTotal)
	t.Logf("padded cost, extraction order   : %d  (%.1fx waste)", naturalCost, float64(naturalCost)/float64(realTotal))
	t.Logf("padded cost, length-sorted order: %d  (%.1fx waste)", sortedCost, float64(sortedCost)/float64(realTotal))
	t.Logf("=> length-sorting cuts padded work by %.1f%%", 100*(1-float64(sortedCost)/float64(naturalCost)))

	// Confirm the modelled saving shows up as wall-clock.
	sample := 256
	idx := NewIndexManager(onnx, newCountingStore(), IndexOptions{BatchSize: EmbedBatchSize, MaxBodyLen: 2000})

	start := time.Now()
	if _, err := idx.embedUnits(ctx, units[:sample], nil); err != nil {
		t.Fatalf("natural: %v", err)
	}
	naturalElapsed := time.Since(start)

	bySize := append([]CodeUnit(nil), units[:sample]...)
	sort.SliceStable(bySize, func(a, b int) bool {
		return len(embeddingText(bySize[a], 2000)) < len(embeddingText(bySize[b], 2000))
	})
	start = time.Now()
	if _, err := idx.embedUnits(ctx, bySize, nil); err != nil {
		t.Fatalf("sorted: %v", err)
	}
	sortedElapsed := time.Since(start)

	t.Logf("wall clock %d units: extraction order %s (%.1f u/s) vs length-sorted %s (%.1f u/s) => %.2fx",
		sample, naturalElapsed.Round(time.Millisecond), float64(sample)/naturalElapsed.Seconds(),
		sortedElapsed.Round(time.Millisecond), float64(sample)/sortedElapsed.Seconds(),
		naturalElapsed.Seconds()/sortedElapsed.Seconds())
}

func sampleUnitsForProbe(t *testing.T, n int) []CodeUnit {
	t.Helper()
	files, err := WalkCodeFiles(context.Background(), "../..")
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	var units []CodeUnit
	for _, f := range files {
		got, err := ExtractFromFile(f, WithIncludeTests(false))
		if err != nil {
			continue
		}
		units = append(units, got...)
		if len(units) >= n {
			break
		}
	}
	if len(units) < n {
		t.Fatalf("only %d units available, need %d", len(units), n)
	}
	return units[:n]
}
