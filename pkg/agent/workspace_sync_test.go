package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCheckWriteStaleness_NewFileAllowed pins the "creating a new file
// never needs a prior read" branch — every other SP-046 §7 check is
// skipped when os.Stat returns a not-exist error.
func TestCheckWriteStaleness_NewFileAllowed(t *testing.T) {
	a := &Agent{}
	path := filepath.Join(t.TempDir(), "brand-new-file.txt")
	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("expected nil error for nonexistent file, got %v", err)
	}
}

// TestCheckWriteStaleness_NotReadThisTurnRefuses pins the core rule:
// writing to an existing file the agent never read should fail with a
// message the agent can act on (call read_file then retry).
func TestCheckWriteStaleness_NotReadThisTurnRefuses(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Age the file beyond the freshness window so we exercise the
	// "no read this turn" branch in isolation from "modified recently".
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	err := a.checkWriteStaleness(path)
	if err == nil {
		t.Fatal("expected refusal when file has not been read this turn")
	}
	if !strings.Contains(err.Error(), "read_file") {
		t.Errorf("error should suggest calling read_file, got %q", err)
	}
}

// TestCheckWriteStaleness_ReadThisTurnAllows pins the happy path: agent
// read the file, then wrote it. No external mutation, no refusal.
func TestCheckWriteStaleness_ReadThisTurnAllows(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	a.RecordFileReadThisTurn(path)

	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("expected nil error after recording a read, got %v", err)
	}
}

// TestCheckWriteStaleness_ResetForNewTurnInvalidates pins the turn-
// boundary reset: a read on turn N should not count as a read on
// turn N+1. Without this, the rule degenerates to "read once per
// session" which doesn't guard against state drift across turns.
func TestCheckWriteStaleness_ResetForNewTurnInvalidates(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	a.RecordFileReadThisTurn(path)
	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("first-turn check should pass, got %v", err)
	}

	a.ResetFileReadsForNewTurn()
	if err := a.checkWriteStaleness(path); err == nil {
		t.Errorf("after turn reset, expected refusal; got nil")
	}
}

// TestCheckWriteStaleness_ModifiedAfterReadRefuses pins the freshness-
// window check: agent read, then something external bumped the mtime
// (a sync push from the browser side, in the SP-046 model). Writing
// would lose the user's edit, so refuse.
func TestCheckWriteStaleness_ModifiedAfterReadRefuses(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "raced.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.RecordFileReadThisTurn(path)
	// Sleep a hair so the subsequent write definitely has a later mtime.
	time.Sleep(15 * time.Millisecond)
	if err := os.WriteFile(path, []byte("v2-from-browser"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := a.checkWriteStaleness(path)
	if err == nil {
		t.Fatal("expected refusal when file was modified after the read")
	}
	if !strings.Contains(err.Error(), "modified") {
		t.Errorf("error should mention the external modification, got %q", err)
	}
}

// TestCheckWriteStaleness_NilAgentNoOp confirms the rule is safe to call
// from contexts that lack a configured Agent (test scaffolding, lazy
// init paths). Avoids cascading nil-panic regressions.
func TestCheckWriteStaleness_NilAgentNoOp(t *testing.T) {
	var a *Agent
	if err := a.checkWriteStaleness("/nonexistent"); err != nil {
		t.Errorf("nil agent should be a no-op, got %v", err)
	}
}

// TestCheckWriteStaleness_UnsyncedEditsRefuses pins the SP-046 §3 conflict
// rule: if the platform sync layer has flagged unsynced browser edits, the
// agent must NOT auto-retry — it should ask the user. Distinguished from
// the staleness rule via errors.Is so the agent's reasoning can branch.
func TestCheckWriteStaleness_UnsyncedEditsRefuses(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "raced.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Even with a fresh read recorded, unsynced edits take precedence.
	a.RecordFileReadThisTurn(path)
	a.SetFileMetadata(path, WorkspaceFileMetadata{
		BrowserSeq:        7,
		LastSyncedBrowser: 5,
	})

	err := a.checkWriteStaleness(path)
	if err == nil {
		t.Fatal("expected refusal with unsynced edits flagged")
	}
	if !errors.Is(err, ErrWriteHasUnsyncedEdits) {
		t.Errorf("error should wrap ErrWriteHasUnsyncedEdits, got %v", err)
	}
	if errors.Is(err, ErrWriteStale) {
		t.Errorf("error should NOT also wrap ErrWriteStale (would confuse the agent's branch)")
	}
	if !strings.Contains(err.Error(), "ask the user") {
		t.Errorf("message should tell the agent to ask the user, got %q", err)
	}
}

// TestCheckWriteStaleness_SyncedMetadataAllows pins the happy path for
// the conflict check: BrowserSeq == LastSyncedBrowser means everything is
// caught up; the regular staleness rule takes over.
func TestCheckWriteStaleness_SyncedMetadataAllows(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "synced.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	a.RecordFileReadThisTurn(path)
	a.SetFileMetadata(path, WorkspaceFileMetadata{
		BrowserSeq:        5,
		LastSyncedBrowser: 5, // caught up
	})

	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("expected nil error with synced metadata, got %v", err)
	}
}

// TestCheckWriteStaleness_StalenessErrorClassification ensures the
// existing "no read this turn" branch reports ErrWriteStale (not
// ErrWriteHasUnsyncedEdits) so the agent's branch in the tool-result
// handler routes correctly.
func TestCheckWriteStaleness_StalenessErrorClassification(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	err := a.checkWriteStaleness(path)
	if err == nil {
		t.Fatal("expected refusal")
	}
	if !errors.Is(err, ErrWriteStale) {
		t.Errorf("error should wrap ErrWriteStale, got %v", err)
	}
	if errors.Is(err, ErrWriteHasUnsyncedEdits) {
		t.Errorf("error should NOT wrap ErrWriteHasUnsyncedEdits")
	}
}

// TestCheckWriteStaleness_FreeTierDegenerate is the SP-046-1e
// verification: a free-tier WASM page that never calls setSyncEndpoint or
// applyFileMetadata should see exactly the native single-replica
// behavior. The conflict-detection path stays a no-op (zero-value
// metadata means BrowserSeq == LastSyncedBrowser == 0, hasUnsynced
// returns false), and the staleness rule's intra-turn check still fires.
//
// If this test ever breaks, the platform-free path has acquired a
// dependency on platform-side metadata pushes — which would silently
// degrade free-tier UX.
func TestCheckWriteStaleness_FreeTierDegenerate(t *testing.T) {
	a := &Agent{} // no SetFileMetadata calls anywhere
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	// Confirms: no metadata cached, GetFileMetadata returns ok=false.
	if _, ok := a.GetFileMetadata(path); ok {
		t.Fatalf("free-tier should have zero cached metadata, but %q was present", path)
	}

	// Without a read this turn → ErrWriteStale (NOT ErrWriteHasUnsyncedEdits).
	err := a.checkWriteStaleness(path)
	if err == nil || !errors.Is(err, ErrWriteStale) {
		t.Errorf("free-tier no-read should be ErrWriteStale, got %v", err)
	}
	if errors.Is(err, ErrWriteHasUnsyncedEdits) {
		t.Errorf("free-tier should never trigger ErrWriteHasUnsyncedEdits")
	}

	// With a read this turn → allowed (single-replica happy path).
	a.RecordFileReadThisTurn(path)
	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("free-tier read-then-write should succeed, got %v", err)
	}
}

// TestSetFileMetadata_RoundTrip verifies that the in-memory store
// preserves values across set/get cycles, including overwriting a prior
// entry (the sync layer expects to call SetFileMetadata repeatedly as
// browser-side edits arrive).
func TestSetFileMetadata_RoundTrip(t *testing.T) {
	a := &Agent{}
	a.SetFileMetadata("a.txt", WorkspaceFileMetadata{BrowserSeq: 1})
	a.SetFileMetadata("b.txt", WorkspaceFileMetadata{BrowserSeq: 2})
	a.SetFileMetadata("a.txt", WorkspaceFileMetadata{BrowserSeq: 3}) // overwrite

	if md, ok := a.GetFileMetadata("a.txt"); !ok || md.BrowserSeq != 3 {
		t.Errorf("a.txt = %+v ok=%v, want BrowserSeq=3", md, ok)
	}
	if md, ok := a.GetFileMetadata("b.txt"); !ok || md.BrowserSeq != 2 {
		t.Errorf("b.txt = %+v ok=%v, want BrowserSeq=2", md, ok)
	}
	if _, ok := a.GetFileMetadata("missing.txt"); ok {
		t.Errorf("missing.txt should not be present")
	}
}

// TestWorkspaceFileMetadata_UnsyncedDetection pins the conflict
// predicate used by the platform-side sync engine. Cheap to test now;
// expensive to debug later if the inequality direction drifts.
func TestWorkspaceFileMetadata_UnsyncedDetection(t *testing.T) {
	cases := []struct {
		name string
		md   WorkspaceFileMetadata
		want bool
	}{
		{"all zero", WorkspaceFileMetadata{}, false},
		{"in sync", WorkspaceFileMetadata{BrowserSeq: 5, LastSyncedBrowser: 5}, false},
		{"unsynced edit", WorkspaceFileMetadata{BrowserSeq: 6, LastSyncedBrowser: 5}, true},
		{"impossible past-future", WorkspaceFileMetadata{BrowserSeq: 4, LastSyncedBrowser: 5}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.md.HasUnsyncedBrowserEdits(); got != c.want {
				t.Errorf("HasUnsyncedBrowserEdits = %v, want %v", got, c.want)
			}
		})
	}
}
