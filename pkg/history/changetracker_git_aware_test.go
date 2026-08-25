package history

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// IsRevertSafe — git-aware staleness guard tests
//
// IsRevertSafe is the canonical guard for ALL rollback/revert paths. It
// layers a content-identity check (disk vs NewCode) with git-awareness
// (is the content committed to HEAD?). The git-awareness layer is the
// fix for the data-loss bug where an agent's edit was committed to git
// (content unchanged) and a later revert silently wrote OriginalCode
// back, undoing the committed work.
//
// The function is tested directly here — no history DB setup is needed
// because IsRevertSafe is a pure predicate over (filename, newCode) +
// the working-tree/git state. Each test sets up a real git repo (not
// just a t.TempDir) to exercise the git-awareness path.
// ---------------------------------------------------------------------------

// runGitInDir runs a git command in dir, failing the test on error.
func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// setupGitRepo initializes a fresh git repository in a temp directory
// with a dummy user config and an initial commit (so HEAD exists).
// Returns the repo directory path. Cleanup is automatic via t.TempDir().
func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitInDir(t, dir, "init", "-b", "main")
	runGitInDir(t, dir, "config", "user.email", "test@test.com")
	runGitInDir(t, dir, "config", "user.name", "Test")

	// Create an initial commit so HEAD exists.
	initPath := filepath.Join(dir, "init.go")
	assert.NoError(t, os.WriteFile(initPath, []byte("package x\n"), 0644))
	runGitInDir(t, dir, "add", "init.go")
	runGitInDir(t, dir, "commit", "-m", "initial commit")
	return dir
}

// commitFile writes content to filename inside dir, then stages and
// commits it. The filename is relative to dir.
func commitFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	fp := filepath.Join(dir, filename)
	assert.NoError(t, os.MkdirAll(filepath.Dir(fp), 0755))
	assert.NoError(t, os.WriteFile(fp, []byte(content), 0644))
	runGitInDir(t, dir, "add", filename)
	runGitInDir(t, dir, "commit", "-m", "add "+filename)
}

// writeFileInDir writes content to filename inside dir.
func writeFileInDir(t *testing.T, dir, filename, content string) {
	t.Helper()
	fp := filepath.Join(dir, filename)
	assert.NoError(t, os.MkdirAll(filepath.Dir(fp), 0755))
	assert.NoError(t, os.WriteFile(fp, []byte(content), 0644))
}

// initialCWDHist is captured at test binary startup, before any test
// has had a chance to os.Chdir into (and out of) a temp directory. It
// is used by withDirHist to guarantee a valid restore target.
var initialCWDHist, _ = os.Getwd()

// withDirHist runs fn inside dir and guarantees the original working
// directory is restored afterwards. It uses initialCWDHist (captured at
// process start) rather than os.Getwd(), which may return an error if a
// prior test left CWD pointing at a deleted temp directory.
func withDirHist(t *testing.T, dir string, fn func()) {
	t.Helper()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to change to %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(initialCWDHist); err != nil {
			t.Errorf("Failed to restore working directory: %v", err)
		}
	}()
	fn()
}

// ---------------------------------------------------------------------------
// Test cases for IsRevertSafe
// ---------------------------------------------------------------------------

// TestIsRevertSafe_CommittedFileMatchingNewCode_ReturnsFalse is THE
// bug scenario. A file was committed to git with content "v1". The
// staleness guard receives NewCode="v1" (as if the agent wrote it and
// the user committed). IsRevertSafe MUST return false — the content is
// committed and reverting to OriginalCode would silently undo committed
// work.
func TestIsRevertSafe_CommittedFileMatchingNewCode_ReturnsFalse(t *testing.T) {
	dir := setupGitRepo(t)
	content := "v1-agent-edit"
	commitFile(t, dir, "feature.txt", content)

	// disk == "v1" == NewCode == HEAD → committed-clean → NOT safe.
	withDirHist(t, dir, func() {
		safe := IsRevertSafe("feature.txt", content)
		assert.False(t, safe, "committed content matching NewCode must NOT be revert-safe (would undo committed work)")
	})
}

// TestIsRevertSafe_StaleFile_ReturnsFalse verifies the existing
// content-staleness behavior: the file on disk differs from NewCode (it
// was modified after the snapshot). This is NOT safe to revert — the
// revert would clobber the newer content.
func TestIsRevertSafe_StaleFile_ReturnsFalse(t *testing.T) {
	dir := setupGitRepo(t)
	writeFileInDir(t, dir, "stale.txt", "current-on-disk")

	// disk = "current-on-disk" ≠ NewCode = "agent-version" → stale.
	withDirHist(t, dir, func() {
		safe := IsRevertSafe("stale.txt", "agent-version")
		assert.False(t, safe, "a file modified after the snapshot must NOT be revert-safe")
	})
}

// TestIsRevertSafe_UntrackedFileMatchingNewCode_ReturnsTrue verifies
// that an untracked file (exists on disk, disk == NewCode, but NOT in
// git) is safe to revert — no git protection applies because the file
// is not version-controlled.
func TestIsRevertSafe_UntrackedFileMatchingNewCode_ReturnsTrue(t *testing.T) {
	dir := setupGitRepo(t)
	content := "untracked content"
	writeFileInDir(t, dir, "untracked.txt", content)

	// disk == NewCode == "untracked content", file is NOT tracked by git.
	withDirHist(t, dir, func() {
		safe := IsRevertSafe("untracked.txt", content)
		assert.True(t, safe, "an untracked file matching NewCode should be revert-safe (no git protection)")
	})
}

// TestIsRevertSafe_NonGitRepo_ReturnsTrue verifies that outside a git
// repository, IsRevertSafe falls back to the content-only check. When
// disk == NewCode, the content is not stale, so it is safe to revert.
func TestIsRevertSafe_NonGitRepo_ReturnsTrue(t *testing.T) {
	dir := t.TempDir() // NO git init
	content := "plain content"
	writeFileInDir(t, dir, "plain.txt", content)

	// disk == NewCode, not a git repo → content check passes → safe.
	withDirHist(t, dir, func() {
		safe := IsRevertSafe("plain.txt", content)
		assert.True(t, safe, "in a non-git repo, a file matching NewCode should be revert-safe")
	})
}

// TestIsRevertSafe_NonGitRepo_StaleFile_ReturnsFalse verifies the
// content-only staleness path still works outside a git repo: if disk
// differs from NewCode, the revert is refused.
func TestIsRevertSafe_NonGitRepo_StaleFile_ReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	writeFileInDir(t, dir, "stale.txt", "disk-content")

	withDirHist(t, dir, func() {
		safe := IsRevertSafe("stale.txt", "newcode-different")
		assert.False(t, safe, "in a non-git repo, a stale file must NOT be revert-safe")
	})
}

// TestIsRevertSafe_CommittedThenModifiedToNewContent_ReturnsTrue
// verifies the boundary: commit "v1", then write "v2" to the working
// tree (uncommitted). Now disk == "v2" == NewCode, but the file differs
// from HEAD. The content is NOT committed → it IS safe to revert (the
// git-protection path does not apply because there are uncommitted
// modifications).
func TestIsRevertSafe_CommittedThenModifiedToNewContent_ReturnsTrue(t *testing.T) {
	dir := setupGitRepo(t)
	commitFile(t, dir, "wip.txt", "v1")

	// Modify the file after committing — now uncommitted.
	writeFileInDir(t, dir, "wip.txt", "v2")

	// disk == "v2" == NewCode, but file differs from HEAD → not committed.
	withDirHist(t, dir, func() {
		safe := IsRevertSafe("wip.txt", "v2")
		assert.True(t, safe, "an uncommitted modification matching NewCode should be revert-safe (content not committed)")
	})
}

// TestIsRevertSafe_EmptyNewCode_ReturnsTrue verifies the edge case:
// empty NewCode means no baseline to compare against. The revert is
// allowed (matching historical isFileStale behavior).
func TestIsRevertSafe_EmptyNewCode_ReturnsTrue(t *testing.T) {
	dir := setupGitRepo(t)
	writeFileInDir(t, dir, "some.txt", "content")

	withDirHist(t, dir, func() {
		safe := IsRevertSafe("some.txt", "")
		assert.True(t, safe, "empty NewCode should be revert-safe (no baseline)")
	})
}

// TestIsRevertSafe_RedactedMarkerNewCode_ReturnsTrue verifies the edge
// case: RedactedContentMarker as NewCode means we can't compare (the
// content was redacted for an external file). The revert is allowed.
func TestIsRevertSafe_RedactedMarkerNewCode_ReturnsTrue(t *testing.T) {
	dir := setupGitRepo(t)
	writeFileInDir(t, dir, "external.txt", "secret")

	withDirHist(t, dir, func() {
		safe := IsRevertSafe("external.txt", RedactedContentMarker)
		assert.True(t, safe, "redacted NewCode should be revert-safe (can't compare)")
	})
}

// TestIsRevertSafe_FileDoesNotExist_ReturnsTrue verifies that a missing
// file is safe to revert — the revert is creating/restoring it.
func TestIsRevertSafe_FileDoesNotExist_ReturnsTrue(t *testing.T) {
	dir := setupGitRepo(t)

	withDirHist(t, dir, func() {
		safe := IsRevertSafe("nonexistent.txt", "some content")
		assert.True(t, safe, "a non-existent file should be revert-safe (restoring it)")
	})
}

// ---------------------------------------------------------------------------
// Decision-matrix test: table-driven coverage of all (content, git) states
// ---------------------------------------------------------------------------

// TestIsRevertSafe_DecisionMatrix runs the guard across all meaningful
// combinations of disk-content state and git-tracking state. This is
// the authoritative specification of the guard's behavior, expressed as
// executable code.
func TestIsRevertSafe_DecisionMatrix(t *testing.T) {
	tests := []struct {
		name      string
		newCode   string
		setupFunc func(t *testing.T) (dir, filename string) // returns repo dir + filename
		wantSafe  bool
	}{
		{
			name:    "committed_clean_matches_newcode",
			newCode: "v1",
			setupFunc: func(t *testing.T) (string, string) {
				dir := setupGitRepo(t)
				commitFile(t, dir, "f.txt", "v1")
				return dir, "f.txt"
			},
			wantSafe: false, // committed → protected
		},
		{
			name:    "committed_modified_uncommitted_matches_newcode",
			newCode: "v2",
			setupFunc: func(t *testing.T) (string, string) {
				dir := setupGitRepo(t)
				commitFile(t, dir, "f.txt", "v1")
				writeFileInDir(t, dir, "f.txt", "v2")
				return dir, "f.txt"
			},
			wantSafe: true, // uncommitted modification → not protected
		},
		{
			name:    "untracked_matches_newcode",
			newCode: "v1",
			setupFunc: func(t *testing.T) (string, string) {
				dir := setupGitRepo(t)
				writeFileInDir(t, dir, "f.txt", "v1")
				return dir, "f.txt"
			},
			wantSafe: true, // untracked → no git protection
		},
		{
			name:    "stale_disk_differs_from_newcode",
			newCode: "agent-version",
			setupFunc: func(t *testing.T) (string, string) {
				dir := setupGitRepo(t)
				writeFileInDir(t, dir, "f.txt", "disk-content")
				return dir, "f.txt"
			},
			wantSafe: false, // stale → skip
		},
		{
			name:    "file_missing",
			newCode: "v1",
			setupFunc: func(t *testing.T) (string, string) {
				dir := setupGitRepo(t)
				return dir, "nonexistent.txt"
			},
			wantSafe: true, // missing → safe to restore/create
		},
		{
			name:    "empty_newcode",
			newCode: "",
			setupFunc: func(t *testing.T) (string, string) {
				dir := setupGitRepo(t)
				writeFileInDir(t, dir, "f.txt", "whatever")
				return dir, "f.txt"
			},
			wantSafe: true, // no baseline → allow
		},
		{
			name:    "redacted_newcode",
			newCode: RedactedContentMarker,
			setupFunc: func(t *testing.T) (string, string) {
				dir := setupGitRepo(t)
				writeFileInDir(t, dir, "f.txt", "secret")
				return dir, "f.txt"
			},
			wantSafe: true, // can't compare → allow
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, filename := tt.setupFunc(t)
			withDirHist(t, dir, func() {
				safe := IsRevertSafe(filename, tt.newCode)
				assert.Equal(t, tt.wantSafe, safe,
					"IsRevertSafe(%q, newCode=%q) returned %v, want %v",
					filename, truncateForLog(tt.newCode), safe, tt.wantSafe)
			})
		})
	}
}

// truncateForLog returns a short representation of s for error messages.
func truncateForLog(s string) string {
	if len(s) > 30 {
		return s[:30] + "..."
	}
	return s
}
