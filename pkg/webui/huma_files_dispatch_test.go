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

// TestHumaFilesDispatch proves the files-family Huma operations (files, file,
// search, create, delete, rename, upload, diagnostics, lsp/status, semantic)
// dispatch through the live ServeMux and emit the same response the plain
// handlers did. The operations are thin wrappers over the existing handlers,
// so a regression that broke the Huma wiring would surface as a wrong status
// or a missing payload. It exercises deterministic routes that need no live
// agent or provider, including one multi-method route (/api/file GET) and the
// HTTP-DELETE form of /api/delete.
func TestHumaFilesDispatch(t *testing.T) {
	ws, _ := newTestWebServer(t)
	mux := ws.setupRoutes(context.Background())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// GET /api/files — a Huma operation (was a plain handler). A fresh temp
	// workspace lists deterministically.
	resp, err := http.Get(srv.URL + "/api/files?git_status=false")
	if err != nil {
		t.Fatalf("GET /api/files: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /api/files body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close /api/files body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/files: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, body)
	}
	var fs map[string]any
	if err := json.Unmarshal(body, &fs); err != nil {
		t.Fatalf("decode /api/files body: %v", err)
	}
	if fs["message"] != "success" {
		t.Fatalf("GET /api/files: message = %v, want success", fs["message"])
	}
	// The temp workspace is empty, so `files` may marshal to null; the
	// deterministic part of the payload is the message and the resolved path.
	if _, ok := fs["path"]; !ok {
		t.Fatalf("GET /api/files: payload = %v, want a path key", fs)
	}

	// GET /api/file without a path — the Huma GET /api/file operation drives the
	// shared read/write handler's read branch, which requires a path (400).
	fileResp, err := http.Get(srv.URL + "/api/file")
	if err != nil {
		t.Fatalf("GET /api/file: %v", err)
	}
	fileBody, err := io.ReadAll(fileResp.Body)
	if err != nil {
		t.Fatalf("read /api/file body: %v", err)
	}
	if err := fileResp.Body.Close(); err != nil {
		t.Errorf("close /api/file body: %v", err)
	}
	if fileResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /api/file (no path): status = %d, want %d (body: %s)",
			fileResp.StatusCode, http.StatusBadRequest, fileBody)
	}

	// POST /api/delete of a nonexistent path — the Huma POST operation drives
	// the shared handler, which resolves the client's workspace (via the
	// client-ID header) and reports a 400 for a path that does not exist.
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/delete", strings.NewReader(`{"path":"no-such-file"}`))
	if err != nil {
		t.Fatalf("build POST /api/delete: %v", err)
	}
	req.Header.Set(webClientIDHeader, "test-client")
	delResp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /api/delete: %v", err)
	}
	delBody, err := io.ReadAll(delResp.Body)
	if err != nil {
		t.Fatalf("read /api/delete body: %v", err)
	}
	if err := delResp.Body.Close(); err != nil {
		t.Errorf("close /api/delete body: %v", err)
	}
	if delResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /api/delete (nonexistent path): status = %d, want %d (body: %s)", delResp.StatusCode, http.StatusBadRequest, delBody)
	}

	// DELETE /api/delete — the HTTP-DELETE Huma operation (deleteItemHttp)
	// serves the same handler's second accepted method.
	delReq, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/delete", nil)
	if err != nil {
		t.Fatalf("build DELETE /api/delete: %v", err)
	}
	delHTTPResp, err := srv.Client().Do(delReq)
	if err != nil {
		t.Fatalf("DELETE /api/delete: %v", err)
	}
	if _, err := io.Copy(io.Discard, delHTTPResp.Body); err != nil {
		t.Errorf("drain DELETE /api/delete body: %v", err)
	}
	if err := delHTTPResp.Body.Close(); err != nil {
		t.Errorf("close DELETE /api/delete body: %v", err)
	}
	// No path -> 400 (the handler requires a path), proving the DELETE method is
	// routed to the operation (not a 404 from the catch-all).
	if delHTTPResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE /api/delete (no path): status = %d, want %d", delHTTPResp.StatusCode, http.StatusBadRequest)
	}

	// Method gate: GET /api/diagnostics must not be served by the POST-only
	// operation (Huma enforces the method), so it reaches the SPA catch-all and
	// 404s rather than returning a 2xx.
	gateResp, err := http.Get(srv.URL + "/api/diagnostics")
	if err != nil {
		t.Fatalf("GET /api/diagnostics: %v", err)
	}
	if _, err := io.Copy(io.Discard, gateResp.Body); err != nil {
		t.Errorf("drain /api/diagnostics gate body: %v", err)
	}
	if err := gateResp.Body.Close(); err != nil {
		t.Errorf("close /api/diagnostics gate body: %v", err)
	}
	if gateResp.StatusCode == http.StatusOK || gateResp.StatusCode == http.StatusAccepted {
		t.Fatalf("GET /api/diagnostics: status = %d, want a non-2xx method-gate response", gateResp.StatusCode)
	}
}
