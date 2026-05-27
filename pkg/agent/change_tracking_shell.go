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
package agent

import (
	"bytes"
	"io/fs"
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

// shellSnapshotMaxFiles caps the number of files visited in a single
// walk. Hit when the user opens sprout inside a directory tree that's
// pathologically large (~/, /, a monorepo root with undeclared bloat
// dirs). The walk aborts cleanly when the cap is reached — the cache
// is partial but the agent isn't blocked. 50000 is well above any
// sane project; mature monorepos rarely exceed 20000 after the
// bloat-dir skips. Mutable (not const) so tests can override.
var shellSnapshotMaxFiles = 50000

// shellSnapshotMaxDuration is the wall-clock budget for a single walk.
// Same purpose as shellSnapshotMaxFiles: bound worst-case cost so a
// misconfigured workspace can't hang the agent. Cold prime + first
// walk of a sprout-sized repo runs in ~30 ms; 500 ms is a 15× safety
// margin.
var shellSnapshotMaxDuration = 500 * time.Millisecond

// overrideShellSnapshotMaxFilesForTest swaps the file-count cap and
// returns the previous value so the test can restore it. Test-only
// hook — production code never calls this.
func overrideShellSnapshotMaxFilesForTest(newCap int) int {
	prev := shellSnapshotMaxFiles
	shellSnapshotMaxFiles = newCap
	return prev
}

// autoSkipFileCountThreshold is the per-directory immediate-child-file
// count that triggers adaptive auto-skip. Directories with more than
// this many direct files (not counting subdirectories or recursive
// descendants) are added to autoSkipDirs for subsequent walks.
//
// Set conservatively: most legitimate source directories have < 200
// direct files. Bloat dirs we want to skip (build outputs, generated
// fixtures, session logs) typically have thousands. 1500 is well above
// honest directories and well below pathological ones, with margin
// for monorepo `internal/` style dirs.
var autoSkipFileCountThreshold = 1500

// shellSnapshotEntry is what the snapshot map stores per file. Content
// is the byte payload (nil if the file was filtered out by size /
// binary checks but we want to remember it existed). Size + ModTime
// are stat metadata used by the fast-path diff: when a walk finds a
// file with matching (size, mtime) the cached content is reused
// without a re-read — the file is treated as unchanged.
//
// Known edge case: filesystems with coarse mtime resolution (or
// kernels that batch mtime updates within a single tick) can report
// identical mtimes for two consecutive writes if they happen fast
// enough. The fast path will miss a same-size modification in that
// case. Real shell_command invocations spawn a process + execute,
// which on every measured FS takes long enough for mtime to advance.
// Tests that mutate files in tight loops use os.Chtimes to force
// distinct mtimes deterministically.
type shellSnapshotEntry struct {
	Content []byte
	Size    int64
	ModTime time.Time
	Skipped string // reason if Content is nil (e.g. "too large", "binary")
}

// shellSnapshotSkipDirs is the set of directory names that are pruned
// outright during the snapshot walk. These are conventional build /
// dependency / VCS-internal directories that we never want to capture:
//
//   - Always huge (`node_modules`, `vendor`, `.gradle`, `.next`,
//     `__pycache__`)
//   - Generated output (`dist`, `build`, `out`, `target`, `coverage`)
//   - Tool-internal state (`.git`, `.idea`, `.vscode`, `.tox`, `.venv`,
//     `venv`)
//
// Hidden DIRS that aren't on this list (e.g., `.github`, `.config`) are
// still walked — they're typically small and may contain user-relevant
// config. Hidden FILES are always walked regardless (a deleted
// `.env.local` is exactly the kind of thing the user wants
// recoverable).
//
// Not exhaustive — additions welcome. The 32 MiB total-bytes budget
// in shellSnapshotMaxTotalBytes is the long-stop defense if a workspace
// has an unusual giant directory we don't recognize.
var shellSnapshotSkipDirs = map[string]bool{
	".git":          true,
	".hg":           true,
	".svn":          true,
	"node_modules":  true,
	"vendor":        true,
	"dist":          true,
	"build":         true,
	"out":           true,
	"target":        true,
	"coverage":      true,
	".next":         true,
	".nuxt":         true,
	".cache":        true,
	".parcel-cache": true,
	".turbo":        true,
	"__pycache__":   true,
	".pytest_cache": true,
	".mypy_cache":   true,
	".ruff_cache":   true,
	".venv":         true,
	"venv":          true,
	".tox":          true,
	".gradle":       true,
	".idea":         true,
	".vscode":       true,
	".direnv":       true,
	// Sprout's own session-output / scratch directories. The tracker
	// writing into .sprout/ and then having to walk .sprout/ on the
	// next shell snapshot would be silly recursion; skipping is safe
	// because the user never edits these directly.
	".sprout": true,
}

// captureShellSnapshot walks workDir recursively and captures the byte
// content of every file inside the size/binary limits, skipping
// well-known bloat directories. Returns a map keyed by absolute path.
//
// Internally this is `walkWorkspace(workDir, nil)` — the cache-aware
// fast path collapses to a full read when no cache is supplied. The
// standalone function survives so tests and the priming step have a
// simple API; production hot-path callers should prefer
// captureShellChangeDelta which reuses an existing baseline.
func (ct *ChangeTracker) captureShellSnapshot(workDir string) map[string]*shellSnapshotEntry {
	snap, _ := ct.walkWorkspace(workDir, nil)
	return snap
}

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
func (ct *ChangeTracker) walkWorkspace(workDir string, old map[string]*shellSnapshotEntry) (map[string]*shellSnapshotEntry, []pendingShellChange) {
	if ct == nil || !ct.enabled || workDir == "" {
		return nil, nil
	}

	absRoot, err := filepath.Abs(workDir)
	if err != nil {
		ct.logf("shell snapshot: resolve %q: %v", workDir, err)
		return nil, nil
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
		maxTotalBytes = ct.shellMaxTotalBytes
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
				if ct.autoSkipDirs != nil && ct.autoSkipDirs[path] {
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
			// Count cached content toward the budget so the budget
			// stays consistent across walks. (No I/O happened.)
			if cached.Content != nil {
				totalBytes += int64(len(cached.Content))
			}
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
	// Critical follow-up: drop snap entries under any newly-learned
	// auto-skip dir AND suppress deletion records for those paths.
	// Without this cleanup, the very next walk would see all the
	// fat-dir files as "missing from snap" and record 12,000 false
	// deletes — which would then ALSO claim "the agent deleted all
	// these files" in the manifest. Worse than not tracking at all.
	newlyLearned := false
	if len(filesPerDir) > 0 {
		if ct.autoSkipDirs == nil {
			ct.autoSkipDirs = make(map[string]bool)
		}
		for dir, count := range filesPerDir {
			if count > autoSkipThreshold {
				if !ct.autoSkipDirs[dir] {
					ct.logf("shell snapshot: auto-skipping fat dir %q (%d direct files) on future walks", dir, count)
					newlyLearned = true
				}
				ct.autoSkipDirs[dir] = true
			}
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

	return snap, changes
}

// isUnderAnyAutoSkipDir reports whether the given absolute file path
// sits inside one of the auto-skipped directories. Uses a clean
// prefix check (with separator) so /work/release-notes.md doesn't
// match an auto-skipped /work/release/ — it'd be wrong to treat the
// adjacent file as auto-skipped just because the dir name is a
// prefix.
func isUnderAnyAutoSkipDir(path string, skipDirs map[string]bool) bool {
	if len(skipDirs) == 0 {
		return false
	}
	for dir := range skipDirs {
		prefix := dir
		if !strings.HasSuffix(prefix, string(filepath.Separator)) {
			prefix += string(filepath.Separator)
		}
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// pendingShellChange is an unattributed diff entry from walkWorkspace,
// ready to be turned into a TrackedFileChange (after dedup against
// direct-hook entries) via the caller.
type pendingShellChange struct {
	Path   string
	Op     string // "create" | "edit" | "delete"
	Before *shellSnapshotEntry
	After  *shellSnapshotEntry
}

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
	if ct == nil || !ct.enabled {
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
	snap, _ := ct.walkWorkspace(workDir, nil)
	if snap == nil {
		snap = map[string]*shellSnapshotEntry{}
	}
	ct.shellCache = snap
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
// Concurrency: serialized via the tracker's internal mutex. Subagents
// each have their own ChangeTracker so cross-subagent calls don't
// interfere.
func (ct *ChangeTracker) TrackShellTurn(workDir, toolCall string) {
	if ct == nil || !ct.enabled {
		return
	}
	if !ct.shellWalkEnabled {
		return
	}
	ct.shellCacheMu.Lock()
	defer ct.shellCacheMu.Unlock()

	if ct.shellCache == nil {
		// First call — populate the baseline silently.
		snap, _ := ct.walkWorkspace(workDir, nil)
		if snap == nil {
			snap = map[string]*shellSnapshotEntry{}
		}
		ct.shellCache = snap
		return
	}

	newSnap, pending := ct.walkWorkspace(workDir, ct.shellCache)
	if newSnap == nil {
		return
	}

	for _, p := range pending {
		ct.appendShellMutation(p.Path, p.Before, p.After, p.Op, toolCall)
	}

	// Rebase the cache so the NEXT shell command's diff is against the
	// state we just observed (not the original session-start state).
	ct.shellCache = newSnap
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
	if ct == nil || !ct.enabled {
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

// isLikelyBinary scans up to shellSnapshotBinarySniffBytes of `data`
// and returns true if it contains a null byte. Simple and matches what
// `git` itself uses for binary detection.
func isLikelyBinary(data []byte) bool {
	n := len(data)
	if n > shellSnapshotBinarySniffBytes {
		n = shellSnapshotBinarySniffBytes
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

// RecordShellMutations diffs a pair of snapshots (taken before/after a
// shell_command invocation) and appends TrackedFileChange entries for
// every file that materially changed. The before-snapshot supplies the
// OriginalCode field so a user can recover untracked-by-git files the
// agent accidentally deleted or mangled.
//
// Dedup: if the path was already recorded this turn via TrackFileWrite
// / TrackFileEdit (the direct tool hooks), the existing entry is kept
// and we don't double-record from the shell diff. The direct entry is
// richer (original_code captured at the source) so it wins.
func (ct *ChangeTracker) RecordShellMutations(before, after map[string]*shellSnapshotEntry, toolCall string) {
	if ct == nil || !ct.enabled {
		return
	}
	if before == nil {
		before = map[string]*shellSnapshotEntry{}
	}
	if after == nil {
		after = map[string]*shellSnapshotEntry{}
	}

	// Build a set of paths already recorded so the shell pass doesn't
	// double-count direct write/edit hooks fired during the same shell
	// command (rare in practice — shell_command rarely re-invokes
	// agent tools — but cheap to defend against).
	already := make(map[string]bool, len(ct.changes))
	for _, ch := range ct.changes {
		already[ch.FilePath] = true
	}

	// Deletions: present in `before`, absent in `after`.
	for path, beforeEntry := range before {
		if _, stillThere := after[path]; stillThere {
			continue
		}
		if already[path] {
			continue
		}
		ct.appendShellMutation(path, beforeEntry, nil, "delete", toolCall)
		already[path] = true
	}

	// Creations and modifications: present in `after`.
	for path, afterEntry := range after {
		if already[path] {
			continue
		}
		beforeEntry := before[path]
		if beforeEntry == nil {
			// Wasn't in the workspace before → creation.
			ct.appendShellMutation(path, nil, afterEntry, "create", toolCall)
			already[path] = true
			continue
		}
		// Both before and after exist → may be unchanged or modified.
		if shellContentsEqual(beforeEntry, afterEntry) {
			continue
		}
		ct.appendShellMutation(path, beforeEntry, afterEntry, "edit", toolCall)
		already[path] = true
	}
}

// appendShellMutation appends a TrackedFileChange given before/after
// snapshot entries. Either pointer may be nil (creation has no before;
// deletion has no after).
func (ct *ChangeTracker) appendShellMutation(path string, before, after *shellSnapshotEntry, op, toolCall string) {
	originalCode := ""
	newCode := ""
	if before != nil {
		if before.Content != nil {
			originalCode = string(before.Content)
		} else if before.Skipped != "" {
			// Path-only record: we know the file existed but its
			// content was filtered out (too big / binary). Use a
			// sentinel so the manifest can communicate "we have no
			// recovery payload" without faking original content.
			originalCode = "[CONTENT NOT CAPTURED: " + before.Skipped + "]"
		}
	}
	if after != nil {
		if after.Content != nil {
			newCode = string(after.Content)
		} else if after.Skipped != "" {
			newCode = "[CONTENT NOT CAPTURED: " + after.Skipped + "]"
		}
	}

	if ct.isOutsideWorkspace(path) {
		originalCode = RedactedContentMarker
		newCode = RedactedContentMarker
	}

	ct.changes = append(ct.changes, TrackedFileChange{
		FilePath:     path,
		OriginalCode: originalCode,
		NewCode:      newCode,
		Operation:    op,
		Timestamp:    time.Now(),
		ToolCall:     toolCall,
	})

	// Publish a file_changed event so the WebUI activity feed surfaces
	// shell-driven mutations in real time, not just direct-tool edits.
	// Action vocabulary mirrors events.FileChangedEvent ("created" /
	// "modified" / "deleted"); map our internal ops accordingly.
	if ct.agent != nil {
		action := "modified"
		switch op {
		case "create", "write":
			action = "created"
		case "delete":
			action = "deleted"
		}
		// For deletes, send the original content (lets the WebUI offer
		// a one-click recover button); for creates, send the new
		// content; for edits, send the new content (the file's
		// current state).
		eventContent := newCode
		if op == "delete" {
			eventContent = originalCode
		}
		ct.agent.PublishFileChange(path, action, eventContent)
	}
}

// shellContentsEqual compares two snapshot entries for byte-level
// equality. Path-only entries (Content == nil) are considered equal
// only if both sides agree on size — we can't differentiate identical
// large files from changed ones without reading them, so we err on
// the side of "treat as unchanged" (avoids spamming the manifest with
// false positives for binary / oversized files that didn't actually
// change). The size+Skipped match is a reasonable proxy.
func shellContentsEqual(a, b *shellSnapshotEntry) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Content != nil && b.Content != nil {
		return bytes.Equal(a.Content, b.Content)
	}
	// At least one side is path-only — compare metadata.
	return a.Size == b.Size && a.Skipped == b.Skipped
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
