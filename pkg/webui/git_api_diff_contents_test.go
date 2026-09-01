//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitDiffFileContents verifies the full before/after extraction used by
// the editable merge view.
func TestGitDiffFileContents(t *testing.T) {
	repo := createTempGitRepo(t)
	server := &ReactWebServer{workspaceRoot: repo}

	// Modified tracked file: original = HEAD version, modified = worktree.
	writeFile(t, filepath.Join(repo, "notes.txt"), "line one\nline two\nchanged\n")
	orig, mod, truncated := server.gitDiffFileContents(repo, "notes.txt")
	if truncated {
		t.Fatal("expected no truncation")
	}
	if orig != "line one\n" {
		t.Fatalf("original = %q, want HEAD version %q", orig, "line one\n")
	}
	if mod != "line one\nline two\nchanged\n" {
		t.Fatalf("modified = %q, want worktree contents", mod)
	}

	// Untracked file: original is empty (new file), modified = contents.
	writeFile(t, filepath.Join(repo, "fresh.txt"), "fresh\n")
	orig, mod, _ = server.gitDiffFileContents(repo, "fresh.txt")
	if orig != "" {
		t.Fatalf("untracked original = %q, want empty", orig)
	}
	if mod != "fresh\n" {
		t.Fatalf("untracked modified = %q, want file contents", mod)
	}

	// Deleted file: modified is empty.
	if err := os.Remove(filepath.Join(repo, "fresh.txt")); err != nil {
		t.Fatal(err)
	}
	_, mod, _ = server.gitDiffFileContents(repo, "fresh.txt")
	if mod != "" {
		t.Fatalf("deleted modified = %q, want empty", mod)
	}
}

// TestHandleAPIGitDiffReturnsFullContents asserts the /api/git/diff response
// carries the full-file contents the merge view requires.
func TestHandleAPIGitDiffReturnsFullContents(t *testing.T) {
	repo := createTempGitRepo(t)
	writeFile(t, filepath.Join(repo, "notes.txt"), "line one\nline two\n")

	server := &ReactWebServer{workspaceRoot: repo}
	req := httptest.NewRequest(http.MethodGet, "/api/git/diff?path=notes.txt", nil)
	w := httptest.NewRecorder()

	server.handleAPIGitDiff(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		OriginalContent   string `json:"original_content"`
		ModifiedContent   string `json:"modified_content"`
		ContentsTruncated bool   `json:"contents_truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.OriginalContent != "line one\n" {
		t.Fatalf("original_content = %q, want HEAD version", response.OriginalContent)
	}
	if response.ModifiedContent != "line one\nline two\n" {
		t.Fatalf("modified_content = %q, want worktree contents", response.ModifiedContent)
	}
	if response.ContentsTruncated {
		t.Fatal("expected contents_truncated=false")
	}
}

// TestGitCommitFileContents verifies parent/commit extraction for the
// commit-detail merge view.
func TestGitCommitFileContents(t *testing.T) {
	repo := createTempGitRepo(t)

	// Second commit modifies notes.txt and adds extra.txt.
	writeFile(t, filepath.Join(repo, "notes.txt"), "line one\nline two\n")
	writeFile(t, filepath.Join(repo, "extra.txt"), "extra\n")
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "second commit")
	hashCmd := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	hashOut, err := hashCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse failed: %v", err)
	}
	hash := strings.TrimSpace(string(hashOut))

	server := &ReactWebServer{workspaceRoot: repo}

	orig, mod, truncated := server.gitCommitFileContents(repo, hash, "notes.txt")
	if truncated {
		t.Fatal("expected no truncation")
	}
	if orig != "line one\n" {
		t.Fatalf("notes original = %q, want parent version", orig)
	}
	if mod != "line one\nline two\n" {
		t.Fatalf("notes modified = %q, want commit version", mod)
	}

	// File added in this commit: parent side is empty.
	orig, mod, _ = server.gitCommitFileContents(repo, hash, "extra.txt")
	if orig != "" {
		t.Fatalf("added-file original = %q, want empty", orig)
	}
	if mod != "extra\n" {
		t.Fatalf("added-file modified = %q, want commit version", mod)
	}

	// File that never existed on either side: both empty, no error.
	orig, mod, _ = server.gitCommitFileContents(repo, hash, "missing.txt")
	if orig != "" || mod != "" {
		t.Fatalf("missing file = %q/%q, want empty", orig, mod)
	}
}

// TestGitDiffFileContentsTruncationBoundary pins the cap semantics: exactly
// at maxDiffFileContentBytes the content IS returned; one byte over is
// omitted with contents_truncated=true. Applies to both sides.
func TestGitDiffFileContentsTruncationBoundary(t *testing.T) {
	repo := createTempGitRepo(t)
	server := &ReactWebServer{workspaceRoot: repo}

	// Text content that contains no NUL bytes (binary heuristic off).
	exactlyCap := strings.Repeat("a", maxDiffFileContentBytes)
	overCap := strings.Repeat("a", maxDiffFileContentBytes+1)

	// Untracked files: original side is empty (no HEAD version), so the
	// worktree side alone drives the boundary.
	writeFile(t, filepath.Join(repo, "at_cap.txt"), exactlyCap)
	orig, mod, truncated := server.gitDiffFileContents(repo, "at_cap.txt")
	if truncated {
		t.Fatal("file exactly at cap must not be truncated")
	}
	if orig != "" {
		t.Fatalf("untracked original = %q, want empty", orig)
	}
	if len(mod) != maxDiffFileContentBytes {
		t.Fatalf("modified length = %d, want %d", len(mod), maxDiffFileContentBytes)
	}

	writeFile(t, filepath.Join(repo, "over_cap.txt"), overCap)
	_, mod, truncated = server.gitDiffFileContents(repo, "over_cap.txt")
	if !truncated {
		t.Fatal("file one byte over cap must be truncated")
	}
	if mod != "" {
		t.Fatalf("over-cap modified = %d bytes, want omitted", len(mod))
	}
}

// TestGitDiffFileContentsBinary verifies binary contents are omitted
// (garbage UTF-8 must never reach the merge view) while the endpoint still
// succeeds — the diff text remains authoritative for display.
func TestGitDiffFileContentsBinary(t *testing.T) {
	repo := createTempGitRepo(t)
	server := &ReactWebServer{workspaceRoot: repo}

	// Small binary worktree file (NUL bytes early — git's heuristic).
	binary := append([]byte("PNG\x00\x01\x02"), make([]byte, 64)...)
	if err := os.WriteFile(filepath.Join(repo, "blob.bin"), binary, 0o644); err != nil {
		t.Fatal(err)
	}
	orig, mod, truncated := server.gitDiffFileContents(repo, "blob.bin")
	if !truncated {
		t.Fatal("binary worktree file must set truncated")
	}
	if mod != "" {
		t.Fatalf("binary modified = %q, want omitted", mod)
	}
	if orig != "" {
		t.Fatalf("binary original = %q, want empty", orig)
	}

	// Text with a NUL byte only past the 8000-byte scan window is NOT
	// binary by git's heuristic — contents flow through.
	lateNul := strings.Repeat("x", 9000) + "\x00" + "tail"
	writeFile(t, filepath.Join(repo, "late_nul.txt"), lateNul)
	_, mod, truncated = server.gitDiffFileContents(repo, "late_nul.txt")
	if truncated {
		t.Fatal("NUL past scan window must not count as binary")
	}
	if mod != lateNul {
		t.Fatal("text-with-late-NUL contents must be returned verbatim")
	}
}

// TestGitCommitFileContentsBinary verifies the commit-side extractor omits
// binary revisions the same way.
func TestGitCommitFileContentsBinary(t *testing.T) {
	repo := createTempGitRepo(t)

	// Commit a small binary file.
	binary := append([]byte("BIN\x00\x00data"), make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(repo, "img.bin"), binary, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "img.bin")
	runGit(t, repo, "commit", "-m", "add binary")
	hashCmd := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	hashOut, err := hashCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse failed: %v", err)
	}
	hash := strings.TrimSpace(string(hashOut))

	server := &ReactWebServer{workspaceRoot: repo}
	orig, mod, truncated := server.gitCommitFileContents(repo, hash, "img.bin")
	if !truncated {
		t.Fatal("binary commit file must set truncated")
	}
	if orig != "" || mod != "" {
		t.Fatalf("binary commit contents = %q/%q, want omitted", orig, mod)
	}
}

// TestLooksBinary unit-pins the heuristic itself.
func TestLooksBinary(t *testing.T) {
	if !looksBinary([]byte("a\x00b")) {
		t.Fatal("early NUL must be binary")
	}
	if looksBinary([]byte("plain text")) {
		t.Fatal("plain text must not be binary")
	}
	if looksBinary(nil) {
		t.Fatal("empty input must not be binary")
	}
	if !looksBinary(append(make([]byte, 8000), 0)) {
		t.Fatal("NUL at the 8000th byte is within the scan window")
	}
	if looksBinary(append([]byte(strings.Repeat("y", 8000)), 0)) {
		t.Fatal("NUL after the scan window must not be binary")
	}
}
