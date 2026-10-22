//go:build !js

package cmd

import (
	"log"
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
// An existing GOMEMLIMIT (set by the runtime before init) is respected, not
// replaced: an operator who bounded the process through the environment or a
// systemd unit meant it. SPROUT_MEMLIMIT overrides both (bytes);
// SPROUT_MEMLIMIT=0 disables the limit entirely, restoring Go's default
// unlimited behavior.
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
	limit, source := resolveSoftMemoryLimit()
	if limit > 0 {
		debug.SetMemoryLimit(limit)
		log.Printf("memory: soft limit %s (from %s)", formatByteSize(limit), source)
		return
	}
	// No limit to apply. Set MaxInt64 explicitly so a limit derived by an
	// earlier init (or a GOMEMLIMIT the caller asked to disable) is cleared
	// rather than left in place.
	debug.SetMemoryLimit(math.MaxInt64)
}

// formatByteSize renders a byte count for the startup log line.
func formatByteSize(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return strconv.FormatFloat(float64(n)/gb, 'f', 1, 64) + " GiB"
	case n >= mb:
		return strconv.FormatFloat(float64(n)/mb, 'f', 1, 64) + " MiB"
	case n >= kb:
		return strconv.FormatFloat(float64(n)/kb, 'f', 1, 64) + " KiB"
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
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

// currentSoftMemoryLimit reports the soft memory limit the runtime currently
// has, or 0 when no limit is set. SetMemoryLimit(-1) returns the current
// value without changing it; an unset limit reports math.MaxInt64.
func currentSoftMemoryLimit() int64 {
	if limit := debug.SetMemoryLimit(-1); limit != math.MaxInt64 {
		return limit
	}
	return 0
}

// resolveSoftMemoryLimit decides the soft memory limit to apply and returns
// it together with the source of the decision. It returns 0 when no limit
// should be set.
//
// Precedence matters: the runtime has already applied GOMEMLIMIT before this
// runs, so an operator's explicit GOMEMLIMIT (a systemd unit, a container
// env, `systemd-run -p Environment=GOMEMLIMIT=2GiB`) must not be silently
// replaced by the derived value. SPROUT_MEMLIMIT stays the sprout-specific
// override and wins over both.
func resolveSoftMemoryLimit() (limit int64, source string) {
	if v := os.Getenv("SPROUT_MEMLIMIT"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			if n > 0 {
				return n, "SPROUT_MEMLIMIT"
			}
			// SPROUT_MEMLIMIT=0 (or negative) explicitly disables the
			// limit, including a GOMEMLIMIT the runtime already applied.
			return 0, "SPROUT_MEMLIMIT (disabled)"
		}
	}

	if current := currentSoftMemoryLimit(); current > 0 {
		return current, "GOMEMLIMIT"
	}

	avail, ok := readMemAvailableProc()
	if !ok || avail <= memAvailableFloorReserve {
		return 0, ""
	}

	limit = avail - memAvailableFloorReserve
	if limit < memLimitFloor {
		limit = memLimitFloor
	}
	return limit, "/proc/meminfo"
}
