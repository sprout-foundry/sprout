//go:build !js

package webui

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"

	"github.com/sprout-foundry/sprout/pkg/txn"
)

// ETH-2 transactional escalation: /api/txn/* daemon endpoint tests. Every
// handler serves a pinned contract shape (docs/txn-protocol.md), the method
// split is the security boundary, and 200 is the code for every reportable
// state — including partial applies and failed commands.

func newTxnTestWebServer(t *testing.T, root string) *ReactWebServer {
	t.Helper()
	server, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	server.workspaceRoot = root
	server.getOrCreateClientContext(defaultWebClientID).WorkspaceRoot = root
	return server
}

func txnTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func newTxnTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
	dir := t.TempDir()
	txnTestGit(t, dir, "init", "-b", "main")
	txnTestGit(t, dir, "config", "user.email", "test@example.com")
	txnTestGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	txnTestGit(t, dir, "add", ".")
	txnTestGit(t, dir, "commit", "-m", "c1")
	return dir
}

func postTxnJSON(t *testing.T, ws *ReactWebServer, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	txnMux(ws).ServeHTTP(rec, req)
	return rec
}

// txnMux registers the real route table (registerGitRoutes is what
// setupRoutes calls for these patterns), so the exact-match /api/txn/*
// registrations themselves are under test rather than just the handlers.
// The full setupRoutes is not used because it also brings up the LSP
// manager, which no txn test needs.
func txnMux(ws *ReactWebServer) *http.ServeMux {
	mux := http.NewServeMux()
	ws.registerGitRoutes(mux)
	return mux
}

// ---------- /api/txn/status ----------

func TestHandleAPITxnStatus_ServesContractJSON(t *testing.T) {
	dir := newTxnTestRepo(t)
	// A second committed file gives a real modification target.
	if err := os.WriteFile(filepath.Join(dir, "extra.go"), []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	txnTestGit(t, dir, "add", "extra.go")
	txnTestGit(t, dir, "commit", "-m", "c2")

	if err := os.WriteFile(filepath.Join(dir, "extra.go"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.out"), []byte("untracked"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}

	ws := newTxnTestWebServer(t, dir)
	req := httptest.NewRequest(http.MethodGet, "/api/txn/status", nil)
	rec := httptest.NewRecorder()
	txnMux(ws).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var status txn.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("body is not the contract JSON: %v\nbody=%s", err, rec.Body.String())
	}
	if !status.InGitRepo || status.Branch != "main" {
		t.Fatalf("status = %+v", status)
	}
	if len(status.DirtyFiles) != 1 || status.DirtyFiles[0] != "extra.go" {
		t.Fatalf("dirty_files = %v", status.DirtyFiles)
	}
	if len(status.UntrackedFiles) != 1 || status.UntrackedFiles[0] != "b.out" {
		t.Fatalf("untracked_files = %v", status.UntrackedFiles)
	}
	if len(status.DeletedFiles) != 1 || status.DeletedFiles[0] != "README.md" {
		t.Fatalf("deleted_files = %v", status.DeletedFiles)
	}
	if status.TotalChanges != 3 {
		t.Fatalf("total_changes = %d, want 3", status.TotalChanges)
	}
	if status.Timestamp == "" {
		t.Fatal("timestamp must be set")
	}
}

func TestHandleAPITxnStatus_NotARepoStill200(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/txn/status", nil)
	rec := httptest.NewRecorder()
	txnMux(ws).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for reportable not-a-repo; body=%s", rec.Code, rec.Body.String())
	}
	var status txn.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("body not contract JSON: %v", err)
	}
	if status.InGitRepo {
		t.Fatal("in_git_repo = true, want false")
	}
	// Lists must be empty arrays on the wire, never null.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"dirty_files", "untracked_files", "deleted_files"} {
		if string(raw[field]) == "null" {
			t.Fatalf("%s must be an empty array, not null; body=%s", field, rec.Body.String())
		}
	}
}

func TestHandleAPITxnStatus_PostIs405(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/txn/status", nil)
	rec := httptest.NewRecorder()
	txnMux(ws).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 — status is read-only", rec.Code)
	}
}

// ---------- /api/txn/push ----------

func TestHandleAPITxnPush_AppliesDelta(t *testing.T) {
	dir := t.TempDir()
	ws := newTxnTestWebServer(t, dir)

	body := `{
		"base": {"git_sha": "", "client": "wasm"},
		"files": [
			{"path": "src/main.go", "content_base64": "` + b64Of("package main\n") + `", "size": 13, "mode": "0644"},
			{"path": "run.sh", "content_base64": "` + b64Of("#!/bin/sh\n") + `", "size": 10, "mode": "0755"}
		],
		"deletes": [],
		"truncated": false,
		"skipped": []
	}`
	rec := postTxnJSON(t, ws, "/api/txn/push", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var result txn.ApplyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("body not contract JSON: %v; body=%s", err, rec.Body.String())
	}
	if result.Applied != 2 || result.Status != txn.StatusOK {
		t.Fatalf("result = %+v", result)
	}
	if got := readFileOf(t, dir, "src/main.go"); got != "package main\n" {
		t.Fatalf("src/main.go = %q", got)
	}
	if info, err := os.Stat(filepath.Join(dir, "run.sh")); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("run.sh mode: %v %v", info, err)
	}
}

func TestHandleAPITxnPush_PartialIsStill200(t *testing.T) {
	dir := t.TempDir()
	ws := newTxnTestWebServer(t, dir)

	body := `{
		"base": {"git_sha": "", "client": "wasm"},
		"files": [
			{"path": "ok.txt", "content_base64": "` + b64Of("fine") + `"},
			{"path": "../escape.txt", "content_base64": "` + b64Of("evil") + `"}
		],
		"deletes": [],
		"truncated": false,
		"skipped": []
	}`
	rec := postTxnJSON(t, ws, "/api/txn/push", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("a partial apply is reportable: %d body=%s", rec.Code, rec.Body.String())
	}
	var result txn.ApplyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("body not contract JSON: %v", err)
	}
	if result.Applied != 1 || result.Status != txn.StatusPartial {
		t.Fatalf("result = %+v, want applied=1 partial", result)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != txn.SkipReasonPathTraversal {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); err == nil {
		t.Fatal("the traversal escaped the workspace root")
	}
}

func TestHandleAPITxnPush_GetIs405(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/txn/push", nil)
	rec := httptest.NewRecorder()
	txnMux(ws).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 — push mutates, it is POST-only", rec.Code)
	}
}

func TestHandleAPITxnPush_InvalidJSONIs400(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	rec := postTxnJSON(t, ws, "/api/txn/push", "not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPITxnPush_OversizedBodyIs413(t *testing.T) {
	dir := t.TempDir()
	ws := newTxnTestWebServer(t, dir)

	// A lazily-generated ~120 MiB body holding ONE JSON value: the point is
	// what the reader does past the 100 MiB cap, not the body itself, so it
	// is never materialized in memory.
	req := httptest.NewRequest(http.MethodPost, "/api/txn/push",
		newHugeBodyReader(`{"path":"f.bin","content_base64":"`, 120, `"}`))
	rec := httptest.NewRecorder()
	txnMux(ws).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	// Nothing may have landed.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("an oversized body must not apply anything, got %d entries", len(entries))
	}
}

// ---------- /api/txn/run ----------

func TestHandleAPITxnRun_ExecutesAndReports(t *testing.T) {
	dir := t.TempDir()
	ws := newTxnTestWebServer(t, dir)

	rec := postTxnJSON(t, ws, "/api/txn/run",
		`{"command": "printf out; printf err 1>&2; exit 7", "timeout_seconds": 30, "workdir": ""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var result txn.RunResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("body not contract JSON: %v; body=%s", err, rec.Body.String())
	}
	if result.Stdout != "out" || result.Stderr != "err" {
		t.Fatalf("streams = %q / %q", result.Stdout, result.Stderr)
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit_code = %d, want 7", result.ExitCode)
	}
	if result.TimedOut || result.Truncated {
		t.Fatalf("flags: %+v", result)
	}
	if result.DurationMs < 0 {
		t.Fatalf("duration_ms = %d", result.DurationMs)
	}
}

func TestHandleAPITxnRun_TimeoutIs200With124(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	rec := postTxnJSON(t, ws, "/api/txn/run",
		`{"command": "sleep 30", "timeout_seconds": 1, "workdir": ""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a timeout is reportable: %d body=%s", rec.Code, rec.Body.String())
	}
	var result txn.RunResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || result.ExitCode != txn.TimeoutExitCode {
		t.Fatalf("result = %+v, want timed_out exit 124", result)
	}
}

func TestHandleAPITxnRun_AbsoluteWorkdirIs400(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	rec := postTxnJSON(t, ws, "/api/txn/run",
		`{"command": "pwd", "timeout_seconds": 5, "workdir": "/etc"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — a run workdir is confined to the workspace", rec.Code)
	}
}

func TestHandleAPITxnRun_TraversalWorkdirIs400(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	rec := postTxnJSON(t, ws, "/api/txn/run",
		`{"command": "pwd", "timeout_seconds": 5, "workdir": "../outside"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleAPITxnRun_RelativeWorkdirRunsInsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := newTxnTestWebServer(t, dir)

	rec := postTxnJSON(t, ws, "/api/txn/run",
		`{"command": "pwd", "timeout_seconds": 10, "workdir": "pkg"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var result txn.RunResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(result.Stdout), "pkg") {
		t.Fatalf("stdout = %q, want a path ending in /pkg", result.Stdout)
	}
}

func TestHandleAPITxnRun_GetIs405(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/txn/run", nil)
	rec := httptest.NewRecorder()
	txnMux(ws).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 — run executes, it is POST-only", rec.Code)
	}
}

func TestHandleAPITxnRun_InvalidJSONIs400(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	rec := postTxnJSON(t, ws, "/api/txn/run", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// ---------- /api/txn/pull ----------

func TestHandleAPITxnPull_ServesContractJSON(t *testing.T) {
	dir := newTxnTestRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src/new.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}

	ws := newTxnTestWebServer(t, dir)
	rec := postTxnJSON(t, ws, "/api/txn/pull", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var manifest txn.DeltaManifest
	if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("body not contract JSON: %v; body=%s", err, rec.Body.String())
	}
	if manifest.Truncated || len(manifest.Skipped) != 0 {
		t.Fatalf("truncated/skipped = %v/%+v", manifest.Truncated, manifest.Skipped)
	}
	if len(manifest.Deletes) != 1 || manifest.Deletes[0] != "README.md" {
		t.Fatalf("deletes = %v", manifest.Deletes)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "src/new.go" {
		t.Fatalf("files = %+v", manifest.Files)
	}
	if decoded, err := base64.StdEncoding.DecodeString(manifest.Files[0].ContentBase64); err != nil || string(decoded) != "package src\n" {
		t.Fatalf("content = %q err=%v", decoded, err)
	}
	if manifest.Base.Client != txn.TxnClientContainer {
		t.Fatalf("base.client = %q, want container", manifest.Base.Client)
	}
}

func TestHandleAPITxnPull_GetIs405(t *testing.T) {
	ws := newTxnTestWebServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/txn/pull", nil)
	rec := httptest.NewRecorder()
	txnMux(ws).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 — pull is POST-only by contract", rec.Code)
	}
}

// ---------- auth boundary ----------

// TestTxnPostRoutesRequireBearer wires the handlers through
// authTokenMiddleware, exactly as server_lifecycle.go does. With
// SPROUT_AUTH_TOKEN configured every mutating method must be rejected
// without a Bearer token, while the read-only status stays open.
func TestTxnPostRoutesRequireBearer(t *testing.T) {
	dir := t.TempDir()
	ws := newTxnTestWebServer(t, dir)
	handler := authTokenMiddleware("secret-token")(http.HandlerFunc(ws.handleAPITxnPush))

	req := httptest.NewRequest(http.MethodPost, "/api/txn/push", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("push without token = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("an unauthorized push must never reach the handler")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/txn/push", strings.NewReader(
		`{"base":{"client":"wasm"},"files":[{"path":"ok.txt","content_base64":"`+b64Of("x")+`"}]}`))
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("push with token = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// The read-only status stays open by design.
	statusHandler := authTokenMiddleware("secret-token")(http.HandlerFunc(ws.handleAPITxnStatus))
	req = httptest.NewRequest(http.MethodGet, "/api/txn/status", nil)
	rec = httptest.NewRecorder()
	statusHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (read-only must stay open)", rec.Code)
	}
}

func TestTxnPostRoutesOpenWithoutToken(t *testing.T) {
	dir := t.TempDir()
	ws := newTxnTestWebServer(t, dir)
	handler := authTokenMiddleware("")(http.HandlerFunc(ws.handleAPITxnPull))

	req := httptest.NewRequest(http.MethodPost, "/api/txn/pull", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pull = %d, want 200 with no token configured; body=%s", rec.Code, rec.Body.String())
	}
}

// ---------- helpers ----------

func b64Of(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func readFileOf(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// hugeBodyReader streams prefix, then n MiB of 'a', then suffix — a single
// ~n MiB JSON value that is never held in memory at once.
type hugeBodyReader struct {
	prefix string
	suffix string
	pos    int
	left   int64
	done   bool
}

func newHugeBodyReader(prefix string, mib int, suffix string) *hugeBodyReader {
	return &hugeBodyReader{prefix: prefix, suffix: suffix, left: int64(mib) << 20}
}

func (r *hugeBodyReader) Read(p []byte) (int, error) {
	if r.pos < len(r.prefix) {
		n := copy(p, r.prefix[r.pos:])
		r.pos += n
		return n, nil
	}
	if r.left > 0 {
		n := len(p)
		if int64(n) > r.left {
			n = int(r.left)
		}
		for i := 0; i < n; i++ {
			p[i] = 'a'
		}
		r.left -= int64(n)
		return n, nil
	}
	if !r.done {
		r.done = true
		return copy(p, r.suffix), nil
	}
	return 0, io.EOF
}
