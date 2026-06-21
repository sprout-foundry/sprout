// recover_file tool: restores a file's tracked content from the
// ChangeTracker's session buffer. Closes the loop between "we captured
// original bytes" and "user/agent can put them back".
//
// The SP-061-2 consolidation rolled three behaviours into one tool via
// the `scope` argument:
//
//   - scope="latest"        (default) Restore the file to the state
//                           immediately before its most-recent tracked
//                           change. The historical recover_file shape.
//
//   - scope="session_start" Restore to the EARLIEST captured original —
//                           the file as it was before the agent touched
//                           it at all this session. Replaces the
//                           revert_my_changes(file=…) scope.
//
//   - scope="bulk"          Treat `path` as a bulk entry's FilePath (a
//                           command label like "git checkout ." or a
//                           dir like "webui/src/"). Walks the entry's
//                           BulkItems and restores every packed file.
//                           Replaces the standalone recover_bulk tool.
//
// Selection rules:
//   - Most-recent matching change for `path` wins for scope="latest"
//     (the tracker records changes in append order).
//   - Earliest matching change wins for scope="session_start".
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
	"strings"
)

func handleRecoverFile(_ context.Context, a *Agent, args map[string]interface{}) (string, error) {
	rawPath, ok := args["path"].(string)
	if !ok || rawPath == "" {
		return "", fmt.Errorf("recover_file: 'path' parameter is required")
	}
	scope := strings.TrimSpace(asString(args["scope"]))
	if scope == "" {
		scope = "latest"
	}

	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return jsonRecoverResult(false, rawPath, "", "change tracking is disabled — nothing to recover from"), nil
	}

	if scope == "bulk" {
		return recoverBulk(a, rawPath)
	}

	abs, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("recover_file: resolve %q: %w", rawPath, err)
	}

	changes := tracker.GetChanges()
	var match *TrackedFileChange
	switch scope {
	case "latest":
		// Walk reverse so the most recent entry for the path wins; also
		// peer inside bulk entries so paths packed into a destructive
		// rollup are recoverable individually.
		match = resolveRecoveryTarget(changes, abs)
	case "session_start":
		// Earliest entry's OriginalCode is the pre-session state.
		// Bulk entries are still scanned (an earliest single-file entry
		// always beats a later bulk for the same path).
		match = resolveEarliestRecoveryTarget(changes, abs)
	default:
		return "", fmt.Errorf("recover_file: unknown scope %q (want 'latest', 'session_start', or 'bulk')", scope)
	}
	if match == nil {
		return jsonRecoverResult(false, abs, "", "no tracked change recorded for this path"), nil
	}

	// C1: Refuse to touch anything outside the workspace root. A crafted
	// or mis-recorded tracker entry (e.g. a shell-mutation diff that
	// escaped the workspace) must not be able to write or delete files
	// in arbitrary directories via the recovery path. The doc comment
	// has always claimed this guarantee; this enforces it.
	if a.IsPathOutsideWorkspace(abs) {
		return jsonRecoverResult(false, abs, "", "path is outside the workspace — refusing cross-workspace restore"), nil
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

// recoverBulk handles scope="bulk": treats `bulkPath` as the FilePath
// of a bulk TrackedFileChange and restores every BulkItem in it.
func recoverBulk(a *Agent, bulkPath string) (string, error) {
	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return jsonRecoverBulkResult(false, bulkPath, 0, 0, "change tracking is disabled — nothing to recover from", nil), nil
	}
	changes := tracker.GetChanges()
	var bulk *TrackedFileChange
	for i := len(changes) - 1; i >= 0; i-- {
		ch := &changes[i]
		if ch.Operation != "bulk" {
			continue
		}
		if ch.FilePath == bulkPath {
			bulk = ch
			break
		}
	}
	if bulk == nil {
		return jsonRecoverBulkResult(false, bulkPath, 0, 0, "no bulk change recorded with that path", nil), nil
	}
	if len(bulk.BulkItems) == 0 {
		return jsonRecoverBulkResult(false, bulkPath, 0, 0, "bulk entry has no recoverable payload (was count-only due to memory cap)", nil), nil
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
		action, ok, msg := a.revertOne(synthesized)
		results = append(results, entry{Path: item.FilePath, Action: action, OK: ok, Message: msg})
		if ok {
			restored++
		} else {
			failed++
		}
	}
	summary := fmt.Sprintf("%d restored, %d failed (bulk=%s, %d items)", restored, failed, bulkPath, len(bulk.BulkItems))
	return jsonRecoverBulkResult(true, bulkPath, restored, failed, summary, results), nil
}

// resolveRecoveryTarget finds the most recent TrackedFileChange that
// covers `abs` — either as a top-level entry or as a per-file item
// packed inside a bulk entry. For bulk items the returned pointer is to
// a synthesized TrackedFileChange that carries the bulk row's Timestamp
// and ToolCall so the recovery reply still has useful provenance.
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

// resolveEarliestRecoveryTarget is the scope="session_start" sibling of
// resolveRecoveryTarget — it walks changes in append order so the FIRST
// matching entry wins (the truest pre-session state). Bulk items count
// as candidates too; the earliest individual entry — bulk-packed or
// otherwise — for the path is the answer.
func resolveEarliestRecoveryTarget(changes []TrackedFileChange, abs string) *TrackedFileChange {
	for i, ch := range changes {
		candidatePath, err := filepath.Abs(ch.FilePath)
		if err == nil && candidatePath == abs && ch.Operation != "bulk" {
			return &changes[i]
		}
		if ch.Operation != "bulk" || len(ch.BulkItems) == 0 {
			continue
		}
		for _, item := range ch.BulkItems {
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

// jsonRecoverBulkResult formats the structured result for scope="bulk".
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
