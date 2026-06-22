package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandleRecoverFile_RefusesOutOfWorkspaceWrite verifies the C1 fix:
// recover_file must NOT write to a path outside the workspace root. A
// crafted tracker entry whose FilePath escapes the workspace must be
// refused with recovered=false, and the external file must remain
// untouched on disk.
func TestHandleRecoverFile_RefusesOutOfWorkspaceWrite(t *testing.T) {
	ws := t.TempDir()

	// External target under the user's home dir (NOT /tmp, which the
	// workspace check treats as inside). Build a sibling temp dir so
	// the path is real but definitely outside ws.
	externalDir := t.TempDir()
	externalTarget := filepath.Join(externalDir, "victim.txt")
	// Pre-existing content that must survive (i.e. not be overwritten).
	if err := os.WriteFile(externalTarget, []byte("original-external"), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	tracker := &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{
				// The tracker LIES: it claims to hold the original
				// content for the external path. Without the boundary
				// check, recover_file would overwrite victim.txt.
				FilePath:     externalTarget,
				OriginalCode: "PWNED",
				Operation:    "delete",
				ToolCall:     "shell_command",
			},
		},
	}
	a := &Agent{changeTracker: tracker, workspaceRoot: ws}
	tracker.agent = a

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": externalTarget})
	if err != nil {
		t.Fatalf("handleRecoverFile returned error: %v", err)
	}

	var res struct {
		Recovered bool   `json:"recovered"`
		Message   string `json:"message"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("output not JSON: %v\n%s", jsonErr, out)
	}
	if res.Recovered {
		t.Errorf("expected recovered=false for out-of-workspace path; got %s", out)
	}

	// The external file must be UNTOUCHED.
	got, _ := os.ReadFile(externalTarget)
	if string(got) != "original-external" {
		t.Errorf("external file was modified! got %q (want %q) — boundary check failed", got, "original-external")
	}
}

// TestHandleRecoverFile_RefusesOutOfWorkspaceDelete verifies the C1 fix
// for the "create" recovery branch (which deletes the file). A created
// file outside the workspace must NOT be deleted by recover_file.
func TestHandleRecoverFile_RefusesOutOfWorkspaceDelete(t *testing.T) {
	ws := t.TempDir()

	externalDir := t.TempDir()
	externalTarget := filepath.Join(externalDir, "created_outside.txt")
	if err := os.WriteFile(externalTarget, []byte("should-survive"), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	tracker := &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{
				// Claims this external file was "created" this session,
				// so recovery would try to os.Remove it.
				FilePath:  externalTarget,
				Operation: "create",
				ToolCall:  "shell_command",
			},
		},
	}
	a := &Agent{changeTracker: tracker, workspaceRoot: ws}
	tracker.agent = a

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": externalTarget})
	if err != nil {
		t.Fatalf("handleRecoverFile returned error: %v", err)
	}

	var res struct {
		Recovered bool `json:"recovered"`
	}
	_ = json.Unmarshal([]byte(out), &res)
	if res.Recovered {
		t.Errorf("expected recovered=false for out-of-workspace create-delete; got %s", out)
	}

	// The external file must still exist.
	if _, statErr := os.Stat(externalTarget); os.IsNotExist(statErr) {
		t.Errorf("external file was deleted! boundary check failed for create-recovery path")
	}
}

// TestHandleRecoverFile_InWorkspaceStillWorks is a regression test: the
// C1 boundary check must NOT break legitimate in-workspace recovery.
func TestHandleRecoverFile_InWorkspaceStillWorks(t *testing.T) {
	ws := t.TempDir()
	inFile := filepath.Join(ws, "config.toml")

	tracker := &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{
				FilePath:     inFile,
				OriginalCode: "port = 8080\n",
				Operation:    "delete",
				ToolCall:     "shell_command",
			},
		},
	}
	a := &Agent{changeTracker: tracker, workspaceRoot: ws}
	tracker.agent = a

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": inFile})
	if err != nil {
		t.Fatalf("handleRecoverFile returned error: %v", err)
	}

	var res struct {
		Recovered bool `json:"recovered"`
	}
	_ = json.Unmarshal([]byte(out), &res)
	if !res.Recovered {
		t.Fatalf("in-workspace recovery should succeed; got %s", out)
	}

	got, _ := os.ReadFile(inFile)
	if string(got) != "port = 8080\n" {
		t.Errorf("in-workspace file should be restored; got %q", got)
	}
}

// TestHandleRecoverFile_RefusesRedactedMarkerWrite is a defense-in-depth
// test for the M1 fix: even if a future code path sets the redacted
// marker as OriginalCode and bypasses isRecoverableOriginal, the final
// guard immediately before os.WriteFile must refuse to persist the
// literal marker string to disk. (isRecoverableOriginal already rejects
// the marker; this documents and locks the write-site invariant.)
func TestHandleRecoverFile_RefusesRedactedMarkerWrite(t *testing.T) {
	ws := t.TempDir()
	inFile := filepath.Join(ws, "redacted.txt")

	tracker := &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{
				// Malformed entry: the marker should never be stored as
				// recoverable content, but if it ever is, recovery must
				// refuse rather than writing "[REDACTED - external file]"
				// into the user's file.
				FilePath:     inFile,
				OriginalCode: RedactedContentMarker,
				Operation:    "delete",
				ToolCall:     "shell_command",
			},
		},
	}
	a := &Agent{changeTracker: tracker, workspaceRoot: ws}
	tracker.agent = a

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": inFile})
	if err != nil {
		t.Fatalf("handleRecoverFile returned error: %v", err)
	}

	var res struct {
		Recovered bool   `json:"recovered"`
		Message   string `json:"message"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("output not JSON: %v\n%s", jsonErr, out)
	}
	if res.Recovered {
		t.Errorf("expected recovered=false for redacted marker content; got %s", out)
	}

	// The file must NOT contain the marker string. (It may not exist at
	// all, which is also acceptable — the point is the marker is never
	// written.)
	if content, statErr := os.ReadFile(inFile); statErr == nil {
		if string(content) == RedactedContentMarker {
			t.Errorf("redacted marker was written to disk! file contains the marker string")
		}
	}
}

// TestHandleRevertMyChanges_SkipsOutOfWorkspace verifies the C1 fix for
// revert_my_changes: out-of-workspace entries are reported as skipped
// (not failures), and their content is NOT written. In-workspace
// siblings are still reverted successfully.
func TestHandleRevertMyChanges_SkipsOutOfWorkspace(t *testing.T) {
	ws := t.TempDir()

	// In-workspace file that should be reverted.
	inFile := filepath.Join(ws, "inside.txt")
	if err := os.WriteFile(inFile, []byte("AFTER"), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	// External file that must NOT be touched.
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "outside.txt")
	if err := os.WriteFile(externalFile, []byte("KEEP-ME"), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	a := &Agent{
		changeTracker: &ChangeTracker{
			enabled: true,
			changes: []TrackedFileChange{
				{FilePath: inFile, Operation: "edit", ToolCall: "edit_file", OriginalCode: "BEFORE"},
				// A stray tracker entry for an external path.
				{FilePath: externalFile, Operation: "edit", ToolCall: "shell_command", OriginalCode: "SHOULD-NOT-WRITE"},
			},
		},
		workspaceRoot: ws,
	}
	a.changeTracker.agent = a

	out, err := handleRevertMyChanges(context.Background(), a, map[string]interface{}{"scope": "all"})
	if err != nil {
		t.Fatalf("handleRevertMyChanges: %v", err)
	}

	var res struct {
		Restored int `json:"restored"`
		Failed   int `json:"failed"`
		Entries  []struct {
			Path    string `json:"path"`
			Message string `json:"message"`
			OK      bool   `json:"ok"`
		} `json:"entries"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("output not JSON: %v\n%s", jsonErr, out)
	}

	// In-workspace file should be restored.
	if res.Restored != 1 {
		t.Errorf("expected 1 restored (in-workspace only), got %d: %s", res.Restored, out)
	}
	got, _ := os.ReadFile(inFile)
	if string(got) != "BEFORE" {
		t.Errorf("in-workspace file should be reverted to BEFORE; got %q", got)
	}

	// External file must be untouched.
	got, _ = os.ReadFile(externalFile)
	if string(got) != "KEEP-ME" {
		t.Errorf("external file was modified! got %q (want %q) — boundary check failed", got, "KEEP-ME")
	}

	// The external entry should appear as a skipped entry, not a silent success.
	foundSkipped := false
	for _, e := range res.Entries {
		if e.Path == externalFile && !e.OK && strings.Contains(e.Message, "outside the workspace") {
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Errorf("expected an entry reporting the external path as skipped/outside-workspace; got %+v", res.Entries)
	}
}
