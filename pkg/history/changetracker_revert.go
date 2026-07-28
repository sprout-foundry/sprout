// Revision revert and staleness checking
package history

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
// inside the current workspace root (determined from os.Getwd()). It is a
// safety guard used by rollback/restore to prevent the history store from
// silently overwriting files outside the project — e.g. committed files whose
// old snapshots are still in the DB.
//
// The history package does not receive an explicit workspace root, so CWD is
// the best available proxy, consistent with how SafeResolvePath in the
// filesystem package falls back to CWD when no root is configured. /tmp paths
// are always allowed (same exception as SafeResolvePath).
//
// Any error during path resolution causes this to return false (skip the
// file) — failing closed is safer than writing to an unexpected location.
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
// intentionally after the snapshot — by a git commit, another session, or
// manual edit — and rolling it back would clobber that change.
//
// Returns false (not stale / safe to rollback) when:
//   - The file doesn't exist on disk (rollback is creating/restoring it)
//   - The disk content matches NewCode exactly (nothing changed since)
//   - NewCode is empty (no baseline to compare against — likely a delete op)
//   - NewCode is the redacted marker (can't compare, allow the rollback)
//
// Returns true (stale / skip rollback) when the disk content differs from
// NewCode, meaning someone modified the file after the agent's snapshot.
func isFileStale(filename, newCode string) bool {
	if newCode == "" || newCode == RedactedContentMarker {
		return false
	}
	current, err := os.ReadFile(filename)
	if err != nil {
		return false // file doesn't exist — safe to restore
	}
	return string(current) != newCode
}

// IsRevertSafe reports whether it is SAFE to proceed with a revert that
// writes OriginalCode back to disk. It is the canonical git-aware
// staleness guard for ALL rollback/revert paths (the history package's
// handleRevisionRollback, the agent_tools RollbackChanges single-file
// path, and the agent package's recover_file / revert_my_changes).
//
// It returns true (safe to proceed) when the revert will NOT clobber
// intentional work, and false (must skip) when it would. The decision
// layers two checks:
//
//  1. Content-identity: if the file on disk no longer matches the
//     content the agent wrote (newCode), it was modified intentionally
//     after the snapshot — return false (stale). Empty or redacted
//     newCode, or a missing file, means there's no baseline to compare
//     against, so the content check is skipped (return true).
//
//  2. Git-awareness (NEW): even when disk == newCode, the agent's edit
//     may have since been committed to git. Writing OriginalCode back
//     would silently undo committed, version-controlled work. If the
//     working-tree copy matches HEAD (committed, clean), return false
//     (protected). A git error (e.g. not a repo, or untracked file)
//     means no git protection applies, so the content check alone
//     decides — return true.
//
// The function never blocks legitimate reverts: outside a git repo, on
// untracked files, or when the file has uncommitted modifications, the
// content check is the sole authority.
func IsRevertSafe(filename, newCode string) bool {
	return IsRevertSafeWithOriginal(filename, newCode, "")
}

// IsRevertSafeWithOriginal is the full-aware staleness guard used by
// recovery paths that have the OriginalCode (the content to be written
// back). The original-aware path allows recovery when the file on disk
// matches HEAD (a destructive git command aligned it to HEAD) but the
// OriginalCode is NOT the HEAD content — meaning the original was
// uncommitted work that the destructive command destroyed. Restoring
// it does NOT undo committed work; it restores destroyed work.
func IsRevertSafeWithOriginal(filename, newCode, originalCode string) bool {
	// 1. Empty or redacted newCode: no baseline to compare against.
	//    Allow (matches the historical isFileStale behaviour).
	if newCode == "" || newCode == RedactedContentMarker {
		return true
	}
	// 2. Read disk content. A missing file means the revert is
	//    restoring/creating it — safe.
	current, err := os.ReadFile(filename)
	if err != nil {
		return true // file doesn't exist — safe to restore/create
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
	committed, gitErr := git.IsFileContentCommitted(filename)
	if gitErr != nil {
		// git check failed (e.g. transient error) — fall back to the
		// conservative content-only behaviour so we don't block a
		// revert the user may genuinely want.
		return true
	}
	if committed {
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
// BOTH the pre-agent state (originalCode) and the agent's edit
// (newCode). It is the restore counterpart of isFileStale.
//
// The restore operation writes newCode back to disk. It is safe when
// the disk currently holds either originalCode (the agent's change was
// rolled back, so restoring re-applies it) or newCode itself (already
// in the target state — a no-op write). It is UNSAFE when the disk
// holds neither — someone modified the file intentionally after the
// snapshot (git commit, another session, manual edit), and restoring
// would silently clobber that work.
//
// Returns false (not stale / safe to restore) when:
//   - The file doesn't exist on disk (restore is creating it)
//   - newCode is empty (no baseline to compare — nothing to restore)
//   - newCode is the redacted marker (can't compare)
//   - The disk content matches originalCode (rolled-back state)
//   - The disk content matches newCode (already in target state)
//
// Returns true (stale / skip restore) when the disk content matches
// neither originalCode nor newCode.
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
// into a single entry, keeping the earliest OriginalCode (the true
// pre-session state for rollback) and the latest NewCode (the current
// intended state for staleness comparison and restore). The latest
// FileRevisionHash is kept so status updates target the most recent
// change record.
//
// Without deduplication, a file edited twice (v0→v1, then v1→v2)
// produces two change records. Rollback's staleness check on the first
// (NewCode=v1) would see disk=v2 and skip, while the second writes
// OriginalCode=v1 — an intermediate state, not the true original v0.
// Dedup ensures rollback sees one entry (OriginalCode=v0, NewCode=v2)
// and correctly restores v0.
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

	// Deduplicate by filename: keep the earliest OriginalCode (true
	// pre-session state) and the latest NewCode (current disk baseline
	// for staleness comparison). Without this, a file edited twice
	// (v0→v1, then v1→v2) produces two change records; the first one's
	// staleness check sees disk=v2 ≠ NewCode=v1 and skips, while the
	// second writes OriginalCode=v1 (an intermediate state, not the
	// true original v0).
	deduped := dedupChangesByFilename(getActiveChanges(group.Changes))
	for _, change := range deduped {
		// Skip files with redacted content (external files)
		if change.OriginalCode == RedactedContentMarker {
			fmt.Printf("  Skipping %s: content was redacted (external file)\n", change.Filename)
			continue
		}

		// Safety check: never write to files outside the current workspace.
		// The history DB may contain snapshots of files that were later moved
		// or committed elsewhere; blindly restoring them would clobber
		// intentional changes outside this project.
		if !isWithinWorkspace(change.Filename) {
			fmt.Printf("  Skipping %s: outside current workspace (safety check)\n", change.Filename)
			continue
		}

		// Staleness guard: if the file on disk no longer matches what the
		// agent wrote (NewCode), it was modified intentionally after this
		// snapshot — by a git commit, another session, or manual edit.
		// Rolling it back would silently clobber that change.
		//
		// IsRevertSafe additionally applies git-awareness: even when disk
		// matches NewCode, if that content has been committed to git HEAD
		// (the work is now version-controlled), the revert is refused so it
		// can't silently undo committed work.
		if !IsRevertSafeWithOriginal(change.Filename, change.NewCode, change.OriginalCode) {
			AuditRevertSkip("handleRevisionRollback", change.Filename, "stale or committed")
			fmt.Printf("  Skipping %s: file modified since snapshot (safety check)\n", change.Filename)
			continue
		}

		fmt.Printf("  Rolling back %s...\n", change.Filename)

		// Write content directly to avoid any encoding transformations
		// Use filesystem.WriteFileWithDir which does raw binary write
		AuditRevertWrite("handleRevisionRollback", change.Filename, "OriginalCode")
		err := filesystem.WriteFileWithDir(change.Filename, []byte(change.OriginalCode), 0644)
		if err != nil {
			return fmt.Errorf("failed to rollback %s: %w", change.Filename, err)
		}
		if err := updateChangeStatus(change.FileRevisionHash, "reverted"); err != nil {
			return fmt.Errorf("failed to update status for %s: %w", change.Filename, err)
		}
	}

	fmt.Println("Revision rollback successful.")
	return nil
}

func handleRevisionRestore(group RevisionGroup) error {
	fmt.Printf("Restoring all changes in revision %s...\n", group.RevisionID)

	// Deduplicate by filename (see handleRevisionRollback for rationale).
	deduped := dedupChangesByFilename(group.Changes)
	for _, change := range deduped {
		// Skip files with redacted content (external files)
		if change.NewCode == RedactedContentMarker {
			fmt.Printf("  Skipping %s: content was redacted (external file)\n", change.Filename)
			continue
		}

		// Safety check: never write to files outside the current workspace.
		// See handleRevisionRollback for rationale.
		if !isWithinWorkspace(change.Filename) {
			fmt.Printf("  Skipping %s: outside current workspace (safety check)\n", change.Filename)
			continue
		}

		// Staleness guard (mirrors handleRevisionRollback): if the file
		// on disk no longer matches either the pre-agent state
		// (OriginalCode) or the agent's edit (NewCode), it was modified
		// intentionally after this snapshot — by a git commit, another
		// session, or manual edit. Restoring would silently clobber that
		// change. Without this guard, restore blindly overwrites files
		// that may contain newer committed work.
		if isFileStaleForRestore(change.Filename, change.OriginalCode, change.NewCode) {
			AuditRevertSkip("handleRevisionRestore", change.Filename, "stale")
			fmt.Printf("  Skipping %s: file modified since snapshot (safety check)\n", change.Filename)
			continue
		}

		fmt.Printf("  Restoring %s...\n", change.Filename)

		// Write content directly to avoid any encoding transformations
		AuditRevertWrite("handleRevisionRestore", change.Filename, "NewCode")
		err := filesystem.WriteFileWithDir(change.Filename, []byte(change.NewCode), 0644)
		if err != nil {
			return fmt.Errorf("failed to restore %s: %w", change.Filename, err)
		}

		// Update status to restored regardless of previous status
		if err := updateChangeStatus(change.FileRevisionHash, "restored"); err != nil {
			return fmt.Errorf("failed to update status for %s: %w", change.Filename, err)
		}
	}

	fmt.Println("Revision restore successful.")
	return nil
}
