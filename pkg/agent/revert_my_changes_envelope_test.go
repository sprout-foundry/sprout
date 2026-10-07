package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent/changes"
)

// TestRevertMyChanges_FileScopeReturnsRevertEnvelope: a single-file revert
// (the WebUI's per-file action) must return the same {restored, failed,
// summary, entries} shape the bulk path returns — not the recover_file
// {recovered, path, action, message} shape — so the UI summary helper can
// read it. Previously the file-scope path returned the recover shape and the
// UI reported no outcome at all.
func TestRevertMyChanges_FileScopeReturnsRevertEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}

	tracker := changes.NewChangeTracker(nil, "")
	tracker.Enable()
	// Earliest entry's OriginalCode is the pre-session state (what
	// scope=session_start restores).
	tracker.MergeChild([]TrackedFileChange{{
		FilePath:     path,
		OriginalCode: "before",
		NewCode:      "after",
		Operation:    "edit",
		ToolCall:     "EditFile",
	}}, "test")

	a := trackerOnlyAgent(tracker)
	out, err := a.RevertMyChanges("", path, "")
	if err != nil {
		t.Fatalf("RevertMyChanges(file): %v", err)
	}

	var env struct {
		Restored int `json:"restored"`
		Failed   int `json:"failed"`
		Summary  string
		Entries  []struct {
			Path string `json:"path"`
			OK   bool   `json:"ok"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("file-scope revert must return the revert envelope; got %q (parse: %v)", out, err)
	}
	if env.Restored != 1 || env.Failed != 0 {
		t.Errorf("expected 1 restored, got restored=%d failed=%d", env.Restored, env.Failed)
	}
	if env.Summary == "" {
		t.Error("summary must be set so the UI can report the outcome")
	}
	if len(env.Entries) != 1 || env.Entries[0].Path != path || !env.Entries[0].OK {
		t.Errorf("expected a single ok entry for %s, got %+v", path, env.Entries)
	}
	// And the file was actually restored on disk.
	got, _ := os.ReadFile(path)
	if string(got) != "before" {
		t.Errorf("file not restored: got %q, want %q", got, "before")
	}
}

// TestRevertMyChanges_FileScopeReportsFailure confirms a failed/stale
// single-file revert reports restored=0 failed=1 (not a silent success).
func TestRevertMyChanges_FileScopeReportsFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.txt")
	// File does NOT exist and there is no recorded change → recover fails.
	tracker := changes.NewChangeTracker(nil, "")
	tracker.Enable()

	a := trackerOnlyAgent(tracker)
	out, err := a.RevertMyChanges("", path, "")
	if err != nil {
		t.Fatalf("RevertMyChanges(file): %v", err)
	}
	var env struct {
		Restored int `json:"restored"`
		Failed   int `json:"failed"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parse: %v (%q)", err, out)
	}
	if env.Restored != 0 || env.Failed != 1 {
		t.Errorf("expected 0 restored / 1 failed for an untracked path, got %+v", env)
	}
}
