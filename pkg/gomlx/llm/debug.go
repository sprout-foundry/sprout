//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"fmt"
	"math"
	"os"
	"runtime"
)

// DebugDecodeComparison runs one decode step both ways — full re-encode and
// cache decode — and compares the logits. Returns the max absolute difference
// and the top-5 tokens from each method. This is a diagnostic method for
// debugging the KV cache.
func (m *Model) DebugDecodeComparison(promptTokens []int, nextToken int) (maxDiff float64, fullTop5, cacheTop5 []int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s, err := m.backend.DefaultGPUStream()
	if err != nil {
		return 0, nil, nil, err
	}
	defer s.Free()
	m.stream = s
	m.arch.SetStream(s)

	// Method 1: Full re-encode
	allTokens := append(append([]int{}, promptTokens...), nextToken)
	fullLogits, err := m.arch.ForwardPrefill(m.makeIDsArray(allTokens), len(allTokens), nil)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("full prefill: %w", err)
	}

	// Method 2: Cache prefill + decode
	cache := NewKVCache(m.cfg.NumLayers, s)
	defer cache.Free()

	_, err = m.arch.ForwardPrefill(m.makeIDsArray(promptTokens), len(promptTokens), cache)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("cache prefill: %w", err)
	}

	cacheLogits, err := m.arch.ForwardDecode(nextToken, len(promptTokens), cache)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("cache decode: %w", err)
	}

	// Compare
	maxDiff = 0
	minLen := len(fullLogits)
	if len(cacheLogits) < minLen {
		minLen = len(cacheLogits)
	}
	for i := 0; i < minLen; i++ {
		d := math.Abs(float64(fullLogits[i] - cacheLogits[i]))
		if d > maxDiff {
			maxDiff = d
		}
	}

	fullTop5 = topKIndices(fullLogits, 5)
	cacheTop5 = topKIndices(cacheLogits, 5)

	return maxDiff, fullTop5, cacheTop5, nil
}

func topKIndices(logits []float32, k int) []int {
	type idxVal struct {
		idx int
		val float32
	}
	items := make([]idxVal, len(logits))
	for i, v := range logits {
		items[i] = idxVal{i, v}
	}
	for i := 0; i < k && i < len(items); i++ {
		maxJ := i
		for j := i + 1; j < len(items); j++ {
			if items[j].val > items[maxJ].val {
				maxJ = j
			}
		}
		items[i], items[maxJ] = items[maxJ], items[i]
	}
	result := make([]int, 0, k)
	for i := 0; i < k && i < len(items); i++ {
		result = append(result, items[i].idx)
	}
	return result
}

// Ensure os import is used (for future debug output)
var _ = os.Stderr
