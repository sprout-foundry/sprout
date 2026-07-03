//go:build linux && !js

package agent

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"time"
)

// probe scans /proc for Node.js processes and sums their RSS memory usage.
// It reads /proc/<pid>/cmdline to detect node processes and /proc/<pid>/statm
// to extract RSS (resident set size) in bytes.
func (w *OOMWatchdog) probe() (*OOMProbeResult, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	pageSize := os.Getpagesize()
	var nodeCount int
	var totalRSSBytes uint64

	for _, entry := range entries {
		// Only numeric entries are process directories.
		if !entry.IsDir() {
			continue
		}
		_, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Read cmdline to check for "node" process.
		cmdlinePath := "/proc/" + entry.Name() + "/cmdline"
		cmdlineData, err := os.ReadFile(cmdlinePath)
		if err != nil {
			// Process may have exited mid-scan; skip it.
			continue
		}

		// cmdline uses null bytes as separators; check if any argument contains "node".
		// We split on null bytes and check each segment to avoid false positives
		// from paths like /usr/bin/nodejs-wrapper (though that's unlikely).
		if !isNodeProcess(cmdlineData) {
			continue
		}

		nodeCount++

		// Read statm to get RSS.
		// Format: size resident shared text lib data dt
		// We want field [1] (resident) in pages.
		statmPath := "/proc/" + entry.Name() + "/statm"
		statmData, err := os.ReadFile(statmPath)
		if err != nil {
			// Process may have exited; skip RSS for this pid.
			continue
		}

		fields := strings.Fields(string(statmData))
		if len(fields) < 2 {
			continue
		}

		residentPages, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		totalRSSBytes += residentPages * uint64(pageSize)
	}

	return &OOMProbeResult{
		NodeCount:     nodeCount,
		TotalRSSBytes: totalRSSBytes,
		Timestamp:     time.Now(),
	}, nil
}

// isNodeProcess checks if the cmdline data (null-separated arguments)
// indicates a Node.js process. It only checks the first argument
// (the executable path) to avoid false positives from flags or
// other arguments that happen to contain "node".
func isNodeProcess(cmdline []byte) bool {
	args := bytes.SplitN(cmdline, []byte{0}, 2)
	if len(args) == 0 || len(args[0]) == 0 {
		return false
	}
	exe := args[0]
	// Match "node" or any path ending with "/node" (e.g., "/usr/bin/node").
	if bytes.Equal(exe, []byte("node")) {
		return true
	}
	if bytes.HasSuffix(exe, []byte("/node")) {
		return true
	}
	return false
}
