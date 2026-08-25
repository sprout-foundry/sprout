//go:build linux && !js

package automate

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// processStartedBefore reports whether the process with the given PID
// started before the given cutoff time. Used to guard against PID reuse:
// if the original process died and the OS recycled its PID, the new
// process will have a later start time. Returns true on any read error
// (fail-open) so that legitimate operations are not blocked by a
// transient /proc read failure or unsupported platform.
func processStartedBefore(pid int, cutoff time.Time) bool {
	if pid <= 0 {
		return false
	}

	bootTime, err := readBootTime()
	if err != nil {
		return true
	}

	startTicks, err := readProcessStartTicks(pid)
	if err != nil {
		return true
	}

	// starttime in /proc/<pid>/stat is measured in clock ticks since boot.
	// SC_CLK_TCK is virtually always 100 on Linux. Using a hardcoded value
	// avoids a cgo/sysconf dependency; the worst case is a ~1% timing skew
	// which does not affect the reuse-detection heuristic.
	const userHZ = 100
	procStart := bootTime.Add(time.Duration(startTicks) * time.Second / userHZ)
	return procStart.Before(cutoff)
}

// readBootTime parses /proc/stat for the btime (boot time) line and
// returns it as a wall-clock time.
func readBootTime() (time.Time, error) {
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

// readProcessStartTicks reads field 22 (starttime) from /proc/<pid>/stat,
// returning the process start time in clock ticks since boot.
func readProcessStartTicks(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}

	s := string(data)
	// Field 2 (comm) is enclosed in parentheses and may contain spaces
	// or additional parens. Find the LAST ')' to skip past it safely.
	parenEnd := strings.LastIndex(s, ")")
	if parenEnd < 0 {
		return 0, fmt.Errorf("malformed %s: no closing paren", "/proc/"+strconv.Itoa(pid)+"/stat")
	}

	// Fields after comm are space-separated starting at field 3 (state).
	// starttime is field 22 → index (22 - 3) = 19 in the post-comm slice.
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
