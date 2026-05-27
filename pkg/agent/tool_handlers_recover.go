// recover_file tool: restores a file's original content from the
// ChangeTracker's session buffer. Closes the loop between "we
// captured original bytes" and "user/agent can put them back".
//
// Selection rules:
//   - Most-recent matching change for `path` wins (the tracker
//     records changes in append order).
//   - The change must have a recoverable OriginalCode (non-empty,
//     not the redacted sentinel, not the path-only sentinel).
//   - For "create" entries (no original existed), recovery is a
//     delete: removing a created file restores the workspace to
//     pre-creation state.
//
// Safety:
//   - Refuses paths outside the workspace root (no cross-workspace
//     restores).
//   - Refuses when the file would resolve to a directory or symlink
//     target.
//   - Returns a structured JSON result so the LLM can reason about
//     success vs. why-it-couldn't.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func handleRecoverFile(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	rawPath, ok := args["path"].(string)
	if !ok || rawPath == "" {
		return "", fmt.Errorf("recover_file: 'path' parameter is required")
	}

	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return jsonRecoverResult(false, rawPath, "", "change tracking is disabled — nothing to recover from"), nil
	}

	abs, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("recover_file: resolve %q: %w", rawPath, err)
	}

	// Walk changes in REVERSE order so the most recent entry for the
	// path wins (the tracker is append-only, so the last write to a
	// given path is the canonical "current" state we want to undo).
	changes := tracker.GetChanges()
	var match *TrackedFileChange
	for i := len(changes) - 1; i >= 0; i-- {
		candidatePath, candidateErr := filepath.Abs(changes[i].FilePath)
		if candidateErr != nil {
			continue
		}
		if candidatePath == abs {
			match = &changes[i]
			break
		}
	}
	if match == nil {
		return jsonRecoverResult(false, abs, "", "no tracked change recorded for this path"), nil
	}

	// "create" with no original → recovery is delete.
	if match.Operation == "create" {
		if removeErr := os.Remove(abs); removeErr != nil && !os.IsNotExist(removeErr) {
			return jsonRecoverResult(false, abs, "delete", fmt.Sprintf("unable to remove created file: %v", removeErr)), nil
		}
		tracker.SyncShellCacheForPath(abs)
		return jsonRecoverResult(true, abs, "delete", fmt.Sprintf("removed file created via %s", match.ToolCall)), nil
	}

	if !isRecoverableOriginal(match.OriginalCode) {
		reason := "original content was not captured (file too large, binary, or outside workspace)"
		return jsonRecoverResult(false, abs, "", reason), nil
	}

	// Refuse to overwrite a directory at this path.
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		return jsonRecoverResult(false, abs, "", "path is a directory — refusing to overwrite"), nil
	}

	// Ensure parent directory exists (the file may have been deleted
	// along with its parent in a `rm -rf` scenario).
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		return jsonRecoverResult(false, abs, "", fmt.Sprintf("unable to create parent dir: %v", mkErr)), nil
	}

	if writeErr := os.WriteFile(abs, []byte(match.OriginalCode), 0o644); writeErr != nil {
		return jsonRecoverResult(false, abs, "", fmt.Sprintf("write failed: %v", writeErr)), nil
	}

	tracker.SyncShellCacheForPath(abs)
	verb := "restored"
	if match.Operation == "delete" {
		verb = "un-deleted"
	}
	msg := fmt.Sprintf("%s file from session buffer (was: %s via %s)", verb, match.Operation, match.ToolCall)
	return jsonRecoverResult(true, abs, verb, msg), nil
}

// jsonRecoverResult formats the tool's structured result so the LLM
// can reason about success/failure and present a coherent reply.
func jsonRecoverResult(ok bool, path, action, message string) string {
	payload := struct {
		Recovered bool   `json:"recovered"`
		Path      string `json:"path"`
		Action    string `json:"action,omitempty"`
		Message   string `json:"message"`
	}{Recovered: ok, Path: path, Action: action, Message: message}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b)
}
