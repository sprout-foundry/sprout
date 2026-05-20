package agent

import (
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
			if got := c.md.hasUnsyncedBrowserEdits(); got != c.want {
				t.Errorf("hasUnsyncedBrowserEdits = %v, want %v", got, c.want)
			}
		})
	}
}
