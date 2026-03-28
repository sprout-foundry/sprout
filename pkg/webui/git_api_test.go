package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

// Helper function to create a test gitFixReviewJob
func newTestGitFixReviewJob() *gitFixReviewJob {
	return &gitFixReviewJob{
		ID:        "test-id",
		SessionID: "test-session",
		Status:    "running",
	}
}

func TestAppendStreamText(t *testing.T) {
	t.Run("single complete line", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		job.appendStreamText("hello\n")

		if len(job.Logs) != 1 {
			t.Fatalf("expected 1 log entry, got %d", len(job.Logs))
		}
		if job.Logs[0] != "hello" {
			t.Errorf("expected log entry 'hello', got %q", job.Logs[0])
		}
	})

	t.Run("multiple complete lines", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		job.appendStreamText("hello\nworld\n")

		if len(job.Logs) != 2 {
			t.Fatalf("expected 2 log entries, got %d: %v", len(job.Logs), job.Logs)
		}
		if job.Logs[0] != "hello" {
			t.Errorf("expected first log entry 'hello', got %q", job.Logs[0])
		}
		if job.Logs[1] != "world" {
			t.Errorf("expected second log entry 'world', got %q", job.Logs[1])
		}
	})

	t.Run("partial line buffered", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		job.appendStreamText("hel")

		if len(job.Logs) != 0 {
			t.Fatalf("expected 0 log entries, got %d: %v", len(job.Logs), job.Logs)
		}
	})

	t.Run("line completed across calls", func(t *testing.T) {
		job := newTestGitFixReviewJob()

		// First call: partial line
		job.appendStreamText("hel")
		if len(job.Logs) != 0 {
			t.Fatalf("after partial append, expected 0 logs, got %d: %v", len(job.Logs), job.Logs)
		}

		// Second call: completes the line
		job.appendStreamText("lo\n")
		if len(job.Logs) != 1 {
			t.Fatalf("after completing line, expected 1 log, got %d: %v", len(job.Logs), job.Logs)
		}
		if job.Logs[0] != "hello" {
			t.Errorf("expected log entry 'hello', got %q", job.Logs[0])
		}
	})

	t.Run("multiple trailing newlines no empty entries", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		job.appendStreamText("hello\n\n\n")

		for i, log := range job.Logs {
			if log == "" {
				t.Errorf("log entry at index %d is empty string", i)
			}
		}
		if len(job.Logs) != 1 || job.Logs[0] != "hello" {
			t.Errorf("expected logs [hello], got %v", job.Logs)
		}
	})

	t.Run("empty lines skipped", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		job.appendStreamText("\n\nhello\n")

		if len(job.Logs) != 1 {
			t.Fatalf("expected 1 log entry, got %d: %v", len(job.Logs), job.Logs)
		}
		if job.Logs[0] != "hello" {
			t.Errorf("expected log entry 'hello', got %q", job.Logs[0])
		}
	})

	t.Run("multi-line chunk partial end", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		job.appendStreamText("line1\nline2\npartial")

		if len(job.Logs) != 2 {
			t.Fatalf("expected 2 log entries, got %d: %v", len(job.Logs), job.Logs)
		}
		if job.Logs[0] != "line1" {
			t.Errorf("expected first log 'line1', got %q", job.Logs[0])
		}
		if job.Logs[1] != "line2" {
			t.Errorf("expected second log 'line2', got %q", job.Logs[1])
		}
	})

	t.Run("log truncation at 2000", func(t *testing.T) {
		job := newTestGitFixReviewJob()

		// Append more than 2000 lines
		for i := 0; i < 2500; i++ {
			job.appendStreamText(fmt.Sprintf("line%d\n", i))
		}

		if len(job.Logs) != 2000 {
			t.Fatalf("expected 2000 log entries after truncation, got %d", len(job.Logs))
		}

		// Verify first entry is line 500 (2500 - 2000 = 500)
		if job.Logs[0] != "line500" {
			t.Errorf("expected first log to be 'line500', got %q", job.Logs[0])
		}

		// Verify last entry is line 2499
		if job.Logs[1999] != "line2499" {
			t.Errorf("expected last log to be 'line2499', got %q", job.Logs[1999])
		}
	})

	t.Run("concurrent appends", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				job.appendStreamText(fmt.Sprintf("line%d\n", n))
			}(i)
		}
		wg.Wait()

		if len(job.Logs) != 100 {
			t.Errorf("expected 100 log entries from concurrent appends, got %d", len(job.Logs))
		}
	})

	t.Run("large single chunk with truncation", func(t *testing.T) {
		job := newTestGitFixReviewJob()

		// Build a single chunk with 2500 lines
		var buf strings.Builder
		for i := 0; i < 2500; i++ {
			fmt.Fprintf(&buf, "line%d\n", i)
		}
		job.appendStreamText(buf.String())

		if len(job.Logs) != 2000 {
			t.Fatalf("expected 2000 log entries after truncation, got %d", len(job.Logs))
		}
		if job.Logs[0] != "line500" {
			t.Errorf("expected first log to be 'line500', got %q", job.Logs[0])
		}
	})
}

func TestFlushStreamBuffer(t *testing.T) {
	t.Run("flush buffered partial", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		job.appendStreamText("hello")
		job.flushStreamBuffer()

		if len(job.Logs) != 1 {
			t.Fatalf("expected 1 log entry, got %d: %v", len(job.Logs), job.Logs)
		}
		if job.Logs[0] != "hello" {
			t.Errorf("expected log entry 'hello', got %q", job.Logs[0])
		}
	})

	t.Run("flush empty buffer no-op", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		initialLogs := len(job.Logs)

		job.flushStreamBuffer()

		if len(job.Logs) != initialLogs {
			t.Fatalf("expected no change in logs, got %d (was %d)", len(job.Logs), initialLogs)
		}
	})

	t.Run("flush trims whitespace", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		job.appendStreamText("  hello  ")
		job.flushStreamBuffer()

		if len(job.Logs) != 1 {
			t.Fatalf("expected 1 log entry, got %d: %v", len(job.Logs), job.Logs)
		}
		if job.Logs[0] != "hello" {
			t.Errorf("expected log entry 'hello' (trimmed), got %q", job.Logs[0])
		}
	})

	t.Run("flush empty after trim no-op", func(t *testing.T) {
		job := newTestGitFixReviewJob()
		initialLogs := len(job.Logs)

		job.appendStreamText("   ")
		job.flushStreamBuffer()

		if len(job.Logs) != initialLogs {
			t.Fatalf("expected no change in logs after trimming empty, got %d (was %d)", len(job.Logs), initialLogs)
		}
	})
}
