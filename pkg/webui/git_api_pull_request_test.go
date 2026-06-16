//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitops "github.com/sprout-foundry/sprout/pkg/git"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newTestGitRepo creates a git repo with an initial commit on main.
func newTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	f := filepath.Join(dir, "README.md")
	if err := os.WriteFile(f, []byte("# hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "Initial commit")
	return dir
}

// ---------------------------------------------------------------------------
// save/restore helpers for the exported git package hooks
// ---------------------------------------------------------------------------

type savedGitHooks struct {
	runGhCommand     func(context.Context, string, ...string) ([]byte, error)
	getDefaultBranch func(context.Context, string) (string, error)
}

func saveGitHooks() savedGitHooks {
	return savedGitHooks{
		runGhCommand:     gitops.RunGhCommand,
		getDefaultBranch: gitops.GetDefaultBranch,
	}
}

func restoreGitHooks(s savedGitHooks) {
	gitops.RunGhCommand = s.runGhCommand
	gitops.GetDefaultBranch = s.getDefaultBranch
}

// ---------------------------------------------------------------------------
// Method / validation tests (no mocking needed)
// ---------------------------------------------------------------------------

func TestHandleAPIGitPullRequestMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/git/pull-request", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIGitPullRequest(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPIGitPullRequestInvalidJSON(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp"
	req := httptest.NewRequest(http.MethodPost, "/api/git/pull-request", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	ws.handleAPIGitPullRequest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIGitPullRequestMissingTitle(t *testing.T) {
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = "/tmp"
	req := httptest.NewRequest(http.MethodPost, "/api/git/pull-request",
		strings.NewReader(`{"title":"","head":"feature"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitPullRequest(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "required") {
		t.Errorf("expected 'required' in body, got: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Success: handler calls CreatePullRequest and returns 200 with result JSON
// ---------------------------------------------------------------------------

func TestHandleAPIGitPullRequestSuccess(t *testing.T) {
	saved := saveGitHooks()
	defer restoreGitHooks(saved)

	dir := newTestGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feature-test")

	// Mock gh CLI to simulate a successful PR creation
	gitops.RunGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return []byte("Creating pull request for feature-test into main\nhttps://github.com/owner/repo/pull/42\n"), nil
	}

	// Clear GH_TOKEN so the API path is skipped (prHTTPClient is unexported)
	// The gh fallback path is the one we can test from the webui package.
	t.Setenv("GH_TOKEN", "")

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = dir

	req := httptest.NewRequest(http.MethodPost, "/api/git/pull-request",
		strings.NewReader(`{"title":"Fix bug","head":"feature-test","base":"main"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitPullRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}
	if resp["url"] != "https://github.com/owner/repo/pull/42" {
		t.Errorf("url = %v, want 'https://github.com/owner/repo/pull/42'", resp["url"])
	}
	if resp["number"] != float64(42) {
		t.Errorf("number = %v, want 42", resp["number"])
	}
	if resp["state"] != "open" {
		t.Errorf("state = %v, want 'open'", resp["state"])
	}
}

// ---------------------------------------------------------------------------
// Error from CreatePullRequest: handler returns 500 with error JSON
// ---------------------------------------------------------------------------

func TestHandleAPIGitPullRequestCreateError(t *testing.T) {
	saved := saveGitHooks()
	defer restoreGitHooks(saved)

	dir := newTestGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feature-error")

	// Mock gh CLI to simulate a failure (no gh available)
	gitops.RunGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return nil, &exec.Error{Name: "gh", Err: os.ErrNotExist}
	}

	// Clear GH_TOKEN so the API path is skipped
	t.Setenv("GH_TOKEN", "")

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = dir

	req := httptest.NewRequest(http.MethodPost, "/api/git/pull-request",
		strings.NewReader(`{"title":"Failing PR","head":"feature-error","base":"main"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitPullRequest(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["success"] != false {
		t.Errorf("expected success=false, got %v", resp["success"])
	}
	if resp["error"] == nil {
		t.Fatal("expected error field in response")
	}
	errMsg := resp["error"].(string)
	if !strings.Contains(errMsg, "gh pr create") {
		t.Errorf("error should contain 'gh pr create' fallback command, got: %v", errMsg)
	}
}

// ---------------------------------------------------------------------------
// Draft PR: handler forwards Draft=true to CreatePullRequest
// ---------------------------------------------------------------------------

func TestHandleAPIGitPullRequestDraft(t *testing.T) {
	saved := saveGitHooks()
	defer restoreGitHooks(saved)

	dir := newTestGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feature-draft")

	var capturedArgs []string
	gitops.RunGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte("https://github.com/owner/repo/pull/1\n"), nil
	}

	t.Setenv("GH_TOKEN", "")

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = dir

	req := httptest.NewRequest(http.MethodPost, "/api/git/pull-request",
		strings.NewReader(`{"title":"Draft PR","head":"feature-draft","base":"main","draft":true}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitPullRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify --draft was passed to gh CLI
	found := false
	for _, arg := range capturedArgs {
		if arg == "--draft" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --draft in gh args, got: %v", capturedArgs)
	}
}

// ---------------------------------------------------------------------------
// Body forwarded: handler passes body through to CreatePullRequest
// ---------------------------------------------------------------------------

func TestHandleAPIGitPullRequestWithBody(t *testing.T) {
	saved := saveGitHooks()
	defer restoreGitHooks(saved)

	dir := newTestGitRepo(t)
	runGit(t, dir, "checkout", "-b", "feature-body")

	var capturedArgs []string
	gitops.RunGhCommand = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		capturedArgs = args
		return []byte("https://github.com/owner/repo/pull/5\n"), nil
	}

	t.Setenv("GH_TOKEN", "")

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = dir

	req := httptest.NewRequest(http.MethodPost, "/api/git/pull-request",
		strings.NewReader(`{"title":"PR with body","body":"Detailed description here","head":"feature-body","base":"main"}`))
	rec := httptest.NewRecorder()
	ws.handleAPIGitPullRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the body was passed through to gh CLI via --body
	bodyFound := false
	for i, arg := range capturedArgs {
		if arg == "--body" && i+1 < len(capturedArgs) {
			if capturedArgs[i+1] == "Detailed description here" {
				bodyFound = true
				break
			}
		}
	}
	if !bodyFound {
		t.Errorf("expected --body 'Detailed description here' in gh args, got: %v", capturedArgs)
	}
}
