package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Staleness guard tests
//
// These tests verify the staleness guard that prevents recover_file and
// revert_my_changes from silently clobbering files modified after the
// agent's snapshot (by git commits, manual edits, or other sessions).
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// handleRecoverFile — staleness guard
// ---------------------------------------------------------------------------

func TestRecoverFile_SkipsStaleFile(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

	ct := NewChangeTracker(agent, "test instruction")
	agent.changeTracker = ct

	// 1. Create the file with original content ("v1").
	filePath := filepath.Join(ws, "target.go")
	originalContent := "v1\n"
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	// 2. Track the edit: original="v1", new="v2".
	newContent := "v2\n"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	// 3. Simulate the agent actually writing "v2" to disk.
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		t.Fatalf("write new content: %v", err)
	}

	// 4. Simulate external modification (git commit, manual edit, etc.)
	//    — overwrite with "v3" so disk != NewCode.
	externalContent := "v3\n"
	if err := os.WriteFile(filePath, []byte(externalContent), 0644); err != nil {
		t.Fatalf("write external content: %v", err)
	}

	// 5. Attempt recovery — the staleness guard should refuse.
	result, err := handleRecoverFile(context.Background(), agent, map[string]interface{}{
		"path": filePath,
	})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}

	// Parse JSON result.
	var res struct {
		Recovered bool   `json:"recovered"`
		Path      string `json:"path"`
		Action    string `json:"action"`
		Message   string `json:"message"`
	}
	if parseErr := json.Unmarshal([]byte(result), &res); parseErr != nil {
		t.Fatalf("parse result JSON: %v (got: %s)", parseErr, result)
	}

	if res.Recovered {
		t.Errorf("expected recovered=false (stale skip), got true")
	}
	if res.Action != "stale_skip" {
		t.Errorf("expected action=stale_skip, got %q", res.Action)
	}

	// The file on disk must STILL contain "v3" — it was NOT clobbered.
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != externalContent {
		t.Errorf("file was clobbered! expected %q, got %q", externalContent, string(content))
	}
}

func TestRecoverFile_ProceedsWhenNotStale(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

	ct := NewChangeTracker(agent, "test instruction")
	agent.changeTracker = ct

	// 1. Create the file with original content ("v1").
	filePath := filepath.Join(ws, "target.go")
	originalContent := "v1\n"
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	// 2. Track the edit: original="v1", new="v2".
	newContent := "v2\n"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	// 3. Simulate the agent actually writing "v2" to disk.
	//    Disk matches NewCode → NOT stale → recovery should proceed.
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		t.Fatalf("write new content: %v", err)
	}

	// 4. Attempt recovery — should succeed and restore "v1".
	result, err := handleRecoverFile(context.Background(), agent, map[string]interface{}{
		"path": filePath,
	})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}

	var res struct {
		Recovered bool   `json:"recovered"`
		Path      string `json:"path"`
		Action    string `json:"action"`
		Message   string `json:"message"`
	}
	if parseErr := json.Unmarshal([]byte(result), &res); parseErr != nil {
		t.Fatalf("parse result JSON: %v (got: %s)", parseErr, result)
	}

	if !res.Recovered {
		t.Errorf("expected recovered=true, got false (message: %s)", res.Message)
	}

	// The file on disk must now contain the original "v1".
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != originalContent {
		t.Errorf("expected original content %q, got %q", originalContent, string(content))
	}
}

// ---------------------------------------------------------------------------
// handleRevertMyChanges — staleness guard
// ---------------------------------------------------------------------------

func TestRevertMyChanges_SkipsStaleFile(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

	ct := NewChangeTracker(agent, "test instruction")
	agent.changeTracker = ct

	// 1. Create the file with original content ("v1").
	filePath := filepath.Join(ws, "target.go")
	originalContent := "v1\n"
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	// 2. Track the edit: original="v1", new="v2".
	newContent := "v2\n"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	// 3. Simulate the agent writing "v2" to disk.
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		t.Fatalf("write new content: %v", err)
	}

	// 4. Simulate external modification — overwrite with "v3".
	externalContent := "v3\n"
	if err := os.WriteFile(filePath, []byte(externalContent), 0644); err != nil {
		t.Fatalf("write external content: %v", err)
	}

	// 5. Attempt revert — the staleness guard should skip this file.
	result, err := handleRevertMyChanges(context.Background(), agent, map[string]interface{}{
		"scope": "all",
	})
	if err != nil {
		t.Fatalf("handleRevertMyChanges: %v", err)
	}

	var res struct {
		Restored int         `json:"restored"`
		Failed   int         `json:"failed"`
		Summary  string      `json:"summary"`
		Entries  interface{} `json:"entries,omitempty"`
	}
	if parseErr := json.Unmarshal([]byte(result), &res); parseErr != nil {
		t.Fatalf("parse result JSON: %v (got: %s)", parseErr, result)
	}

	if res.Restored != 0 {
		t.Errorf("expected 0 restored, got %d", res.Restored)
	}
	if res.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", res.Failed)
	}

	// The file on disk must STILL contain "v3".
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != externalContent {
		t.Errorf("file was clobbered! expected %q, got %q", externalContent, string(content))
	}
}

func TestRevertMyChanges_ProceedsWhenNotStale(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

	ct := NewChangeTracker(agent, "test instruction")
	agent.changeTracker = ct

	// 1. Create the file with original content ("v1").
	filePath := filepath.Join(ws, "target.go")
	originalContent := "v1\n"
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("write original: %v", err)
	}

	// 2. Track the edit: original="v1", new="v2".
	newContent := "v2\n"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	// 3. Simulate the agent writing "v2" to disk.
	//    Disk matches NewCode → NOT stale → revert should proceed.
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		t.Fatalf("write new content: %v", err)
	}

	// 4. Attempt revert — should succeed and restore "v1".
	result, err := handleRevertMyChanges(context.Background(), agent, map[string]interface{}{
		"scope": "all",
	})
	if err != nil {
		t.Fatalf("handleRevertMyChanges: %v", err)
	}

	var res struct {
		Restored int         `json:"restored"`
		Failed   int         `json:"failed"`
		Summary  string      `json:"summary"`
		Entries  interface{} `json:"entries,omitempty"`
	}
	if parseErr := json.Unmarshal([]byte(result), &res); parseErr != nil {
		t.Fatalf("parse result JSON: %v (got: %s)", parseErr, result)
	}

	if res.Restored != 1 {
		t.Errorf("expected 1 restored, got %d", res.Restored)
	}
	if res.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", res.Failed)
	}

	// The file on disk must now contain the original "v1".
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != originalContent {
		t.Errorf("expected original content %q, got %q", originalContent, string(content))
	}
}

// ---------------------------------------------------------------------------
// handleRecoverFile — create operation with staleness guard
// ---------------------------------------------------------------------------

func TestRecoverFile_CreateOp_SkipsStaleFile(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

	ct := NewChangeTracker(agent, "test instruction")
	agent.changeTracker = ct

	// 1. Track a "create" operation: file doesn't exist yet,
	//    so OriginalCode will be "" and NewCode will be "v1".
	filePath := filepath.Join(ws, "newfile.go")
	newContent := "v1\n"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	// Verify it's recorded as a create.
	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Operation != "create" {
		t.Fatalf("expected operation=create, got %q", changes[0].Operation)
	}

	// 2. Simulate the agent creating the file with "v1" on disk.
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		t.Fatalf("write new content: %v", err)
	}

	// 3. Simulate someone replacing the created file with real work ("v2").
	externalContent := "v2\n"
	if err := os.WriteFile(filePath, []byte(externalContent), 0644); err != nil {
		t.Fatalf("write external content: %v", err)
	}

	// 4. Attempt recovery — the staleness guard should refuse to delete.
	result, err := handleRecoverFile(context.Background(), agent, map[string]interface{}{
		"path": filePath,
	})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}

	var res struct {
		Recovered bool   `json:"recovered"`
		Path      string `json:"path"`
		Action    string `json:"action"`
		Message   string `json:"message"`
	}
	if parseErr := json.Unmarshal([]byte(result), &res); parseErr != nil {
		t.Fatalf("parse result JSON: %v (got: %s)", parseErr, result)
	}

	if res.Recovered {
		t.Errorf("expected recovered=false (stale skip), got true")
	}
	if res.Action != "stale_skip" {
		t.Errorf("expected action=stale_skip, got %q", res.Action)
	}

	// The file on disk must STILL exist with "v2" — it was NOT deleted.
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v (file may have been deleted!) — %v", filePath, err)
	}
	if string(content) != externalContent {
		t.Errorf("file was clobbered! expected %q, got %q", externalContent, string(content))
	}
}

// ---------------------------------------------------------------------------
// isStaleForRevert — direct unit tests
// ---------------------------------------------------------------------------

func TestIsStaleForRevert_EmptyNewCode(t *testing.T) {
	ws := t.TempDir()
	filePath := filepath.Join(ws, "file.txt")
	os.WriteFile(filePath, []byte("anything"), 0644)

	// Empty newCode → no baseline → not stale.
	if isStaleForRevert(filePath, "") {
		t.Error("empty newCode should return false (not stale)")
	}
}

func TestIsStaleForRevert_RedactedNewCode(t *testing.T) {
	ws := t.TempDir()
	filePath := filepath.Join(ws, "file.txt")
	os.WriteFile(filePath, []byte("anything"), 0644)

	// Redacted newCode → can't compare → not stale.
	if isStaleForRevert(filePath, RedactedContentMarker) {
		t.Error("redacted newCode should return false (not stale)")
	}
}

func TestIsStaleForRevert_FileDoesNotExist(t *testing.T) {
	// File doesn't exist → safe to proceed (create/delete is fine).
	if isStaleForRevert("/nonexistent/path/that/does/not/exist.txt", "some content") {
		t.Error("nonexistent file should return false (not stale)")
	}
}

func TestIsStaleForRevert_FileMatchesNewCode(t *testing.T) {
	ws := t.TempDir()
	filePath := filepath.Join(ws, "file.txt")
	content := "exact match content\n"
	os.WriteFile(filePath, []byte(content), 0644)

	// Disk content == NewCode → not stale.
	if isStaleForRevert(filePath, content) {
		t.Error("matching content should return false (not stale)")
	}
}

func TestIsStaleForRevert_FileDiffersFromNewCode(t *testing.T) {
	ws := t.TempDir()
	filePath := filepath.Join(ws, "file.txt")
	diskContent := "what's actually on disk\n"
	os.WriteFile(filePath, []byte(diskContent), 0644)

	// Disk content != NewCode → stale.
	if !isStaleForRevert(filePath, "what the agent recorded as new\n") {
		t.Error("different content should return true (stale)")
	}
}

// ---------------------------------------------------------------------------
// Multi-edit regression tests
//
// When a file is edited multiple times in one session:
//   Edit 1: v0 → v1  (OriginalCode=v0, NewCode=v1)
//   Edit 2: v1 → v2  (OriginalCode=v1, NewCode=v2)
//
// The staleness check MUST compare disk against the LATEST NewCode (v2),
// not the earliest (v1). Otherwise the agent's own second write looks
// "stale" and the revert is silently skipped.
// ---------------------------------------------------------------------------

func TestRecoverFile_MultiEdit_SessionStart_Proceeds(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

	ct := NewChangeTracker(agent, "test instruction")
	agent.changeTracker = ct

	filePath := filepath.Join(ws, "multi.go")

	// Start with v0 on disk.
	v0 := "v0-original\n"
	if err := os.WriteFile(filePath, []byte(v0), 0644); err != nil {
		t.Fatalf("write v0: %v", err)
	}

	// Edit 1: v0 → v1
	v1 := "v1-first-edit\n"
	if err := ct.TrackFileEdit(filePath, v0, v1); err != nil {
		t.Fatalf("TrackFileEdit 1: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(v1), 0644); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	// Edit 2: v1 → v2
	v2 := "v2-second-edit\n"
	if err := ct.TrackFileEdit(filePath, v1, v2); err != nil {
		t.Fatalf("TrackFileEdit 2: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(v2), 0644); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	// Verify two changes are tracked.
	changes := ct.GetChanges()
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}

	// Recover with scope="session_start" — should use earliest OriginalCode
	// (v0) for the write, but LATEST NewCode (v2) for the staleness check.
	result, err := handleRecoverFile(context.Background(), agent, map[string]interface{}{
		"path":  filePath,
		"scope": "session_start",
	})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}

	var res struct {
		Recovered bool   `json:"recovered"`
		Path      string `json:"path"`
		Action    string `json:"action"`
		Message   string `json:"message"`
	}
	if parseErr := json.Unmarshal([]byte(result), &res); parseErr != nil {
		t.Fatalf("parse result JSON: %v (got: %s)", parseErr, result)
	}

	if !res.Recovered {
		t.Errorf("expected recovered=true for multi-edit, got false (message: %s)", res.Message)
	}

	// File on disk must now contain v0 (the true pre-session original).
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != v0 {
		t.Errorf("expected v0 %q, got %q", v0, string(content))
	}
}

func TestRevertMyChanges_MultiEdit_Proceeds(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

	ct := NewChangeTracker(agent, "test instruction")
	agent.changeTracker = ct

	filePath := filepath.Join(ws, "multi.go")

	// Start with v0 on disk.
	v0 := "v0-original\n"
	if err := os.WriteFile(filePath, []byte(v0), 0644); err != nil {
		t.Fatalf("write v0: %v", err)
	}

	// Edit 1: v0 → v1
	v1 := "v1-first-edit\n"
	if err := ct.TrackFileEdit(filePath, v0, v1); err != nil {
		t.Fatalf("TrackFileEdit 1: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(v1), 0644); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	// Edit 2: v1 → v2
	v2 := "v2-second-edit\n"
	if err := ct.TrackFileEdit(filePath, v1, v2); err != nil {
		t.Fatalf("TrackFileEdit 2: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(v2), 0644); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	// Verify two changes are tracked.
	changes := ct.GetChanges()
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}

	// Revert — should dedup to one candidate (OriginalCode=v0, NewCode=v2),
	// staleness check passes (disk=v2 == NewCode=v2), and v0 is written.
	result, err := handleRevertMyChanges(context.Background(), agent, map[string]interface{}{
		"scope": "all",
	})
	if err != nil {
		t.Fatalf("handleRevertMyChanges: %v", err)
	}

	var res struct {
		Restored int         `json:"restored"`
		Failed   int         `json:"failed"`
		Summary  string      `json:"summary"`
		Entries  interface{} `json:"entries,omitempty"`
	}
	if parseErr := json.Unmarshal([]byte(result), &res); parseErr != nil {
		t.Fatalf("parse result JSON: %v (got: %s)", parseErr, result)
	}

	if res.Restored != 1 {
		t.Errorf("expected 1 restored, got %d (summary: %s)", res.Restored, res.Summary)
	}
	if res.Failed != 0 {
		t.Errorf("expected 0 failed, got %d (summary: %s)", res.Failed, res.Summary)
	}

	// File on disk must now contain v0 (the true pre-session original).
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != v0 {
		t.Errorf("expected v0 %q, got %q", v0, string(content))
	}
}

func TestRecoverFile_MultiEdit_SessionStart_SkipsGenuinelyStaleFile(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

	ct := NewChangeTracker(agent, "test instruction")
	agent.changeTracker = ct

	filePath := filepath.Join(ws, "multi.go")

	// Start with v0 on disk.
	v0 := "v0-original\n"
	if err := os.WriteFile(filePath, []byte(v0), 0644); err != nil {
		t.Fatalf("write v0: %v", err)
	}

	// Edit 1: v0 → v1
	v1 := "v1-first-edit\n"
	if err := ct.TrackFileEdit(filePath, v0, v1); err != nil {
		t.Fatalf("TrackFileEdit 1: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(v1), 0644); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	// Edit 2: v1 → v2
	v2 := "v2-second-edit\n"
	if err := ct.TrackFileEdit(filePath, v1, v2); err != nil {
		t.Fatalf("TrackFileEdit 2: %v", err)
	}
	if err := os.WriteFile(filePath, []byte(v2), 0644); err != nil {
		t.Fatalf("write v2: %v", err)
	}

	// Simulate external modification AFTER all agent edits.
	v3 := "v3-external-commit\n"
	if err := os.WriteFile(filePath, []byte(v3), 0644); err != nil {
		t.Fatalf("write v3: %v", err)
	}

	// Recover with scope="session_start" — staleness check uses latest
	// NewCode (v2), disk has v3 → STALE → skip.
	result, err := handleRecoverFile(context.Background(), agent, map[string]interface{}{
		"path":  filePath,
		"scope": "session_start",
	})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}

	var res struct {
		Recovered bool   `json:"recovered"`
		Path      string `json:"path"`
		Action    string `json:"action"`
		Message   string `json:"message"`
	}
	if parseErr := json.Unmarshal([]byte(result), &res); parseErr != nil {
		t.Fatalf("parse result JSON: %v (got: %s)", parseErr, result)
	}

	if res.Recovered {
		t.Errorf("expected recovered=false (genuinely stale), got true")
	}
	if res.Action != "stale_skip" {
		t.Errorf("expected action=stale_skip, got %q", res.Action)
	}

	// File on disk must STILL contain v3 — not clobbered.
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != v3 {
		t.Errorf("file was clobbered! expected v3 %q, got %q", v3, string(content))
	}
}
