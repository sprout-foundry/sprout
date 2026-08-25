package agent

// Memory gate: prevents OOM kills when subagents run memory-intensive shell commands.
// Three-tier policy: ≥16 GB allow, 8–16 GB sleep+retry, <8 GB refuse. Fails open on read failure.

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// DefaultMemoryGate returns a MemoryGate with production defaults.
func DefaultMemoryGate() *MemoryGate {
	return &MemoryGate{}
}

// MemoryGateError is returned when available memory is below the threshold.
type MemoryGateError struct {
	AvailableBytes int64
	RequiredBytes  int64
	Retried        bool
}

func (e *MemoryGateError) Error() string {
	gb := float64(e.AvailableBytes) / 1024 / 1024 / 1024
	reqGB := float64(e.RequiredBytes) / 1024 / 1024 / 1024
	if e.Retried {
		return fmt.Sprintf("memory gate: insufficient memory after retries (%.1f GB available, %.1f GB required)", gb, reqGB)
	}
	return fmt.Sprintf("memory gate: insufficient memory (%.1f GB available, %.1f GB required)", gb, reqGB)
}

// MemoryGate checks available system memory before allowing memory-intensive operations.
type MemoryGate struct {
	// MinMemoryBytes is the hard minimum; below this the gate refuses immediately. Default: 8 GB.
	MinMemoryBytes int64
	// RetryMinBytes is the retry threshold; between MinMemoryBytes and this value the gate sleeps and retries. Default: 16 GB.
	RetryMinBytes int64
	// RetrySleep is the duration to sleep between retries. Default: 30s.
	RetrySleep time.Duration
	// MaxRetries is the maximum number of retry attempts. Default: 5.
	MaxRetries int
	// readMem is an optional override for testing. When nil, uses the platform-specific reader.
	readMem func() (int64, error)
}

// Default thresholds.
const (
	DefaultMinMemoryBytes = 8 * 1024 * 1024 * 1024  // 8 GB
	DefaultRetryMinBytes  = 16 * 1024 * 1024 * 1024 // 16 GB
	DefaultRetrySleep     = 30 * time.Second
	DefaultMaxRetries     = 5
)

// Check verifies that sufficient memory is available. Returns nil when sufficient or check fails (fail-open).
// Returns *MemoryGateError when memory is below the threshold.
func (g *MemoryGate) Check() error {
	// Apply defaults for zero values.
	minMem := g.MinMemoryBytes
	if minMem == 0 {
		minMem = DefaultMinMemoryBytes
	}
	retryMin := g.RetryMinBytes
	if retryMin == 0 {
		retryMin = DefaultRetryMinBytes
	}
	sleep := g.RetrySleep
	if sleep == 0 {
		sleep = DefaultRetrySleep
	}
	maxRetries := g.MaxRetries
	if maxRetries == 0 {
		maxRetries = DefaultMaxRetries
	}

	readMem := g.readMem
	if readMem == nil {
		readMem = readMemAvailable
	}

	// First check.
	avail, err := readMem()
	if err != nil {
		// Fail open: if we can't read memory info, allow execution.
		return nil
	}

	// Sufficient memory — fast path.
	if avail >= retryMin {
		return nil
	}

	// Below hard minimum — refuse immediately.
	if avail < minMem {
		return &MemoryGateError{
			AvailableBytes: avail,
			RequiredBytes:  minMem,
			Retried:        false,
		}
	}

	// Between min and retry threshold — sleep and retry.
	for i := 0; i < maxRetries; i++ {
		time.Sleep(sleep)
		avail, err = readMem()
		if err != nil {
			return nil // fail open
		}
		if avail >= retryMin {
			return nil
		}
		if avail < minMem {
			return &MemoryGateError{
				AvailableBytes: avail,
				RequiredBytes:  minMem,
				Retried:        true,
			}
		}
	}

	// Exhausted retries — still below retry threshold.
	return &MemoryGateError{
		AvailableBytes: avail,
		RequiredBytes:  retryMin,
		Retried:        true,
	}
}

// ---------------------------------------------------------------------------
// Platform-specific memory readers
// ---------------------------------------------------------------------------

// readMemAvailable dispatches to the platform-specific reader.
func readMemAvailable() (int64, error) {
	switch runtime.GOOS {
	case "linux", "android":
		// Android's kernel exposes the same /proc/meminfo as desktop Linux;
		// MemAvailable is the right metric on both.
		return readMemLinux()
	case "darwin":
		return readMemDarwin()
	default:
		return 0, agenterrors.NewAgent("memory_gate", fmt.Sprintf("memory gate: unsupported OS %s", runtime.GOOS), nil)
	}
}

// readMemLinux reads MemAvailable from /proc/meminfo (in kB) and converts to bytes.
func readMemLinux() (int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, agenterrors.NewTransientError("read /proc/meminfo", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		// Lines look like: "MemAvailable:  12345678 kB"
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0, agenterrors.NewInvalidInputError(fmt.Sprintf("parse MemAvailable value %q", fields[1]), err)
			}
			return kb * 1024, nil
		}
	}
	return 0, agenterrors.NewAgent("memory_gate", "MemAvailable not found in /proc/meminfo", nil)
}

// readMemDarwin estimates available memory on macOS using vm_stat (free + speculative + purgeable pages).
func readMemDarwin() (int64, error) {
	freePages, err := getVMStatField("Pages free")
	if err != nil {
		return 0, agenterrors.NewTransientError("vm_stat Pages free", err)
	}
	// Speculative pages are clean cached pages that macOS eagerly reclaims.
	// Purgable pages are app-allocated memory registered for purging.
	speculative, _ := getVMStatField("Pages speculative")
	purgable, _ := getVMStatField("Pages purgeable")

	pageSize, err := getPageSize()
	if err != nil {
		// Fallback to Apple Silicon default page size.
		pageSize = 16384
	}
	return (freePages + speculative + purgable) * pageSize, nil
}

// getVMStatField parses an arbitrary named field from `vm_stat` output.
func getVMStatField(fieldName string) (int64, error) {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, agenterrors.NewTransientError("run vm_stat", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		// Remove trailing period (vm_stat ends each line with ".")
		line = strings.TrimSuffix(line, ".")
		if strings.HasPrefix(line, fieldName+":") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return 0, agenterrors.NewInvalidInputError(fmt.Sprintf("unexpected vm_stat line format: %q", line), nil)
			}
			val, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
			if err != nil {
				return 0, agenterrors.NewInvalidInputError(fmt.Sprintf("parse %s value %q", fieldName, parts[len(parts)-1]), err)
			}
			return val, nil
		}
	}
	return 0, agenterrors.NewAgent("memory_gate", fmt.Sprintf("%s not found in vm_stat output", fieldName), nil)
}

// getPageSize reads the system page size via `sysctl hw.pagesize`.
func getPageSize() (int64, error) {
	out, err := exec.Command("sysctl", "-n", "hw.pagesize").Output()
	if err != nil {
		return 0, agenterrors.NewTransientError("run sysctl hw.pagesize", err)
	}
	val, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, agenterrors.NewInvalidInputError(fmt.Sprintf("parse page size %q", strings.TrimSpace(string(out))), err)
	}
	return val, nil
}

// IsMemoryIntensiveCommand returns true when a shell command is likely to
// spawn multiple processes or workers that consume significant memory.
// Test runners, bundlers, and compilers are the primary targets.
func IsMemoryIntensiveCommand(cmd string) bool {
	cmdLower := strings.ToLower(cmd)
	keywords := []string{
		"vitest", "jest", "mocha", "ava",
		"npm test", "npm run test", "yarn test", "pnpm test",
		"npx vitest", "npx jest", "npx playwright",
		"webpack", "vite", "esbuild", "rollup",
		"tsc", "go build", "go test", "cargo build", "cargo test",
		"next build", "nuxt build",
		"playwright test", "cypress",
		"make build", "make test",
	}
	for _, kw := range keywords {
		if strings.Contains(cmdLower, kw) {
			return true
		}
	}
	return false
}
