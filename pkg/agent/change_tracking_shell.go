// Shell-mutation tracking for ChangeTracker.
//
// The base ChangeTracker (change_tracking.go) only captures writes the
// agent performs via the structured file tools (write_file, edit_file,
// patch_structured_file, write_structured_file). Plenty of legitimate
// agent actions mutate files outside those tools — `sed -i`, `mv`, `rm`,
// `cp`, `tee`, `awk -i inplace`, build scripts, formatters, etc. — and
// none of them currently appear in the manifest the subagent returns to
// its primary.
//
// This file adds a "before/after" snapshot pass around every
// shell_command invocation:
//
//  1. Before the shell runs, walk the workspace tree and capture file
//     bytes for everything inside size/binary limits, skipping
//     well-known bloat directories (.git, node_modules, dist, …).
//     Works whether or not the workspace is a git repo — no git
//     dependency, no reliance on git's tracked/untracked classification.
//  2. Run the shell command.
//  3. Walk again afterwards. Diff against the "before" map. Each
//     deletion, modification, or creation that isn't already in the
//     tracker becomes a new TrackedFileChange with the captured
//     original content (when available — preserved so a user can
//     recover an accidentally-deleted file from the session buffer,
//     git-tracked or not).
//
// Size + binary filters keep this cheap and safe: 1 MiB ceiling per file
// (so we don't buffer node_modules-style giants), plus a null-byte
// sniff in the first 8 KiB so binaries aren't stored as text. A
// per-snapshot total-bytes budget caps memory.
//
// This file is the primary entry point containing the main orchestration
// methods. Supporting code is split across:
//
//   - change_tracking_snapshot.go — walk, file I/O, binary detection
//   - change_tracking_mutations.go — mutation recording and bulk rollup
//   - change_tracking_autoskip.go — adaptive auto-skip learning
//   - change_tracking_shell_persist.go — cross-session persistence
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"
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
// appends every detected mutation to the change tracker (with dedup
// against direct-hook entries that fired during the same window),
// then rebases the baseline to the new state.
//
// If the cache hasn't been primed yet this call auto-primes — the
// pre-shell state is captured but no changes are recorded the first
// time (we have no baseline to compare against). To track the very
// first shell command's mutations, call PrimeShellTracking once at
// agent session start before the first shell_command runs.
//
// Honors the per-tracker shellWalkEnabled knob — when disabled the
// call is a no-op so users with weird workspaces can keep direct-tool
// tracking without paying the walker's cost.
//
// `destructive` should be set when the shell command can clobber active
// changes (`git checkout .`, `git reset --hard`, …). It flips the walk
// into the safer mode that bypasses autoSkipDirs and emits per-file
// rather than rolling up — see shell_destructive.go for the classifier
// and walkWorkspace for the behaviour switch.
//
// Concurrency: serialized via the tracker's internal mutex. Subagents
// each have their own ChangeTracker so cross-subagent calls don't
// interfere.
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

	if ct.shellCache == nil || ct.shellCacheRoot != absWorkDir {
		// Either first call, or the shell `cd`'d into a different
		// directory since the cache was built. Re-prime silently
		// against the new workDir — diffing a cache built for one
		// root against a walk of another would classify every file
		// outside the old root as a "create" (the 14k-entry runaway
		// session that motivated shellCacheRoot). Priming is always
		// non-destructive even when the triggering command is.
		snap, _, _ := ct.walkWorkspace(workDir, nil, false)
		if snap == nil {
			snap = map[string]*shellSnapshotEntry{}
		}
		ct.shellCache = snap
		ct.shellCacheRoot = absWorkDir
		return
	}

	// `git stash` and `git stash pop` are uniquely dangerous to the shell-
	// walk diff because the stash pop's 3-way merge can silently revert
	// files to a state the agent never wrote. The diff would detect those
	// reverted files as "modified by the shell command" and record them
	// as agent mutations with empty/placeholder .original content.
	//
	// Other destructive git commands (checkout, restore, reset) revert
	// files to HEAD — a known-good baseline — and the diff correctly
	// attributes those reverts with real OriginalCode for recovery.
	// Stash is different because the stash entry may predate the current
	// working state, so the "original" captured is meaningless.
	//
	// When a stash operation is detected, re-prime the cache (new state
	// = new baseline) instead of diffing against the stale pre-stash
	// cache.
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

	// Surface truncation as a manifest entry on destructive walks. Non-
	// destructive truncation already gets logged; for destructive ops we
	// want it impossible to miss because partial coverage might hide
	// reverts the user expected to be recoverable. Recorded as Operation
	// "warning" so the UI can highlight it distinctly.
	if truncated && destructive {
		ct.appendChange(TrackedFileChange{
			FilePath:  toolCall,
			Operation: "warning",
			NewCode:   "walk truncated during destructive command — coverage is partial. Re-run sprout in a smaller subdirectory or increase the walker budget if recovery completeness matters.",
			Timestamp: time.Now(),
			ToolCall:  toolCall,
		})
	}

	// Destructive commands above the bulk threshold collapse into a
	// single recoverable entry. Below the threshold we keep the per-file
	// shape so small reverts stay easy to scan flat. Non-destructive
	// commands always emit per-file — the build-rollup machinery is
	// dormant in production today (RecordShellMutations isn't wired in
	// from this path), so we don't touch their behaviour here.
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
// against its current on-disk state. Called by the direct file-write
// hooks (TrackFileWrite, TrackFileEdit) so the cache reflects writes
// the agent just performed via structured-file tools — without this,
// the next TrackShellTurn walk would see the new content as a stat
// mismatch against stale cache and record a duplicate "edit" entry
// even though no shell command touched the file.
//
// Safe to call when the cache hasn't been primed yet (no-op) — there's
// no baseline to keep in sync.
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
