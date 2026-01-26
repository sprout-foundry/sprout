package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGroupChangesByRevisionAndActive(t *testing.T) {
	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	changes := []ChangeLog{
		{RequestHash: "r1", FileRevisionHash: "f1", Filename: "a.txt", Status: "active", Timestamp: t1},
		{RequestHash: "r1", FileRevisionHash: "f2", Filename: "b.txt", Status: "reverted", Timestamp: t2},
		{RequestHash: "r2", FileRevisionHash: "f3", Filename: "c.txt", Status: "active", Timestamp: t2},
	}
	groups := groupChangesByRevision(changes)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// Ensure groups are sorted by timestamp desc (r2 first)
	if groups[0].RevisionID != "r2" {
		t.Fatalf("expected r2 first, got %s", groups[0].RevisionID)
	}
	// getActiveChanges returns only active entries
	act := getActiveChanges(groups[1].Changes)
	if len(act) != 1 || act[0].Filename != "a.txt" {
		t.Fatalf("unexpected active set: %v", act)
	}
}

func TestSortChangesByTimestamp(t *testing.T) {
	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	arr := []ChangeLog{
		{Timestamp: t1}, {Timestamp: t2}, {Timestamp: t1},
	}
	sortChangesByTimestamp(arr)
	if !(arr[0].Timestamp.Equal(t2) && arr[1].Timestamp.Equal(t1)) {
		t.Fatalf("expected sorted desc by timestamp, got %v", arr)
	}
}

func TestRecordAndFetchChanges_Roundtrip(t *testing.T) {
	// Run in isolated temp working dir
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	// Create base revision and a change
	revID, err := RecordBaseRevision("req1", "do x", "ok", []APIMessage{})
	if err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}
	if revID == "" {
		t.Fatalf("expected non-empty revision id")
	}

	if err := RecordChangeWithDetails(revID, "file.go", "old", "new", "desc", "note", "prompt", "llm-msg", "model-x"); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	// Fetch and validate
	list, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 change, got %d", len(list))
	}
	if list[0].Filename != "file.go" || list[0].Description != "desc" || list[0].OriginalCode != "old" || list[0].NewCode != "new" {
		t.Fatalf("unexpected change data: %+v", list[0])
	}

	// Update status and verify via underlying file
	if err := updateChangeStatus(list[0].FileRevisionHash, "reverted"); err != nil {
		t.Fatalf("updateChangeStatus: %v", err)
	}
	metaPath := filepath.Join(".ledit/changes", list[0].FileRevisionHash, "metadata.json")
	b, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if string(b) == "" {
		t.Fatalf("metadata unexpectedly empty")
	}
}
