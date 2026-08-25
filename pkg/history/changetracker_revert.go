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
		// agent wrote (NewCode), it was modified after this snapshot.
		// IsRevertSafe additionally applies git-awareness.
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

		// Staleness guard: if the file on disk no longer matches either
		// the pre-agent state or the agent's edit, it was modified after
		// the snapshot — restoring would silently clobber that change.
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
