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

	gitops "github.com/sprout-foundry/sprout/pkg/git"
)

// ETH-1 sync-on-resume: /api/sync daemon endpoint tests. The endpoint serves
// the pinned git.SyncReport contract (same JSON `sprout sync` prints) so the
// platform can probe a resumed container.
//
// The method split is the security boundary: GET/HEAD is status-only and
// reachable unauthenticated; only POST may pull, and POST to /api/* requires
// Bearer auth whenever SPROUT_AUTH_TOKEN is configured.

func newSyncTestWebServer(t *testing.T, root string) *ReactWebServer {
	t.Helper()
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	server.workspaceRoot = root
	server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = root
	return server
}

func syncTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestHandleAPISync_ServesContractJSON(t *testing.T) {
	dir := t.TempDir()
	syncTestGit(t, dir, "init", "-b", "main")
	syncTestGit(t, dir, "config", "user.email", "test@example.com")
	syncTestGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncTestGit(t, dir, "add", ".")
	syncTestGit(t, dir, "commit", "-m", "c1")

	server := newSyncTestWebServer(t, dir)

	req := httptest.NewRequest(http.MethodGet, "/api/sync", nil)
	rec := httptest.NewRecorder()
	server.handleAPISync(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var report gitops.SyncReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("body is not the contract JSON: %v\nbody=%s", err, rec.Body.String())
	}
	if !report.InGitRepo {
		t.Fatalf("in_git_repo = false, want true; body=%s", rec.Body.String())
	}
	// GET is status-only by construction — no pull param is needed (and
	// none is honored).
	if report.Pull.Result != gitops.SyncPullNotAttempted {
		t.Fatalf("pull.result = %q, want not_attempted on GET", report.Pull.Result)
	}
}

func TestHandleAPISync_NotARepoStill200(t *testing.T) {
	server := newSyncTestWebServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/sync", nil)
	rec := httptest.NewRecorder()
	server.handleAPISync(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for reportable not-a-repo; body=%s", rec.Code, rec.Body.String())
	}
	var report gitops.SyncReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("body not contract JSON: %v", err)
	}
	if report.InGitRepo {
		t.Fatal("in_git_repo = true, want false")
	}
}

func TestHandleAPISync_Catastrophic500(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — 0000-mode dir would still be readable")
	}
	base := t.TempDir()
	bad := filepath.Join(base, "unreadable")
	if err := os.MkdirAll(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o755) })

	server := newSyncTestWebServer(t, bad)
	req := httptest.NewRequest(http.MethodGet, "/api/sync", nil)
	rec := httptest.NewRecorder()
	server.handleAPISync(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPISync_MethodNotAllowed(t *testing.T) {
	server := newSyncTestWebServer(t, t.TempDir())
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(method, "/api/sync", nil)
		rec := httptest.NewRecorder()
		server.handleAPISync(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want 405", method, rec.Code)
		}
	}
}

// TestHandleAPISync_GetIsStatusOnlyEvenWithPullParam is the regression test
// for the unauthenticated-mutating-GET hole: GET/HEAD are passed through
// authTokenMiddleware unconditionally, so a GET must never be able to
// trigger a pull — not even with an explicit ?pull=1.
func TestHandleAPISync_GetIsStatusOnlyEvenWithPullParam(t *testing.T) {
	origin := t.TempDir()
	syncTestGit(t, origin, "init", "-b", "main")
	syncTestGit(t, origin, "config", "user.email", "test@example.com")
	syncTestGit(t, origin, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncTestGit(t, origin, "add", ".")
	syncTestGit(t, origin, "commit", "-m", "c1")

	// A clone that is behind its origin, so a real pull would move HEAD.
	bare := filepath.Join(t.TempDir(), "origin.git")
	syncTestGit(t, origin, "init", "--bare", "-b", "main", bare)
	syncTestGit(t, origin, "remote", "add", "origin", bare)
	syncTestGit(t, origin, "push", "-u", "origin", "main")
	work := filepath.Join(t.TempDir(), "work")
	syncTestGit(t, origin, "clone", bare, work)
	syncTestGit(t, work, "config", "user.email", "test@example.com")
	syncTestGit(t, work, "config", "user.name", "Test User")
	syncTestGit(t, origin, "commit", "--allow-empty", "-m", "origin advances")
	syncTestGit(t, origin, "push", "origin", "main")

	headBefore := syncHeadOf(t, work)

	server := newSyncTestWebServer(t, work)
	req := httptest.NewRequest(http.MethodGet, "/api/sync?pull=1&pull=true", nil)
	rec := httptest.NewRecorder()
	server.handleAPISync(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var report gitops.SyncReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("body not contract JSON: %v; body=%s", err, rec.Body.String())
	}
	if !report.InGitRepo {
		t.Fatalf("in_git_repo = false, want true; body=%s", rec.Body.String())
	}
	if report.Pull.Result != gitops.SyncPullNotAttempted || report.Pull.Attempted {
		t.Fatalf("GET must be status-only, got pull={%v %q}", report.Pull.Attempted, report.Pull.Result)
	}
	if got := syncHeadOf(t, work); got != headBefore {
		t.Fatalf("GET ?pull=1 moved HEAD %s -> %s; a GET must never mutate the repo", headBefore, got)
	}
	// behind reflects the last fetch only — no network access happened.
	if report.Behind != 0 {
		t.Fatalf("behind = %d, want 0 (no fetch may run on GET)", report.Behind)
	}
}

// TestHandleAPISync_PostPulls exercises the mutating half: POST is the only
// method that may pull.
func TestHandleAPISync_PostPulls(t *testing.T) {
	origin := t.TempDir()
	syncTestGit(t, origin, "init", "-b", "main")
	syncTestGit(t, origin, "config", "user.email", "test@example.com")
	syncTestGit(t, origin, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncTestGit(t, origin, "add", ".")
	syncTestGit(t, origin, "commit", "-m", "c1")

	bare := filepath.Join(t.TempDir(), "origin.git")
	syncTestGit(t, origin, "init", "--bare", "-b", "main", bare)
	syncTestGit(t, origin, "remote", "add", "origin", bare)
	syncTestGit(t, origin, "push", "-u", "origin", "main")
	work := filepath.Join(t.TempDir(), "work")
	syncTestGit(t, origin, "clone", bare, work)
	syncTestGit(t, work, "config", "user.email", "test@example.com")
	syncTestGit(t, work, "config", "user.name", "Test User")
	syncTestGit(t, origin, "commit", "--allow-empty", "-m", "origin advances")
	syncTestGit(t, origin, "push", "origin", "main")

	headBefore := syncHeadOf(t, work)

	server := newSyncTestWebServer(t, work)
	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rec := httptest.NewRecorder()
	server.handleAPISync(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var report gitops.SyncReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("body not contract JSON: %v; body=%s", err, rec.Body.String())
	}
	if !report.Pull.Attempted || report.Pull.Result != gitops.SyncPullFastForwarded {
		t.Fatalf("POST pull = {%v %q}, want attempted fast_forwarded", report.Pull.Attempted, report.Pull.Result)
	}
	if got := syncHeadOf(t, work); got == headBefore {
		t.Fatal("POST pull did not move HEAD")
	}
	if report.LastCommit.Subject != "origin advances" {
		t.Fatalf("last_commit.subject = %q, want 'origin advances'", report.LastCommit.Subject)
	}
}

// TestHandleAPISync_PostPull0DegradesToStatusOnly checks the symmetry
// escape hatch: an explicit ?pull=0 on POST never touches the repo.
func TestHandleAPISync_PostPull0DegradesToStatusOnly(t *testing.T) {
	dir := t.TempDir()
	syncTestGit(t, dir, "init", "-b", "main")
	syncTestGit(t, dir, "config", "user.email", "test@example.com")
	syncTestGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncTestGit(t, dir, "add", ".")
	syncTestGit(t, dir, "commit", "-m", "c1")

	server := newSyncTestWebServer(t, dir)
	req := httptest.NewRequest(http.MethodPost, "/api/sync?pull=0", nil)
	rec := httptest.NewRecorder()
	server.handleAPISync(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var report gitops.SyncReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("body not contract JSON: %v", err)
	}
	if report.Pull.Result != gitops.SyncPullNotAttempted || report.Pull.Attempted {
		t.Fatalf("POST ?pull=0 must be status-only, got pull={%v %q}", report.Pull.Attempted, report.Pull.Result)
	}
}

// TestHandleAPISync_PostRequiresBearerWithToken wires the real handler
// through authTokenMiddleware, exactly as server_lifecycle.go does. With
// SPROUT_AUTH_TOKEN configured the mutating method must be rejected without
// a Bearer token, while the read-only GET stays open.
func TestHandleAPISync_PostRequiresBearerWithToken(t *testing.T) {
	dir := t.TempDir()
	syncTestGit(t, dir, "init", "-b", "main")
	syncTestGit(t, dir, "config", "user.email", "test@example.com")
	syncTestGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncTestGit(t, dir, "add", ".")
	syncTestGit(t, dir, "commit", "-m", "c1")

	server := newSyncTestWebServer(t, dir)
	handler := authTokenMiddleware("secret-token")(http.HandlerFunc(server.handleAPISync))

	// POST without a Bearer token → 401, and the repo is untouched.
	headBefore := syncHeadOf(t, dir)
	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST without token status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if got := syncHeadOf(t, dir); got != headBefore {
		t.Fatal("an unauthorized POST must never reach the sync handler")
	}

	// POST with the correct token → passes the boundary and reports.
	req = httptest.NewRequest(http.MethodPost, "/api/sync?pull=0", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST with token status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// GET stays open by design (status-only, unauthenticated).
	req = httptest.NewRequest(http.MethodGet, "/api/sync", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (status-only must stay open)", rec.Code)
	}
}

// TestHandleAPISync_PostOpenWithoutToken covers the localhost/dev posture:
// with no token configured the middleware is a no-op and both methods work.
func TestHandleAPISync_PostOpenWithoutToken(t *testing.T) {
	dir := t.TempDir()
	syncTestGit(t, dir, "init", "-b", "main")
	syncTestGit(t, dir, "config", "user.email", "test@example.com")
	syncTestGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncTestGit(t, dir, "add", ".")
	syncTestGit(t, dir, "commit", "-m", "c1")

	server := newSyncTestWebServer(t, dir)
	handler := authTokenMiddleware("")(http.HandlerFunc(server.handleAPISync))

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/api/sync", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200 with no token configured; body=%s", method, rec.Code, rec.Body.String())
		}
	}
}

// syncHeadOf returns the full HEAD sha of a repo dir.
func syncHeadOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestHandleAPIBootstrap_SyncFieldPresent(t *testing.T) {
	dir := t.TempDir()
	syncTestGit(t, dir, "init", "-b", "main")
	syncTestGit(t, dir, "config", "user.email", "test@example.com")
	syncTestGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	syncTestGit(t, dir, "add", ".")
	syncTestGit(t, dir, "commit", "-m", "c1")

	server := newSyncTestWebServer(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	rec := httptest.NewRecorder()
	server.handleAPIBootstrap(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("bootstrap body not JSON: %v", err)
	}
	raw, ok := payload["sync"]
	if !ok {
		t.Fatalf("bootstrap response missing 'sync' field; body=%s", rec.Body.String())
	}
	var report gitops.SyncReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("sync field not the contract JSON: %v; raw=%s", err, string(raw))
	}
	if !report.InGitRepo {
		t.Fatalf("sync.in_git_repo = false, want true; body=%s", rec.Body.String())
	}
	// Bootstrap must never mutate the repo — pull always not_attempted.
	if report.Pull.Result != gitops.SyncPullNotAttempted {
		t.Fatalf("bootstrap sync pull.result = %q, want not_attempted", report.Pull.Result)
	}
}

func TestHandleAPIBootstrap_SyncFailureNeverFailsBootstrap(t *testing.T) {
	// Not a repo → sync computed but in_git_repo=false; bootstrap still 200.
	server := newSyncTestWebServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	rec := httptest.NewRecorder()
	server.handleAPIBootstrap(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when sync degrades; body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("bootstrap body not JSON: %v", err)
	}
	raw, ok := payload["sync"]
	if !ok {
		t.Fatal("bootstrap response missing 'sync' field")
	}
	var report gitops.SyncReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("sync field not contract JSON: %v", err)
	}
	if report.InGitRepo {
		t.Fatal("sync.in_git_repo = true, want false for non-repo root")
	}
}
