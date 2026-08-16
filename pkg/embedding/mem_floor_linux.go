//go:build linux && !js

package embedding

import (
	"os"
	"strconv"
	"strings"
)

func init() {
	memAvailableFn = linuxMemAvailable
}

// linuxMemAvailable reads MemAvailable from /proc/meminfo, converting kB to bytes.
func linuxMemAvailable() (uint64, bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	return parseMemAvailable(string(data))
}

// parseMemAvailable extracts MemAvailable from /proc/meminfo content, converting
// kB to bytes. Returns (0, false) when the line is missing or malformed.
func parseMemAvailable(content string) (uint64, bool) {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}
