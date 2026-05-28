//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestWorktreeInfoIsZero(t *testing.T) {
	t.Run("empty path is zero", func(t *testing.T) {
		wt := WorktreeInfo{}
		if !wt.IsZero() {
			t.Error("expected IsZero to be true for empty WorktreeInfo")
		}
	})

	t.Run("with path is not zero", func(t *testing.T) {
		wt := WorktreeInfo{Path: "/some/path"}
		if wt.IsZero() {
			t.Error("expected IsZero to be false when path is set")
		}
	})

	t.Run("other fields set but path empty is zero", func(t *testing.T) {
		wt := WorktreeInfo{Branch: "main", IsMain: true}
		if !wt.IsZero() {
			t.Error("expected IsZero to be true when only path is checked")
		}
	})
}

func TestParseWorktreeListOutputEmpty(t *testing.T) {
	ws := &ReactWebServer{}
	result := ws.parseWorktreeListOutput("", "main", "/workspace")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 worktrees, got %d", len(result))
	}
}

func TestParseWorktreeListOutputSingleWorktree(t *testing.T) {
	ws := &ReactWebServer{}
	output := `worktree /home/user/project
HEAD abc123
branch refs/heads/main
root /home/user/project/.git

`
	result := ws.parseWorktreeListOutput(output, "main", "/home/user/project")
	if len(result) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(result))
	}
	if result[0].Path != "/home/user/project" {
		t.Errorf("expected path /home/user/project, got %q", result[0].Path)
	}
	if result[0].Branch != "main" {
		t.Errorf("expected branch main, got %q", result[0].Branch)
	}
	if !result[0].IsMain {
		t.Error("expected IsMain to be true")
	}
	if !result[0].IsCurrent {
		t.Error("expected IsCurrent to be true")
	}
}

func TestParseWorktreeListOutputMultipleWorktrees(t *testing.T) {
	ws := &ReactWebServer{}
	output := `worktree /home/user/project
HEAD abc123
branch refs/heads/main
root /home/user/project/.git

worktree /home/user/feature-wt
HEAD def456
branch refs/heads/feature-branch
root /home/user/project/.git

`
	result := ws.parseWorktreeListOutput(output, "main", "/home/user/project")
	if len(result) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(result))
	}

	// First is main
	if !result[0].IsMain {
		t.Error("expected first to be main")
	}
	if result[0].Branch != "main" {
		t.Errorf("expected first branch main, got %q", result[0].Branch)
	}

	// Second is feature with parent info
	if result[1].Branch != "feature-branch" {
		t.Errorf("expected second branch feature-branch, got %q", result[1].Branch)
	}
	if result[1].ParentPath != "/home/user/project" {
		t.Errorf("expected parent path /home/user/project, got %q", result[1].ParentPath)
	}
	if result[1].ParentBranch != "main" {
		t.Errorf("expected parent branch main, got %q", result[1].ParentBranch)
	}
}

func TestParseWorktreeListOutputNoCurrentMatch(t *testing.T) {
	ws := &ReactWebServer{}
	output := `worktree /home/user/feature-wt
HEAD abc123
branch refs/heads/feature
root /home/user/project/.git

`
	result := ws.parseWorktreeListOutput(output, "main", "/home/user/project")
	if len(result) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(result))
	}
	if result[0].IsCurrent {
		t.Error("expected IsCurrent to be false when no path matches")
	}
}

func TestHandleAPIGitWorktreesMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktrees", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktrees(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreesNotGitRepo(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp/non-git-dir"
	req := httptest.NewRequest(http.MethodGet, "/api/git/worktrees", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktrees(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-git-repo (returns empty), got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeCreateMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/worktree-create", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCreate(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeCreateMissingPath(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp"
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-create", strings.NewReader(`{"branch":"feature"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCreate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeCreateMissingBranch(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp"
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-create", strings.NewReader(`{"path":"/some/path"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCreate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeCreateInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp"
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-create", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCreate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeRemoveMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/worktree-remove", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeRemove(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeRemoveMissingPath(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp"
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-remove", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeRemove(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeRemoveInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp"
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-remove", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeRemove(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeCheckoutMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/worktree-checkout", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCheckout(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeCheckoutMissingPath(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp"
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-checkout", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCheckout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitWorktreeCheckoutInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp"
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-checkout", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCheckout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
