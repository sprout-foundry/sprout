//go:build darwin && arm64 && cgo

package mlx

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lmlx -lmlxc

#include <mlx/c/memory.h>
#include <sys/types.h>
#include <sys/sysctl.h>

const char sprout_memsize_name[] = "hw.memsize";
*/
import "C"

import "unsafe"

// MemoryStats summarizes MLX's allocator state.
type MemoryStats struct {
	Active     uint64 // bytes currently held by live arrays
	Cache      uint64 // bytes pooled by the allocator for reuse
	Peak       uint64 // peak active bytes since load (or ResetPeakMemory)
	Limit      uint64 // active-memory soft limit (0 = unlimited)
	CacheLimit uint64 // pooled-buffer cache limit (0 = unlimited)
}

// ActiveMemory returns bytes currently held by live arrays.
func ActiveMemory() (uint64, error) {
	var res C.size_t
	if err := checkRC(C.mlx_get_active_memory(&res), "mlx_get_active_memory"); err != nil {
		return 0, err
	}
	return uint64(res), nil
}

// CacheMemory returns bytes pooled by the allocator for reuse.
func CacheMemory() (uint64, error) {
	var res C.size_t
	if err := checkRC(C.mlx_get_cache_memory(&res), "mlx_get_cache_memory"); err != nil {
		return 0, err
	}
	return uint64(res), nil
}

// PeakMemory returns peak active bytes since load (or ResetPeakMemory).
func PeakMemory() (uint64, error) {
	var res C.size_t
	if err := checkRC(C.mlx_get_peak_memory(&res), "mlx_get_peak_memory"); err != nil {
		return 0, err
	}
	return uint64(res), nil
}

// MemoryLimit returns the active-memory soft limit (0 = unlimited).
func MemoryLimit() (uint64, error) {
	var res C.size_t
	if err := checkRC(C.mlx_get_memory_limit(&res), "mlx_get_memory_limit"); err != nil {
		return 0, err
	}
	return uint64(res), nil
}

// SetMemoryLimit sets the active-memory soft limit (0 = unlimited).
func SetMemoryLimit(limit uint64) error {
	var res C.size_t
	return checkRC(C.mlx_set_memory_limit(&res, C.size_t(limit)), "mlx_set_memory_limit")
}

// SetCacheLimit bounds the allocator's pooled-buffer cache (0 = unlimited).
// Freed buffers beyond this cap are returned to the OS instead of pooled.
// MLX pools all freed buffers by default, which makes long-running
// generation appear to leak RSS even though the Go heap stays flat.
func SetCacheLimit(limit uint64) error {
	lastCacheLimit = limit
	var res C.size_t
	return checkRC(C.mlx_set_cache_limit(&res, C.size_t(limit)), "mlx_set_cache_limit")
}

// ClearCache releases all pooled buffers back to the OS.
func ClearCache() error {
	return checkRC(C.mlx_clear_cache(), "mlx_clear_cache")
}

// ResetPeakMemory resets the peak-memory watermark.
func ResetPeakMemory() error {
	return checkRC(C.mlx_reset_peak_memory(), "mlx_reset_peak_memory")
}

// Snapshot returns current allocator stats.
func Snapshot() (MemoryStats, error) {
	var s MemoryStats
	var err error
	if s.Active, err = ActiveMemory(); err != nil {
		return s, err
	}
	if s.Cache, err = CacheMemory(); err != nil {
		return s, err
	}
	if s.Peak, err = PeakMemory(); err != nil {
		return s, err
	}
	if s.Limit, err = MemoryLimit(); err != nil {
		return s, err
	}
	// The C API has no cache-limit getter; report what we last set.
	s.CacheLimit = lastCacheLimit
	return s, nil
}

var lastCacheLimit uint64

// TotalSystemRAM returns the machine's physical RAM in bytes via sysctl
// (hw.memsize), or 0 if the sysctl call fails. This is the SP-134 RAM-gate
// source of truth for deciding whether a model can fit in unified memory.
func TotalSystemRAM() uint64 {
	var mem uint64
	sz := C.size_t(unsafe.Sizeof(mem))
	if rc := C.sysctlbyname(&C.sprout_memsize_name[0], unsafe.Pointer(&mem), &sz, nil, 0); rc != 0 {
		return 0
	}
	return mem
}
