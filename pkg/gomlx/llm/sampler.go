//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

package llm

import (
	"container/heap"
	"math"
	"math/rand"
)

// logitHeapItem pairs a scaled logit value with its token index for the
// max-heap used in top-K/top-P filtering.
type logitHeapItem struct {
	value float32
	idx   int
}

// logitHeap implements heap.Interface as a max-heap over logitHeapItem.
type logitHeap []logitHeapItem

func (h logitHeap) Len() int           { return len(h) }
func (h logitHeap) Less(i, j int) bool { return h[i].value > h[j].value }
func (h logitHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *logitHeap) Push(x any)        { *h = append(*h, x.(logitHeapItem)) }
func (h *logitHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// sample picks the next token from logits using temperature + top-k/top-p sampling.
func sample(logits []float32, cfg GenerateConfig) int {
	if cfg.Temperature <= 0 {
		return argmax(logits)
	}

	// Apply temperature: logits / T
	scaled := make([]float32, len(logits))
	for i, v := range logits {
		scaled[i] = v / cfg.Temperature
	}

	useTopK := cfg.TopK > 0 && cfg.TopK < len(scaled)
	useTopP := cfg.TopP > 0 && cfg.TopP < 1.0

	// Candidate indices, sorted descending by scaled logit. Built with a
	// max-heap so we never sort the full [vocab] array (the old sort.Slice
	// approach cost ~2 × O(V log V) per token and dominated decode time).
	var cand []int
	if useTopK || useTopP {
		limit := len(scaled)
		if useTopK {
			limit = cfg.TopK
		}
		h := make(logitHeap, len(scaled))
		for i, v := range scaled {
			h[i] = logitHeapItem{value: v, idx: i}
		}
		heap.Init(&h)
		cand = make([]int, 0, limit)
		for len(cand) < limit && h.Len() > 0 {
			item := heap.Pop(&h).(logitHeapItem)
			cand = append(cand, item.idx)
		}
	}

	// Softmax over the candidate set (or the full array when no filter).
	maxVal := float32(math.Inf(-1))
	consider := func(i int) {
		if scaled[i] > maxVal {
			maxVal = scaled[i]
		}
	}
	if cand == nil {
		for i := range scaled {
			consider(i)
		}
	} else {
		for _, i := range cand {
			consider(i)
		}
	}

	probs := make([]float64, len(scaled))
	sum := 0.0
	accumulate := func(i int) {
		e := math.Exp(float64(scaled[i] - maxVal))
		probs[i] = e
		sum += e
	}
	if cand == nil {
		for i := range scaled {
			accumulate(i)
		}
	} else {
		for _, i := range cand {
			accumulate(i)
		}
	}

	// Top-P (nucleus) filtering: cand is already ordered by descending
	// probability, so the surviving set is a prefix of it.
	keepCount := len(cand)
	if useTopP && cand != nil {
		cum := 0.0
		keepCount = 0
		for _, i := range cand {
			cum += probs[i] / sum
			keepCount++
			if cum >= float64(cfg.TopP) {
				break
			}
		}
	}

	// Sample from the (filtered) distribution.
	r := rand.Float64() * sum
	cumulative := 0.0
	pick := func(i int) bool {
		cumulative += probs[i]
		return r <= cumulative
	}
	if useTopP && cand != nil {
		for j := 0; j < keepCount; j++ {
			if pick(cand[j]) {
				return cand[j]
			}
		}
		return cand[keepCount-1]
	}
	if cand != nil {
		for _, i := range cand {
			if pick(i) {
				return i
			}
		}
		return cand[len(cand)-1]
	}
	for i := range probs {
		if pick(i) {
			return i
		}
	}
	return len(probs) - 1
}

// argmax returns the index of the maximum value.
func argmax(logits []float32) int {
	bestIdx := 0
	bestVal := logits[0]
	for i, v := range logits {
		if v > bestVal {
			bestVal = v
			bestIdx = i
		}
	}
	return bestIdx
}

// applyRepetitionPenalty penalizes tokens that have appeared in the recent
// context. For each token in the context, if its logit is positive, divide it
// by the penalty; if negative, multiply by the penalty. This follows the
// CTRL paper (Keskar et al., 2019) formulation.
func applyRepetitionPenalty(logits []float32, recentTokens []int, penalty float32) {
	seen := make(map[int]bool)
	for _, id := range recentTokens {
		if id < 0 || id >= len(logits) || seen[id] {
			continue
		}
		seen[id] = true
		if logits[id] > 0 {
			logits[id] /= penalty
		} else {
			logits[id] *= penalty
		}
	}
}
