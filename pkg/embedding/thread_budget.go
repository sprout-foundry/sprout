package embedding

import (
	"os"
	"os/exec"
	goruntime "runtime"
	"strconv"
	"strings"
)

// defaultIntraOpThreads returns the ONNX intra-op thread count for this
// process. The budget is NumCPU-1 (leave one core for foreground work),
// divided across concurrent sprout processes so that N instances on one
// machine share the cores rather than each spawning NumCPU ORT threads
// and oversubscribing the CPU.
//
// Process detection counts sprout binaries by matching the running
// process names against os.Args[0]. A manual override is available via
// SPROUT_ONNX_THREADS; a value of 0 disables the feature (picks 1).
func defaultIntraOpThreads() int {
	if v := strings.TrimSpace(os.Getenv("SPROUT_ONNX_THREADS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			if n == 0 {
				return 1
			}
			return n
		}
	}

	totalCores := goruntime.NumCPU()
	coresForSprout := totalCores - 1 // leave one core for foreground work
	if coresForSprout < 1 {
		coresForSprout = 1
	}

	procs := countSproutProcesses()
	if procs < 1 {
		procs = 1
	}

	threads := coresForSprout / procs
	if threads < 1 {
		threads = 1
	}
	if threads > 8 {
		threads = 8
	}
	return threads
}

// countSproutProcesses returns the number of sprout processes currently
// running on this machine. It reads /proc (Linux), falls back to pgrep
// (macOS/other Unix), and returns 1 on any failure (conservative — assume
// we're the only instance rather than oversubscribing).
func countSproutProcesses() int {
	if n := countViaProc(); n > 0 {
		return n
	}
	if n := countViaPgrep(); n > 0 {
		return n
	}
	return 1
}

// countViaProc counts sprout processes by scanning /proc/*/comm on Linux.
func countViaProc() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	binaryName := commName(os.Args[0])
	if binaryName == "" {
		return 0
	}
	var count int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		comm, err := os.ReadFile("/proc/" + entry.Name() + "/comm")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == binaryName {
			count++
		}
	}
	return count
}

// countViaPgrep counts sprout processes using pgrep as a fallback.
func countViaPgrep() int {
	// pgrep returns one PID per line; count lines.
	binaryName := commName(os.Args[0])
	if binaryName == "" {
		return 0
	}
	out, err := os_exec("pgrep", "-x", binaryName)
	if err != nil {
		// pgrep -x matches exact process name; try fuzzy for longer names
		out, err = os_exec("pgrep", "-f", os.Args[0])
		if err != nil {
			return 0
		}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return 0
	}
	return len(lines)
}

// commName extracts the process name from a binary path (e.g.
// "/home/user/go/bin/sprout" → "sprout"). Linux /proc/*/comm is
// truncated to 15 chars, so match against that.
func commName(path string) string {
	if path == "" {
		return ""
	}
	// Strip directory
	name := path
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	// /proc/*/comm is truncated to 15 characters
	if len(name) > 15 {
		name = name[:15]
	}
	return name
}

// os_exec runs a command and returns its stdout.
func os_exec(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
