package embedding

// Shared inference-chunk planning for batch embedding providers.
//
// Every batched provider (ONNX CPU, MLX Metal) pads each row in an inference
// chunk up to that chunk's longest row, and transformer self-attention
// materializes a [batch, heads, seq, seq] score tensor — so a chunk's cost
// grows with rows × seqLen², not rows × seqLen. ONE long unit in a 32-row
// chunk makes all 32 rows pay its length. The planner below is the single
// implementation of that bound; both providers must chunk identically so
// index builds cost the same regardless of backend.

// defaultBatchChunkSize caps the ROW count per inference call. Throughput
// scales near-linearly up to ~16–32 rows, after which memory bandwidth
// dominates.
const defaultBatchChunkSize = 32

// defaultBatchAttentionBudget caps the ATTENTION cost per inference call,
// measured in rows × seqLen² "score cells".
//
// At ~48 bytes per score cell (12 heads × float32) this budget keeps a call
// near ~400 MB. Short units still batch the full 32 rows (a 256-token chunk
// costs 32 × 256² ≈ 2.1M cells, well under budget); only long units drop the
// row count, down to a floor of 1 so a single maxSeqLen unit always proceeds.
//
// The regression this guards against: one 2048-token unit in a 32-row chunk
// forced all 32 rows to pad to 2048, so attention cost hit 32 × 2048² and a
// routine index build held ~18 GB of native allocations in a single call —
// which, on the MLX backend, the allocator's freed-buffer pool then held
// indefinitely (observed as an 8.7 GB daemon footprint with a 154 MB Go
// heap).
const defaultBatchAttentionBudget = 8 << 20 // 8M cells ≈ 400 MB

// inferenceChunk is one inference call: the input rows (indices into the
// caller's batch, in order) and the padded sequence length for the call.
type inferenceChunk struct {
	Rows   []int
	SeqLen int
}

// planInferenceChunks partitions tokenized sequence lengths into chunks
// bounded by BOTH the row cap and the attention budget. Zero-length rows are
// excluded — callers give them a zero vector without an inference call, and
// excluding them keeps chunks packed with real work.
func planInferenceChunks(lens []int32) []inferenceChunk {
	work := make([]int, 0, len(lens))
	for i, n := range lens {
		if n > 0 {
			work = append(work, i)
		}
	}

	var chunks []inferenceChunk
	for s := 0; s < len(work); {
		// Grow the chunk while it stays within both bounds. Every row pads
		// up to the chunk's longest row, so admitting a long row re-prices
		// every row already in the chunk — hence the budget is re-checked
		// against the candidate maxLen, not the incoming row alone. At least
		// one row is always admitted so a single maxSeqLen unit still makes
		// progress.
		maxLen, e := 0, s
		for e < len(work) && e-s < defaultBatchChunkSize {
			candLen := maxLen
			if n := int(lens[work[e]]); n > candLen {
				candLen = n
			}
			rows := e - s + 1
			if rows > 1 && int64(rows)*int64(candLen)*int64(candLen) > defaultBatchAttentionBudget {
				break
			}
			maxLen = candLen
			e++
		}
		chunks = append(chunks, inferenceChunk{
			Rows:   append([]int(nil), work[s:e]...),
			SeqLen: maxLen,
		})
		s = e
	}
	return chunks
}
