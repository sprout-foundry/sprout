//go:build !js

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

// ---------------------------------------------------------------------------
// Helpers for checkout handler tests that need a real git repo + worktree
// ---------------------------------------------------------------------------

// runGitInDir runs a git command in the given directory.
func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, string(out))
	}
}

// setupGitRepoWithWorktree creates a temp git repo (at mainPath) and a
// sibling worktree (at worktreePath). Returns mainPath, worktreePath.
// Both paths are resolved through filepath.EvalSymlinks so they match
// what filepathAbsEval and getWorkspaceRootForRequest produce.
func setupGitRepoWithWorktree(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main")
	worktreePath := filepath.Join(dir, "feature-wt")

	if err := os.MkdirAll(mainPath, 0o755); err != nil {
		t.Fatalf("mkdir main: %v", err)
	}

	runGitInDir(t, mainPath, "init")
	runGitInDir(t, mainPath, "config", "user.name", "test")
	runGitInDir(t, mainPath, "config", "user.email", "test@test.com")

	// Need at least one commit before adding a worktree.
	if err := os.WriteFile(filepath.Join(mainPath, "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitInDir(t, mainPath, "add", "README.md")
	runGitInDir(t, mainPath, "commit", "-m", "initial commit")

	// Create worktree on a new branch.
	runGitInDir(t, mainPath, "worktree", "add", "-b", "feature", worktreePath)

	// Resolve symlinks so paths match what filepathAbsEval and
	// getWorkspaceRootForRequest produce (macOS /var → /private/var).
	var err error
	mainPath, err = filepath.EvalSymlinks(mainPath)
	if err != nil {
		t.Fatalf("eval mainPath: %v", err)
	}
	worktreePath, err = filepath.EvalSymlinks(worktreePath)
	if err != nil {
		t.Fatalf("eval worktreePath: %v", err)
	}

	return mainPath, worktreePath
}

// newWorktreeTestWebServer creates a ReactWebServer suitable for worktree checkout
// tests, rooted at the given workspaceRoot.
func newWorktreeTestWebServer(t *testing.T, workspaceRoot string) *ReactWebServer {
	t.Helper()
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ws.workspaceRoot = workspaceRoot
	return ws
}

// ---------------------------------------------------------------------------
// New checkout handler tests
// ---------------------------------------------------------------------------

// TestHandleAPIGitWorktreeCheckoutSiblingAccepted verifies that a sibling
// worktree (outside the daemon root) is accepted by the checkout handler
// and returns 200 instead of the old 400 "workspace boundary" rejection.
func TestHandleAPIGitWorktreeCheckoutSiblingAccepted(t *testing.T) {
	mainPath, worktreePath := setupGitRepoWithWorktree(t)

	ws := newWorktreeTestWebServer(t, mainPath)
	// Make daemonRoot different from mainPath so the old isWithinWorkspace
	// check would have rejected worktreePath (a sibling to mainPath).
	ws.daemonRoot = mainPath

	body := fmt.Sprintf(`{"path":"%s"}`, worktreePath)
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-checkout", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCheckout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for sibling worktree checkout, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["workspace"] != worktreePath {
		t.Errorf("expected workspace %q in response, got %v", worktreePath, resp["workspace"])
	}
}

// TestHandleAPIGitWorktreeCheckoutUpdatesWorkspaceRootUnconditionally
// verifies that ws.workspaceRoot is always updated, even when the
// clientID is NOT "default".
func TestHandleAPIGitWorktreeCheckoutUpdatesWorkspaceRootUnconditionally(t *testing.T) {
	mainPath, worktreePath := setupGitRepoWithWorktree(t)

	ws := newWorktreeTestWebServer(t, mainPath)

	// Use a non-default client ID via the header.
	body := fmt.Sprintf(`{"path":"%s"}`, worktreePath)
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-checkout", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "non-default-client")
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCheckout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// ws.workspaceRoot must be updated regardless of clientID.
	if ws.workspaceRoot != worktreePath {
		t.Errorf("ws.workspaceRoot = %q, want %q (not updated for non-default clientID)",
			ws.workspaceRoot, worktreePath)
	}

	// The per-client context should also be updated.
	ws.mutex.RLock()
	ctx := ws.clientContexts["non-default-client"]
	ws.mutex.RUnlock()
	if ctx == nil {
		t.Fatal("client context not found for non-default-client")
	}
	if ctx.WorkspaceRoot != worktreePath {
		t.Errorf("ctx.WorkspaceRoot = %q, want %q", ctx.WorkspaceRoot, worktreePath)
	}
}

// TestHandleAPIGitWorktreeCheckoutUpdatesChatSessionWorktreePath verifies
// that the active chat session's WorktreePath follows the checkout when
// it is empty or matches the previous workspace root.
func TestHandleAPIGitWorktreeCheckoutUpdatesChatSessionWorktreePath(t *testing.T) {
	mainPath, worktreePath := setupGitRepoWithWorktree(t)

	ws := newWorktreeTestWebServer(t, mainPath)

	// Pre-populate the client context with chat sessions.
	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(defaultWebClientID)
	// Chat A: empty WorktreePath — should be updated.
	chatA := newChatSession("chat-a", "Chat A")
	// Chat B: WorktreePath matches the main workspace — should be updated.
	chatB := newChatSession("chat-b", "Chat B")
	chatB.WorktreePath = mainPath
	ctx.ChatSessions = map[string]*chatSession{
		defaultChatID: chatA,
		"chat-b":      chatB,
	}
	ctx.DefaultChatID = defaultChatID
	ws.mutex.Unlock()

	body := fmt.Sprintf(`{"path":"%s"}`, worktreePath)
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-checkout", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCheckout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Chat A (empty WorktreePath) → updated to worktreePath.
	if got := chatA.getWorktreePath(); got != worktreePath {
		t.Errorf("chat-a WorktreePath = %q, want %q (empty path should follow checkout)", got, worktreePath)
	}
	// Chat B (WorktreePath == mainPath) → updated to worktreePath.
	if got := chatB.getWorktreePath(); got != worktreePath {
		t.Errorf("chat-b WorktreePath = %q, want %q (matching previous workspace should follow checkout)", got, worktreePath)
	}
}

// TestHandleAPIGitWorktreeCheckoutDoesNotClobberBoundWorktree verifies that
// a chat session explicitly bound to a different worktree is NOT clobbered
// when the user switches to another worktree.
func TestHandleAPIGitWorktreeCheckoutDoesNotClobberBoundWorktree(t *testing.T) {
	mainPath, worktreePath := setupGitRepoWithWorktree(t)

	ws := newWorktreeTestWebServer(t, mainPath)

	// Create a second worktree for the explicitly-bound chat.
	otherWorktreePath := filepath.Join(filepath.Dir(mainPath), "other-wt")
	runGitInDir(t, mainPath, "worktree", "add", "-b", "other", otherWorktreePath)
	// Resolve symlinks so the path matches what filepathAbsEval produces.
	var err error
	otherWorktreePath, err = filepath.EvalSymlinks(otherWorktreePath)
	if err != nil {
		t.Fatalf("eval otherWorktreePath: %v", err)
	}

	ws.mutex.Lock()
	ctx := ws.getOrCreateClientContextLocked(defaultWebClientID)
	// Default chat: empty WorktreePath — should be updated to feature worktree.
	chatDefault := newChatSession(defaultChatID, "Default")
	// Chat bound to a different worktree — should NOT be clobbered.
	chatBound := newChatSession("chat-bound", "Bound Chat")
	chatBound.WorktreePath = otherWorktreePath
	ctx.ChatSessions = map[string]*chatSession{
		defaultChatID: chatDefault,
		"chat-bound":  chatBound,
	}
	ctx.DefaultChatID = defaultChatID
	ws.mutex.Unlock()

	body := fmt.Sprintf(`{"path":"%s"}`, worktreePath)
	req := httptest.NewRequest(http.MethodPost, "/api/git/worktree-checkout", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleAPIGitWorktreeCheckout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Default chat (empty WorktreePath) → updated.
	if got := chatDefault.getWorktreePath(); got != worktreePath {
		t.Errorf("default chat WorktreePath = %q, want %q", got, worktreePath)
	}
	// Bound chat (different worktree) → NOT clobbered.
	if got := chatBound.getWorktreePath(); got != otherWorktreePath {
		t.Errorf("bound chat WorktreePath = %q, want %q (must NOT be clobbered)", got, otherWorktreePath)
	}
}
