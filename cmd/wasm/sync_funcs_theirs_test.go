//go:build js && wasm

package main

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// These tests cover the their-content stash mechanism used in
// workspace_patch conflict handling. They run only in WASM builds
// since the stash functions are defined in sync_funcs.go which
// requires js+wasm.

func TestStashTheirsContent_RoundTrip(t *testing.T) {
	path := "/path/to/file.txt.theirs"
	content := "container content here"

	stashTheirsContent(path, content)

	retrieved, ok := peekTheirsContent(path)
	if !ok {
		t.Fatal("peekTheirsContent should return true for stashed path")
	}
	if retrieved != content {
		t.Errorf("expected %q, got %q", content, retrieved)
	}
}

func TestStashTheirsContent_MultipleEntries(t *testing.T) {
	stashTheirsContent("/a.txt.theirs", "content A")
	stashTheirsContent("/b.txt.theirs", "content B")

	a, okA := peekTheirsContent("/a.txt.theirs")
	if !okA || a != "content A" {
		t.Errorf("expected content A, got %q (ok=%v)", a, okA)
	}

	b, okB := peekTheirsContent("/b.txt.theirs")
	if !okB || b != "content B" {
		t.Errorf("expected content B, got %q (ok=%v)", b, okB)
	}
}

func TestStashTheirsContent_MissingKey(t *testing.T) {
	_, ok := peekTheirsContent("/nonexistent.txt.theirs")
	if ok {
		t.Error("peekTheirsContent should return false for non-existent path")
	}
}

func TestStashTheirsContent_Overwrite(t *testing.T) {
	path := "/overwrite.txt.theirs"

	stashTheirsContent(path, "first value")
	stashTheirsContent(path, "second value")

	retrieved, ok := peekTheirsContent(path)
	if !ok {
		t.Fatal("should find stashed path")
	}
	if retrieved != "second value" {
		t.Errorf("expected 'second value', got %q", retrieved)
	}
}

func TestStashFileMetadata_RoundTrip(t *testing.T) {
	path := "/metadata.txt"
	md := agent.WorkspaceFileMetadata{
		BrowserSeq:        42,
		ContainerSeq:      10,
		LastSyncedBrowser: 38,
	}

	stashFileMetadata(path, md)

	retrieved, ok := peekFileMetadata(path)
	if !ok {
		t.Fatal("peekFileMetadata should return true for stashed path")
	}
	if retrieved.BrowserSeq != md.BrowserSeq {
		t.Errorf("BrowserSeq: expected %d, got %d", md.BrowserSeq, retrieved.BrowserSeq)
	}
	if retrieved.ContainerSeq != md.ContainerSeq {
		t.Errorf("ContainerSeq: expected %d, got %d", md.ContainerSeq, retrieved.ContainerSeq)
	}
}

func TestStashFileMetadata_WithUnsyncedEdits(t *testing.T) {
	path := "/unsynced.txt"
	md := agent.WorkspaceFileMetadata{
		BrowserSeq:        10,
		ContainerSeq:      3,
		LastSyncedBrowser: 5,
	}

	stashFileMetadata(path, md)

	retrieved, ok := peekFileMetadata(path)
	if !ok {
		t.Fatal("should find stashed metadata")
	}
	if !retrieved.HasUnsyncedBrowserEdits() {
		t.Error("retrieved metadata should indicate unsynced browser edits")
	}
}

func TestApplyAllStashedMetadata(t *testing.T) {
	// Note: In the current WASM build there is no long-lived Agent,
	// so this is a placeholder for when the agent loop is wired up (Tier 2b).
	// The actual integration test for ApplyAllStashedMetadata lives in
	// pkg/agent/workspace_sync_metadata_test.go which tests the same
	// logic through Agent.SetFileMetadata/GetFileMetadata directly.
}
