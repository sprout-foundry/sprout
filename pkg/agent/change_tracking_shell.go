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
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
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
	snap, _, _ := ct.walkWorkspace(workDir, nil, false)
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

	return snap, changes, truncated
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

	if ct.shellCache == nil {
		// First call — populate the baseline silently. Priming is
		// inherently non-destructive (no diff is computed) so we
		// always pass false here, even if the calling command is
		// destructive — the safer walk happens on the next turn.
		snap, _, _ := ct.walkWorkspace(workDir, nil, false)
		if snap == nil {
			snap = map[string]*shellSnapshotEntry{}
		}
		ct.shellCache = snap
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
		ct.changes = append(ct.changes, TrackedFileChange{
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

// shellBulkThreshold is the per-command mutation count that flips the
// recorder into rollup mode. Below this, every change records
// individually so small refactors and one-off edits keep fine-grained
// entries. Above it, the recorder assumes the command was a build /
// install / clone / unzip and collapses each top-level workspace
// directory's worth of churn into a single "bulk" row.
//
// SP-061-1 v2: the trigger is now per-COMMAND volume only. The
// previous design also required a per-directory floor as the primary
// gate, which missed real-world bulk operations whose output fanned
// out across many small directories:
//
//   - `git clone repo .` → ~5k files spread across hundreds of dirs,
//     none individually heavy → no rollup, the changes panel drowned
//   - `unzip flat-archive.zip` → similar fanout
//   - `pip install --target env/` → output buried in
//     `env/lib/python3.x/site-packages/...` with few files per leaf
//
// Static skip dirs (`node_modules`, `.venv`, `vendor`, …) still get
// pruned by the walker before they ever reach this code path, so the
// common composer / pip-into-.venv / npm cases are already silent.
// This threshold is a long-stop for everything else.
var shellBulkThreshold = 200

// shellBulkBucketMin is a SECONDARY filter applied only within bulk
// mode: buckets with fewer than this many items emit per-file even
// though the overall command was bulk. Without this, a single source
// edit made adjacent to a build command (`make && touch src/lib.go`)
// would render as a useless "src/ — 1 file" rollup row and lose the
// per-file view/revert affordances. The number is intentionally low —
// the primary gate is shellBulkThreshold; this is just protection
// for the handful of meaningful edits that ride along with a heavy
// command. It is NOT the v1 per-dir gate and does not need to be
// high enough to discriminate between fan-out shapes.
var shellBulkBucketMin = 10

// pendingShellMutation is the intermediate shape used by
// RecordShellMutations to defer "emit per-file vs roll up by directory"
// until we've counted everything. Mirrors the appendShellMutation
// argument list verbatim — only the destination differs.
type pendingShellMutation struct {
	path   string
	before *shellSnapshotEntry
	after  *shellSnapshotEntry
	op     string // "create" | "edit" | "delete"
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
//
// SP-061-1: when a single shell command churns more than
// shellBulkThreshold paths AND some top-level workspace directory
// owns at least shellBulkPerDirMin of them, that directory is rolled
// up into a single "bulk" entry and added to autoSkipDirs so future
// walks skip it entirely. The rollup carries BulkCount so the UI can
// render "dist/ — 1,247 files (build output)" instead of stacking
// thousands of individual rows.
func (ct *ChangeTracker) RecordShellMutations(before, after map[string]*shellSnapshotEntry, toolCall string) {
	if ct == nil || !ct.IsEnabled() {
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

	// Phase 1: collect candidate mutations into a deferred slice so we
	// can decide between per-file emission and bulk rollup AFTER we
	// know the total count.
	pending := make([]pendingShellMutation, 0, len(before)+len(after))

	// Deletions: present in `before`, absent in `after`.
	for path, beforeEntry := range before {
		if _, stillThere := after[path]; stillThere {
			continue
		}
		if already[path] {
			continue
		}
		pending = append(pending, pendingShellMutation{path: path, before: beforeEntry, op: "delete"})
		already[path] = true
	}

	// Creations and modifications: present in `after`.
	for path, afterEntry := range after {
		if already[path] {
			continue
		}
		beforeEntry := before[path]
		if beforeEntry == nil {
			pending = append(pending, pendingShellMutation{path: path, after: afterEntry, op: "create"})
			already[path] = true
			continue
		}
		if shellContentsEqual(beforeEntry, afterEntry) {
			continue
		}
		pending = append(pending, pendingShellMutation{path: path, before: beforeEntry, after: afterEntry, op: "edit"})
		already[path] = true
	}

	// Phase 2: emit. Most shell commands fall below the threshold and
	// take the straight per-file path; only build-style commands hit
	// the rollup branch.
	if len(pending) < shellBulkThreshold {
		for _, p := range pending {
			ct.appendShellMutation(p.path, p.before, p.after, p.op, toolCall)
		}
		return
	}
	ct.emitWithBulkRollup(pending, toolCall)
}

// emitWithBulkRollup is the rollup path taken when a single shell
// command exceeded shellBulkThreshold. Strategy:
//
//   1. Bucket the pending mutations by their TOP-LEVEL workspace
//      directory. Root-level files (no enclosing dir) form a special
//      bucket of their own.
//   2. Root-level files always emit per-file — there is no useful
//      directory label for them ("workspace root" reads as everything)
//      and they're overwhelmingly config / lockfile / README edits
//      that the user wants to see.
//   3. Each non-root bucket collapses into ONE rollup row labeled by
//      the deepest workspace-relative path ALL items in the bucket
//      share. So `repo/foo.js + repo/bar/baz.js` → `repo/`; if every
//      item sits under `env/lib/python3.x/site-packages/...` the
//      label sharpens to that deepest shared prefix.
//   4. The top-level dir for each bucket joins autoSkipDirs so the
//      next shell command's walk doesn't re-traverse it.
//
// Honest cross-directory refactors above the threshold (e.g. a
// 250-file rename touching 25 top-level dirs) collapse into 25 rollup
// rows — still terse, still recognizable. The model's primary edit
// tools (write_file, edit_file) bypass this code path entirely, so the
// rollup never absorbs intentional per-file work.
func (ct *ChangeTracker) emitWithBulkRollup(pending []pendingShellMutation, toolCall string) {
	workspaceRoot := ""
	if ct.agent != nil {
		workspaceRoot = ct.agent.workspaceRoot
	}
	absWorkspace := workspaceRoot
	if workspaceRoot != "" {
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
				absWorkspace = resolved
			} else {
				absWorkspace = abs
			}
		}
	}

	type bucket struct {
		topDir string // workspace-relative top-level dir, "" for root-level
		items  []pendingShellMutation
	}
	buckets := map[string]*bucket{}
	bucketOrder := []string{} // preserve deterministic emit order

	for _, p := range pending {
		topDir := topLevelDirRelativeTo(absWorkspace, p.path)
		b, ok := buckets[topDir]
		if !ok {
			b = &bucket{topDir: topDir}
			buckets[topDir] = b
			bucketOrder = append(bucketOrder, topDir)
		}
		b.items = append(b.items, p)
	}

	for _, key := range bucketOrder {
		b := buckets[key]
		// Root-level files: always per-file. There's no honest
		// directory label and these are typically meaningful
		// config / lockfile / README edits the user wants to see.
		//
		// Light buckets (below shellBulkBucketMin): also per-file.
		// These are the handful of source edits riding along with a
		// heavy build/install command. Rolling them up would replace
		// "src/lib.go was modified" with "src/ — 1 file" and lose the
		// view/revert affordance for an edit the user almost certainly
		// cares about. The PRIMARY rollup gate (shellBulkThreshold)
		// already fired at the COMMAND level above; this secondary
		// filter just exempts the light buckets from collapse.
		if b.topDir == "" || len(b.items) < shellBulkBucketMin {
			for _, p := range b.items {
				ct.appendShellMutation(p.path, p.before, p.after, p.op, toolCall)
			}
			continue
		}
		label := deepestCommonAncestorRel(absWorkspace, b.items)
		if label == "" {
			label = b.topDir
		}
		// Convert pendingShellMutation (build-rollup's internal shape)
		// into the canonical pendingShellChange so packBulkItems can
		// process both bulk paths through one helper.
		asChanges := make([]pendingShellChange, len(b.items))
		for i, it := range b.items {
			asChanges[i] = pendingShellChange{Path: it.path, Op: it.op, Before: it.before, After: it.after}
		}
		ct.appendBulkRollup(label, asChanges, absWorkspace, toolCall)
		ct.addAutoSkipDir(absWorkspace, b.topDir)
	}
}

// topLevelDirRelativeTo returns the first path component of `path`
// relative to `workspaceRoot`, or "" when the path is at the
// workspace root (no nested directory) or outside it.
func topLevelDirRelativeTo(workspaceRoot, path string) string {
	if workspaceRoot == "" {
		return ""
	}
	absPath := path
	if abs, err := filepath.Abs(path); err == nil {
		absPath = abs
	}
	rel, err := filepath.Rel(workspaceRoot, absPath)
	if err != nil {
		return ""
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	idx := strings.IndexRune(rel, filepath.Separator)
	if idx < 0 {
		return "" // file lives directly at workspace root
	}
	return rel[:idx]
}

// deepestCommonAncestorRel returns the deepest workspace-relative
// directory that contains every path in `items`. Operates on the
// parent directory of each path (because rollups represent the
// containing folder, not the files themselves). Falls back to "" if
// any path can't be made workspace-relative or if there's no shared
// prefix beyond the workspace root.
func deepestCommonAncestorRel(workspaceRoot string, items []pendingShellMutation) string {
	if workspaceRoot == "" || len(items) == 0 {
		return ""
	}
	sep := string(filepath.Separator)
	var commonSegs []string
	initialized := false
	for _, it := range items {
		absPath := it.path
		if abs, err := filepath.Abs(it.path); err == nil {
			absPath = abs
		}
		rel, err := filepath.Rel(workspaceRoot, absPath)
		if err != nil {
			return ""
		}
		if rel == "." || strings.HasPrefix(rel, "..") {
			return ""
		}
		dir := filepath.Dir(rel)
		var segs []string
		if dir == "." {
			segs = []string{}
		} else {
			segs = strings.Split(dir, sep)
		}
		if !initialized {
			commonSegs = segs
			initialized = true
			continue
		}
		// Truncate commonSegs to the shared prefix.
		n := len(commonSegs)
		if len(segs) < n {
			n = len(segs)
		}
		i := 0
		for ; i < n; i++ {
			if commonSegs[i] != segs[i] {
				break
			}
		}
		commonSegs = commonSegs[:i]
		if len(commonSegs) == 0 {
			break
		}
	}
	return strings.Join(commonSegs, sep)
}

// appendBulkRollup records a single TrackedFileChange that stands in
// for the files churned under `dir`. The path stored is workspace-
// relative with a trailing slash so the UI can recognise the entry as
// a directory rollup at a glance.
//
// Items get packed into BulkItems via packBulkItems so the bulk row is
// recoverable per-file. When the cumulative byte total exceeds the
// payload cap, BulkItems is left empty and the entry behaves as the
// historical count-only row — the UI/tools can detect this via
// BulkCount > 0 && len(BulkItems) == 0.
func (ct *ChangeTracker) appendBulkRollup(dir string, items []pendingShellChange, workspaceRoot, toolCall string) {
	displayPath := dir + string(filepath.Separator)
	packed, overBudget := ct.packBulkItems(items)
	if overBudget {
		log.Printf("[change-tracker] bulk rollup over %d-byte cap (%d files) for %q from %q — recording count-only entry and adding to auto-skip", shellDestructiveBulkMaxPayloadBytes, len(items), displayPath, toolCall)
	} else {
		log.Printf("[change-tracker] bulk rollup: %s (%d files) from %q — recording with %d packed items and adding to auto-skip", displayPath, len(items), toolCall, len(packed))
	}
	entry := TrackedFileChange{
		FilePath:  displayPath,
		Operation: "bulk",
		Timestamp: time.Now(),
		ToolCall:  toolCall,
		BulkCount: len(items),
	}
	if !overBudget {
		entry.BulkItems = packed
	}
	ct.changes = append(ct.changes, entry)
	// Publish the rollup over the bus too so the UI can refresh.
	if ct.agent != nil && ct.agent.eventBus != nil {
		absDir := filepath.Join(workspaceRoot, dir)
		ct.agent.eventBus.Publish(
			events.EventTypeFileChanged,
			events.FileChangedEvent(absDir, "shell_bulk", toolCall),
		)
	}
}

// shellDestructiveBulkThreshold is the per-command mutation count above
// which a destructive shell command (e.g. `git checkout .`) collapses
// into a single recoverable bulk entry instead of N per-file rows.
// Set deliberately low (10) — once a destructive op touches enough
// files to be hard to scan flat, the bulk row's expand-to-recover UX
// wins. Below the threshold the per-file shape keeps small reverts
// easy to read in the changes panel.
var shellDestructiveBulkThreshold = 10

// shellDestructiveBulkMaxPayloadBytes caps the total before+after
// content packed into a single recoverable bulk entry. Same ceiling as
// the walk's content budget (shellSnapshotMaxTotalBytes, 32 MiB) so we
// never hold more bytes for recovery than the walker would have read
// in the first place. Past the cap we degrade the bulk to count-only
// and the entry is reported as !Recoverable.
const shellDestructiveBulkMaxPayloadBytes = shellSnapshotMaxTotalBytes

// appendDestructiveBulkRollup records a single TrackedFileChange that
// stands in for an entire destructive shell command (e.g. `git
// checkout .`) reverting many files. Unlike the build-rollup path,
// every per-file mutation is packed into the entry's BulkItems so the
// recovery tools can:
//
//   - restore one specific file inside the bulk (recover_file matches
//     against BulkItems), or
//   - restore everything in one shot (recover_bulk).
//
// When the cumulative before+after payload exceeds the cap, the entry
// degrades to a count-only bulk (BulkItems == nil) and we log a warning
// — the user can still see "300 files reverted" but per-file recovery
// is out of reach. This is the same upper bound the walker applies, so
// hitting it means we ran into a workspace that's outside the change
// tracker's overall budget regardless of bulk shape.
func (ct *ChangeTracker) appendDestructiveBulkRollup(pending []pendingShellChange, toolCall string) {
	items, overBudget := ct.packBulkItems(pending)

	entry := TrackedFileChange{
		FilePath:  toolCall, // command label — the UI renders this as the bulk row's heading
		Operation: "bulk",
		Timestamp: time.Now(),
		ToolCall:  toolCall,
		BulkCount: len(pending),
	}
	if overBudget {
		log.Printf("[change-tracker] destructive bulk over %d-byte cap (%d files); recording count-only entry from %q", shellDestructiveBulkMaxPayloadBytes, len(pending), toolCall)
	} else {
		entry.BulkItems = items
	}
	ct.changes = append(ct.changes, entry)

	// Publish a file-changed event so the UI refreshes. The "path" here
	// is the command label, not a real file path; the changes-panel
	// renderer is the source of truth for resolving bulk entries.
	if ct.agent != nil && ct.agent.eventBus != nil {
		ct.agent.eventBus.Publish(
			events.EventTypeFileChanged,
			events.FileChangedEvent(toolCall, "shell_bulk", toolCall),
		)
	}
}

// packBulkItems extracts the per-file recovery payload from each
// pending mutation, stopping when the cumulative byte total exceeds
// shellDestructiveBulkMaxPayloadBytes. Returns the items collected so
// far (may be empty) and an overBudget flag — callers should drop the
// items when overBudget is true so a count-only bulk row is recorded
// without misleading partial recovery.
func (ct *ChangeTracker) packBulkItems(pending []pendingShellChange) ([]TrackedBulkItem, bool) {
	items := make([]TrackedBulkItem, 0, len(pending))
	var payloadBytes int64
	for _, p := range pending {
		original := ""
		newer := ""
		if p.Before != nil {
			if p.Before.Content != nil {
				original = string(p.Before.Content)
			} else if p.Before.Skipped != "" {
				original = "[CONTENT NOT CAPTURED: " + p.Before.Skipped + "]"
			}
		}
		if p.After != nil {
			if p.After.Content != nil {
				newer = string(p.After.Content)
			} else if p.After.Skipped != "" {
				newer = "[CONTENT NOT CAPTURED: " + p.After.Skipped + "]"
			}
		}
		if ct.isOutsideWorkspace(p.Path) {
			original = RedactedContentMarker
			newer = RedactedContentMarker
		}
		payloadBytes += int64(len(original)) + int64(len(newer))
		if payloadBytes > shellDestructiveBulkMaxPayloadBytes {
			return nil, true
		}
		items = append(items, TrackedBulkItem{
			FilePath:     p.Path,
			OriginalCode: original,
			NewCode:      newer,
			Operation:    p.Op,
		})
	}
	return items, false
}

// addAutoSkipDir registers an absolute directory path with the shell-
// walk auto-skip set so subsequent walks don't re-traverse it. Safe to
// call repeatedly; persisting failures are logged but not surfaced —
// the in-process skip still applies for the rest of the session.
func (ct *ChangeTracker) addAutoSkipDir(workspaceRoot, relDir string) {
	if ct == nil {
		return
	}
	abs := filepath.Join(workspaceRoot, relDir)
	if ct.autoSkipDirs == nil {
		ct.autoSkipDirs = map[string]bool{}
	}
	if ct.autoSkipDirs[abs] {
		return
	}
	ct.autoSkipDirs[abs] = true
	if err := saveAutoSkipDirsFor(workspaceRoot, ct.autoSkipDirs); err != nil {
		log.Printf("[change-tracker] failed to persist auto-skip dirs after bulk rollup: %v", err)
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
