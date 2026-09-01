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
