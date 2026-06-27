package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// SP-077: ChangeTracker must not record recoverable snapshots for deltas
// caused by git operations (merge, checkout, reset, pull).
//
// The root cause: when a git command brings committed content into the
// working tree, the shell-mutation walker sees the resulting file changes
// as agent-authored mutations. The "before" bytes (stale relative to the
// now-current HEAD) get recorded as recoverable OriginalCode, and a later
// recovery/rollback writes them back — silently reverting committed work.
//
// The fix (filterGitSourcedDeltas) checks each delta's post-operation
// content against HEAD: if it matches, the delta was git-sourced and is
// suppressed.
//
// These tests reproduce the incident in real git repos to exercise the
// full git.CommittedFilePaths → filterGitSourcedDeltas path.
// ---------------------------------------------------------------------------

// runGitInDirCT runs a git command in dir, failing the test on error.
func runGitInDirCT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// setupGitRepoCT initializes a fresh git repo in a temp directory with a
// dummy user config and an initial commit (so HEAD exists).
func setupGitRepoCT(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitInDirCT(t, dir, "init", "-b", "main")
	runGitInDirCT(t, dir, "config", "user.email", "test@test.com")
	runGitInDirCT(t, dir, "config", "user.name", "Test")

	// Initial commit so HEAD exists.
	mustWriteFile(t, filepath.Join(dir, "init.go"), []byte("package x\n"))
	runGitInDirCT(t, dir, "add", "init.go")
	runGitInDirCT(t, dir, "commit", "-m", "initial commit")
	return dir
}

// TestSP077_SuppressesGitMergeDeltas reproduces the core incident: after
// a git merge brings committed content into the working tree, the shell
// tracker must NOT record those files as recoverable mutations.
//
// Setup: a committed file exists at HEAD. We simulate the pre-merge
// state by putting stale content on disk (mimicking a pre-merge tree),
// priming the tracker against it, then restoring HEAD content (mimicking
// the merge). The tracker should detect that the post-operation content
// matches HEAD and suppress the delta.
func TestSP077_SuppressesGitMergeDeltas(t *testing.T) {
	dir := setupGitRepoCT(t)

	// Create and commit a file at HEAD.
	committed := filepath.Join(dir, "feature.go")
	mustWriteFile(t, committed, []byte("package main\n\nfunc Feature() {}\n"))
	runGitInDirCT(t, dir, "add", "feature.go")
	runGitInDirCT(t, dir, "commit", "-m", "add feature")

	// Simulate the pre-merge state: put stale (pre-merge) content on
	// disk. This represents what the working tree looked like before
	// the merge brought in the committed content.
	staleContent := []byte("package main // pre-merge stub\n")
	mustWriteFile(t, committed, staleContent)

	tracker := newTrackerForShellTest(t)
	tracker.PrimeShellTracking(dir)

	// Simulate the merge: restore the committed content. The walker
	// will see this as an edit (stale → committed). Without SP-077,
	// it would record staleContent as recoverable OriginalCode.
	mustWriteFile(t, committed, []byte("package main\n\nfunc Feature() {}\n"))
	bumpMtime(t, committed)

	tracker.TrackShellTurn(dir, "git merge feature/something", false)

	// The delta should have been suppressed — no changes recorded.
	for _, ch := range tracker.changes {
		if ch.FilePath == committed && ch.OriginalCode != "" && ch.OriginalCode != "[CONTENT NOT CAPTURED: " {
			t.Errorf("SP-077: committed file delta was NOT suppressed — recorded OriginalCode for %s: op=%s", ch.FilePath, ch.Operation)
		}
	}
	if len(tracker.changes) == 0 {
		return // clean: no changes at all
	}
	// If any changes were recorded, they must not be for the committed file.
	for _, ch := range tracker.changes {
		if ch.FilePath == committed {
			t.Errorf("SP-077: expected zero changes for committed file, but got: %+v", ch)
		}
	}
}

// TestSP077_PreservesGitCheckoutDeltas confirms that `git checkout --
// file` — a destructive command that destroys uncommitted work — is
// PRESERVED by the filter so the destroyed content is recoverable via
// recover_file. This is the key behavior change from the original
// SP-077: destructive commands that align files to HEAD but destroy
// uncommitted work in the process must keep the delta so the work can
// be recovered.
func TestSP077_PreservesGitCheckoutDeltas(t *testing.T) {
	dir := setupGitRepoCT(t)

	// Create and commit a file at HEAD.
	file := filepath.Join(dir, "config.go")
	mustWriteFile(t, file, []byte("port = 8080\n"))
	runGitInDirCT(t, dir, "add", "config.go")
	runGitInDirCT(t, dir, "commit", "-m", "add config")

	// Simulate uncommitted changes (what the agent might have done).
	mustWriteFile(t, file, []byte("port = 9090\n"))
	bumpMtime(t, file)

	tracker := newTrackerForShellTest(t)
	tracker.PrimeShellTracking(dir)

	// Run the actual git checkout to revert to HEAD.
	runGitInDirCT(t, dir, "checkout", "--", "config.go")

	tracker.TrackShellTurn(dir, "git checkout -- config.go", true)

	// The checkout destroyed uncommitted work (port=9090). The delta
	// should be PRESERVED so the destroyed content is recoverable.
	var found bool
	for _, ch := range tracker.changes {
		if ch.FilePath == file {
			found = true
			if ch.OriginalCode != "port = 9090\n" {
				t.Errorf("expected OriginalCode 'port = 9090\\n', got %q", ch.OriginalCode)
			}
			if ch.NewCode != "port = 8080\n" {
				t.Errorf("expected NewCode 'port = 8080\\n', got %q", ch.NewCode)
			}
		}
	}
	if !found {
		t.Errorf("SP-077: git checkout delta was NOT preserved — expected recoverable entry for destroyed uncommitted work on %s", file)
	}
}

// TestSP077_KeepsLegitimateEdits confirms that normal agent edits (sed,
// awk, formatters) which produce content DIFFERENT from HEAD are still
// tracked. This is the critical non-regression: the filter must not
// suppress real agent work, only git-sourced deltas.
func TestSP077_KeepsLegitimateEdits(t *testing.T) {
	dir := setupGitRepoCT(t)

	// Create and commit a file at HEAD.
	file := filepath.Join(dir, "main.go")
	mustWriteFile(t, file, []byte("package main\n\nfunc main() {}\n"))
	runGitInDirCT(t, dir, "add", "main.go")
	runGitInDirCT(t, dir, "commit", "-m", "add main")

	tracker := newTrackerForShellTest(t)
	tracker.PrimeShellTracking(dir)

	// Simulate a legitimate agent edit (sed -i, formatter, etc.) that
	// produces content DIFFERENT from HEAD.
	mustWriteFile(t, file, []byte("package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"))
	bumpMtime(t, file)

	tracker.TrackShellTurn(dir, "sed -i 's/}/.../' main.go", false)

	// The edit should be tracked — it's real agent work, not a git op.
	found := false
	for _, ch := range tracker.changes {
		if ch.FilePath == file {
			found = true
			if ch.OriginalCode != "package main\n\nfunc main() {}\n" {
				t.Errorf("expected OriginalCode to be the committed content, got %q", ch.OriginalCode)
			}
			if ch.Operation != "edit" {
				t.Errorf("expected op=edit, got %q", ch.Operation)
			}
		}
	}
	if !found {
		t.Errorf("SP-077: legitimate edit was incorrectly suppressed for %s", file)
	}
}

// TestSP077_KeepsFileDeletions confirms that real file deletions (rm)
// are still tracked even in a git repo. Deletes have After==nil so they
// can never match HEAD — the filter must pass them through.
func TestSP077_KeepsFileDeletions(t *testing.T) {
	dir := setupGitRepoCT(t)

	// Create and commit a file.
	file := filepath.Join(dir, "temp.go")
	mustWriteFile(t, file, []byte("package main\n"))
	runGitInDirCT(t, dir, "add", "temp.go")
	runGitInDirCT(t, dir, "commit", "-m", "add temp")

	// Now delete it (simulating `rm`).
	tracker := newTrackerForShellTest(t)
	tracker.PrimeShellTracking(dir)

	if err := os.Remove(file); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	tracker.TrackShellTurn(dir, "rm temp.go", false)

	// The deletion should be tracked — it's a real file removal, not
	// a git-sourced delta. The content is gone from disk and does NOT
	// match HEAD, so the filter correctly passes it through.
	found := false
	for _, ch := range tracker.changes {
		if ch.FilePath == file {
			found = true
			if ch.Operation != "delete" {
				t.Errorf("expected op=delete, got %q", ch.Operation)
			}
		}
	}
	if !found {
		t.Errorf("SP-077: real file deletion was incorrectly suppressed for %s", file)
	}
}

// TestSP077_KeepsUntrackedFileCreations confirms that newly-created
// untracked files (not yet committed) are still tracked. These are the
// primary recovery value of the ChangeTracker — the user wants to
// recover accidentally-created files.
func TestSP077_KeepsUntrackedFileCreations(t *testing.T) {
	dir := setupGitRepoCT(t)

	tracker := newTrackerForShellTest(t)
	tracker.PrimeShellTracking(dir)

	// Create a new untracked file (simulating `echo ... > newfile`).
	newFile := filepath.Join(dir, "scratch.txt")
	mustWriteFile(t, newFile, []byte("scratch content"))

	tracker.TrackShellTurn(dir, "echo 'scratch content' > scratch.txt", false)

	// The creation should be tracked — untracked files are never in
	// the committed set, so the filter passes them through.
	found := false
	for _, ch := range tracker.changes {
		if ch.FilePath == newFile {
			found = true
			if ch.Operation != "create" {
				t.Errorf("expected op=create, got %q", ch.Operation)
			}
		}
	}
	if !found {
		t.Errorf("SP-077: untracked file creation was incorrectly suppressed for %s", newFile)
	}
}

// TestSP077_NoGitRepoRecordsEverything confirms that in a non-git
// workspace, ALL deltas are recorded (no filtering). The filter is a
// no-op when git is not available.
func TestSP077_NoGitRepoRecordsEverything(t *testing.T) {
	dir := t.TempDir() // NO git init

	file := filepath.Join(dir, "config.txt")
	mustWriteFile(t, file, []byte("port=8080"))

	tracker := newTrackerForShellTest(t)
	tracker.PrimeShellTracking(dir)

	// Edit the file.
	mustWriteFile(t, file, []byte("port=9090"))
	bumpMtime(t, file)

	tracker.TrackShellTurn(dir, "sed -i 's/8080/9090/' config.txt", false)

	if len(tracker.changes) != 1 {
		t.Fatalf("expected 1 change in non-git repo, got %d: %+v", len(tracker.changes), tracker.changes)
	}
	ch := tracker.changes[0]
	if ch.FilePath != file {
		t.Errorf("expected change for %s, got %s", file, ch.FilePath)
	}
	if ch.OriginalCode != "port=8080" {
		t.Errorf("expected OriginalCode 'port=8080', got %q", ch.OriginalCode)
	}
}
