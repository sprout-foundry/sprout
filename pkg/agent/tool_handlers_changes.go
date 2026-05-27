// Agent-facing tool that returns a manifest of files the change tracker
// has observed this session. Lets the model self-audit before reporting
// completion, generate accurate commit messages, or distinguish its own
// edits from prior working-tree state.
//
// Output format is a JSON object (one tool call, predictable parse) with
// a `files` array and metadata. Each file entry carries: path, op (one
// of "create" / "edit" / "delete"), tool (the originating tool name),
// and a `recoverable` flag indicating whether the tracker has the
// original bytes available for a recovery operation. Recovery is only
// possible for files whose original content was captured at change
// time — shell-snapshot path-only entries (too large / binary / outside
// workspace) report `recoverable: false`.
package agent

import (
	"context"
	"encoding/json"
)

func handleListChanges(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	tracker := a.GetChangeTracker()
	if tracker == nil || !tracker.IsEnabled() {
		return `{"revision_id":"","enabled":false,"count":0,"files":[]}`, nil
	}

	changes := tracker.GetChanges()
	type fileEntry struct {
		Path        string `json:"path"`
		Op          string `json:"op"`
		Tool        string `json:"tool"`
		Recoverable bool   `json:"recoverable"`
	}
	files := make([]fileEntry, 0, len(changes))
	for _, ch := range changes {
		files = append(files, fileEntry{
			Path:        ch.FilePath,
			Op:          ch.Operation,
			Tool:        ch.ToolCall,
			Recoverable: isRecoverableOriginal(ch.OriginalCode),
		})
	}

	out := struct {
		RevisionID string      `json:"revision_id"`
		Enabled    bool        `json:"enabled"`
		Count      int         `json:"count"`
		Files      []fileEntry `json:"files"`
	}{
		RevisionID: tracker.GetRevisionID(),
		Enabled:    true,
		Count:      len(files),
		Files:      files,
	}

	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// isRecoverableOriginal reports whether a TrackedFileChange.OriginalCode
// value represents real recoverable content. The tracker uses three
// "non-content" sentinels: empty string (for created files — no
// original existed), the redacted marker (external workspace), and the
// "[CONTENT NOT CAPTURED: …]" prefix (shell snapshot filtered out).
func isRecoverableOriginal(original string) bool {
	if original == "" {
		return false
	}
	if original == RedactedContentMarker {
		return false
	}
	if len(original) >= len("[CONTENT NOT CAPTURED:") &&
		original[:len("[CONTENT NOT CAPTURED:")] == "[CONTENT NOT CAPTURED:" {
		return false
	}
	return true
}
