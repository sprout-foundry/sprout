//go:build darwin && arm64 && cgo && mlx

package embedding

import (
	"os"
	"runtime"
	"syscall"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

// mlxProviderAvailable reports whether the MLX Metal provider should be used.
// True only on Apple Silicon with a GPU, at least 8 GB RAM, and no explicit
// CPU override. The RAM check prevents memory pressure on low-end machines
// where the fp16 model weights (~307 MB) plus working tensors would compete
// with the OS for unified memory.
func mlxProviderAvailable() bool {
	if !mlx.Available() {
		return false
	}

	// Skip on machines with less than 8 GB RAM. Apple's unified memory means
	// GPU allocations share system RAM; loading a 307 MB model plus transient
	// attention tensors on a 4-8 GB machine risks memory pressure.
	if totalRAM := getTotalSystemRAM(); totalRAM > 0 && totalRAM < 8*1024*1024*1024 {
		return false
	}

	// Respect explicit disable via env var (checked in createProvider, but
	// also checked here for callers that call mlxProviderAvailable directly).
	if os.Getenv("SPROUT_EMBEDDING_BACKEND") == "cpu" {
		return false
	}

	_ = runtime.GOARCH // ensure runtime import is used
	return true
}

// getTotalSystemRAM returns total system RAM in bytes, or 0 if unknown.
func getTotalSystemRAM() int64 {
	memStr, err := syscall.Sysctl("hw.memsize")
	if err != nil || memStr == "" {
		return 0
	}
	var mem int64
	for _, c := range memStr {
		if c >= '0' && c <= '9' {
			mem = mem*10 + int64(c-'0')
		}
	}
	return mem
}
