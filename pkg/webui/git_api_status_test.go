//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestHandleAPIGitStatusMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/git/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitStatus(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitStageMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/stage", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitStage(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitStageMissingPath(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/stage", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitStage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitStageInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/stage", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIGitStage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitUnstageMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/unstage", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitUnstage(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitDiscardMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/discard", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitDiscard(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitStageAllMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/stage-all", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitStageAll(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitUnstageAllMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/unstage-all", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitUnstageAll(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitBranchesMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/git/branches", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitBranches(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitCheckoutMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/checkout", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitCheckout(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitCheckoutMissingBranch(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitCheckout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitCheckoutInvalidBranchName(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader(`{"branch":"--bad"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitCheckout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for branch starting with dash, got %d", rec.Code)
	}
}

func TestHandleAPIGitCheckoutInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIGitCheckout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitCreateBranchMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/create-branch", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitCreateBranch(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitCreateBranchMissingName(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/create-branch", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitCreateBranch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitPullMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/pull", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitPull(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitPushMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/push", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitPush(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitCommitMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/commit", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitCommit(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitCommitMissingMessage(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/commit", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitCommit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitCommitInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/commit", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIGitCommit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitCommitMessageMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/commit-message", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitCommitMessage(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitRevertMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/revert", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitRevert(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitRevertMissingCommit(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/revert", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitRevert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitRevertInvalidCommit(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/revert", strings.NewReader(`{"commit":"--bad"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitRevert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleAPIGitRevertInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.SetWorkspaceRoot("/tmp")
	req := httptest.NewRequest(http.MethodPost, "/api/git/revert", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPIGitRevert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetAllGitFilesStatus(t *testing.T) {
	status := &GitStatus{
		Staged:    []GitFile{{Path: "staged.txt"}},
		Modified:  []GitFile{{Path: "modified.txt"}},
		Untracked: []GitFile{{Path: "untracked.txt"}},
		Deleted:   []GitFile{{Path: "deleted.txt"}},
		Renamed:   []GitFile{{Path: "renamed.txt"}},
	}

	files := getAllGitFiles(status)
	if len(files) != 5 {
		t.Fatalf("expected 5 files, got %d", len(files))
	}
}

func TestGetAllGitFilesEmptyStatus(t *testing.T) {
	status := &GitStatus{}
	files := getAllGitFiles(status)
	if files == nil {
		t.Error("expected empty slice, got nil")
	}
}

// A branch that exists only on the remote (one a task pushed after the
// clone) is fetched and checked out.
func TestHandleAPIGitCheckoutFetchesRemoteBranch(t *testing.T) {
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	origin := t.TempDir()
	run(origin, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(origin, "add", ".")
	run(origin, "commit", "-q", "-m", "init")
	clone := filepath.Join(t.TempDir(), "clone")
	run(filepath.Dir(clone), "clone", "-q", origin, clone)
	// The task's branch appears on the remote after the clone.
	run(origin, "checkout", "-q", "-b", "sprout/add-a-form-abcd1234")
	if err := os.WriteFile(filepath.Join(origin, "form.html"), []byte("<form></form>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(origin, "add", ".")
	run(origin, "commit", "-q", "-m", "form")
	run(origin, "checkout", "-q", "main")

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = clone
	ws.workspaceRoot = clone
	req := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader(`{"branch":"sprout/add-a-form-abcd1234"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitCheckout(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(clone, "form.html")); err != nil {
		t.Fatalf("the task's work is not in the workspace: %v", err)
	}

	missing := httptest.NewRequest(http.MethodPost, "/api/git/checkout", strings.NewReader(`{"branch":"sprout/nope"}`))
	rec = httptest.NewRecorder()
	ws.handleAPIGitCheckout(rec, missing)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a branch nowhere, got %d", rec.Code)
	}
}
