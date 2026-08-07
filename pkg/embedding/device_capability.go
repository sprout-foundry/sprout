package embedding

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// dualModelMemoryFloor is the minimum total system RAM (bytes) for the
// dual-model architecture (Jina Code v2 for code + Gemma for text). Below
// this, only the primary Gemma model loads — running both providers
// simultaneously on small devices costs more in memory pressure, thermal
// throttling, and ORT intra-op contention than the code-specific retrieval
// quality is worth. The fallback is automatic: codeAvailable stays false
// and code queries route through Gemma.
//
// The 16 GB floor is measured, not arbitrary. On an 11 GB Snapdragon phone
// (Termux), running both models pinned RSS at ~2 GB and pushed 5 GB into
// zram swap, dropping sustained CPU clocks from 2.4 GHz to 1.5 GHz via
// thermal throttle and cutting Jina throughput to ~2 rec/s (vs ~150 rec/s
// on a desktop CPU at the same batch size). Single-model operation on the
// same device holds ~1 GB RSS, no swap pressure, sustained clocks. 16 GB
// is also the natural desktop/laptop cutoff below which a user is likely
// on a constrained device (mini-PC, older laptop, phone-as-host).
const dualModelMemoryFloor = 16 << 30 // 16 GB

// totalSystemMemory returns the system's total physical RAM in bytes, best
// effort. Returns false when the value can't be determined (non-Linux, or
// /proc/meminfo unreadable); callers must treat that as "don't know, don't
// gate" rather than "small device".
func totalSystemMemory() (int64, bool) {
	// /proc/meminfo is the canonical source on Linux and Android. macOS and
	// Windows don't expose total RAM via /proc; for those platforms the
	// dual-model path stays enabled by default (desktop/laptop hardware
	// generally has the headroom).
	if runtime.GOOS != "linux" && runtime.GOOS != "android" {
		return 0, false
	}
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	return parseMemTotal(string(data))
}

// parseMemTotal extracts MemTotal (in bytes) from /proc/meminfo content.
// Pure function for testability — see device_capability_test.go.
func parseMemTotal(meminfo string) (int64, bool) {
	for _, line := range strings.Split(meminfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil || kb <= 0 {
				return 0, false
			}
			return kb * 1024, true
		}
	}
	return 0, false
}

// dualModelSupported reports whether the device has enough RAM to run both
// the code-specific model and the general model concurrently without
// pathological memory pressure. When false, the EmbeddingManager skips
// loading the code provider and routes everything through the primary.
func dualModelSupported() bool {
	mem, ok := totalSystemMemory()
	if !ok {
		// Can't read memory (non-Linux, or /proc unavailable). Don't gate —
		// let the existing download/init failure path handle it.
		return true
	}
	return mem >= dualModelMemoryFloor
}
