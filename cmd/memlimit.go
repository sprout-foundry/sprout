//go:build !js

package cmd

import (
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// init sets a Go runtime soft memory limit derived from /proc/meminfo on the
// first sprout invocation. Without this, Go's default GC (GOGC=100, no
// GOMEMLIMIT) lets the heap grow to 2× live size before collecting — which on
// memory-constrained platforms like Android/Termux (~3 GB available, often
// less right after a full `make build-all`) triggers the kernel Low Memory
// Killer and kills the entire Termux process along with every session.
//
// Setting a soft limit makes the GC aware of the physical ceiling: it runs
// more aggressively as the heap approaches the limit, preventing the RSS
// spike that crosses the LMK threshold. The limit is derived from MemAvailable
// so it adapts to whatever headroom exists at launch. After the first run,
// the build process's memory has been fully reclaimed and there is ample
// headroom — which is why the second run always succeeds.
//
// SPROUT_MEMLIMIT overrides the derived value (bytes). SPROUT_MEMLIMIT=0
// disables the limit entirely (restoring Go's default unlimited behavior).
func init() {
	setSoftMemoryLimit()
}

const (
	// memLimitFloor is the minimum soft limit we'll set, regardless of how
	// little memory the system reports. Go needs enough headroom for its
	// runtime (goroutine stacks, GC metadata) before the app's own heap;
	// setting it lower than this causes constant GC thrashing with no
	// benefit.
	memLimitFloor = 512 * 1024 * 1024 // 512 MB
	// memAvailableFloorReserve is subtracted from MemAvailable before
	// choosing the limit, leaving headroom for the OS and other processes
	// so we don't grab every last byte.
	memAvailableFloorReserve = 384 * 1024 * 1024 // 384 MB
)

// setSoftMemoryLimit reads available system memory and applies a Go soft
// memory limit. It is safe to call on any platform — on non-Linux (or when
// /proc/meminfo is unreadable) it silently does nothing, preserving the
// default unlimited behavior on macOS, Windows, and dev machines.
func setSoftMemoryLimit() {
	if v := os.Getenv("SPROUT_MEMLIMIT"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			if n > 0 {
				debug.SetMemoryLimit(n)
			} else {
				// SPROUT_MEMLIMIT=0 (or negative) explicitly disables
				// the limit. Set to MaxInt64 so the GC treats it as
				// unlimited (init() may have set a derived limit earlier).
				debug.SetMemoryLimit(math.MaxInt64)
			}
			return
		}
	}

	avail, ok := readMemAvailableProc()
	if !ok || avail <= memAvailableFloorReserve {
		return
	}

	limit := avail - memAvailableFloorReserve
	if limit < memLimitFloor {
		limit = memLimitFloor
	}

	debug.SetMemoryLimit(limit)
}

// readMemAvailableProc reads MemAvailable from /proc/meminfo. Returns
// (bytes, true) on success, (0, false) if the file is missing or unparseable.
func readMemAvailableProc() (int64, bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil || kb <= 0 {
				return 0, false
			}
			return kb * 1024, true
		}
	}
	return 0, false
}
