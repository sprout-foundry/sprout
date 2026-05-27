package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestHandleListChanges_NoTracker(t *testing.T) {
	a := &Agent{} // no changeTracker
	out, err := handleListChanges(context.Background(), a, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"enabled":false`) {
		t.Errorf("expected enabled:false in JSON when tracker is nil; got %q", out)
	}
	if !strings.Contains(out, `"count":0`) {
		t.Errorf("expected count:0 when no tracker; got %q", out)
	}
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
