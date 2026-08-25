//go:build linux && !js

package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// tryReadProcStartTime reads the process creation time for pid from /proc.
// Returns (time, true) on success, (zero, false) if /proc is unavailable
// or the pid does not exist.
func tryReadProcStartTime(pid int) (time.Time, bool) {
	bootTime, err := readProcBootTime()
	if err != nil {
		return time.Time{}, false
	}

	startTicks, err := readProcStartTicks(pid)
	if err != nil {
		return time.Time{}, false
	}

	const userHZ = 100
	return bootTime.Add(time.Duration(startTicks) * time.Second / userHZ), true
}

func readProcBootTime() (time.Time, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "btime ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return time.Time{}, fmt.Errorf("malformed btime line")
		}
		secs, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse btime: %w", err)
		}
		return time.Unix(secs, 0), nil
	}
	return time.Time{}, fmt.Errorf("btime not found in /proc/stat")
}

func readProcStartTicks(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}

	s := string(data)
	parenEnd := strings.LastIndex(s, ")")
	if parenEnd < 0 {
		return 0, fmt.Errorf("malformed stat: no closing paren")
	}

	fields := strings.Fields(s[parenEnd+1:])
	if len(fields) < 20 {
		return 0, fmt.Errorf("malformed stat: only %d fields after comm", len(fields))
	}

	starttime, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse starttime: %w", err)
	}
	return starttime, nil
}
