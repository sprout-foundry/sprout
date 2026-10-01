package agent

// workspace_sync_ops.go — the workspace sync-op apply layer: the SyncOp
// / SyncOpResult types, the workspace-path resolution (resolveWorkspacePath),
// and the Agent apply / batch / status methods (ApplySyncOp,
// applySyncOpInternal, ApplySyncOpBatch, GetSyncStatus). Split out of
// workspace_sync.go.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// SyncOp represents a single file operation sent from the browser to the
// container as part of the workspace sync protocol.
type SyncOp struct {
	OpType     string `json:"op_type"`     // "write", "delete", or "rename"
	Path       string `json:"path"`        // Target file path (relative to workspace root)
	Content    string `json:"content"`     // For write ops: the file content
	NewPath    string `json:"new_path"`    // For rename ops: the destination path
	BrowserSeq int64  `json:"browser_seq"` // Monotonically increasing browser-side seq number
	Timestamp  int64  `json:"timestamp"`   // Unix milliseconds when the op was created
}

// SyncOpResult is the server response to a SyncOp application.
type SyncOpResult struct {
	Accepted     bool   `json:"accepted"`        // Whether the op was applied
	ConflictPath string `json:"conflict_path"`   // Set if there's a container-side conflict (path to .theirs file)
	ContainerSeq int64  `json:"container_seq"`   // Current container sequence after applying
	Error        string `json:"error,omitempty"` // Error message if not accepted
}

// resolveWorkspacePath joins workspaceRoot with relPath, resolves symlinks,
// and verifies the result is within workspaceRoot. Returns an error if
// path traversal is attempted.
func resolveWorkspacePath(workspaceRoot, relPath string) (string, error) {
	if relPath == "" {
		return "", agenterrors.NewValidation("relative path must not be empty", nil)
	}

	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", agenterrors.Wrap(err, "resolve workspace root")
	}

	// Resolve symlinks in workspace root for consistent path resolution and
	// comparison. Without this, on platforms where /tmp → /private/tmp,
	// paths joined against absRoot would fail the Rel() check against
	// the symlink-resolved path.
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		resolvedRoot = absRoot
	}

	resolved := filepath.Join(resolvedRoot, relPath)

	// Try to resolve symlinks in the full path.
	evaluated, evalErr := filepath.EvalSymlinks(resolved)
	if evalErr != nil {
		if os.IsNotExist(evalErr) {
			// File doesn't exist yet; resolve parent directory symlinks instead.
			// NOTE: when EvalSymlinks fails it returns an empty string, so we
			// MUST use `resolved` (the pre-evaluation path) for parent/base ops.
			parent := filepath.Dir(resolved)
			resolvedParent, perr := filepath.EvalSymlinks(parent)
			if perr == nil {
				resolved = filepath.Join(resolvedParent, filepath.Base(resolved))
			} else {
				// Parent also doesn't exist (e.g. deeply nested new file).
				// Fall back to the original joined path — it's still safe because
				// resolvedRoot was resolved above and the join uses relPath directly.
				resolved = filepath.Join(resolvedRoot, relPath)
			}
		} else {
			return "", agenterrors.Wrap(evalErr, "resolve path")
		}
	} else {
		resolved = evaluated
	}

	// Verify the resolved path is within the workspace root
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", agenterrors.NewValidation(fmt.Sprintf("path traversal attempted: %q is outside workspace root %q", resolved, resolvedRoot), nil)
	}

	return resolved, nil
}

// applySyncMu serializes all ApplySyncOp and ApplySyncOpBatch calls to
// prevent TOCTOU races between the stat/check/apply phases.
var applySyncMu sync.Mutex

// ApplySyncOp applies a single SyncOp to the workspace filesystem.
// It validates the operation, checks for conflicts with container-side
// changes, applies the change, and updates the file metadata.
func (a *Agent) ApplySyncOp(op SyncOp, workspaceRoot string) SyncOpResult {
	if a == nil {
		return SyncOpResult{
			Accepted: false,
			Error:    "agent is nil",
		}
	}
	applySyncMu.Lock()
	defer applySyncMu.Unlock()
	return a.applySyncOpInternal(op, workspaceRoot)
}

// applySyncOpInternal is the unlocked core of ApplySyncOp. It contains all
// the validation, conflict detection, and filesystem work. Callers are
// responsible for holding applySyncMu (ApplySyncOp does this individually,
// ApplySyncOpBatch holds it across the whole batch).
func (a *Agent) applySyncOpInternal(op SyncOp, workspaceRoot string) SyncOpResult {

	// 1. Validate op type
	switch op.OpType {
	case "write", "delete", "rename":
	default:
		return SyncOpResult{
			Accepted: false,
			Error:    fmt.Sprintf("invalid op_type %q: must be write, delete, or rename", op.OpType),
		}
	}

	// 2. Validate path is non-empty
	if op.Path == "" {
		return SyncOpResult{
			Accepted: false,
			Error:    "path must not be empty",
		}
	}

	// 3. Resolve file path with traversal protection
	resolvedPath, err := resolveWorkspacePath(workspaceRoot, op.Path)
	if err != nil {
		return SyncOpResult{
			Accepted: false,
			Error:    err.Error(),
		}
	}

	// 4. Get current metadata for the path
	md, hasMetadata := a.GetFileMetadata(op.Path)

	// 5. Conflict detection: if container has unsynced writes, produce a .theirs file
	if hasMetadata && md.ContainerSeq > md.LastSyncedContainer {
		// Container has writes the browser hasn't acknowledged — potential conflict.
		// Save current container content to <path>.theirs
		themsPath := resolvedPath + ".theirs"
		currentContent, readErr := os.ReadFile(resolvedPath)
		if readErr == nil {
			writeErr := os.WriteFile(themsPath, currentContent, 0644)
			if writeErr != nil {
				return SyncOpResult{
					Accepted:     false,
					ConflictPath: op.Path + ".theirs",
					Error:        fmt.Sprintf("failed to write .theirs file: %v", writeErr),
				}
			}
		}
		return SyncOpResult{
			Accepted:     false,
			ConflictPath: op.Path + ".theirs",
			ContainerSeq: md.ContainerSeq,
			Error:        fmt.Sprintf("container has unsynced writes: container_seq=%d > last_synced_container=%d", md.ContainerSeq, md.LastSyncedContainer),
		}
	}

	// 6. Apply the operation
	switch op.OpType {
	case "write":
		parentDir := filepath.Dir(resolvedPath)
		if mkdirErr := os.MkdirAll(parentDir, 0755); mkdirErr != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to create parent directories: %v", mkdirErr),
			}
		}
		if writeErr := os.WriteFile(resolvedPath, []byte(op.Content), 0644); writeErr != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to write file: %v", writeErr),
			}
		}

	case "delete":
		if removeErr := os.Remove(resolvedPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to delete file: %v", removeErr),
			}
		}
		// Remove metadata for deleted file
		a.fileMetadata.set(op.Path, WorkspaceFileMetadata{})
		return SyncOpResult{
			Accepted:     true,
			ContainerSeq: 0,
		}

	case "rename":
		if op.NewPath == "" {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        "new_path must not be empty for rename operation",
			}
		}
		resolvedNewPath, err := resolveWorkspacePath(workspaceRoot, op.NewPath)
		if err != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        err.Error(),
			}
		}
		// Create parent directories for destination
		parentDir := filepath.Dir(resolvedNewPath)
		if mkdirErr := os.MkdirAll(parentDir, 0755); mkdirErr != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to create parent directories for rename target: %v", mkdirErr),
			}
		}
		if renameErr := os.Rename(resolvedPath, resolvedNewPath); renameErr != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to rename file: %v", renameErr),
			}
		}
		// Move metadata from old path to new path
		a.fileMetadata.set(op.Path, WorkspaceFileMetadata{}) // clear old
		op.Path = op.NewPath                                 // update key for subsequent metadata update
	}

	// 7. Update metadata
	md.BrowserSeq = op.BrowserSeq
	md.LastSyncedBrowser = op.BrowserSeq
	md.ContainerSeq++
	md.ModifiedAt = time.Now()
	a.SetFileMetadata(op.Path, md)

	return SyncOpResult{
		Accepted:     true,
		ContainerSeq: md.ContainerSeq,
	}
}

// ApplySyncOpBatch applies a slice of SyncOps in order, collecting results.
// Stops on the first conflict, returning Accepted=false for remaining ops.
func (a *Agent) ApplySyncOpBatch(ops []SyncOp, workspaceRoot string) []SyncOpResult {
	if a == nil {
		results := make([]SyncOpResult, len(ops))
		for i := range results {
			results[i] = SyncOpResult{
				Accepted: false,
				Error:    "agent is nil",
			}
		}
		return results
	}
	applySyncMu.Lock()
	defer applySyncMu.Unlock()

	results := make([]SyncOpResult, 0, len(ops))

	for _, op := range ops {
		result := a.applySyncOpInternal(op, workspaceRoot)
		results = append(results, result)
		if !result.Accepted {
			// Remaining ops get Accepted=false
			for i := len(ops) - len(results); i > 0; i-- {
				results = append(results, SyncOpResult{
					Accepted: false,
					Error:    "skipped due to earlier conflict in batch",
				})
			}
			break
		}
	}

	return results
}

// GetSyncStatus returns a map of path → WorkspaceFileMetadata for all
// currently tracked files in the workspace metadata store.
func (a *Agent) GetSyncStatus() map[string]WorkspaceFileMetadata {
	if a == nil {
		return nil
	}

	a.fileReadsMu.Lock()
	store := a.fileMetadata
	a.fileReadsMu.Unlock()

	if store == nil {
		return nil
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	result := make(map[string]WorkspaceFileMetadata, len(store.m))
	for path, md := range store.m {
		result[path] = md
	}

	return result
}
