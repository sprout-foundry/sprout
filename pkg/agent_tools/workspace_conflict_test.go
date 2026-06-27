package tools

import (
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Mock EventPublisher — captures Publish calls for assertion
// ---------------------------------------------------------------------------

type publishedEvent struct {
	eventType string
	data      any
}

type mockEventPublisher struct {
	events []publishedEvent
}

func (m *mockEventPublisher) Publish(eventType string, data any) {
	m.events = append(m.events, publishedEvent{eventType: eventType, data: data})
}

// ---------------------------------------------------------------------------
// Helper: setConflictState sets browser_seq > last_synced_browser to simulate
// unsynced browser edits for conflict testing.
// ---------------------------------------------------------------------------

// setConflictState sets browser_seq > last_synced_browser to simulate unsynced edits.
func setConflictState(ss *SyncState, path string, browserSeq, lastSyncedBrowser int64) {
	ss.mu.Lock()
	m, ok := ss.files[path]
	if !ok {
		m = ss.getOrCreate(path)
	}
	m.BrowserSeq = browserSeq
	m.LastSyncedBrowser = lastSyncedBrowser
	ss.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Helper: set up a SyncState with a conflict condition in a temp directory.
// Returns the absolute file path to use and a cleanup function registered
// via t.Cleanup.
// ---------------------------------------------------------------------------

func setupConflictInTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "workspace_conflict_test")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	// chdir into the temp dir so we can use a relative path (the production
	// guard rejects absolute paths and .. segments).
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir into temp dir: %v", err)
	}

	return "test_file.go"
}

// ---------------------------------------------------------------------------
// 1. TestSingleSideEdit_CleanApply
//
// Browser edits are fully synced (browser_seq == last_synced_browser).
// A subsequent container patch should apply cleanly — no conflict.
// ---------------------------------------------------------------------------

func TestSingleSideEdit_CleanApply(t *testing.T) {
	ss := NewSyncState()
	path := setupConflictInTempDir(t)

	// Browser applies an edit — syncs fully (browser_seq == last_synced_browser).
	meta, err := ss.ApplyBrowserOp(path, "browser content v1")
	if err != nil {
		t.Fatalf("ApplyBrowserOp: %v", err)
	}
	if meta.BrowserSeq != meta.LastSyncedBrowser {
		t.Fatalf("after ApplyBrowserOp, browser_seq=%d should equal last_synced_browser=%d",
			meta.BrowserSeq, meta.LastSyncedBrowser)
	}

	// Container now writes a patch. Since browser_seq <= last_synced_browser,
	// this is NOT a conflict.
	event := &PatchEvent{
		Path:           path,
		ContainerSeq:   10,
		Content:        "container content v10",
		BaseBrowserSeq: 1,
	}
	browserContent := "browser content v1"
	resultMeta, conflict, err := ss.HandleContainerPatchWithConflictDetection(path, event, browserContent, nil)
	if err != nil {
		t.Fatalf("HandleContainerPatchWithConflictDetection: %v", err)
	}
	if conflict != nil {
		t.Fatalf("expected no conflict on clean single-side edit, got %+v", conflict)
	}
	if resultMeta.ContainerSeq != 10 {
		t.Errorf("ContainerSeq = %d; want 10", resultMeta.ContainerSeq)
	}
	if resultMeta.LastSyncedContainer != 10 {
		t.Errorf("LastSyncedContainer = %d; want 10", resultMeta.LastSyncedContainer)
	}

	// Verify no .theirs file was created.
	theirsPath := path + ".theirs"
	if _, err := os.Stat(theirsPath); !os.IsNotExist(err) {
		t.Errorf("no .theirs file should exist on clean apply, but %s exists", theirsPath)
	}
}

// ---------------------------------------------------------------------------
// 2. TestSimultaneousEdit_ConflictDetection
//
// Browser has unsynced edits (browser_seq > last_synced_browser).
// A container patch should be rejected as a conflict, .theirs file written.
// ---------------------------------------------------------------------------

func TestSimultaneousEdit_ConflictDetection(t *testing.T) {
	ss := NewSyncState()
	path := setupConflictInTempDir(t)

	// Start with a browser op.
	ss.ApplyBrowserOp(path, "browser content")

	// Create the gap: browser made another edit the container hasn't seen.
	setConflictState(ss, path, 2, 1)

	// Container tries to push a patch.
	containerContent := "container content for conflict"
	event := &PatchEvent{
		Path:         path,
		ContainerSeq: 5,
		Content:      containerContent,
	}

	resultMeta, conflict, err := ss.HandleContainerPatchWithConflictDetection(path, event, "browser v2 content", nil)
	if err != nil {
		t.Fatalf("HandleContainerPatchWithConflictDetection: %v", err)
	}
	if conflict == nil {
		t.Fatal("expected ConflictResult when browser_seq > last_synced_browser, got nil")
	}
	if resultMeta == nil {
		t.Fatal("expected metadata copy, got nil")
	}

	// Verify ConflictResult fields.
	if conflict.Path != path {
		t.Errorf("Path = %q; want %q", conflict.Path, path)
	}
	expectedTheirs := path + ".theirs"
	if conflict.TheirsPath != expectedTheirs {
		t.Errorf("TheirsPath = %q; want %q", conflict.TheirsPath, expectedTheirs)
	}
	expectedContainerHash := computeHash(containerContent)
	if conflict.HashContainer != expectedContainerHash {
		t.Errorf("HashContainer = %q; want %q", conflict.HashContainer, expectedContainerHash)
	}
	expectedBrowserHash := computeHash("browser v2 content")
	if conflict.HashBrowser != expectedBrowserHash {
		t.Errorf("HashBrowser = %q; want %q", conflict.HashBrowser, expectedBrowserHash)
	}
	if conflict.Message == "" {
		t.Error("Message should not be empty")
	}
	if !strings.Contains(conflict.Message, path) {
		t.Errorf("Message should contain path %q; got: %s", path, conflict.Message)
	}

	// Verify the .theirs file was actually written to disk.
	theirsContent, err := os.ReadFile(expectedTheirs)
	if err != nil {
		t.Fatalf("reading .theirs file: %v", err)
	}
	if string(theirsContent) != containerContent {
		t.Errorf(".theirs content = %q; want %q", string(theirsContent), containerContent)
	}

	// Verify metadata was NOT updated (conflict path skips mutation).
	// ContainerSeq should still be whatever ApplyBrowserOp set it to (1).
	if resultMeta.ContainerSeq != 1 {
		t.Errorf("ContainerSeq should be unchanged on conflict; got %d, expected 1", resultMeta.ContainerSeq)
	}
}

// ---------------------------------------------------------------------------
// 3. TestNoConflictBaseline
//
// Fresh SyncState with no prior edits — any container patch should apply
// cleanly since both counters are zero.
// ---------------------------------------------------------------------------

func TestNoConflictBaseline(t *testing.T) {
	ss := NewSyncState()
	path := setupConflictInTempDir(t)

	event := &PatchEvent{
		Path:         path,
		ContainerSeq: 42,
		Content:      "fresh content",
	}
	meta, conflict, err := ss.HandleContainerPatchWithConflictDetection(path, event, "", nil)
	if err != nil {
		t.Fatalf("HandleContainerPatchWithConflictDetection: %v", err)
	}
	if conflict != nil {
		t.Fatalf("expected no conflict on fresh file, got %+v", conflict)
	}
	if meta.ContainerSeq != 42 {
		t.Errorf("ContainerSeq = %d; want 42", meta.ContainerSeq)
	}
	if meta.LastSyncedContainer != 42 {
		t.Errorf("LastSyncedContainer = %d; want 42", meta.LastSyncedContainer)
	}
	if meta.BrowserSeq != 0 {
		t.Errorf("BrowserSeq = %d; want 0 on baseline", meta.BrowserSeq)
	}

	// Verify no .theirs file was created.
	if _, err := os.Stat(path + ".theirs"); !os.IsNotExist(err) {
		t.Error("no .theirs file should exist on clean baseline apply")
	}
}

// ---------------------------------------------------------------------------
// 4. TestEventPayloadContents
//
// When a conflict is detected and an event bus is provided, the bus should
// receive a properly structured event.
// ---------------------------------------------------------------------------

func TestEventPayloadContents(t *testing.T) {
	ss := NewSyncState()
	path := setupConflictInTempDir(t)

	// Create conflict state.
	ss.ApplyBrowserOp(path, "browser content")
	setConflictState(ss, path, 3, 1)

	bus := &mockEventPublisher{}

	containerContent := "container payload for event"
	event := &PatchEvent{
		Path:         path,
		ContainerSeq: 8,
		Content:      containerContent,
	}
	_, conflict, err := ss.HandleContainerPatchWithConflictDetection(path, event, "browser v3", bus)
	if err != nil {
		t.Fatalf("HandleContainerPatchWithConflictDetection: %v", err)
	}
	if conflict == nil {
		t.Fatal("expected conflict to trigger event emission, got nil")
	}

	// Verify the mock captured exactly one Publish call.
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 published event; got %d", len(bus.events))
	}

	ev := bus.events[0]
	if ev.eventType != eventWorkspaceConflict {
		t.Errorf("event type = %q; want %q", ev.eventType, eventWorkspaceConflict)
	}

	data, ok := ev.data.(map[string]interface{})
	if !ok {
		t.Fatalf("event data should be map[string]interface{}, got %T", ev.data)
	}

	// Verify required keys and values.
	if data["path"] != path {
		t.Errorf("path = %v; want %v", data["path"], path)
	}
	if data["theirs_path"] != path+".theirs" {
		t.Errorf("theirs_path = %v; want %v", data["theirs_path"], path+".theirs")
	}
	if data["hash_container"] != computeHash(containerContent) {
		t.Errorf("hash_container = %v; want %v",
			data["hash_container"], computeHash(containerContent))
	}
	if data["hash_browser"] != computeHash("browser v3") {
		t.Errorf("hash_browser = %v; want %v",
			data["hash_browser"], computeHash("browser v3"))
	}
	if _, hasModifiedAt := data["modified_at"]; !hasModifiedAt {
		t.Error("event payload should contain modified_at field")
	}
}

// ---------------------------------------------------------------------------
// 5. TestNilEventBus_NoPanic
//
// A nil event bus must not cause a panic — the nil guard in the
// implementation should allow conflict detection to proceed normally.
// ---------------------------------------------------------------------------

func TestNilEventBus_NoPanic(t *testing.T) {
	ss := NewSyncState()
	path := setupConflictInTempDir(t)

	// Create conflict state.
	ss.ApplyBrowserOp(path, "content")
	setConflictState(ss, path, 2, 1)

	event := &PatchEvent{
		Path:         path,
		ContainerSeq: 1,
		Content:      "data",
	}

	// Should NOT panic with nil event bus.
	meta, conflict, err := ss.HandleContainerPatchWithConflictDetection(path, event, "b", nil)
	if err != nil {
		t.Fatalf("HandleContainerPatchWithConflictDetection: %v", err)
	}
	if meta == nil {
		t.Fatal("expected metadata copy, got nil")
	}
	if conflict == nil {
		t.Fatal("expected conflict, got nil")
	}
	// The .theirs file should still be created.
	if _, err := os.Stat(path + ".theirs"); os.IsNotExist(err) {
		t.Error(".theirs file should still be written even without an event bus")
	}
}

// ---------------------------------------------------------------------------
// 6. TestComputeHashCorrectness
//
// Verify computeHash produces known SHA-256 hex digests for standard inputs.
// ---------------------------------------------------------------------------

func TestComputeHashCorrectness(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "hello",
			input: "hello",
			want:  "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name:  "empty string",
			input: "",
			want:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:  "mixed case and symbols",
			input: "Hello World! 123",
			want:  "cd06e07d713167ec823db9a37fe3dfb1d709cb229f5332d2dcdc79dee0e8bce1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeHash(tt.input)
			if got != tt.want {
				t.Errorf("computeHash(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
