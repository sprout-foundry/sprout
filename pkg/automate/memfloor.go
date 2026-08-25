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

// defaultDarwinPageSize is the page size on all current macOS hardware;
// used when neither the vm_stat header nor the pagesize command yield one.
const defaultDarwinPageSize int64 = 16384

func readMemAvailableDarwin() (int64, error) {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, fmt.Errorf("vm_stat: %w", err)
	}
	pageSize := pageSizeFromVMStatHeader(string(out))
	if pageSize <= 0 {
		if pOut, perr := exec.Command("pagesize").Output(); perr == nil {
			if n, nerr := strconv.ParseInt(strings.TrimSpace(string(pOut)), 10, 64); nerr == nil && n > 0 {
				pageSize = n
			}
		}
	}
	if pageSize <= 0 {
		pageSize = defaultDarwinPageSize
	}
	return availableBytesFromVMStat(string(out), pageSize)
}

// availableBytesFromVMStat reports available memory from vm_stat output
// using XNU's freeable-page set (vm_page_freeable(): free + inactive +
// speculative + purgeable). Returns an error when the required "Pages free"
// field is absent or unparseable; other missing fields count as 0.
func availableBytesFromVMStat(out string, pageSize int64) (int64, error) {
	free, err := vmStatFieldPages(out, "Pages free")
	if err != nil {
		return 0, err
	}
	inactive, _ := vmStatFieldPages(out, "Pages inactive")
	speculative, _ := vmStatFieldPages(out, "Pages speculative")
	purgeable, _ := vmStatFieldPages(out, "Pages purgeable")
	return (free + inactive + speculative + purgeable) * pageSize, nil
}

// vmStatFieldPages reads one field's page count; missing or unparseable
// fields are 0, but a missing "Pages free" is an error.
func vmStatFieldPages(out, field string) (int64, error) {
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 || strings.TrimSpace(parts[0]) != field {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimRight(strings.TrimSpace(parts[1]), "."), 10, 64)
		if err != nil || n < 0 {
			if field == "Pages free" {
				return 0, fmt.Errorf("bad Pages free value in vm_stat")
			}
			return 0, nil
		}
		return n, nil
	}
	if field == "Pages free" {
		return 0, fmt.Errorf("Pages free not found in vm_stat")
	}
	return 0, nil
}

// pageSizeFromVMStatHeader extracts the page size from the vm_stat header
// line ("Mach Virtual Memory Statistics: (page size of 16384 bytes)").
// Returns 0 when the header is absent or unparseable.
func pageSizeFromVMStatHeader(out string) int64 {
	for _, line := range strings.Split(out, "\n") {
		const marker = "page size of "
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(marker):])
		var num strings.Builder
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			num.WriteRune(r)
		}
		if n, err := strconv.ParseInt(num.String(), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}
