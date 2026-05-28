//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
	ws.workspaceRoot = "/tmp"
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
