//go:build darwin && arm64 && cgo

package llm

import (
	"log"
	"os"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// debugMemEnabled reports whether fine-grained memory checkpointing is on
// (see logGenMem, called from generateLocked around the post-prefill
// prefix-cache bookkeeping — a large, unexplained memory spike was traced to
// somewhere in that handoff at long context, independent of decode
// strategy).
func debugMemEnabled() bool {
	return os.Getenv("SPROUT_LOCAL_DEBUG") == "1" && os.Getenv("SPROUT_GEN_MEM") == "1"
}

func logGenMem(tag string) {
	if !debugMemEnabled() {
		return
	}
	stats, err := mlx.Snapshot()
	if err != nil {
		return
	}
	log.Printf("llm: genmem %s: active=%.1fMB cache=%.1fMB peak=%.1fMB",
		tag, float64(stats.Active)/1048576, float64(stats.Cache)/1048576, float64(stats.Peak)/1048576)
}
