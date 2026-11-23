//go:build js && wasm

package main

import "testing"

func TestLegacyRelocationsMovesTheOldRootProjectIntoTheProjectFolder(t *testing.T) {
	files := []IDBFile{
		{Path: "/README.md"},
		{Path: "/src/main.go"},
		{Path: "/home/user/.config/sprout/providers/platform.json"},
		{Path: "/tmp/scratch.txt"},
	}
	got := legacyRelocations(files, "/workspace", "/home/user")
	want := map[string]string{
		"/README.md":   "/workspace/README.md",
		"/src/main.go": "/workspace/src/main.go",
	}
	if len(got) != len(want) {
		t.Fatalf("moves = %v, want %v", got, want)
	}
	for from, to := range want {
		if got[from] != to {
			t.Errorf("%s -> %q, want %q", from, got[from], to)
		}
	}
}

func TestLegacyRelocationsRunOnlyBeforeTheProjectFolderHasFiles(t *testing.T) {
	files := []IDBFile{
		{Path: "/workspace/README.md"},
		{Path: "/notes.txt"},
	}
	if got := legacyRelocations(files, "/workspace", "/home/user"); len(got) != 0 {
		t.Errorf("a project folder already in use must not be touched, got moves %v", got)
	}
}
