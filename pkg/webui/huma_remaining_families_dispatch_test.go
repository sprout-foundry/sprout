//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHumaRemainingFamiliesDispatch proves the workspace/instances, terminal,
// sync/txn, command, proxy, cross-family-misc, and design/starters Huma
// operations dispatch through the live ServeMux and emit the same responses
// the plain handlers did. The operations are thin wrappers over the existing
// handlers (see huma_workspace.go, huma_terminal.go, huma_sync_txn.go,
// huma_command.go, huma_proxy.go, huma_misc.go, and huma_design_starters.go),
// so a regression that broke the Huma wiring would surface as a wrong status,
// a missing payload, or a wrong method gate. It exercises one representative
// deterministic route per family (plus the multi-method and method-gate
// forms) that needs no live agent, provider, or git repository.
func TestHumaRemainingFamiliesDispatch(t *testing.T) {
	ws, _ := newTestWebServer(t)
	mux := ws.setupRoutes(context.Background())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	do := func(t *testing.T, method, path string, body string) *http.Response {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, srv.URL+path, rd)
		if err != nil {
			t.Fatalf("build %s %s: %v", method, path, err)
		}
		req.Header.Set(webClientIDHeader, "test-client")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	readAll := func(t *testing.T, resp *http.Response) []byte {
		t.Helper()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return b
	}

	// --- workspace/instances: GET /api/workspace (workspaceGet) ---
	resp := do(t, http.MethodGet, "/api/workspace", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/workspace: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, readAll(t, resp))
	}
	body := readAll(t, resp)
	var wsGet map[string]any
	if err := json.Unmarshal(body, &wsGet); err != nil {
		t.Fatalf("decode /api/workspace: %v (body: %s)", err, body)
	}
	if _, ok := wsGet["workspace_root"]; !ok {
		t.Fatalf("GET /api/workspace: payload = %v, want a workspace_root key", wsGet)
	}

	// POST /api/workspace (workspaceSet) — empty path is 400, proving the
	// POST operation is registered and the handler ran (not a 404 catch-all).
	resp = do(t, http.MethodPost, "/api/workspace", `{}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /api/workspace (no path): status = %d, want %d (body: %s)", resp.StatusCode, http.StatusBadRequest, readAll(t, resp))
	}

	// --- terminal: GET /api/terminal/history (terminalHistoryGet) ---
	resp = do(t, http.MethodGet, "/api/terminal/history", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/terminal/history: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, readAll(t, resp))
	}
	body = readAll(t, resp)
	var hist map[string]any
	if err := json.Unmarshal(body, &hist); err != nil {
		t.Fatalf("decode /api/terminal/history: %v (body: %s)", err, body)
	}
	if _, ok := hist["history"]; !ok {
		t.Fatalf("GET /api/terminal/history: payload = %v, want a history key", hist)
	}

	// --- sync/txn: GET /api/txn/status (txnStatus, read-only) ---
	// The test workspace is a fresh temp dir (not a git repo), so the
	// preflight reports in_git_repo=false. Proves the GET operation is the
	// read-only half (not the POSTs).
	resp = do(t, http.MethodGet, "/api/txn/status", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/txn/status: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, readAll(t, resp))
	}
	body = readAll(t, resp)
	var txnStatus map[string]any
	if err := json.Unmarshal(body, &txnStatus); err != nil {
		t.Fatalf("decode /api/txn/status: %v (body: %s)", err, body)
	}
	if got, ok := txnStatus["in_git_repo"]; !ok || got != false {
		t.Fatalf("GET /api/txn/status: in_git_repo = %v (ok=%v), want false (body: %s)", got, ok, body)
	}

	// GET /api/sync (syncStatus, status-only) — not a git repo, so the
	// reconciliation report is returned with a non-git state.
	resp = do(t, http.MethodGet, "/api/sync", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/sync: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, readAll(t, resp))
	}
	_ = readAll(t, resp)

	// --- command: POST /api/command/execute (commandExecute) — invalid JSON is 400 ---
	resp = do(t, http.MethodPost, "/api/command/execute", "{not json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /api/command/execute (bad json): status = %d, want %d (body: %s)", resp.StatusCode, http.StatusBadRequest, readAll(t, resp))
	}

	// --- proxy: GET /api/proxy/chat/status (proxyChatStatus) ---
	resp = do(t, http.MethodGet, "/api/proxy/chat/status", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/proxy/chat/status: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, readAll(t, resp))
	}
	body = readAll(t, resp)
	var proxyStatus map[string]any
	if err := json.Unmarshal(body, &proxyStatus); err != nil {
		t.Fatalf("decode /api/proxy/chat/status: %v (body: %s)", err, body)
	}
	if _, ok := proxyStatus["active"]; !ok {
		t.Fatalf("GET /api/proxy/chat/status: payload = %v, want an active key", proxyStatus)
	}

	// --- misc: GET /api/browse (browseDir) ---
	resp = do(t, http.MethodGet, "/api/browse", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/browse: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, readAll(t, resp))
	}
	body = readAll(t, resp)
	var browseResp map[string]any
	if err := json.Unmarshal(body, &browseResp); err != nil {
		t.Fatalf("decode /api/browse: %v (body: %s)", err, body)
	}
	if _, hasFiles := browseResp["files"]; !hasFiles {
		if _, hasError := browseResp["error"]; !hasError {
			t.Fatalf("GET /api/browse: payload = %v, want a files or error key", browseResp)
		}
	}

	// --- design: GET /api/design/status (designStatus) ---
	resp = do(t, http.MethodGet, "/api/design/status", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/design/status: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, readAll(t, resp))
	}
	body = readAll(t, resp)
	var designResp map[string]any
	if err := json.Unmarshal(body, &designResp); err != nil {
		t.Fatalf("decode /api/design/status: %v (body: %s)", err, body)
	}
	if _, ok := designResp["exists"]; !ok {
		t.Fatalf("GET /api/design/status: payload = %v, want an exists key", designResp)
	}

	// --- starters: GET /api/starters (startersList) ---
	resp = do(t, http.MethodGet, "/api/starters", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/starters: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, readAll(t, resp))
	}
	body = readAll(t, resp)
	var startersResp map[string]any
	if err := json.Unmarshal(body, &startersResp); err != nil {
		t.Fatalf("decode /api/starters: %v (body: %s)", err, body)
	}
	if _, ok := startersResp["starters"]; !ok {
		t.Fatalf("GET /api/starters: payload = %v, want a starters key", startersResp)
	}

	// Method-gate spot checks: a method that no operation registered must not
	// be served as 2xx (Huma enforces the operation's method, so a wrong
	// method reaches the SPA catch-all and 404s rather than returning a 2xx).
	for _, gate := range []struct {
		method, path string
	}{
		{http.MethodDelete, "/api/workspace"},        // only GET+POST registered
		{http.MethodDelete, "/api/terminal/history"}, // only GET+POST registered
		{http.MethodDelete, "/api/txn/status"},       // only GET registered
		{http.MethodDelete, "/api/command/execute"},  // only POST registered
		{http.MethodDelete, "/api/proxy/chat"},       // only POST registered
		{http.MethodDelete, "/api/browse"},           // only GET registered
		{http.MethodDelete, "/api/design/status"},    // only GET registered
		{http.MethodDelete, "/api/starters"},         // only GET registered
	} {
		resp = do(t, gate.method, gate.path, "")
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
			t.Fatalf("%s %s: status = %d, want a non-2xx method-gate response (body: %s)",
				gate.method, gate.path, resp.StatusCode, readAll(t, resp))
		}
	}
}
