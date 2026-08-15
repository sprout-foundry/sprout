//go:build darwin && arm64 && cgo

package qwen35

import (
	"log"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// logPrefillLayerMem reports MLX's own allocator accounting after a prefill
// layer, gated behind SPROUT_PREFILL_LAYER_MEM=1 (see prefillInternalChunk).
// Used to find which layer(s) a long-context prefill's peak memory comes
// from, since MLX's peak counter is a single running high-water mark with no
// per-op breakdown of its own.
func logPrefillLayerMem(layerIdx int, kind string, seqLen int) {
	stats, err := mlx.Snapshot()
	if err != nil {
		return
	}
	live, gcFinalized := mlx.ArrayLiveStats()
	log.Printf("llm: prefill layer %d (%s) seqLen=%d: active=%.1fMB cache=%.1fMB peak=%.1fMB live=%d gcFinalized=%d",
		layerIdx, kind, seqLen, float64(stats.Active)/1048576, float64(stats.Cache)/1048576, float64(stats.Peak)/1048576, live, gcFinalized)
}
