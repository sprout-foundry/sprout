package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// setupCompactionTestDirs swaps the package-level changesDir /
// revisionsDir to t.TempDir() so tests don't touch real history.
// Returns (revisionsDir, changesDir) for direct asserts.
func setupCompactionTestDirs(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	revDir := filepath.Join(tmp, "revisions")
	chgDir := filepath.Join(tmp, "changes")
	if err := os.MkdirAll(revDir, 0o755); err != nil {
		t.Fatalf("mkdir revisions: %v", err)
	}
	if err := os.MkdirAll(chgDir, 0o755); err != nil {
		t.Fatalf("mkdir changes: %v", err)
	}
	setPathsForTesting(chgDir, revDir)
	t.Cleanup(func() {
		setPathsForTesting(projectChangesDir, projectRevisionsDir)
	})
	return revDir, chgDir
}

// fabricateRevision creates a hot-tier revision directory with
// instructions.txt + llm_response.txt + conversation.json and N
// associated change records (with payloads). Returns the revision ID.
//
// `ageOffset` lets the test stagger creation order via mtime; more
// negative = older.
func fabricateRevision(t *testing.T, revDir, chgDir, id string, ageOffset time.Duration, files []string) {
	t.Helper()
	rPath := filepath.Join(revDir, id)
	if err := os.MkdirAll(rPath, 0o755); err != nil {
		t.Fatalf("mkdir rev: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rPath, "instructions.txt"), []byte("do the thing"), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rPath, "llm_response.txt"), []byte("here is the response"), 0o644); err != nil {
		t.Fatalf("write response: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rPath, "conversation.json"), []byte(`[{"role":"user","content":"hi"}]`), 0o644); err != nil {
		t.Fatalf("write conversation: %v", err)
	}

	for i, fname := range files {
		cHash := id + "-" + strconv.Itoa(i)
		cPath := filepath.Join(chgDir, cHash)
		if err := os.MkdirAll(cPath, 0o755); err != nil {
			t.Fatalf("mkdir change: %v", err)
		}
		meta := ChangeMetadata{
			Version:          1,
			Filename:         fname,
			FileRevisionHash: cHash,
			RequestHash:      id,
			Timestamp:        time.Now().Add(ageOffset),
			Status:           "active",
		}
		mb, _ := json.MarshalIndent(meta, "", "  ")
		if err := os.WriteFile(filepath.Join(cPath, "metadata.json"), mb, 0o644); err != nil {
			t.Fatalf("write meta: %v", err)
		}
		// Payload files (kept simple; tests only check
		// presence/absence after compaction).
		safe := fname
		for j := 0; j < len(safe); j++ {
			if safe[j] == '/' || safe[j] == '\\' {
				safe = safe[:j] + "_" + safe[j+1:]
			}
		}
		_ = os.WriteFile(filepath.Join(cPath, safe+".original"), []byte("OLD"), 0o644)
		_ = os.WriteFile(filepath.Join(cPath, safe+".updated"), []byte("NEW"), 0o644)
	}

	// Stamp mtime so test ordering is deterministic regardless of
	// real-clock granularity.
	at := time.Now().Add(ageOffset)
	if err := os.Chtimes(rPath, at, at); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func TestCompactRevisions_TierBoundaries(t *testing.T) {
	revDir, chgDir := setupCompactionTestDirs(t)

	// Three revisions, distinct mtimes: newest, middle, oldest.
	fabricateRevision(t, revDir, chgDir, "rev-newest", -1*time.Second, []string{"a.go"})
	fabricateRevision(t, revDir, chgDir, "rev-middle", -10*time.Second, []string{"b.go"})
	fabricateRevision(t, revDir, chgDir, "rev-oldest", -20*time.Second, []string{"c.go"})

	// Hot=1, Warm=1, rest dropped.
	policy := RetentionPolicy{HotCount: 1, WarmCount: 1}
	stats, err := CompactRevisions(policy)
	if err != nil {
		t.Fatalf("CompactRevisions: %v", err)
	}
	if stats.HotKept != 1 {
		t.Errorf("expected 1 hot, got %+v", stats)
	}
	if stats.WarmDemoted != 1 {
		t.Errorf("expected 1 warm demoted, got %+v", stats)
	}
	if stats.Dropped != 1 {
		t.Errorf("expected 1 dropped, got %+v", stats)
	}

	// Hot: conversation.json present.
	if !fileExists(filepath.Join(revDir, "rev-newest", "conversation.json")) {
		t.Errorf("hot rev should keep conversation.json")
	}

	// Warm: conversation.json gone but instructions intact.
	if fileExists(filepath.Join(revDir, "rev-middle", "conversation.json")) {
		t.Errorf("warm rev should drop conversation.json")
	}
	if !fileExists(filepath.Join(revDir, "rev-middle", "instructions.txt")) {
		t.Errorf("warm rev should keep instructions.txt")
	}

	// Dropped: directory and change records gone.
	if _, err := os.Stat(filepath.Join(revDir, "rev-oldest")); !os.IsNotExist(err) {
		t.Errorf("dropped rev dir should be gone")
	}
	if _, err := os.Stat(filepath.Join(chgDir, "rev-oldest-0")); !os.IsNotExist(err) {
		t.Errorf("dropped rev's change dir should also be gone")
	}
}

func TestCompactRevisions_IsIdempotent(t *testing.T) {
	revDir, chgDir := setupCompactionTestDirs(t)

	fabricateRevision(t, revDir, chgDir, "rev1", -1*time.Second, []string{"a.go"})
	fabricateRevision(t, revDir, chgDir, "rev2", -10*time.Second, []string{"b.go"})

	policy := RetentionPolicy{HotCount: 1, WarmCount: 1}

	// First pass: rev2 demoted to warm.
	if _, err := CompactRevisions(policy); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	// Second pass: must be a no-op (no demotions, no drops).
	stats, err := CompactRevisions(policy)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if stats.WarmDemoted != 0 || stats.Dropped != 0 {
		t.Errorf("second pass should be a no-op; got %+v", stats)
	}
}

func TestTouchRevision_BumpsMtime(t *testing.T) {
	revDir, chgDir := setupCompactionTestDirs(t)

	fabricateRevision(t, revDir, chgDir, "rev1", -1*time.Hour, []string{"a.go"})
	before, err := os.Stat(filepath.Join(revDir, "rev1"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if err := TouchRevision("rev1"); err != nil {
		t.Fatalf("TouchRevision: %v", err)
	}

	after, _ := os.Stat(filepath.Join(revDir, "rev1"))
	if !after.ModTime().After(before.ModTime()) {
		t.Errorf("TouchRevision should advance mtime: before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

func TestCompactRevisions_TouchedWarmRevPromotedBackToHot(t *testing.T) {
	revDir, chgDir := setupCompactionTestDirs(t)

	// rev-old starts oldest → warm tier on first pass.
	fabricateRevision(t, revDir, chgDir, "rev-old", -1*time.Hour, []string{"old.go"})
	fabricateRevision(t, revDir, chgDir, "rev-new1", -1*time.Second, []string{"n1.go"})
	fabricateRevision(t, revDir, chgDir, "rev-new2", -2*time.Second, []string{"n2.go"})

	// Hot=2, Warm=1 — rev-old should land in warm.
	policy := RetentionPolicy{HotCount: 2, WarmCount: 1}
	if _, err := CompactRevisions(policy); err != nil {
		t.Fatalf("initial compaction: %v", err)
	}
	if fileExists(filepath.Join(revDir, "rev-old", "conversation.json")) {
		t.Fatalf("rev-old should be warm (conversation dropped)")
	}

	// User comes back; view_history touches rev-old → mtime bumped.
	if err := TouchRevision("rev-old"); err != nil {
		t.Fatalf("TouchRevision: %v", err)
	}

	// Next pass: rev-old is now newest by mtime, so it lands in hot.
	// Its conversation is gone (can't be restored), but the dir
	// survives — no drop because it's now in the hot tier.
	if _, err := CompactRevisions(policy); err != nil {
		t.Fatalf("post-touch compaction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(revDir, "rev-old")); os.IsNotExist(err) {
		t.Errorf("touched rev should survive next compaction pass")
	}
	// Verify mtime promotion put it ahead of a previously-hot rev:
	// one of rev-new1/rev-new2 must now be in warm (conversation gone).
	hotConv := fileExists(filepath.Join(revDir, "rev-new2", "conversation.json"))
	if hotConv {
		t.Errorf("rev-new2 should have been demoted to warm after rev-old's touch promoted it ahead")
	}
}

func TestCompactRevisions_NoOpOnEmptyDir(t *testing.T) {
	setupCompactionTestDirs(t)
	policy := RetentionPolicy{HotCount: 200, WarmCount: 500}
	stats, err := CompactRevisions(policy)
	if err != nil {
		t.Fatalf("CompactRevisions on empty: %v", err)
	}
	if stats.TotalRevisions != 0 {
		t.Errorf("expected 0 total on empty dir, got %d", stats.TotalRevisions)
	}
}

func TestCompactRevisions_ArchiveFrozenMovesToHoldingArea(t *testing.T) {
	revDir, chgDir := setupCompactionTestDirs(t)

	fabricateRevision(t, revDir, chgDir, "rev-keep", -1*time.Second, []string{"a.go"})
	fabricateRevision(t, revDir, chgDir, "rev-archive", -1*time.Hour, []string{"b.go"})

	policy := RetentionPolicy{HotCount: 1, ArchiveFrozen: true}
	if _, err := CompactRevisions(policy); err != nil {
		t.Fatalf("CompactRevisions: %v", err)
	}

	// rev-archive should have moved into _frozen/, not been deleted.
	if _, err := os.Stat(filepath.Join(revDir, "_frozen", "rev-archive")); err != nil {
		t.Errorf("archive_frozen=true should move dropped rev to _frozen/, got err: %v", err)
	}
	// Original location must be gone.
	if _, err := os.Stat(filepath.Join(revDir, "rev-archive")); !os.IsNotExist(err) {
		t.Errorf("archived rev should not remain at original path")
	}
}
