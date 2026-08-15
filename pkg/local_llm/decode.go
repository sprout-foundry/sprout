//go:build darwin && arm64 && cgo && mlx

package local_llm

import (
	"math"
	"math/rand"
	"sort"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

// sample picks the next token from logits using temperature + top-k/top-p sampling.
func sample(logits []float32, cfg GenerateConfig, s *mlx.Stream) (int, error) {
	if cfg.Temperature <= 0 {
		// Greedy: return argmax
		bestIdx := 0
		bestVal := logits[0]
		for i, v := range logits {
			if v > bestVal {
				bestVal = v
				bestIdx = i
			}
		}
		return bestIdx, nil
	}

	// Apply temperature: logits / T
	scaled := make([]float32, len(logits))
	for i, v := range logits {
		scaled[i] = v / cfg.Temperature
	}

	// Top-K filtering: keep only the top K tokens
	if cfg.TopK > 0 && cfg.TopK < len(scaled) {
		// Find the K-th largest value via partial sort
		indices := make([]int, len(scaled))
		for i := range indices {
			indices[i] = i
		}
		sort.Slice(indices, func(a, b int) bool {
			return scaled[indices[a]] > scaled[indices[b]]
		})
		// Zero out everything outside top-K
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

	// Top-P (nucleus) filtering: keep tokens that cumulatively reach top_p
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
		// Renormalize over kept tokens
		sum = 0
		for i := range probs {
			if !keep[i] {
				probs[i] = 0
			}
			sum += probs[i]
		}
	}

	// Sample
	r := mathRandFloat64() * sum
	cumulative := 0.0
	for i, p := range probs {
		cumulative += p
		if r <= cumulative {
			return i, nil
		}
	}

	return len(probs) - 1, nil
}

// mathRandFloat64 returns a random float64 in [0, 1). Used for stochastic
// sampling. Not crypto-safe, which is fine — this is text generation.
func mathRandFloat64() float64 { return rand.Float64() }
