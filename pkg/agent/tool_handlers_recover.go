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
	// resolveRecoveryTarget also peers inside bulk entries so files
	// packed into a `git checkout .`-style rollup are recoverable
	// individually.
	changes := tracker.GetChanges()
	match := resolveRecoveryTarget(changes, abs)
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

// handleRecoverBulk restores every per-file item packed inside a bulk
// TrackedFileChange identified by `bulk_path` (the bulk entry's
// FilePath — for destructive bulks this is the command label like
// "shell_command"; for build bulks it's the workspace-relative dir with
// trailing "/"). Walks BulkItems in append order and applies the same
// per-file recovery logic recover_file uses.
//
// Returns a JSON envelope with per-file outcomes so the caller can
// report exactly what happened.
func handleRecoverBulk(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	rawPath, ok := args["bulk_path"].(string)
	if !ok || rawPath == "" {
		return "", fmt.Errorf("recover_bulk: 'bulk_path' parameter is required")
	}

	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return jsonRecoverBulkResult(false, rawPath, 0, 0, "change tracking is disabled — nothing to recover from", nil), nil
	}

	// Match against bulk entries by FilePath. Walk in reverse so the
	// most recent bulk with this label wins — the typical user intent
	// is "undo the LAST `git checkout .`", not the first.
	changes := tracker.GetChanges()
	var bulk *TrackedFileChange
	for i := len(changes) - 1; i >= 0; i-- {
		ch := &changes[i]
		if ch.Operation != "bulk" {
			continue
		}
		if ch.FilePath == rawPath {
			bulk = ch
			break
		}
	}
	if bulk == nil {
		return jsonRecoverBulkResult(false, rawPath, 0, 0, "no bulk change recorded with that path", nil), nil
	}
	if len(bulk.BulkItems) == 0 {
		return jsonRecoverBulkResult(false, rawPath, 0, 0, "bulk entry has no recoverable payload (was count-only due to memory cap)", nil), nil
	}

	type entry struct {
		Path    string `json:"path"`
		Action  string `json:"action"`
		Message string `json:"message,omitempty"`
		OK      bool   `json:"ok"`
	}
	results := make([]entry, 0, len(bulk.BulkItems))
	var restored, failed int
	for _, item := range bulk.BulkItems {
		// Reuse the existing per-file recovery by synthesizing a
		// TrackedFileChange and going through revertOne.
		synthesized := TrackedFileChange{
			FilePath:     item.FilePath,
			OriginalCode: item.OriginalCode,
			NewCode:      item.NewCode,
			Operation:    item.Operation,
			Timestamp:    bulk.Timestamp,
			ToolCall:     bulk.ToolCall,
		}
		action, ok, msg := revertOne(synthesized, tracker)
		results = append(results, entry{Path: item.FilePath, Action: action, OK: ok, Message: msg})
		if ok {
			restored++
		} else {
			failed++
		}
	}
	summary := fmt.Sprintf("%d restored, %d failed (bulk=%s, %d items)", restored, failed, rawPath, len(bulk.BulkItems))
	return jsonRecoverBulkResult(true, rawPath, restored, failed, summary, results), nil
}

// jsonRecoverBulkResult formats the structured result for recover_bulk.
// Shape mirrors revert_my_changes so consumers (the LLM, the WebUI)
// don't need a new envelope type.
func jsonRecoverBulkResult(found bool, bulkPath string, restored, failed int, summary string, entries any) string {
	payload := struct {
		Found    bool   `json:"found"`
		BulkPath string `json:"bulk_path"`
		Restored int    `json:"restored"`
		Failed   int    `json:"failed"`
		Summary  string `json:"summary"`
		Entries  any    `json:"entries,omitempty"`
	}{Found: found, BulkPath: bulkPath, Restored: restored, Failed: failed, Summary: summary, Entries: entries}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b)
}

// resolveRecoveryTarget finds the most recent TrackedFileChange that
// covers `abs` — either as a top-level entry or as a per-file item
// packed inside a bulk entry (e.g. the rollup produced by `git checkout
// .` in destructive mode). For bulk items the returned pointer is to a
// synthesized TrackedFileChange that carries the bulk row's Timestamp
// and ToolCall so the recovery reply still has useful provenance.
//
// Returns nil when no entry matches.
func resolveRecoveryTarget(changes []TrackedFileChange, abs string) *TrackedFileChange {
	for i := len(changes) - 1; i >= 0; i-- {
		ch := &changes[i]
		candidatePath, err := filepath.Abs(ch.FilePath)
		if err == nil && candidatePath == abs && ch.Operation != "bulk" {
			return ch
		}
		if ch.Operation != "bulk" || len(ch.BulkItems) == 0 {
			continue
		}
		for j := len(ch.BulkItems) - 1; j >= 0; j-- {
			item := ch.BulkItems[j]
			itemPath, ierr := filepath.Abs(item.FilePath)
			if ierr != nil || itemPath != abs {
				continue
			}
			synthesized := TrackedFileChange{
				FilePath:     item.FilePath,
				OriginalCode: item.OriginalCode,
				NewCode:      item.NewCode,
				Operation:    item.Operation,
				Timestamp:    ch.Timestamp,
				ToolCall:     ch.ToolCall,
			}
			return &synthesized
		}
	}
	return nil
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
