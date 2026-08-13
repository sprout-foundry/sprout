//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

package llm

import (
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// weightBytesForModel returns the on-disk size of the model's safetensors
// shards in bytes. This is the floor for resident weight memory: MLX holds
// the quantized tensors (plus dequantization work buffers) in unified memory.
func weightBytesForModel(modelDir string) (int64, error) {
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return 0, fmt.Errorf("read model dir: %w", err)
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".safetensors") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return 0, fmt.Errorf("stat %s: %w", e.Name(), err)
		}
		total += info.Size()
	}
	return total, nil
}

// ModelMemoryGate reports whether the model at modelDir can safely load on
// this machine, following SP-134's RAM gate: on Apple Silicon, GPU and CPU
// share unified RAM, so a large model competes with the OS for physical
// memory. The check is conservative: weights must fit inside roughly half of
// physical RAM, leaving room for the working set (KV cache, activations,
// dequant buffers) and the rest of the system.
//
// Returns nil when the model is small enough (or RAM is unknown — the gate
// is advisory on stub/non-Metal builds). Returns an error with a clear
// message when the machine is likely to run out of memory during generation.
//
// SPROUT_ALLOW_OVERWEIGHT=1 forces the gate open for power users on
// swap-backed machines (e.g. validating a raw BF16 fine-tune before
// quantizing it); the server still applies ApplyMemoryLimits so allocation
// fails cleanly instead of wedging Metal.
func ModelMemoryGate(modelDir string) error {
	if os.Getenv("SPROUT_ALLOW_OVERWEIGHT") == "1" {
		return nil
	}
	backend := tensor.DetectBackend()
	var ram uint64
	if backend != nil {
		ram = backend.TotalSystemRAM()
	} else {
		ram = 8 * 1024 * 1024 * 1024 // 8 GB fallback
	}
	if ram == 0 {
		return nil // can't measure — don't block
	}
	w, err := weightBytesForModel(modelDir)
	if err != nil || w == 0 {
		return nil // can't measure weights — don't block
	}

	// The working set roughly doubles the resident footprint (KV cache,
	// activations, quantized-matmul scratch). Hard-block only when weights
	// alone already threaten the system (>= 50% of RAM). For models in the
	// 25-50% band the server still sets MLX memory/cache limits so allocation
	// fails cleanly instead of blocking the process in a Metal cond wait.
	if w*2 > int64(ram) {
		return fmt.Errorf(
			"model weights are %.1f GB but this machine has %.1f GB RAM; "+
				"generation would exhaust unified memory (SP-134 RAM gate). "+
				"Use a smaller quantized model (e.g. the 0.8B) or a larger machine.",
			float64(w)/1073741824, float64(ram)/1073741824)
	}
	return nil
}

// ApplyMemoryLimits configures the tensor backend's allocator for long-running
// server use on a RAM-constrained machine. It sets:
//
//   - an active-memory soft limit (fail fast with an error instead of
//     blocking the process in a Metal cond wait when the KV cache or an
//     activation tensor would exceed available RAM), and
//   - a pooled-buffer cache limit (return freed buffers to the OS instead of
//     letting the cache grow without bound — the runtime pools every freed
//     buffer by default, which makes long-running generation look like a leak).
//
// Limits are sized off physical RAM (or left at defaults when RAM can't
// be measured, e.g. stub builds). A minimum margin keeps the OS and other
// processes alive while the model generates.
func ApplyMemoryLimits() error {
	backend := tensor.DetectBackend()
	if backend == nil {
		return nil
	}
	ram := backend.TotalSystemRAM()
	if ram == 0 {
		return nil // can't size limits — leave defaults
	}

	// Keep at least ~2 GB for the OS and the rest of the system. The active
	// limit is the larger of (RAM - 2 GB) and (RAM / 2), so tiny machines
	// still get a usable slice but never the full machine.
	//
	// This margin was 3 GB until it was measured to cause a severe decode
	// slowdown (2-3x) for larger models at long context on a 16 GB machine:
	// with a 5.8 GB model at 8K context, a 13 GB active limit put MLX's
	// allocator too close to the ceiling, triggering aggressive buffer
	// eviction on every layer. 2 GB of margin was the smallest tested value
	// that stayed clean; 2.5 GB still reproduced the slowdown.
	const minMargin = 2 * 1024 * 1024 * 1024
	var active uint64
	if ram > minMargin {
		active = ram - minMargin
	}
	if half := ram / 2; active < half {
		active = half
	}
	if err := backend.SetMemoryLimit(active); err != nil {
		return fmt.Errorf("set memory limit: %w", err)
	}

	// Cap pooled buffers at ~1.5 GB so long server runs return freed
	// workspace to the OS between requests instead of accumulating it.
	const cacheCap = 1536 * 1024 * 1024
	if err := backend.SetCacheLimit(cacheCap); err != nil {
		return fmt.Errorf("set cache limit: %w", err)
	}
	return nil
}

// TrimCachedMemory returns pooled buffers to the OS. Call after a request
// completes so the next request starts from a clean slate instead of the
// previous request's pooled cache.
func TrimCachedMemory() error {
	backend := tensor.DetectBackend()
	if backend == nil {
		return nil
	}
	return backend.ClearCache()
}
