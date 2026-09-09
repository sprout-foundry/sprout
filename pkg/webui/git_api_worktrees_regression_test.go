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

	"github.com/sprout-foundry/sprout/pkg/events"
)

// newWorktreeTestServer builds a server whose daemon root is a temp dir
// containing a real git repo (the workspace). Mirrors the production
// topology: daemon root ⊃ repo, worktrees conventionally sit BESIDE the
// repo ("../feature-x" from the workspace root).
func newWorktreeTestServer(t *testing.T) (ws *ReactWebServer, daemonRoot, repoDir string) {
	t.Helper()
	daemonRoot = t.TempDir()
	repoDir = filepath.Join(daemonRoot, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
		{"add", "-A"},
		{"commit", "-qm", "init", "--allow-empty"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = repoDir
	return ws, daemonRoot, repoDir
}

// TestGitWorktreeCreate_RelativePathAnchoredAtWorkspace pins the fix for
// the "worktree creation not working" report: relative paths typed in the
// WebUI ("../feature-x", documented as workspace-root-relative) were
// resolved against the daemon's PROCESS working directory. A daemon
// started in $HOME resolved "../feature-x" outside the daemon root and
// the request died with path_outside_workspace — or worse, created the
// worktree in the wrong place ("x" landed in $HOME, not beside the repo).
func TestGitWorktreeCreate_RelativePathAnchoredAtWorkspace(t *testing.T) {
	ws, daemonRoot, repoDir := newWorktreeTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree/create",
		strings.NewReader(`{"path":"../feature-x","branch":"feature-x"}`))
	req.Header.Set(webClientIDHeader, "wt-test")
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	wantPath := filepath.Join(daemonRoot, "feature-x")
	// macOS /var → /private/var symlink: EvalSymlinks canonicalizes, so
	// compare resolved forms.
	wantResolved, err := filepath.EvalSymlinks(wantPath)
	if err != nil || wantResolved == "" {
		wantResolved = wantPath
	}
	if resp.Path != wantResolved {
		t.Fatalf("worktree path = %q, want sibling-of-repo %q", resp.Path, wantResolved)
	}
	if info, statErr := os.Stat(wantPath + "/.git"); statErr != nil {
		t.Fatalf("worktree not created at %s: %v", wantPath, statErr)
	} else if info.IsDir() {
		// git worktrees use a .git FILE pointing at the main repo's worktree dir
		t.Fatalf(".git at %s is a directory, expected the worktree marker file", wantPath)
	}

	// Cleanup: remove via the API to exercise the same anchored resolution.
	req2 := httptest.NewRequest(http.MethodPost, "/api/git/worktree/remove",
		strings.NewReader(`{"path":"../feature-x"}`))
	req2.Header.Set(webClientIDHeader, "wt-test")
	rec2 := httptest.NewRecorder()
	// Set the client workspace so resolution anchors at the repo (the
	// request goes through getWorkspaceRootForRequest).
	ws2, _, _ := ws, daemonRoot, repoDir
	_ = ws2
	ws.handleAPIGitWorktreeRemove(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("remove: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Fatalf("worktree still present after remove: %v", err)
	}
}

// TestGitWorktreeCreate_SimpleRelativeInsideWorkspace: a bare name ("wt-x")
// must resolve INSIDE the workspace, not the daemon CWD.
func TestGitWorktreeCreate_SimpleRelativeInsideWorkspace(t *testing.T) {
	ws, daemonRoot, repoDir := newWorktreeTestServer(t)
	_ = daemonRoot

	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree/create",
		strings.NewReader(`{"path":"wt-x","branch":"wt-x-branch"}`))
	req.Header.Set(webClientIDHeader, "wt-test2")
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	want := filepath.Join(repoDir, "wt-x")
	if wantResolved, evalErr := filepath.EvalSymlinks(want); evalErr == nil {
		want = wantResolved
	}
	if resp.Path != want {
		t.Fatalf("worktree path = %q, want %q (workspace-anchored, not daemon CWD)", resp.Path, want)
	}
}

// TestGitWorktreeCreate_RejectsEscapeBeyondSibling: "../../escape" resolves
// above the workspace parent — outside every allowed boundary.
func TestGitWorktreeCreate_RejectsEscapeBeyondSibling(t *testing.T) {
	ws, _, _ := newWorktreeTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree/create",
		strings.NewReader(`{"path":"../../escape","branch":"escape"}`))
	req.Header.Set(webClientIDHeader, "wt-test3")
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCreate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "path_outside_workspace") {
		t.Fatalf("expected path_outside_workspace code, got %s", rec.Body.String())
	}
}
