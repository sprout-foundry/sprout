//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"math"
	"math/rand"
	"sort"
)

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

	// Top-K filtering: keep only the top K tokens
	if cfg.TopK > 0 && cfg.TopK < len(scaled) {
		indices := make([]int, len(scaled))
		for i := range indices {
			indices[i] = i
		}
		sort.Slice(indices, func(a, b int) bool {
			return scaled[indices[a]] > scaled[indices[b]]
		})
		threshold := scaled[indices[cfg.TopK-1]]
		for i := range scaled {
			if scaled[i] < threshold {
				scaled[i] = float32(math.Inf(-1))
			}
		}
	}

	// Softmax
	maxVal := float32(math.Inf(-1))
	for _, v := range scaled {
		if v > maxVal {
			maxVal = v
		}
	}

	var sum float64
	probs := make([]float64, len(scaled))
	for i, v := range scaled {
		e := math.Exp(float64(v - maxVal))
		probs[i] = e
		sum += e
	}

	// Top-P (nucleus) filtering
	if cfg.TopP < 1.0 {
		indices := make([]int, len(probs))
		for i := range indices {
			indices[i] = i
		}
		sort.Slice(indices, func(a, b int) bool {
			return probs[indices[a]] > probs[indices[b]]
		})

		cumProb := 0.0
		keep := make(map[int]bool)
		for _, idx := range indices {
			cumProb += probs[idx]
			keep[idx] = true
			if cumProb >= float64(cfg.TopP) {
				break
			}
		}
		sum = 0
		for i := range probs {
			if !keep[i] {
				probs[i] = 0
			}
			sum += probs[i]
		}
	}

	// Sample from the distribution
	r := rand.Float64() * sum
	cumulative := 0.0
	for i, p := range probs {
		cumulative += p
		if r <= cumulative {
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
