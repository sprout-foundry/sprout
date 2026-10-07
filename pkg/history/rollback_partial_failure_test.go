package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRollbackContinuesPastWriteFailure proves a revision rollback does not
// abort on the first file whose write fails: the remaining files are still
// rolled back, the returned error reports the failure, and the successfully
// rolled-back files are actually restored on disk.
func TestRollbackContinuesPastWriteFailure(t *testing.T) {
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldDir) }()

	revisionID, _ := RecordBaseRevision("partial-fail-rev", "partial failure", "resp", []APIMessage{})

	// Two good files plus one whose write-back fails: its recorded target is
	// a DIRECTORY, so the write errors (EISDIR). NewCode is empty so the
	// staleness guard has no baseline and allows the attempt (step 1).
	good := []string{"a_good.go", "c_good.go"}
	for _, f := range good {
		if err := os.WriteFile(f, []byte("new-"+f), 0600); err != nil {
			t.Fatal(err)
		}
		if err := RecordChangeWithDetails(revisionID, f, "orig-"+f, "new-"+f, "edit", "", "", "", "m"); err != nil {
			t.Fatalf("record change %s: %v", f, err)
		}
		if err := os.WriteFile(f, []byte("new-"+f), 0600); err != nil {
			t.Fatal(err)
		}
	}

	dirTarget := filepath.Join(testDir, "b_fail_dir")
	if err := os.Mkdir(dirTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	// NewCode empty → the staleness guard allows the write attempt.
	if err := RecordChangeWithDetails(revisionID, dirTarget, "orig-fail", "", "edit", "", "", "", "m"); err != nil {
		t.Fatalf("record change %s: %v", dirTarget, err)
	}

	err := RevertChangeByRevisionID(revisionID)
	if err == nil {
		t.Fatalf("expected a non-nil error naming the failed file count")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error should mention the failure, got: %v", err)
	}

	// The good files MUST be rolled back (the whole point: one failure does
	// not abort the rest).
	for _, f := range good {
		got, readErr := os.ReadFile(f)
		if readErr != nil {
			t.Fatalf("read %s: %v", f, readErr)
		}
		if string(got) != "orig-"+f {
			t.Errorf("%s not rolled back after a sibling failure: got %q", f, got)
		}
	}
}

// TestIsFileStaleReadErrorIsUnsafe proves a non-ENOENT read error is treated
// as stale (refuse), not as "file absent — safe to revert". A directory path
// yields a read error that is not IsNotExist.
func TestIsFileStaleReadErrorIsUnsafe(t *testing.T) {
	testDir := t.TempDir()
	dirAsFile := filepath.Join(testDir, "adir")
	if err := os.Mkdir(dirAsFile, 0o755); err != nil {
		t.Fatal(err)
	}
	// Reading a directory returns an error that is not IsNotExist.
	if _, err := os.ReadFile(dirAsFile); err == nil || os.IsNotExist(err) {
		t.Skip("platform returns no/ENOENT error reading a directory; cannot exercise this path")
	}
	if !isFileStale(dirAsFile, "some-new-content") {
		t.Error("a non-ENOENT read error must be treated as stale (unsafe), not safe")
	}
	if IsRevertSafeAt(dirAsFile, "some-new-content", "orig", time.Time{}) {
		t.Error("IsRevertSafeAt must refuse when the file cannot be read for a non-ENOENT reason")
	}
}

// TestRevertSafeAtMissingFileIsSafe pins the complementary case: a genuinely
// missing file is safe to restore.
func TestRevertSafeAtMissingFileIsSafe(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.go")
	if isFileStale(missing, "x") {
		t.Error("a missing file is safe to restore, not stale")
	}
	if !IsRevertSafeAt(missing, "x", "orig", time.Time{}) {
		t.Error("IsRevertSafeAt must allow restoring a missing file")
	}
}
