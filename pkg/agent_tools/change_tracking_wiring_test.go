package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SP-fix: the seed-tool migration left write_file / edit_file /
// shell_command without any ChangeTracker hooks — the session change log
// (Agent Changes panel, /api/changes/*, revert tooling) recorded nothing.
// These tests pin the wiring: when the env carries Track* funcs, the
// handlers must invoke them on every successful mutation.

func TestWriteHandler_InvokesTrackFileWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tracked.txt")

	var trackedPath, trackedContent string
	env := ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs: &ToolFuncSet{
			TrackFileWrite: func(filePath string, content string) error {
				trackedPath = filePath
				trackedContent = content
				return nil
			},
		},
	}

	h := &writeFileHandler{}
	res, err := h.Execute(context.Background(), env, map[string]any{
		"path":    path,
		"content": "hello tracked world",
	})
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("write_file returned tool error: %s", res.Output)
	}

	if trackedPath != path {
		t.Errorf("tracked path: want %q, got %q", path, trackedPath)
	}
	if trackedContent != "hello tracked world" {
		t.Errorf("tracked content: got %q", trackedContent)
	}
}

func TestWriteHandler_NoTrackerStillWrites(t *testing.T) {
	// Nil TrackFileWrite (standalone handler use) must not break the write.
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.txt")

	env := ToolEnv{WorkspaceRoot: dir, ToolFuncs: &ToolFuncSet{}}
	h := &writeFileHandler{}
	_, err := h.Execute(context.Background(), env, map[string]any{
		"path":    path,
		"content": "no tracker here",
	})
	if err != nil {
		t.Fatalf("write_file with nil tracker failed: %v", err)
	}
	if b, readErr := os.ReadFile(path); readErr != nil || string(b) != "no tracker here" {
		t.Fatalf("file not written correctly: %v %q", readErr, string(b))
	}
}

func TestEditHandler_InvokesTrackFileEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit-me.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
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
		"old_str": "beta",
		"new_str": "BETA",
	}); err != nil {
		t.Fatalf("edit_file failed: %v", err)
	}

	if !strings.Contains(trackedOriginal, "beta") {
		t.Errorf("tracked original content missing pre-edit text: %q", trackedOriginal)
	}
	if !strings.Contains(trackedNew, "BETA") {
		t.Errorf("tracked new content missing post-edit text: %q", trackedNew)
	}
}

func TestShellHandler_InvokesTrackShellCommand(t *testing.T) {
	dir := t.TempDir()

	var trackedCommand string
	env := ToolEnv{
		WorkspaceRoot: dir,
		ToolFuncs: &ToolFuncSet{
			TrackShellCommand: func(command string) error {
				trackedCommand = command
				return nil
			},
		},
	}

	h := &shellCommandHandler{}
	if _, err := h.Execute(context.Background(), env, map[string]any{
		"command": "echo shell-tracking-probe",
	}); err != nil {
		t.Fatalf("shell_command failed: %v", err)
	}

	if trackedCommand != "echo shell-tracking-probe" {
		t.Errorf("tracked command: got %q", trackedCommand)
	}
}
