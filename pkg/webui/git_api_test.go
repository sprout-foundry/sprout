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

func TestHandleAPIGitDiffRejectsInvalidMethod(t *testing.T) {
	server := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/git/diff?path=test.txt", nil)
	w := httptest.NewRecorder()

	server.handleAPIGitDiff(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleAPIGitDiffRejectsMissingPath(t *testing.T) {
	server := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/diff", nil)
	w := httptest.NewRecorder()

	server.handleAPIGitDiff(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleAPIGitDiffForModifiedFile(t *testing.T) {
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
		Message      string `json:"message"`
		Path         string `json:"path"`
		HasStaged    bool   `json:"has_staged"`
		HasUnstaged  bool   `json:"has_unstaged"`
		StagedDiff   string `json:"staged_diff"`
		UnstagedDiff string `json:"unstaged_diff"`
		Diff         string `json:"diff"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Message != "success" {
		t.Fatalf("expected success message, got %q", response.Message)
	}
	if response.Path != "notes.txt" {
		t.Fatalf("expected path notes.txt, got %q", response.Path)
	}
	if response.HasStaged {
		t.Fatalf("expected no staged diff")
	}
	if !response.HasUnstaged {
		t.Fatalf("expected unstaged diff")
	}
	if !strings.Contains(response.UnstagedDiff, "notes.txt") {
		t.Fatalf("expected unstaged diff to include file name, got: %s", response.UnstagedDiff)
	}
	if !strings.Contains(response.Diff, "Unstaged changes") {
		t.Fatalf("expected combined diff to include unstaged header, got: %s", response.Diff)
	}
}

func TestHandleAPIGitDiffForUntrackedFile(t *testing.T) {
	repo := createTempGitRepo(t)
	writeFile(t, filepath.Join(repo, "new_file.txt"), "brand new content\n")

	server := &ReactWebServer{workspaceRoot: repo}
	req := httptest.NewRequest(http.MethodGet, "/api/git/diff?path=new_file.txt", nil)
	w := httptest.NewRecorder()

	server.handleAPIGitDiff(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		HasUnstaged  bool   `json:"has_unstaged"`
		UnstagedDiff string `json:"unstaged_diff"`
		Diff         string `json:"diff"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.HasUnstaged {
		t.Fatalf("expected untracked file to produce unstaged diff")
	}
	if !strings.Contains(response.UnstagedDiff, "new_file.txt") {
		t.Fatalf("expected unstaged diff to reference file, got: %s", response.UnstagedDiff)
	}
	if !strings.Contains(response.Diff, "Unstaged changes") {
		t.Fatalf("expected combined diff to include unstaged header, got: %s", response.Diff)
	}
}

func createTempGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for this test")
	}

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	writeFile(t, filepath.Join(repo, "notes.txt"), "line one\n")
	runGit(t, repo, "add", "notes.txt")
	runGit(t, repo, "commit", "-m", "initial commit")

	return repo
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
