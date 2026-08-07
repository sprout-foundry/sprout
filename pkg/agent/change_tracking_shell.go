// Shell-mutation tracking: captures before/after snapshots around shell_command
// invocations to detect file changes the structured tools miss (sed, mv, rm, etc.).
// Supporting code: change_tracking_snapshot.go, change_tracking_mutations.go,
// change_tracking_autoskip.go, change_tracking_shell_persist.go.
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/git"
)

const (
	// shellSnapshotMaxFileBytes caps the size of a single file's bytes
	// the snapshot will hold. Larger files are recorded as a path-only
	// entry (so deletion / mutation is still reported), but their
	// content isn't preserved for recovery. 1 MiB is generous for
	// source / config / docs and rules out node_modules-style giants.
	shellSnapshotMaxFileBytes = 1 * 1024 * 1024

	// shellSnapshotMaxTotalBytes caps cumulative bytes across all
	// files in one snapshot. Prevents pathological workspaces (e.g.,
	// a directory full of small JSON dumps) from ballooning memory
	// per shell invocation. 32 MiB is plenty of headroom for normal
	// work; over the cap, additional files are recorded as path-only.
	shellSnapshotMaxTotalBytes = 32 * 1024 * 1024

	// shellSnapshotBinarySniffBytes is the number of leading bytes
	// inspected when deciding whether a file is binary. A null byte in
	// the sniff window classifies the file as binary and we skip its
	// content capture (recording the path-only entry).
	shellSnapshotBinarySniffBytes = 8 * 1024
)

// PrimeShellTracking captures the workspace's current state as the
// baseline against which future shell_command invocations are diffed.
// Idempotent: a second call against the already-primed tracker is a
// no-op. Safe to call multiple times — only the first does work.
//
// Lazy callers can skip this and rely on TrackShellTurn to auto-prime
// on first invocation; in that mode the first shell_command's own
// pre-state is captured but no changes are recorded for it (the
// initial walk IS the baseline). When the first shell command's
// mutations need to be tracked, PrimeShellTracking should be called
// from EnableChangeTracking so the baseline pre-exists.
func (ct *ChangeTracker) PrimeShellTracking(workDir string) {
	if ct == nil || !ct.IsEnabled() {
		return
	}
	if !ct.shellWalkEnabled {
		return
	}
	ct.shellCacheMu.Lock()
	defer ct.shellCacheMu.Unlock()
	if ct.shellCache != nil {
		return
	}
	// Seed the auto-skip set from the persisted cross-session learning
	// for this workspace BEFORE walking — that way the first walk
	// already avoids known-fat dirs and pays the budget on fresh
	// content only.
	if absRoot, err := filepath.Abs(workDir); err == nil {
		persisted := loadAutoSkipDirsFor(absRoot)
		if len(persisted) > 0 {
			if ct.autoSkipDirs == nil {
				ct.autoSkipDirs = make(map[string]bool, len(persisted))
			}
			for d := range persisted {
				ct.autoSkipDirs[d] = true
			}
		}
	}
	snap, _, _ := ct.walkWorkspace(workDir, nil, false)
	if snap == nil {
		snap = map[string]*shellSnapshotEntry{}
	}
	ct.shellCache = snap
	if absRoot, err := filepath.Abs(workDir); err == nil {
		ct.shellCacheRoot = absRoot
	}
}

// TrackShellTurn diffs the workspace against the primed baseline,
// records mutations, and rebases the baseline to the new state.
// Auto-primes if the cache hasn't been primed yet (no changes recorded first time).
// `destructive` enables the safer mode that bypasses autoSkipDirs.
func (ct *ChangeTracker) TrackShellTurn(workDir, toolCall string, destructive bool) {
	if ct == nil || !ct.IsEnabled() {
		return
	}
	if !ct.shellWalkEnabled {
		return
	}
	ct.shellCacheMu.Lock()
	defer ct.shellCacheMu.Unlock()

	absWorkDir, absErr := filepath.Abs(workDir)
	if absErr != nil {
		absWorkDir = workDir
	}

	// Re-prime if the workDir changed since the cache was built.
	// Diffing a cache built for one root against a walk of another
	// would classify every file outside the old root as a "create".
	if ct.shellCache == nil || ct.shellCacheRoot != absWorkDir {
		snap, _, _ := ct.walkWorkspace(workDir, nil, false)
		if snap == nil {
			snap = map[string]*shellSnapshotEntry{}
		}
		ct.shellCache = snap
		ct.shellCacheRoot = absWorkDir
		return
	}

	// git stash is uniquely dangerous: the stash pop's 3-way merge can
	// silently revert files to a state the agent never wrote. Re-prime
	// the cache instead of diffing against a stale pre-stash baseline.
	if destructive && isGitStashOperation(toolCall) {
		snap, _, _ := ct.walkWorkspace(workDir, nil, true)
		if snap == nil {
			snap = map[string]*shellSnapshotEntry{}
		}
		ct.shellCache = snap
		ct.shellCacheRoot = absWorkDir
		ct.logf("git stash operation detected (%s), re-primed shell cache (no diff against stale baseline)", toolCall)
		return
	}

	newSnap, pending, truncated := ct.walkWorkspace(workDir, ct.shellCache, destructive)
	if newSnap == nil {
		return
	}

	// Filter out deltas caused by git operations. When git brings committed
	// content into the working tree, those aren't agent-authored edits.
	// For non-destructive commands: suppressed. For destructive: preserved
	// if before-content differed from after (uncommitted agent work).
	pending = ct.filterGitSourcedDeltas(pending, workDir, destructive)

	// Surface truncation as a manifest entry on destructive walks.
	if truncated && destructive {
		ct.appendChange(TrackedFileChange{
			FilePath:  toolCall,
			Operation: "warning",
			NewCode:   "walk truncated during destructive command — coverage is partial. Re-run sprout in a smaller subdirectory or increase the walker budget if recovery completeness matters.",
			Timestamp: time.Now(),
			ToolCall:  toolCall,
		})
	}

	// Destructive commands above the bulk threshold collapse into a single
	// recoverable entry. Below the threshold we keep per-file shape.
	if destructive && len(pending) >= shellDestructiveBulkThreshold {
		ct.appendDestructiveBulkRollup(pending, toolCall)
	} else {
		for _, p := range pending {
			ct.appendShellMutation(p.Path, p.Before, p.After, p.Op, toolCall)
		}
	}

	// Rebase the cache so the NEXT shell command's diff is against the
	// state we just observed (not the original session-start state).
	ct.shellCache = newSnap
	ct.shellCacheRoot = absWorkDir
}

// SyncShellCacheForPath refreshes the shell cache entry for one path
// against its current on-disk state. Called by direct file-write hooks
// so the cache reflects writes the agent just performed.
func (ct *ChangeTracker) SyncShellCacheForPath(path string) {
	if ct == nil || !ct.IsEnabled() {
		return
	}
	ct.shellCacheMu.Lock()
	defer ct.shellCacheMu.Unlock()
	if ct.shellCache == nil {
		return
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}

	info, statErr := os.Stat(abs)
	if statErr != nil {
		// File doesn't exist anymore — drop the cache entry.
		delete(ct.shellCache, abs)
		return
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		delete(ct.shellCache, abs)
		return
	}

	entry := &shellSnapshotEntry{
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	switch {
	case info.Size() > shellSnapshotMaxFileBytes:
		entry.Skipped = "too large"
	default:
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			entry.Skipped = "read failed"
		} else if isLikelyBinary(data) {
			entry.Skipped = "binary"
		} else {
			entry.Content = data
		}
	}
	ct.shellCache[abs] = entry
}

// filterGitSourcedDeltas removes deltas whose post-operation content
// matches git HEAD — i.e. deltas that a git operation produced by
// aligning the working tree to committed content. These aren't
// agent-authored edits; recording them would persist stale bytes.
// Batched via git.CommittedFilePaths for O(1) per-delta lookup.
// Non-repo workspaces skip filtering; path-only entries are kept.
func (ct *ChangeTracker) filterGitSourcedDeltas(pending []pendingShellChange, workDir string, destructive bool) []pendingShellChange {
	if len(pending) == 0 {
		return pending
	}
	committed, err := git.CommittedFilePaths(workDir)
	if err != nil || committed == nil {
		return pending
	}
	kept := pending[:0] // reuse backing array
	for _, p := range pending {
		// A delta where the post-state (After) matches HEAD means git
		// brought the file to a committed state. Deletes have After==nil
		// so they can never match HEAD — keep them (a real deletion by
		// `rm` should stay recoverable). Path-only entries (Skipped) are
		// also kept — no content payload to protect against.
		if p.After != nil && p.After.Content != nil && committed[p.Path] {
			if destructive {
	// For destructive commands, check whether the BEFORE content was different.
				if p.Before != nil && p.Before.Content != nil && !shellContentsEqual(p.Before, p.After) {
					ct.logf("preserving delta for %s (destructive git command changed content from uncommitted state)", p.Path)
					kept = append(kept, p)
					continue
				}
			}
			ct.logf("suppressing git-sourced delta for %s (post-op content matches HEAD)", p.Path)
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

// logf routes a debug-level shell-snapshot message through the agent's
// logger when available, falling back to a stderr write otherwise.
// Keeps the snapshot path silent on success and quietly informative
// on the rare error.
func (ct *ChangeTracker) logf(format string, args ...any) {
	if ct.agent != nil && ct.agent.Logger() != nil {
		ct.agent.Logger().Debug(format+"\n", args...)
		return
	}
	// Avoid pulling in fmt just for a swallowed warning here; if the
	// agent is nil the tracker is in an unusual state (test path) and
	// silent is fine.
	_ = strings.TrimSpace(format)
}

// isGitStashOperation reports whether `command` contains a `git stash`
// invocation (bare stash, push, pop, apply, drop, clear — but NOT
// list/show which are read-only). Used by the ChangeTracker to detect
// when a stash operation has potentially corrupted the working tree
// via merge conflicts, so the cache can be re-primed instead of
// diffed against a stale baseline.
func isGitStashOperation(command string) bool {
	for _, seg := range splitForGitRevertCheck(command) {
		fields := strings.Fields(seg)
		if len(fields) < 2 {
			continue
		}
		gitIdx := -1
		for i, f := range fields {
			if f == "git" {
				gitIdx = i
				break
			}
		}
		if gitIdx == -1 || gitIdx+1 >= len(fields) {
			continue
		}
		subIdx := gitIdx + 1
		for subIdx < len(fields) {
			f := fields[subIdx]
			if strings.HasPrefix(f, "-") {
				if f == "-c" || f == "-C" {
					subIdx += 2
				} else {
					subIdx++
				}
				continue
			}
			break
		}
		if subIdx >= len(fields) {
			continue
		}
		sub := strings.TrimRight(fields[subIdx], ");\"'")
		if sub != "stash" {
			continue
		}
		// Check sub-subcommand: list/show are read-only, everything else
		// (including bare stash) is a stash operation.
		if subIdx+1 < len(fields) {
			subSub := strings.TrimRight(fields[subIdx+1], ");\"'")
			if subSub == "list" || subSub == "show" {
				continue
			}
		}
		return true
	}
	return false
}

// splitForGitRevertCheck splits a command at &&, ||, ;, | boundaries.
// Not quote-aware (same trade-off as splitShellSegments in
// shell_destructive.go — false positive direction is safe here).
func splitForGitRevertCheck(cmd string) []string {
	replaced := cmd
	for _, op := range []string{"&&", "||"} {
		replaced = strings.ReplaceAll(replaced, op, "\x00")
	}
	for _, op := range []string{";", "|"} {
		replaced = strings.ReplaceAll(replaced, op, "\x00")
	}
	parts := strings.Split(replaced, "\x00")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
