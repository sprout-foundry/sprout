// Revision revert and staleness checking
package history

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/git"
)

// RevertChangeByRevisionID reverts all changes associated with a given revision ID.
func RevertChangeByRevisionID(revisionID string) error {
	changes, err := fetchAllChanges()
	if err != nil {
		return fmt.Errorf("failed to fetch all changes: %w", err)
	}
	if len(changes) == 0 {
		return errors.New("no changes recorded to revert")
	}

	revisionGroups := groupChangesByRevision(changes)

	var targetGroup *RevisionGroup
	for i := range revisionGroups {
		if revisionGroups[i].RevisionID == revisionID {
			targetGroup = &revisionGroups[i]
			break
		}
	}

	if targetGroup == nil {
		return fmt.Errorf("revision ID '%s' not found", revisionID)
	}

	activeChanges := getActiveChanges(targetGroup.Changes)
	if len(activeChanges) == 0 {
		return fmt.Errorf("no active changes found for revision ID '%s' to revert", revisionID)
	}

	if err := handleRevisionRollback(*targetGroup); err != nil {
		return fmt.Errorf("error during revision rollback for ID '%s': %w", revisionID, err)
	}

	return nil
}

// isTmpPath reports whether the resolved path lives under the system temp
// directory. macOS resolves /tmp to /private/tmp via a symlink, so both forms
// are checked. This mirrors filesystem.isInTmpPath, which is not exported.
func isTmpPath(path string) bool {
	cleanPath := filepath.Clean(path)
	if strings.HasPrefix(cleanPath, "/tmp/") || cleanPath == "/tmp" ||
		strings.HasPrefix(cleanPath, "/private/tmp/") || cleanPath == "/private/tmp" {
		return true
	}
	// Also allow os.TempDir() — on macOS this is /var/folders/.../T/
	// which is the per-user temp directory.
	tmpDir := filepath.Clean(os.TempDir())
	if tmpDir != "/tmp" && tmpDir != "/private/tmp" &&
		(strings.HasPrefix(cleanPath, tmpDir+string(filepath.Separator)) || cleanPath == tmpDir) {
		return true
	}
	// Windows-style temp paths
	lowerPath := strings.ToLower(cleanPath)
	if strings.Contains(lowerPath, "\\temp\\") || strings.Contains(lowerPath, "\\tmp\\") {
		return true
	}
	return false
}

// isWithinWorkspace reports whether the given filename resolves to a path
// inside the current workspace root (determined from os.Getwd()). /tmp paths
// are always allowed. Any error during path resolution returns false.
func isWithinWorkspace(filename string) bool {
	if filename == "" {
		return false
	}

	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}

	cleanPath := filepath.Clean(filename)
	absPath := cleanPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwdAbs, cleanPath)
	}
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return false
	}

	// /tmp is always allowed (same exception as SafeResolvePath).
	if isTmpPath(absPath) {
		return true
	}

	// Resolve symlinks on the file path. The file may not exist yet (rollback
	// can restore a deleted file), so fall back to the parent directory if the
	// file itself cannot be evaluated.
	resolvedAbs, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// Try resolving the parent directory instead (file may not exist).
		resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(absPath))
		if parentErr != nil {
			// Neither file nor parent exists; use the un-resolved path.
			// On macOS, /var/folders/... symlinks to /private/var/folders/...
			// and the resolved CWD comparison handles this correctly as long
			// as we also try the un-resolved CWD below.
			resolvedAbs = absPath
		} else {
			resolvedAbs = filepath.Join(resolvedParent, filepath.Base(absPath))
		}
	}

	resolvedCwd, err := filepath.EvalSymlinks(cwdAbs)
	if err != nil {
		return false
	}

	relPath, err := filepath.Rel(resolvedCwd, resolvedAbs)
	if err != nil {
		return false
	}

	// A relative path starting with ".." escapes the workspace root.
	if !strings.HasPrefix(relPath, "..") {
		return true
	}

	// On macOS, /var → /private/var symlink can cause the resolved CWD to
	// differ from the un-resolved abs path. Try matching with the original
	// (un-resolved) forms as a fallback.
	if cwdAbs != resolvedCwd {
		relPath2, err := filepath.Rel(cwdAbs, absPath)
		if err == nil && !strings.HasPrefix(relPath2, "..") {
			return true
		}
	}
	return false
}

// isFileStale reports whether the file on disk differs from the content
// the agent wrote (change.NewCode). When true, the file was modified
// after the snapshot and rolling it back would clobber that change.
//
// A read error other than "not found" (permission denied, I/O error) is
// treated as STALE (true = do not proceed): we cannot prove the on-disk
// content matches the snapshot, so a destructive write-back must not be
// considered safe. Only a genuinely missing file is safe to restore.
func isFileStale(filename, newCode string) bool {
	if newCode == "" || newCode == RedactedContentMarker {
		return false
	}
	current, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return false // file doesn't exist — safe to restore
		}
		return true // unreadable — cannot verify, refuse to clobber
	}
	return string(current) != newCode
}

// IsRevertSafe reports whether it is SAFE to proceed with a revert that
// writes OriginalCode back to disk. It returns true when the revert will
// NOT clobber intentional work, and false when it would. The decision
// layers two checks: content-identity and git-awareness.
func IsRevertSafe(filename, newCode string) bool {
	return IsRevertSafeWithOriginal(filename, newCode, "")
}

// IsRevertSafeWithOriginal is the full-aware staleness guard used by
// recovery paths that have the OriginalCode. The original-aware path
// allows recovery when the file on disk matches HEAD but the OriginalCode
// is NOT the HEAD content — meaning the original was uncommitted work.
func IsRevertSafeWithOriginal(filename, newCode, originalCode string) bool {
	return IsRevertSafeAt(filename, newCode, originalCode, time.Time{})
}

// IsRevertSafeAt is IsRevertSafeWithOriginal for a change recorded at
// changedAt. When the file matches HEAD, git history decides what that
// means: a commit touching the file at or after the change means the agent's
// work was committed — reverting would undo it, so refuse. With no such
// commit, HEAD predates the change, so a match means a destructive git
// command (checkout, reset) rewound the file — restoring OriginalCode brings
// back destroyed uncommitted work. A zero changedAt skips the timing check.
func IsRevertSafeAt(filename, newCode, originalCode string, changedAt time.Time) bool {
	// 1. Empty or redacted newCode: no baseline to compare against.
	//    Allow (matches the historical isFileStale behaviour).
	if newCode == "" || newCode == RedactedContentMarker {
		return true
	}
	// 2. Read disk content. A missing file means the revert is
	//    restoring/creating it — safe. Any OTHER read error (permission,
	//    I/O) means we cannot verify the on-disk state, so refuse rather
	//    than risk clobbering content we failed to read.
	current, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return true // file doesn't exist — safe to restore/create
		}
		return false // unreadable — cannot verify, do not proceed
	}
	// 3. disk != newCode: the file was modified after the snapshot
	//    (git commit, manual edit, another session) — STALE, skip.
	if string(current) != newCode {
		return false
	}
	// 4. disk == newCode, but check git: if this content is committed
	//    to HEAD, reverting to OriginalCode would undo committed work.
	//    BUT: if originalCode is provided and does NOT match HEAD, then
	//    the snapshot captured uncommitted work that a destructive git
	//    command (checkout, reset, clean) aligned to HEAD. Restoring
	//    originalCode does NOT undo committed work — it restores
	//    destroyed uncommitted work. Allow it.
	state, gitErr := git.FileCommitState(filename)
	if gitErr != nil {
		// git check failed (e.g. transient error) — fall back to the
		// conservative content-only behaviour so we don't block a
		// revert the user may genuinely want.
		return true
	}
	if state.CommittedClean {
		// Commit timestamps have second resolution; compare at that grain
		// so a commit made in the same second as the change still counts.
		if !changedAt.IsZero() && !state.LastCommit.IsZero() && !state.LastCommit.Before(changedAt.Truncate(time.Second)) {
			return false
		}
		// File matches HEAD → committed work. But if the originalCode
		// is different from what's on disk (HEAD), restoring it would
		// bring back uncommitted work that git destroyed — not undo a
		// commit. Allow this specific case.
		if originalCode != "" && originalCode != RedactedContentMarker && originalCode != string(current) {
			return true
		}
		// File matches HEAD and original would write HEAD content back
		// (or original is empty/redacted) → refuse to avoid undoing
		// committed work.
		return false
	}
	// 5. disk == newCode and not committed → safe to revert
	//    (the historical behaviour).
	return true
}

// isFileStaleForRestore reports whether the file on disk differs from
// BOTH the pre-agent state (originalCode) and the agent's edit (newCode).
// Returns false when the disk content matches either (safe to restore),
// and true when it matches neither (stale / skip restore).
func isFileStaleForRestore(filename, originalCode, newCode string) bool {
	if newCode == "" || newCode == RedactedContentMarker {
		return false
	}
	current, err := os.ReadFile(filename)
	if err != nil {
		return false // file doesn't exist — safe to restore (create)
	}
	currentStr := string(current)
	return currentStr != originalCode && currentStr != newCode
}

// dedupChangesByFilename collapses multiple changes to the same file
// into a single entry, keeping the earliest OriginalCode and the latest
// NewCode and FileRevisionHash. Without deduplication, a file edited
// twice produces two change records with incorrect rollback behavior.
func dedupChangesByFilename(changes []ChangeLog) []ChangeLog {
	if len(changes) <= 1 {
		return changes
	}

	// Changes are sorted by timestamp descending (most recent first).
	// Walk in REVERSE (oldest first) so the first occurrence wins for
	// OriginalCode; track the latest NewCode and FileRevisionHash.
	sortChangesByTimestamp(changes) // ensures most-recent-first

	earliest := make(map[string]int) // filename → index in result
	var result []ChangeLog

	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		if idx, exists := earliest[change.Filename]; exists {
			// We've seen this file. Patch in the latest NewCode and
			// FileRevisionHash (this entry is newer because we're
			// iterating from oldest to newest).
			result[idx].NewCode = change.NewCode
			result[idx].FileRevisionHash = change.FileRevisionHash
		} else {
			earliest[change.Filename] = len(result)
			result = append(result, change)
		}
	}

	return result
}

func handleRevisionRollback(group RevisionGroup) error {
	fmt.Printf("Rolling back all changes in revision %s...\n", group.RevisionID)

	// Deduplicate by filename: keep the earliest OriginalCode and the latest NewCode.
	deduped := dedupChangesByFilename(getActiveChanges(group.Changes))
	var rolledBack, skipped, failed int
	for _, change := range deduped {
		// Skip files with redacted content (external files)
		if change.OriginalCode == RedactedContentMarker {
			fmt.Printf("  Skipping %s: content was redacted (external file)\n", change.Filename)
			skipped++
			continue
		}

		// Safety check: never write to files outside the current workspace.
		// The history DB may contain snapshots of files that were later moved
		// or committed elsewhere; blindly restoring them would clobber
		// intentional changes outside this project.
		if !isWithinWorkspace(change.Filename) {
			fmt.Printf("  Skipping %s: outside current workspace (safety check)\n", change.Filename)
			skipped++
			continue
		}

		// Staleness guard: if the file on disk no longer matches what the
		// agent wrote (NewCode), it was modified after this snapshot.
		// IsRevertSafe additionally applies git-awareness.
		if !IsRevertSafeAt(change.Filename, change.NewCode, change.OriginalCode, change.Timestamp) {
			AuditRevertSkip("handleRevisionRollback", change.Filename, "stale or committed")
			fmt.Printf("  Skipping %s: file modified since snapshot (safety check)\n", change.Filename)
			skipped++
			continue
		}

		fmt.Printf("  Rolling back %s...\n", change.Filename)

		// Write content directly to avoid any encoding transformations
		// Use filesystem.WriteFileWithDir which does raw binary write
		AuditRevertWrite("handleRevisionRollback", change.Filename, "OriginalCode")
		err := filesystem.WriteFileWithDir(change.Filename, []byte(change.OriginalCode), 0644)
		if err != nil {
			// Do NOT abort the whole rollback on one file: continue so the
			// rest of the revision is handled, then report a summary. Aborting
			// mid-way left the tree part-reverted with no accounting.
			fmt.Printf("  Failed to roll back %s: %v\n", change.Filename, err)
			failed++
			continue
		}
		// The write succeeded — the file IS reverted. A status-update failure
		// is recorded but must not report the write as failed (that would
		// diverge the DB from disk and invite a double-revert later).
		if err := updateChangeStatus(change.FileRevisionHash, "reverted"); err != nil {
			fmt.Printf("  Warning: %s rolled back but status update failed: %v\n", change.Filename, err)
			AuditRevertSkip("handleRevisionRollback", change.Filename, "status update failed after write")
		}
		rolledBack++
	}

	fmt.Printf("Revision rollback finished: %d rolled back, %d skipped, %d failed.\n", rolledBack, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("%d file(s) failed to roll back (see output above)", failed)
	}
	return nil
}

func handleRevisionRestore(group RevisionGroup) error {
	fmt.Printf("Restoring all changes in revision %s...\n", group.RevisionID)

	// Deduplicate by filename (see handleRevisionRollback for rationale).
	deduped := dedupChangesByFilename(group.Changes)
	var restored, skipped, failed int
	for _, change := range deduped {
		// Skip files with redacted content (external files)
		if change.NewCode == RedactedContentMarker {
			fmt.Printf("  Skipping %s: content was redacted (external file)\n", change.Filename)
			skipped++
			continue
		}

		// Safety check: never write to files outside the current workspace.
		// See handleRevisionRollback for rationale.
		if !isWithinWorkspace(change.Filename) {
			fmt.Printf("  Skipping %s: outside current workspace (safety check)\n", change.Filename)
			skipped++
			continue
		}

		// Staleness guard: if the file on disk no longer matches either
		// the pre-agent state or the agent's edit, it was modified after
		// the snapshot — restoring would silently clobber that change.
		if isFileStaleForRestore(change.Filename, change.OriginalCode, change.NewCode) {
			AuditRevertSkip("handleRevisionRestore", change.Filename, "stale")
			fmt.Printf("  Skipping %s: file modified since snapshot (safety check)\n", change.Filename)
			skipped++
			continue
		}

		fmt.Printf("  Restoring %s...\n", change.Filename)

		// Write content directly to avoid any encoding transformations
		AuditRevertWrite("handleRevisionRestore", change.Filename, "NewCode")
		err := filesystem.WriteFileWithDir(change.Filename, []byte(change.NewCode), 0644)
		if err != nil {
			// Continue rather than abort — see handleRevisionRollback.
			fmt.Printf("  Failed to restore %s: %v\n", change.Filename, err)
			failed++
			continue
		}

		// Update status to restored regardless of previous status. The write
		// already succeeded; a status failure is a warning, not a restore
		// failure (keeps DB and disk consistent in intent).
		if err := updateChangeStatus(change.FileRevisionHash, "restored"); err != nil {
			fmt.Printf("  Warning: %s restored but status update failed: %v\n", change.Filename, err)
			AuditRevertSkip("handleRevisionRestore", change.Filename, "status update failed after write")
		}
		restored++
	}

	fmt.Printf("Revision restore finished: %d restored, %d skipped, %d failed.\n", restored, skipped, failed)
	if failed > 0 {
		return fmt.Errorf("%d file(s) failed to restore (see output above)", failed)
	}
	return nil
}
