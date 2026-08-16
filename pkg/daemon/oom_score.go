//go:build !js

package daemon

import (
	"fmt"
	"strconv"
	"strings"
)

// preferredOOMVictimDelta marks a background helper as a preferred OOM
// victim: high enough to outrank ordinary user-facing processes (adj 0),
// low enough to stay below throwaway container processes pinned at 1000.
const preferredOOMVictimDelta = 200

// readOOMScoreAdj and writeOOMScoreAdj are test seams over the process's
// oom_score_adj value, installed by platform files (linux reads/writes
// /proc/self/oom_score_adj; other platforms no-op). Tests override them to
// exercise the clamp math without touching /proc.
var (
	readOOMScoreAdj  func() (string, error)
	writeOOMScoreAdj func(string) error
)

// PreferOOMVictim raises this process's oom_score_adj so the kernel is more
// likely to choose it as an OOM victim over user-facing processes, which
// stay at the default 0. A long-lived background helper holding a large
// working set should sacrifice itself before the kernel takes a
// user-facing process. No-op on platforms without oom_score_adj; the error
// is non-fatal and callers should log and continue.
func PreferOOMVictim() error {
	return raiseOOMScoreAdj(preferredOOMVictimDelta)
}

// raiseOOMScoreAdj adds delta to the current oom_score_adj, clamped to the
// kernel's valid range [-1000, 1000]: values outside it would be rejected
// with EINVAL (or, below -1000, would invert the desired priority).
func raiseOOMScoreAdj(delta int) error {
	currentStr, err := readOOMScoreAdj()
	if err != nil {
		return fmt.Errorf("read oom_score_adj: %w", err)
	}
	current, err := strconv.Atoi(strings.TrimSpace(currentStr))
	if err != nil {
		return fmt.Errorf("parse oom_score_adj %q: %w", currentStr, err)
	}
	next := current + delta
	if next < -1000 {
		next = -1000
	}
	if next > 1000 {
		next = 1000
	}
	if err := writeOOMScoreAdj(strconv.Itoa(next)); err != nil {
		return fmt.Errorf("write oom_score_adj: %w", err)
	}
	return nil
}
