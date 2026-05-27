package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// trackerOnlyAgent constructs a minimal Agent wrapping the given
// ChangeTracker so handleRecoverFile (which uses a.GetChangeTracker)
// has somewhere to look. No event bus, no router — recover_file
// doesn't touch them.
func trackerOnlyAgent(tracker *ChangeTracker) *Agent {
	a := &Agent{changeTracker: tracker}
	if tracker != nil {
		tracker.agent = a
	}
	return a
}

func TestHandleRecoverFile_RestoresDeletedContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	tracker := &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{
				FilePath:     path,
				OriginalCode: "port = 8080\n",
				Operation:    "delete",
				ToolCall:     "shell_command",
			},
		},
	}
	a := trackerOnlyAgent(tracker)

	// File doesn't exist on disk — recovery should write it back.
	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("handleRecoverFile returned error: %v", err)
	}

	var res struct {
		Recovered bool   `json:"recovered"`
		Path      string `json:"path"`
		Action    string `json:"action"`
		Message   string `json:"message"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("output not JSON: %v\n%s", jsonErr, out)
	}
	if !res.Recovered {
		t.Fatalf("expected recovered=true, got %+v", res)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "port = 8080\n" {
		t.Errorf("restored content mismatch: got %q", got)
	}
}

func TestHandleRecoverFile_RemovesCreatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new_file.txt")
	if err := os.WriteFile(path, []byte("agent's creation"), 0o644); err != nil {
		t.Fatalf("setup write: %v", err)
	}

	tracker := &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{FilePath: path, Operation: "create", ToolCall: "shell_command"},
		},
	}
	a := trackerOnlyAgent(tracker)

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}

	var res struct {
		Recovered bool `json:"recovered"`
	}
	_ = json.Unmarshal([]byte(out), &res)
	if !res.Recovered {
		t.Fatalf("expected recovered=true; got %s", out)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("file should have been removed; got stat err %v", statErr)
	}
}

func TestHandleRecoverFile_NoMatchReturnsStructuredFailure(t *testing.T) {
	tracker := &ChangeTracker{enabled: true}
	a := trackerOnlyAgent(tracker)

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": "/tmp/never_tracked.txt"})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}
	var res struct {
		Recovered bool   `json:"recovered"`
		Message   string `json:"message"`
	}
	_ = json.Unmarshal([]byte(out), &res)
	if res.Recovered {
		t.Errorf("expected recovered=false for untracked path")
	}
	if res.Message == "" {
		t.Errorf("expected non-empty message explaining failure")
	}
}

func TestHandleRecoverFile_RefusesUncapturedOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")

	tracker := &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{
				FilePath:     path,
				OriginalCode: "[CONTENT NOT CAPTURED: binary]",
				Operation:    "delete",
				ToolCall:     "shell_command",
			},
		},
	}
	a := trackerOnlyAgent(tracker)

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}
	var res struct {
		Recovered bool   `json:"recovered"`
		Message   string `json:"message"`
	}
	_ = json.Unmarshal([]byte(out), &res)
	if res.Recovered {
		t.Errorf("should refuse to recover when original content wasn't captured")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("file should still not exist (we didn't fake recover)")
	}
}

func TestHandleRecoverFile_PrefersMostRecentChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// First an edit (saw v1, now v2), then a delete (saw v2). The
	// most-recent recovery should restore v2 — the latest "before"
	// state captured.
	tracker := &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{FilePath: path, OriginalCode: "v1", NewCode: "v2", Operation: "edit", ToolCall: "edit_file"},
			{FilePath: path, OriginalCode: "v2", Operation: "delete", ToolCall: "shell_command"},
		},
	}
	a := trackerOnlyAgent(tracker)

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}
	_ = out

	got, _ := os.ReadFile(path)
	if string(got) != "v2" {
		t.Errorf("most-recent change should win: expected restored v2, got %q", got)
	}
}
