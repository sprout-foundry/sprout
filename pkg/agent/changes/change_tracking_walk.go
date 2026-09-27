// The ChangeTracker workspace walk (split from change_tracking_snapshot.go).
// walkWorkspace is the core cache-aware walker: it reads the tree (cold prime
// or stat fast-path), computes create/edit/delete changes against the old
// snapshot, applies the adaptive auto-skip learning, and surfaces deletions.
// The config knobs, types, and small helpers live in change_tracking_snapshot.go.
package changes

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// walkWorkspace walks workDir, returning (newSnapshot, changes).
//
//   - If `old` is nil, every file is read fresh (cold prime). No
//     changes are computed — the caller treats the returned snapshot
//     as a baseline.
//   - If `old` is non-nil, we take the stat fast path: for every file
//     whose (size, mtime) match the cached entry, we reuse the cached
//     content without re-reading. Only files with a stat mismatch get
//     a fresh content read. Changes (create / edit / delete) are
//     computed against `old` and returned in the changes slice; the
//     caller is responsible for appending them to ct.changes (so it
//     can apply dedup against direct-hook entries first).
//
// Walking a 5000-file tree with no changes takes ~5-20 ms (stat-only
// fast path) instead of ~280 ms (full content read). Cold prime cost
// is paid once per agent session.
// walkWorkspace's `destructive` parameter switches the walker into the
// safer (slower) mode used for `git checkout .` and friends — see
// shell_destructive.go for the classifier. In destructive mode:
//
//   - The adaptive autoSkipDirs map is IGNORED for dir pruning, so dirs
//     that previously crossed the count threshold are still walked. A
//     destructive op might be reverting active edits inside one of them.
//   - We do NOT add to autoSkipDirs or persist it: nothing learned from a
//     destructive walk's fan-out should bias future non-destructive walks.
//   - Static shellSnapshotSkipDirs is still honored (`.git`, `node_modules`,
//     `dist`, etc. — universally inappropriate to track).
func (ct *ChangeTracker) walkWorkspace(workDir string, old map[string]*shellSnapshotEntry, destructive bool) (map[string]*shellSnapshotEntry, []pendingShellChange, bool) {
	if ct == nil || !ct.IsEnabled() || workDir == "" {
		return nil, nil, false
	}

	absRoot, err := filepath.Abs(workDir)
	if err != nil {
		ct.logf("shell snapshot: resolve %q: %v", workDir, err)
		return nil, nil, false
	}

	snap := make(map[string]*shellSnapshotEntry, len(old))
	var changes []pendingShellChange
	var totalBytes int64
	var truncated bool

	// Apply per-tracker budget overrides if set; otherwise fall back
	// to the package defaults. Zero on the tracker means "no override".
	maxFiles := shellSnapshotMaxFiles
	if ct.shellMaxFiles > 0 {
		maxFiles = ct.shellMaxFiles
	}
	maxTotalBytes := int64(shellSnapshotMaxTotalBytes)
	if ct.shellMaxTotalBytes > 0 {
		maxTotalBytes = int64(ct.shellMaxTotalBytes)
	}
	maxDuration := shellSnapshotMaxDuration
	if ct.shellMaxDuration > 0 {
		maxDuration = ct.shellMaxDuration
	}
	autoSkipThreshold := autoSkipFileCountThreshold
	if ct.shellAutoSkipFileCountThreshold > 0 {
		autoSkipThreshold = ct.shellAutoSkipFileCountThreshold
	}
	deadline := time.Now().Add(maxDuration)

	// Per-directory immediate-child file counts. Used to populate
	// autoSkipDirs after the walk: any directory whose direct file
	// count exceeds the threshold gets added so subsequent walks skip
	// it entirely. Counts only direct children, not recursive — a
	// monorepo's top-level pkg/ dir with thousands of *descendant*
	// files but few direct ones should never be auto-skipped.
	filesPerDir := make(map[string]int)

	walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		// Budget check at every entry — both file count and wall clock
		// matter. SkipAll stops the walk cleanly with whatever we've
		// collected so far; the caller logs the partial-coverage
		// warning below.
		if len(snap) >= maxFiles || time.Now().After(deadline) {
			truncated = true
			return filepath.SkipAll
		}

		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if path != absRoot {
				if shellSnapshotSkipDirs[d.Name()] {
					return fs.SkipDir
				}
				// Adaptive skip: consult the per-tracker learned set.
				// Pays attention to the absolute path so we skip the
				// exact dir we previously identified as fat — not all
				// dirs that happen to share its name.
				//
				// In destructive mode (e.g. `git checkout .`) we walk
				// these dirs anyway — the whole point of the override
				// is to capture before-content even for dirs we'd
				// normally skip, so reverts there stay recoverable.
				if !destructive && ct.autoSkipDirs != nil && ct.autoSkipDirs[path] {
					return fs.SkipDir
				}
			}
			return nil
		}

		// Skip symlinks (could point outside workspace and leak content).
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}

		// Stat fast path: cache hit + matching (size, mtime) → reuse
		// without re-reading content. This is the optimization that
		// turns a 280 ms full read into a 5–20 ms stat-only walk on
		// the common no-change case.
		if cached, ok := old[path]; ok && cached.Size == info.Size() && cached.ModTime.Equal(info.ModTime()) {
			snap[path] = cached
			// NOTE: cached content is intentionally NOT counted toward
			// totalBytes. The budget guards *new* allocations on this
			// walk; cached content was already read (and counted) during
			// a prior walk and remains in memory regardless. Counting it
			// again would double-count and could exhaust the budget
			// purely from unchanged files, starving fresh reads of files
			// that actually changed (causing the change to be missed).
			return nil
		}

		// Stat changed (or no cache for this path) — read fresh.
		entry := &shellSnapshotEntry{
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}

		switch {
		case info.Size() > shellSnapshotMaxFileBytes:
			entry.Skipped = "too large"
		case totalBytes+info.Size() > maxTotalBytes:
			entry.Skipped = "snapshot budget exceeded"
		default:
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				entry.Skipped = "read failed"
			} else if isLikelyBinary(data) {
				entry.Skipped = "binary"
			} else {
				entry.Content = data
				totalBytes += info.Size()
			}
		}
		snap[path] = entry
		filesPerDir[filepath.Dir(path)]++

		// Compute the change (if any) against the old cache.
		if old != nil {
			prev := old[path]
			if prev == nil {
				changes = append(changes, pendingShellChange{Path: path, Op: "create", Before: nil, After: entry})
			} else if !shellContentsEqual(prev, entry) {
				changes = append(changes, pendingShellChange{Path: path, Op: "edit", Before: prev, After: entry})
			}
		}
		return nil
	})

	if walkErr != nil {
		ct.logf("shell snapshot: walk %q: %v", absRoot, walkErr)
	}

	if truncated {
		// Surface the partial-coverage state once per walk, loudly
		// enough to be visible in logs but not as an error (the
		// fallback behavior is graceful — coverage is partial, not
		// broken). Common trigger: agent rooted in a huge directory
		// like ~ where the workspace contains millions of files.
		ct.logf("shell snapshot: walk truncated at %d files / %v (workspace too large for full coverage — run sprout in a smaller subdirectory or expand shellSnapshotSkipDirs)",
			len(snap), shellSnapshotMaxDuration)
	}

	// Adaptive learning: any directory whose immediate child file count
	// exceeded the threshold gets added to autoSkipDirs so the NEXT
	// walk avoids it entirely. The cost of a fat directory is paid at
	// most once per session — first walk pays it (bounded by budget),
	// then we learn and never visit it again. Persistence across
	// sessions is left as a future improvement; today the set rebuilds
	// each EnableChangeTracking.
	//
	// Skipped entirely in destructive mode — a `git checkout .` that
	// fans out across a previously-untracked dir shouldn't teach the
	// walker to skip that dir on future (non-destructive) walks. The
	// fan-out is an artifact of the destructive op, not the workspace.
	//
	// Critical follow-up: drop snap entries under any newly-learned
	// auto-skip dir AND suppress deletion records for those paths.
	// Without this cleanup, the very next walk would see all the
	// fat-dir files as "missing from snap" and record 12,000 false
	// deletes — which would then ALSO claim "the agent deleted all
	// these files" in the manifest. Worse than not tracking at all.
	newlyLearned := false
	if !destructive && len(filesPerDir) > 0 {
		if ct.autoSkipDirs == nil {
			ct.autoSkipDirs = make(map[string]bool)
		}

		// Build cumulative descendant file counts by bubbling leaf
		// counts upward to all ancestors. Directories whose total
		// descendant count exceeds the threshold are bloat — even if
		// no single subdirectory individually tripped it (e.g. Swift
		// SPM's .build/index-build/.../records/ with 9000 files
		// spread across hundreds of shallow subdirs).
		cumulative := make(map[string]int, len(filesPerDir))
		for dir, count := range filesPerDir {
			// Add this dir's direct files to itself and all ancestors.
			for d := dir; d != "" && d != absRoot; {
				cumulative[d] += count
				parent := filepath.Dir(d)
				if parent == d {
					break // reached filesystem root
				}
				d = parent
			}
		}

		// Collect dirs that exceed either the direct-children threshold
		// OR the cumulative-descendant threshold, then keep only the
		// shallowest per subtree so we skip .build/ instead of each of
		// its leaf subdirs individually.
		//
		// Two thresholds:
		//   - autoSkipThreshold (1500): high-confidence direct-children
		//     check. No legitimate source dir has 1500 direct file children.
		//   - autoSkipCumulativeThreshold (10000): catches distributed
		//     bloat (files spread across many shallow subdirs). Set far
		//     above any honest source tree (monorepo src/ might hit 3000,
		//     build artifacts start at 3000-5000 and hit 30000+).
		fatDirs := make(map[string]int) // dir → file count (direct or cumulative)
		for dir, count := range filesPerDir {
			if count > autoSkipThreshold {
				fatDirs[dir] = count
			}
		}
		for dir, count := range cumulative {
			if count > autoSkipCumulativeThreshold {
				// Keep the larger label (direct vs cumulative) for logging
				if _, exists := fatDirs[dir]; !exists {
					fatDirs[dir] = count
				}
			}
		}
		for dir, count := range fatDirs {
			// If any ancestor is also fat, skip this one — the ancestor
			// subsumes it and produces a broader skip.
			subsumed := false
			for other := range fatDirs {
				if other != dir && strings.HasPrefix(dir, other+string(filepath.Separator)) {
					subsumed = true
					break
				}
			}
			if subsumed {
				continue
			}
			if !ct.autoSkipDirs[dir] {
				ct.logf("shell snapshot: auto-skipping fat dir %q (%d descendant files) on future walks", dir, count)
				newlyLearned = true
			}
			ct.autoSkipDirs[dir] = true
		}
	}
	if newlyLearned {
		// Drop everything under any auto-skipped dir from the snap so
		// the rebased cache matches what subsequent skip-respecting
		// walks will see.
		for path := range snap {
			if isUnderAnyAutoSkipDir(path, ct.autoSkipDirs) {
				delete(snap, path)
			}
		}
		// Persist so the next agent session in the same workspace
		// starts pre-loaded — no need to re-learn. Best-effort: any
		// I/O failure just means we re-learn next time, which is the
		// pre-persistence behavior anyway.
		if err := saveAutoSkipDirsFor(absRoot, ct.autoSkipDirs); err != nil {
			ct.logf("shell snapshot: persist auto-skip dirs failed (will re-learn next session): %v", err)
		}
	}

	// Deletions: paths in `old` but not in the new snapshot.
	//
	// CAVEAT: when the walk was truncated, "not in the new snapshot"
	// can mean "we ran out of budget before reaching it" — not a real
	// delete. Same applies to paths under newly-auto-skipped dirs.
	// Suppress delete records in both cases to avoid false-positive
	// manifest entries for files we just didn't get to.
	if old != nil && !truncated {
		for path, prev := range old {
			if _, stillThere := snap[path]; stillThere {
				continue
			}
			if isUnderAnyAutoSkipDir(path, ct.autoSkipDirs) {
				continue
			}
			changes = append(changes, pendingShellChange{Path: path, Op: "delete", Before: prev, After: nil})
		}
	}

	return snap, changes, truncated
}
