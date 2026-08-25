//go:build !js

package cmd

import (
	"math"
	"runtime/debug"
	"testing"
)

func TestReadMemAvailableProc(t *testing.T) {
	// On any Linux/Android host /proc/meminfo exists and contains
	// MemAvailable. We don't assert exact values, just that the reader
	// returns a positive number.
	bytes, ok := readMemAvailableProc()
	if ok {
		if bytes <= 0 {
			t.Fatalf("readMemAvailableProc returned ok=true but bytes=%d", bytes)
		}
	}
	// If ok is false (non-Linux CI), the test still passes — the function
	// is designed to return false gracefully.
}

func TestSetSoftMemoryLimit_FromEnv(t *testing.T) {
	// Save and restore the limit + env.
	original := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(original) })

	t.Setenv("SPROUT_MEMLIMIT", "1073741824") // 1 GiB
	setSoftMemoryLimit()

	// debug.SetMemoryLimit(-1) returns the current limit without changing it.
	current := debug.SetMemoryLimit(-1)
	if current != int64(1073741824) {
		t.Errorf("expected limit 1 GiB (1073741824), got %d", current)
	}
}

func TestSetSoftMemoryLimit_DisableViaZero(t *testing.T) {
	original := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(original) })

	t.Setenv("SPROUT_MEMLIMIT", "0")
	setSoftMemoryLimit()

	// A limit of 0 means "no limit" — Go represents this internally as
	// math.MaxInt64 via SetMemoryLimit(math.MaxInt64).
	current := debug.SetMemoryLimit(-1)
	if current != math.MaxInt64 {
		t.Errorf("expected no limit (math.MaxInt64) when SPROUT_MEMLIMIT=0, got %d", current)
	}
}

func TestSetSoftMemoryLimit_NoEnvRespectsFloor(t *testing.T) {
	original := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(original) })

	// Clear the override so the /proc/meminfo path is exercised.
	t.Setenv("SPROUT_MEMLIMIT", "")
	setSoftMemoryLimit()

	current := debug.SetMemoryLimit(-1)
	// On Linux/Android the limit should be set; on other platforms it
	// may remain -1 (no /proc/meminfo). Either is acceptable.
	if current == -1 {
		t.Skip("no /proc/meminfo available — skipping derived-limit check")
	}
	if current < int64(memLimitFloor) {
		t.Errorf("derived limit %d is below floor %d", current, memLimitFloor)
	}
}
