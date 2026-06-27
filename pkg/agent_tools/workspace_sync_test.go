package tools

import (
	"sync"
	"testing"
)

// --- SyncState: UpdateContainerPatch ---

func TestWorkspace_UpdateContainerPatch_FreshFile_SetsSeqs(t *testing.T) {
	ss := NewSyncState()

	event := &PatchEvent{
		Path:           "pkg/foo/bar.go",
		ContainerSeq:   5,
		Content:        "package foo\n",
		BaseBrowserSeq: 0,
	}

	meta, err := ss.UpdateContainerPatch("pkg/foo/bar.go", event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.ContainerSeq != 5 {
		t.Errorf("ContainerSeq = %d; want 5", meta.ContainerSeq)
	}
	if meta.LastSyncedContainer != 5 {
		t.Errorf("LastSyncedContainer = %d; want 5", meta.LastSyncedContainer)
	}
	if meta.BrowserSeq != 0 {
		t.Errorf("BrowserSeq = %d; want 0", meta.BrowserSeq)
	}
	if meta.LastSyncedBrowser != 0 {
		t.Errorf("LastSyncedBrowser = %d; want 0", meta.LastSyncedBrowser)
	}
}

func TestWorkspace_UpdateContainerPatch_Conflict_BrowserUnsynced(t *testing.T) {
	ss := NewSyncState()

	// Simulate the browser having unsynced edits.
	ss.mu.Lock()
	ss.files["pkg/foo/bar.go"] = &FileMetadata{
		BrowserSeq:        10,
		ContainerSeq:      3,
		LastSyncedBrowser: 5,
		LastSyncedContainer: 3,
	}
	ss.mu.Unlock()

	// Container tries to push a patch but browser_seq (10) > last_synced_browser (5).
	event := &PatchEvent{
		Path:           "pkg/foo/bar.go",
		ContainerSeq:   6,
		Content:        "package foo\n",
		BaseBrowserSeq: 5,
	}

	_, err := ss.UpdateContainerPatch("pkg/foo/bar.go", event)
	if err == nil {
		t.Fatal("expected error when browser has unsynced edits; got nil")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected non-empty error message")
	}

	// Verify the error mentions unsynced edits.
	// The spec says the error should reference browser_seq and last_synced_browser.
	expectContains := []string{
		"unsynced edits",
		"browser_seq=10",
		"last_synced_browser=5",
	}
	for _, sub := range expectContains {
		if !containsStr(err.Error(), sub) {
			t.Errorf("error should contain %q; got: %s", sub, err.Error())
		}
	}

	// Verify state was NOT modified on conflict.
	meta, ok := ss.GetMetadata("pkg/foo/bar.go")
	if !ok {
		t.Fatal("metadata should still exist after failed update")
	}
	if meta.ContainerSeq != 3 {
		t.Errorf("ContainerSeq should be unchanged (3); got %d", meta.ContainerSeq)
	}
}

func TestWorkspace_UpdateContainerPatch_MultipleSeqs(t *testing.T) {
	ss := NewSyncState()

	seqs := []int64{1, 3, 7}
	for i, seq := range seqs {
		event := &PatchEvent{
			Path:         "pkg/foo/bar.go",
			ContainerSeq: seq,
			Content:      "v" + itoa(seq),
		}
		meta, err := ss.UpdateContainerPatch("pkg/foo/bar.go", event)
		if err != nil {
			t.Fatalf("round %d: unexpected error: %v", i, err)
		}
		if meta.ContainerSeq != seq {
			t.Errorf("round %d: ContainerSeq = %d; want %d", i, meta.ContainerSeq, seq)
		}
		if meta.LastSyncedContainer != seq {
			t.Errorf("round %d: LastSyncedContainer = %d; want %d", i, meta.LastSyncedContainer, seq)
		}
	}
}

// --- SyncState: ApplyBrowserOp ---

func TestWorkspace_ApplyBrowserOp_BumpsSeqs(t *testing.T) {
	ss := NewSyncState()

	meta, err := ss.ApplyBrowserOp("pkg/foo/bar.go", "new content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First op bumps BrowserSeq from 0 to 1.
	if meta.BrowserSeq != 1 {
		t.Errorf("BrowserSeq = %d; want 1", meta.BrowserSeq)
	}
	if meta.LastSyncedBrowser != 1 {
		t.Errorf("LastSyncedBrowser = %d; want 1", meta.LastSyncedBrowser)
	}
	// ContainerSeq syncs to match BrowserSeq.
	if meta.ContainerSeq != 1 {
		t.Errorf("ContainerSeq = %d; want 1", meta.ContainerSeq)
	}
}

func TestWorkspace_ApplyBrowserOp_SecondOp_BumpsAgain(t *testing.T) {
	ss := NewSyncState()

	_, err := ss.ApplyBrowserOp("pkg/foo/bar.go", "first")
	if err != nil {
		t.Fatalf("first op: %v", err)
	}

	meta, err := ss.ApplyBrowserOp("pkg/foo/bar.go", "second")
	if err != nil {
		t.Fatalf("second op: %v", err)
	}

	if meta.BrowserSeq != 2 {
		t.Errorf("BrowserSeq = %d; want 2", meta.BrowserSeq)
	}
	if meta.LastSyncedBrowser != 2 {
		t.Errorf("LastSyncedBrowser = %d; want 2", meta.LastSyncedBrowser)
	}
	if meta.ContainerSeq != 2 {
		t.Errorf("ContainerSeq = %d; want 2", meta.ContainerSeq)
	}
}

func TestWorkspace_ApplyBrowserOp_MultipleFiles_IndependentSeqs(t *testing.T) {
	ss := NewSyncState()

	_, err := ss.ApplyBrowserOp("a.go", "content a")
	if err != nil {
		t.Fatalf("a.go: %v", err)
	}
	_, err = ss.ApplyBrowserOp("a.go", "content a2")
	if err != nil {
		t.Fatalf("a.go second: %v", err)
	}
	_, err = ss.ApplyBrowserOp("b.go", "content b")
	if err != nil {
		t.Fatalf("b.go: %v", err)
	}

	metaA, ok := ss.GetMetadata("a.go")
	if !ok {
		t.Fatal("a.go metadata not found")
	}
	if metaA.BrowserSeq != 2 {
		t.Errorf("a.go BrowserSeq = %d; want 2", metaA.BrowserSeq)
	}

	metaB, ok := ss.GetMetadata("b.go")
	if !ok {
		t.Fatal("b.go metadata not found")
	}
	if metaB.BrowserSeq != 1 {
		t.Errorf("b.go BrowserSeq = %d; want 1", metaB.BrowserSeq)
	}
}

// --- SyncState: GetMetadata ---

func TestWorkspace_GetMetadata_Nonexistent_ReturnsNilFalse(t *testing.T) {
	ss := NewSyncState()
	meta, ok := ss.GetMetadata("no-such-file.go")
	if ok {
		t.Error("expected ok=false for nonexistent path")
	}
	if meta != nil {
		t.Error("expected nil metadata for nonexistent path")
	}
}

func TestWorkspace_GetMetadata_ReturnsCopy(t *testing.T) {
	ss := NewSyncState()
	_, err := ss.ApplyBrowserOp("x.go", "initial")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Get metadata, mutate the returned copy, then get again.
	meta1, ok := ss.GetMetadata("x.go")
	if !ok {
		t.Fatal("x.go not found")
	}
	meta1.BrowserSeq = 999

	meta2, ok := ss.GetMetadata("x.go")
	if !ok {
		t.Fatal("x.go not found on second get")
	}
	if meta2.BrowserSeq != 1 {
		t.Errorf("second GetMetadata should return original value (1); got %d", meta2.BrowserSeq)
	}
}

// --- SyncState: GetAllMetadata ---

func TestWorkspace_GetAllMetadata_Empty(t *testing.T) {
	ss := NewSyncState()
	result := ss.GetAllMetadata()
	if len(result) != 0 {
		t.Errorf("expected empty map; got %d entries", len(result))
	}
}

func TestWorkspace_GetAllMetadata_ReturnsIndependentCopies(t *testing.T) {
	ss := NewSyncState()
	_, err := ss.ApplyBrowserOp("a.go", "content")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err = ss.ApplyBrowserOp("b.go", "content")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	snapshot := ss.GetAllMetadata()
	if len(snapshot) != 2 {
		t.Fatalf("expected 2 entries; got %d", len(snapshot))
	}

	// Mutate the returned values.
	snapshot["a.go"].BrowserSeq = -1
	snapshot["b.go"].ContainerSeq = -1

	// Internal state should be unaffected.
	inner, ok := ss.GetMetadata("a.go")
	if !ok {
		t.Fatal("a.go not found after snapshot mutation")
	}
	if inner.BrowserSeq != 1 {
		t.Errorf("internal a.go BrowserSeq should be 1; got %d (snapshot mutation leaked)", inner.BrowserSeq)
	}
	inner, ok = ss.GetMetadata("b.go")
	if !ok {
		t.Fatal("b.go not found after snapshot mutation")
	}
	if inner.ContainerSeq != 1 {
		t.Errorf("internal b.go ContainerSeq should be 1; got %d (snapshot mutation leaked)", inner.ContainerSeq)
	}
}

// --- Envelope construction ---

func TestWorkspace_NewPatchInEnvelope(t *testing.T) {
	env := NewPatchInEnvelope("file content", "pkg/x.go", 42)

	if env.Type != EnvelopeTypePatchIn {
		t.Errorf("Type = %q; want %q", env.Type, EnvelopeTypePatchIn)
	}

	payload, ok := env.Payload.(PatchInPayload)
	if !ok {
		t.Fatalf("Payload is %T; want PatchInPayload", env.Payload)
	}

	if payload.Path != "pkg/x.go" {
		t.Errorf("payload.Path = %q; want %q", payload.Path, "pkg/x.go")
	}
	if payload.Content != "file content" {
		t.Errorf("payload.Content = %q; want %q", payload.Content, "file content")
	}
	if payload.BrowserSeq != 42 {
		t.Errorf("payload.BrowserSeq = %d; want 42", payload.BrowserSeq)
	}

	if env.Error != "" {
		t.Errorf("Error should be empty; got %q", env.Error)
	}
}

func TestWorkspace_NewPatchOutEnvelope(t *testing.T) {
	event := &PatchEvent{
		Path:         "pkg/y.go",
		ContainerSeq: 7,
		Content:      "package y\n",
	}

	env := NewPatchOutEnvelope(event)

	if env.Type != EnvelopeTypePatchOut {
		t.Errorf("Type = %q; want %q", env.Type, EnvelopeTypePatchOut)
	}

	payload, ok := env.Payload.(*PatchEvent)
	if !ok {
		t.Fatalf("Payload is %T; want *PatchEvent", env.Payload)
	}

	if payload.Path != "pkg/y.go" {
		t.Errorf("payload.Path = %q; want %q", payload.Path, "pkg/y.go")
	}
	if payload.ContainerSeq != 7 {
		t.Errorf("payload.ContainerSeq = %d; want 7", payload.ContainerSeq)
	}
}

func TestWorkspace_NewHeartbeatEnvelope(t *testing.T) {
	env := NewHeartbeatEnvelope()

	if env.Type != EnvelopeTypeHeartbeat {
		t.Errorf("Type = %q; want %q", env.Type, EnvelopeTypeHeartbeat)
	}
	// Heartbeat can have nil payload.
	if env.Payload != nil {
		t.Errorf("Payload should be nil for heartbeat; got %v", env.Payload)
	}
}

func TestWorkspace_EnvelopeTypeConstants(t *testing.T) {
	if EnvelopeTypePatchIn != "workspace.patch_in" {
		t.Errorf("EnvelopeTypePatchIn = %q; want %q", EnvelopeTypePatchIn, "workspace.patch_in")
	}
	if EnvelopeTypePatchOut != "workspace.patch_out" {
		t.Errorf("EnvelopeTypePatchOut = %q; want %q", EnvelopeTypePatchOut, "workspace.patch_out")
	}
	if EnvelopeTypeHeartbeat != "workspace.heartbeat" {
		t.Errorf("EnvelopeTypeHeartbeat = %q; want %q", EnvelopeTypeHeartbeat, "workspace.heartbeat")
	}
}

// --- Thread safety ---

func TestWorkspace_UpdateContainerPatch_Concurrent_NoRace(t *testing.T) {
	ss := NewSyncState()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := "concurrent/file" + itoa(int64(idx)) + ".go"
			event := &PatchEvent{
				Path:         path,
				ContainerSeq: int64(idx) + 1,
				Content:      "content",
			}
			_, err := ss.UpdateContainerPatch(path, event)
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	snapshot := ss.GetAllMetadata()
	if len(snapshot) != 10 {
		t.Errorf("expected 10 files; got %d", len(snapshot))
	}
}

func TestWorkspace_ApplyBrowserOp_Concurrent_NoRace(t *testing.T) {
	ss := NewSyncState()
	var wg sync.WaitGroup

	// Multiple goroutines operate on different files.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := "concurrent/op" + itoa(int64(idx)) + ".go"
			_, err := ss.ApplyBrowserOp(path, "content")
			if err != nil {
				t.Errorf("goroutine %d: unexpected error: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	snapshot := ss.GetAllMetadata()
	if len(snapshot) != 10 {
		t.Errorf("expected 10 files; got %d", len(snapshot))
	}
}

func TestWorkspace_MixedOperations_Concurrent_NoRace(t *testing.T) {
	ss := NewSyncState()
	var wg sync.WaitGroup

	// Interleave UpdateContainerPatch and ApplyBrowserOp across shared paths.
	for i := 0; i < 5; i++ {
		wg.Add(2)
		path := "mixed/shared" + itoa(int64(i)) + ".go"
		go func() {
			defer wg.Done()
			event := &PatchEvent{
				Path:         path,
				ContainerSeq: 1,
				Content:      "container",
			}
			ss.UpdateContainerPatch(path, event)
		}()
		go func() {
			defer wg.Done()
			ss.ApplyBrowserOp(path, "browser")
		}()
	}
	wg.Wait()

	// All 5 paths should exist (exact seqs are non-deterministic due to race,
	// but we can verify presence).
	snapshot := ss.GetAllMetadata()
	if len(snapshot) != 5 {
		t.Errorf("expected 5 files; got %d", len(snapshot))
	}
}

// --- Integration: Container emits, browser acks ---

func TestWorkspace_ContainerPatchThenBrowserAck_FullFlow(t *testing.T) {
	ss := NewSyncState()

	// 1. Container emits patch (agent writes file).
	event := &PatchEvent{
		Path:           "pkg/app/main.go",
		ContainerSeq:   3,
		Content:        "package main\n",
		BaseBrowserSeq: 0,
	}
	meta, err := ss.UpdateContainerPatch("pkg/app/main.go", event)
	if err != nil {
		t.Fatalf("UpdateContainerPatch: %v", err)
	}
	if meta.ContainerSeq != 3 {
		t.Errorf("ContainerSeq = %d; want 3", meta.ContainerSeq)
	}
	if meta.LastSyncedContainer != 3 {
		t.Errorf("LastSyncedContainer = %d; want 3", meta.LastSyncedContainer)
	}

	// 2. Browser applies an edit (user makes change in editor).
	meta, err = ss.ApplyBrowserOp("pkg/app/main.go", "package main\n// edited")
	if err != nil {
		t.Fatalf("ApplyBrowserOp: %v", err)
	}
	// BrowserSeq bumps to 1, ContainerSeq syncs to match.
	if meta.BrowserSeq != 1 {
		t.Errorf("BrowserSeq = %d; want 1", meta.BrowserSeq)
	}
	if meta.LastSyncedBrowser != 1 {
		t.Errorf("LastSyncedBrowser = %d; want 1", meta.LastSyncedBrowser)
	}
	if meta.ContainerSeq != 1 {
		t.Errorf("ContainerSeq = %d; want 1", meta.ContainerSeq)
	}

	// 3. Now container can push again (browser is synced).
	event2 := &PatchEvent{
		Path:         "pkg/app/main.go",
		ContainerSeq: 4,
		Content:      "package main\n// updated",
	}
	meta, err = ss.UpdateContainerPatch("pkg/app/main.go", event2)
	if err != nil {
		t.Fatalf("second UpdateContainerPatch: %v", err)
	}
	if meta.ContainerSeq != 4 {
		t.Errorf("ContainerSeq = %d; want 4", meta.ContainerSeq)
	}
	if meta.LastSyncedContainer != 4 {
		t.Errorf("LastSyncedContainer = %d; want 4", meta.LastSyncedContainer)
	}
}

// --- Helper ---

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && findSubstr(s, substr)
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
