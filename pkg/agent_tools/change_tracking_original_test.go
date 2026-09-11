package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The pre-write original must be captured BEFORE WriteFile mutates the
// file. A tracker that received the post-write bytes as the "original"
// would make every recovery a silent no-op.
func TestWriteHandler_TracksPreWriteOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("original v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var trackedOriginal string
	env := ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs: &ToolFuncSet{
			TrackFileWrite: func(filePath string, originalContent string, content string) error {
				trackedOriginal = originalContent
				return nil
			},
		},
	}

	h := &writeFileHandler{}
	if _, err := h.Execute(context.Background(), env, map[string]any{
		"path":    path,
		"content": "original v1\nplus an edit\n",
	}); err != nil {
		t.Fatalf("write_file failed: %v", err)
	}

	if trackedOriginal != "original v1\n" {
		t.Errorf("tracked original: want pre-write content, got %q", trackedOriginal)
	}
}

// Overwriting a file that was EMPTY must track original="" as a real
// pre-state, not skip tracking.
func TestWriteHandler_TracksOverwriteOfEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	var trackedOriginal, trackedNew string
	env := ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs: &ToolFuncSet{
			TrackFileWrite: func(filePath string, originalContent string, content string) error {
				trackedOriginal = originalContent
				trackedNew = content
				return nil
			},
		},
	}

	h := &writeFileHandler{}
	if _, err := h.Execute(context.Background(), env, map[string]any{
		"path":    path,
		"content": "now non-empty\n",
	}); err != nil {
		t.Fatalf("write_file failed: %v", err)
	}

	if trackedOriginal != "" {
		t.Errorf("tracked original: want empty, got %q", trackedOriginal)
	}
	if trackedNew != "now non-empty\n" {
		t.Errorf("tracked new: got %q", trackedNew)
	}
}

// An edit to a file that was effectively EMPTY (single newline) must
// still be tracked — reverting it is the only way back to the empty
// state. This is the regression case: the tracker previously skipped
// tracking whenever pre-edit content was the empty string, and an
// all-whitespace file edit leaves no recoverable original.
func TestEditHandler_TracksEditOfWhitespaceOnlyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blank.yaml")
	if err := os.WriteFile(path, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var trackedOriginal, trackedNew string
	env := ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs: &ToolFuncSet{
			TrackFileEdit: func(filePath string, originalContent string, newContent string) error {
				trackedOriginal = originalContent
				trackedNew = newContent
				return nil
			},
		},
	}

	h := &editFileHandler{}
	if _, err := h.Execute(context.Background(), env, map[string]any{
		"path":    path,
		"old_str": "\n",
		"new_str": "key: value\n",
	}); err != nil {
		t.Fatalf("edit_file failed: %v", err)
	}

	if trackedOriginal != "\n" {
		t.Errorf("tracked original: want %q (pre-edit state), got %q", "\n", trackedOriginal)
	}
	if trackedNew != "key: value\n" {
		t.Errorf("tracked new: want post-edit content, got %q", trackedNew)
	}
}
