//go:build !js

package automate

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// DefaultMinAvailableBytes is the MemAvailable floor below which automate
// workflows refuse to start. A workflow agent is a full sprout process
// (~400MB+ RSS); launching one on a memory-starved host gets the process
// killed by the OS low-memory killer minutes in — exit -1, no session trail,
// tokens already spent. Refusing up front turns that into an actionable error.
const DefaultMinAvailableBytes int64 = 1536 << 20

// CheckMemoryFloor returns an error when available memory is below the
// launch floor. Floor precedence: $SPROUT_AUTOMATE_MIN_MEM_MB (0 disables
// the check) → DefaultMinAvailableBytes. Platforms without a reader and
// unreadable metrics never block a launch — measurement failure is not a
// reason to refuse.
func CheckMemoryFloor() error {
	if v := strings.TrimSpace(os.Getenv("SPROUT_AUTOMATE_MIN_MEM_MB")); v != "" {
		mb, err := strconv.ParseInt(v, 10, 64)
		if err == nil && mb <= 0 {
			return nil
		}
		if err == nil {
			return checkFloor(mb << 20)
		}
		fmt.Fprintf(os.Stderr, "warn: invalid SPROUT_AUTOMATE_MIN_MEM_MB %q — using default\n", v)
	}
	return checkFloor(DefaultMinAvailableBytes)
}

func checkFloor(floor int64) error {
	avail, err := availableMemory()
	if err != nil {
		return nil
	}
	if avail >= floor {
		return nil
	}
	return fmt.Errorf(
		"available memory %s is below the %.1f GB floor for launching a workflow agent "+
			"(likely to be OOM-killed mid-run); free memory or close other sprout instances, "+
			"or override with SPROUT_AUTOMATE_MIN_MEM_MB=<mb> (0 disables)",
		formatBytes(avail), float64(floor)/(1<<30))
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d bytes", b)
	}
}

func availableMemory() (int64, error) {
	switch runtime.GOOS {
	case "linux", "android":
		return readMemAvailableLinux()
	case "darwin":
		return readMemAvailableDarwin()
	default:
		return 0, fmt.Errorf("no memory reader for %s", runtime.GOOS)
	}
}

func readMemAvailableLinux() (int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return kb * 1024, nil
		}
	}
	return 0, fmt.Errorf("MemAvailable not found in /proc/meminfo")
}

func readMemAvailableDarwin() (int64, error) {
	free, err := vmStatPages("Pages free")
	if err != nil {
		return 0, err
	}
	speculative, _ := vmStatPages("Pages speculative")
	purgable, _ := vmStatPages("Pages purgeable")
	pageSize := int64(16384)
	if out, err := exec.Command("pagesize").Output(); err == nil {
		if n, perr := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); perr == nil && n > 0 {
			pageSize = n
		}
	}
	return (free + speculative + purgable) * pageSize, nil
}

func vmStatPages(field string) (int64, error) {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, field) {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		v := strings.TrimRight(strings.TrimSpace(parts[1]), ".")
		return strconv.ParseInt(v, 10, 64)
	}
	return 0, fmt.Errorf("field %q not found in vm_stat", field)
}
