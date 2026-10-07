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

// TestHumaGitDispatch proves the git-family Huma operations (the /api/git/*
// read and write routes) dispatch through the live ServeMux and emit the same
// response the plain handlers did. The operations are thin wrappers over the
// existing handlers, so a regression that broke the Huma wiring would surface
// as a wrong status or a missing payload. It exercises deterministic read
// routes that need no live agent or provider: a GET read, a path-variable GET,
// and a method gate.
func TestHumaGitDispatch(t *testing.T) {
	ws, _ := newTestWebServer(t)
	mux := ws.setupRoutes(context.Background())
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// GET /api/git/branches — a Huma operation (was a plain handler). The
	// response is deterministic in either case: a git workspace lists branches
	// and a non-git one reports "not_git_repo".
	resp, err := srv.Client().Get(srv.URL + "/api/git/branches")
	if err != nil {
		t.Fatalf("GET /api/git/branches: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /api/git/branches body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close /api/git/branches body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/git/branches: status = %d, want %d (body: %s)", resp.StatusCode, http.StatusOK, body)
	}
	var br map[string]any
	if err := json.Unmarshal(body, &br); err != nil {
		t.Fatalf("decode /api/git/branches body: %v", err)
	}
	if _, hasBranches := br["branches"]; !hasBranches {
		t.Fatalf("GET /api/git/branches: response missing `branches` field (body: %s)", body)
	}

	// GET /api/git/commit/show — a path-variable Huma operation (was a plain
	// handler with a query-parameter fallback). A missing `sha` is a 400.
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/git/commit/show", nil)
	if err != nil {
		t.Fatalf("build GET /api/git/commit/show: %v", err)
	}
	req.Header.Set("X-Client-Id", "test-client")
	showResp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/git/commit/show: %v", err)
	}
	showBody, err := io.ReadAll(showResp.Body)
	if err != nil {
		t.Fatalf("read /api/git/commit/show body: %v", err)
	}
	if err := showResp.Body.Close(); err != nil {
		t.Errorf("close /api/git/commit/show body: %v", err)
	}
	if showResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET /api/git/commit/show (no sha): status = %d, want %d (body: %s)", showResp.StatusCode, http.StatusBadRequest, showBody)
	}

	// GET /api/git/stage — a write route served by a GET should be rejected
	// by the method gate (the Huma operation is registered for POST only; a
	// wrong method reaches the SPA catch-all, the same as the plain handler).
	gateResp, err := srv.Client().Get(srv.URL + "/api/git/stage")
	if err != nil {
		t.Fatalf("GET /api/git/stage: %v", err)
	}
	if _, err := io.Copy(io.Discard, gateResp.Body); err != nil {
		t.Errorf("drain /api/git/stage body: %v", err)
	}
	if err := gateResp.Body.Close(); err != nil {
		t.Errorf("close /api/git/stage body: %v", err)
	}
	if gateResp.StatusCode == http.StatusOK && strings.Contains(strings.ToLower(gateResp.Header.Get("Content-Type")), "application/json") {
		t.Fatalf("GET /api/git/stage unexpectedly served a JSON 200; the method gate should reject it (status %d)", gateResp.StatusCode)
	}
}
