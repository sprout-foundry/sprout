// Snapshot walking and file I/O for ChangeTracker shell-mutation tracking.
//
// Walks the workspace tree before and after shell commands, capturing
// file bytes inside size/binary limits, then diffing the two snapshots.
package agent

import (
	"bytes"
	"path/filepath"
	"strings"
	"time"
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

// autoSkipCumulativeThreshold is the cumulative-descendant-file count
// that triggers adaptive auto-skip for distributed bloat — directories
// whose total recursive file count exceeds this value even though no
// single subdirectory individually exceeds autoSkipFileCountThreshold.
//
// This is set MUCH higher than autoSkipFileCountThreshold because
// cumulative counts naturally include all nested source files. A
// legitimate monorepo src/ dir with 3000 source files must never be
// auto-skipped — the agent edits those files. Build artifacts, by
// contrast, start at 3000–5000 and routinely exceed 10000 (Swift
// SPM's .build/ alone can hit 30000).
//
// The two-threshold design gives us:
//   - autoSkipFileCountThreshold (1500): catches "flat" bloat dirs
//     with thousands of direct children (e.g., a releases/ dir of
//     tarballs). High confidence — no legitimate dir has 1500 direct
//     file children.
//   - autoSkipCumulativeThreshold (10000): catches "distributed" bloat
//     dirs with files spread across many shallow subdirs (e.g.,
//     .build/index-build/records/6D/, 6E/, …). Lower confidence but
//     still far above honest source trees.
var autoSkipCumulativeThreshold = 10000

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
	// Swift Package Manager build output (can be tens of thousands of
	// indexed files — see ~9000+ records in .build/index-build/).
	".build": true,
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
