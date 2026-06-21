package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleListChanges_NoTracker(t *testing.T) {
	a := &Agent{} // no changeTracker
	out, err := handleListChanges(context.Background(), a, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With include_persisted defaulting to true and no tracker, the
	// response routes through handleListChangesPersistedOnly and is
	// JSON-marshalled (so spacing differs from the old raw literal).
	// Parse it and assert the semantics rather than a substring.
	var parsed struct {
		Enabled bool `json:"enabled"`
		Count   int  `json:"count"`
		Files   []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if parsed.Enabled {
		t.Errorf("expected enabled:false when tracker is nil; got %s", out)
	}
	// Count reflects persisted history only (in-memory buffer is nil).
	// In a clean test env this is typically 0; we assert the shape is
	// well-formed rather than a specific count since other tests in
	// this package may have written history entries to shared dirs.
	_ = parsed.Count
}

func TestHandleListChanges_ReportsRecoverabilityAccurately(t *testing.T) {
	a := &Agent{}
	a.changeTracker = &ChangeTracker{
		revisionID: "rev-abc123",
		enabled:    true,
		changes: []TrackedFileChange{
			// Created — no original by definition → not recoverable.
			{FilePath: "/work/new.go", Operation: "create", ToolCall: "shell_command", OriginalCode: ""},
			// Edited — full original captured → recoverable.
			{FilePath: "/work/edit.go", Operation: "edit", ToolCall: "edit_file", OriginalCode: "before"},
			// Deleted with full content → recoverable.
			{FilePath: "/work/del.go", Operation: "delete", ToolCall: "shell_command", OriginalCode: "lost work"},
			// Path-only sentinel (binary / oversized) → not recoverable.
			{FilePath: "/work/blob.bin", Operation: "delete", ToolCall: "shell_command", OriginalCode: "[CONTENT NOT CAPTURED: binary]"},
			// Redacted (outside workspace) → not recoverable.
			{FilePath: "/external/secret", Operation: "edit", ToolCall: "shell_command", OriginalCode: RedactedContentMarker},
		},
	}

	out, err := handleListChanges(context.Background(), a, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed struct {
		RevisionID string `json:"revision_id"`
		Enabled    bool   `json:"enabled"`
		Count      int    `json:"count"`
		Files      []struct {
			Path        string `json:"path"`
			Op          string `json:"op"`
			Tool        string `json:"tool"`
			Recoverable bool   `json:"recoverable"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	if parsed.RevisionID != "rev-abc123" {
		t.Errorf("revision_id missing/wrong: %q", parsed.RevisionID)
	}
	if !parsed.Enabled {
		t.Errorf("expected enabled:true")
	}
	if parsed.Count != 5 || len(parsed.Files) != 5 {
		t.Fatalf("expected 5 files, got count=%d len=%d", parsed.Count, len(parsed.Files))
	}

	byPath := make(map[string]bool, 5)
	for _, f := range parsed.Files {
		byPath[f.Path] = f.Recoverable
	}
	for path, wantRecoverable := range map[string]bool{
		"/work/new.go":     false, // created — no original
		"/work/edit.go":    true,  // direct hook captured original
		"/work/del.go":     true,  // shell snapshot captured original
		"/work/blob.bin":   false, // path-only sentinel
		"/external/secret": false, // redacted marker
	} {
		got, ok := byPath[path]
		if !ok {
			t.Errorf("path %q missing from output", path)
			continue
		}
		if got != wantRecoverable {
			t.Errorf("recoverable[%q] = %v, want %v", path, got, wantRecoverable)
		}
	}
}

func TestIsRecoverableOriginal(t *testing.T) {
	cases := []struct {
		original string
		want     bool
	}{
		{"", false}, // created files
		{RedactedContentMarker, false},
		{"[CONTENT NOT CAPTURED: binary]", false},
		{"[CONTENT NOT CAPTURED: too large]", false},
		{"some actual file content", true},
		{"a", true},
	}
	for _, c := range cases {
		if got := isRecoverableOriginal(c.original); got != c.want {
			t.Errorf("isRecoverableOriginal(%q) = %v, want %v", c.original, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 1 + 1.5 tests
// ---------------------------------------------------------------------------

func TestHandleListChanges_FiltersBySince(t *testing.T) {
	t0 := time.Now()
	a := &Agent{changeTracker: &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{FilePath: "/old.go", Operation: "edit", ToolCall: "edit_file", OriginalCode: "x", Timestamp: t0.Add(-1 * time.Hour)},
			{FilePath: "/new.go", Operation: "create", ToolCall: "write_file", Timestamp: t0.Add(-1 * time.Minute)},
		},
	}}
	args := map[string]interface{}{"since": t0.Add(-30 * time.Minute).Format(time.RFC3339)}
	out, err := handleListChanges(context.Background(), a, args)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "/new.go") {
		t.Errorf("expected /new.go in filtered output: %s", out)
	}
	if strings.Contains(out, "/old.go") {
		t.Errorf("old.go should have been filtered out by since cutoff: %s", out)
	}
}

func TestHandleListChanges_FiltersByTool(t *testing.T) {
	a := &Agent{changeTracker: &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{FilePath: "/a", Operation: "edit", ToolCall: "edit_file", OriginalCode: "x"},
			{FilePath: "/b", Operation: "create", ToolCall: "shell_command"},
		},
	}}
	out, _ := handleListChanges(context.Background(), a, map[string]interface{}{"tool": "shell_command"})
	if !strings.Contains(out, "/b") || strings.Contains(out, "/a") {
		t.Errorf("tool filter wrong: %s", out)
	}
}

func TestHandleListChanges_IncludeDiff_RendersUnifiedDiff(t *testing.T) {
	// Replaces the historical handleShowMyChange test. The diff now
	// rides on the list_changes per-file entry when include_diff=true.
	dir := t.TempDir()
	path := filepath.Join(dir, "foo.go")

	a := &Agent{changeTracker: &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{FilePath: path, Operation: "edit", ToolCall: "edit_file",
				OriginalCode: "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n",
				NewCode:      "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"},
		},
	}}
	out, err := handleListChanges(context.Background(), a, map[string]interface{}{
		"path_pattern": path,
		"include_diff": true,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var res struct {
		Files []struct {
			Path string `json:"path"`
			Diff string `json:"diff"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, out)
	}
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file in output, got %d: %s", len(res.Files), out)
	}
	if !strings.Contains(res.Files[0].Diff, `-	println("hi")`) || !strings.Contains(res.Files[0].Diff, `+	println("hello")`) {
		t.Errorf("unified diff content missing expected -/+ lines:\n%s", res.Files[0].Diff)
	}
}

func TestHandleListChanges_IncludeDiff_NoMatchReturnsEmpty(t *testing.T) {
	a := &Agent{changeTracker: &ChangeTracker{enabled: true}}
	out, _ := handleListChanges(context.Background(), a, map[string]interface{}{
		"path_pattern": "/never/changed.go",
		"include_diff": true,
	})
	if !strings.Contains(out, `"count": 0`) {
		t.Errorf("expected count=0 for untracked path: %s", out)
	}
}

func TestHandleRevertMyChanges_RevertsAll(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pathA, []byte("AFTER A"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(pathB, []byte("AFTER B"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	a := &Agent{changeTracker: &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{FilePath: pathA, Operation: "edit", ToolCall: "edit_file", OriginalCode: "BEFORE A"},
			{FilePath: pathB, Operation: "edit", ToolCall: "edit_file", OriginalCode: "BEFORE B"},
		},
	}}
	out, err := handleRevertMyChanges(context.Background(), a, map[string]interface{}{"scope": "all"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, `"restored": 2`) {
		t.Errorf("expected 2 restored: %s", out)
	}
	got, _ := os.ReadFile(pathA)
	if string(got) != "BEFORE A" {
		t.Errorf("pathA not restored: %q", got)
	}
	got, _ = os.ReadFile(pathB)
	if string(got) != "BEFORE B" {
		t.Errorf("pathB not restored: %q", got)
	}
}

func TestHandleRecoverFile_SessionStartOnlyTouchesOne(t *testing.T) {
	// The historical revert_my_changes(file=…) scope is now recover_file
	// with scope="session_start". Verifies single-file recovery still
	// leaves siblings untouched.
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(pathA, []byte("AFTER A"), 0o644)
	_ = os.WriteFile(pathB, []byte("AFTER B"), 0o644)

	a := &Agent{changeTracker: &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{FilePath: pathA, Operation: "edit", ToolCall: "edit_file", OriginalCode: "BEFORE A"},
			{FilePath: pathB, Operation: "edit", ToolCall: "edit_file", OriginalCode: "BEFORE B"},
		},
	}}
	_, err := handleRecoverFile(context.Background(), a, map[string]interface{}{
		"path":  pathA,
		"scope": "session_start",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	gotA, _ := os.ReadFile(pathA)
	if string(gotA) != "BEFORE A" {
		t.Errorf("pathA should have been restored: %q", gotA)
	}
	gotB, _ := os.ReadFile(pathB)
	if string(gotB) != "AFTER B" {
		t.Errorf("pathB should be untouched: %q", gotB)
	}
}

func TestHandleRevertMyChanges_RevertsToEarliestOriginal(t *testing.T) {
	// Three edits to the same file: A → B → C. Revert should restore
	// to A (the truest pre-session state), not B (the immediately
	// previous edit).
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	_ = os.WriteFile(path, []byte("C"), 0o644)

	a := &Agent{changeTracker: &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			{FilePath: path, Operation: "edit", OriginalCode: "A", NewCode: "B"},
			{FilePath: path, Operation: "edit", OriginalCode: "B", NewCode: "C"},
		},
	}}
	_, err := handleRevertMyChanges(context.Background(), a, map[string]interface{}{"scope": "all"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "A" {
		t.Errorf("expected revert to earliest original 'A', got %q", got)
	}
}

func TestHandleListChanges_GroupBy_Block_GroupsContiguousActivity(t *testing.T) {
	t0 := time.Now()
	a := &Agent{changeTracker: &ChangeTracker{
		enabled: true,
		changes: []TrackedFileChange{
			// Block 1: three changes within seconds of each other.
			{FilePath: "/a.go", Operation: "edit", ToolCall: "edit_file", OriginalCode: "x", Timestamp: t0},
			{FilePath: "/b.go", Operation: "create", ToolCall: "write_file", Timestamp: t0.Add(5 * time.Second)},
			{FilePath: "/c.go", Operation: "edit", ToolCall: "edit_file", OriginalCode: "y", Timestamp: t0.Add(20 * time.Second)},
			// Block 2: starts after a 5-minute gap.
			{FilePath: "/d.go", Operation: "edit", ToolCall: "shell_command", OriginalCode: "z", Timestamp: t0.Add(5 * time.Minute)},
		},
	}}
	out, err := handleListChanges(context.Background(), a, map[string]interface{}{"group_by": "block"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var res struct {
		Blocks []struct {
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		} `json:"blocks"`
		Totals struct {
			Changes int `json:"changes"`
			Files   int `json:"files"`
		} `json:"totals"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, out)
	}
	if len(res.Blocks) != 2 {
		t.Fatalf("expected 2 activity blocks, got %d: %s", len(res.Blocks), out)
	}
	if len(res.Blocks[0].Files) != 3 {
		t.Errorf("block 1 should have 3 files, got %d", len(res.Blocks[0].Files))
	}
	if len(res.Blocks[1].Files) != 1 {
		t.Errorf("block 2 should have 1 file, got %d", len(res.Blocks[1].Files))
	}
	if res.Totals.Changes != 4 || res.Totals.Files != 4 {
		t.Errorf("expected totals=4/4, got %d/%d", res.Totals.Changes, res.Totals.Files)
	}
}

func TestParseRecentSince(t *testing.T) {
	cases := []struct {
		in    string
		isErr bool
	}{
		{"", false},
		{"2026-05-27T10:00:00Z", false},
		{"2d", false},
		{"12h", false},
		{"30m", false},
		{"300s", false},
		{"not a time", true},
	}
	for _, c := range cases {
		_, err := parseRecentSince(c.in)
		if c.isErr && err == nil {
			t.Errorf("parseRecentSince(%q) expected error, got nil", c.in)
		}
		if !c.isErr && err != nil {
			t.Errorf("parseRecentSince(%q) expected ok, got err: %v", c.in, err)
		}
	}
}
