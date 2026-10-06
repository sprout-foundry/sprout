//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The /api/stats and /api/config routes are registered as Huma operations, so
// the tests below drive them end-to-end through the real router (setupRoutes →
// mux → Huma handler) rather than a plain handler. This proves the Huma
// registration is wired into the live route table (a regression that drops the
// registration 404s the request) and that the response payload is unchanged.

// serveHumaRoute builds the live route table for ws and serves req through it,
// returning the recorder.
func serveHumaRoute(t *testing.T, ws *ReactWebServer, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := ws.setupRoutes(context.Background())
	if mux == nil {
		t.Fatal("setupRoutes returned nil")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestHumaStats_Success drives GET /api/stats through the Huma operation and
// checks the response is the server-statistics payload the frontend expects.
func TestHumaStats_Success(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := serveHumaRoute(t, ws, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode stats response: %v", err)
	}
	for _, k := range []string{
		"uptime_seconds", "connections", "queries", "terminal_sessions",
		"client_context_count", "server_time", "start_time", "uptime",
	} {
		if _, ok := resp[k]; !ok {
			t.Errorf("expected %q in /api/stats response", k)
		}
	}
	// provider/model are always present (empty string when no provider is set).
	if _, ok := resp["provider"]; !ok {
		t.Error("expected provider field in /api/stats response")
	}
}

// TestHumaStats_WrongMethod_ReturnsNotFound confirms a wrong-method request to
// the Huma route does not reach the handler. Because the SPA catch-all (/ →
// handleIndex) matches the path before any 405 method-mismatch applies, the
// response is the app's standard unknown-API 404 (api_endpoint_not_found) —
// the same shape every other unknown /api/* route returns.
func TestHumaStats_WrongMethod_ReturnsNotFound(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/stats", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := serveHumaRoute(t, ws, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for POST /api/stats, got %d: %s", rec.Code, rec.Body.String())
	}
	var errBody map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if errBody["code"] != "api_endpoint_not_found" {
		t.Errorf("expected code api_endpoint_not_found, got %v", errBody["code"])
	}
}

// TestHumaConfig_Success drives GET /api/config through the Huma operation and
// checks the configuration payload shape (this is the end-to-end wiring check
// for the migrated route; the payload content is asserted by
// TestHandleAPIConfig_Success in api_misc_extra_test.go).
func TestHumaConfig_Success(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := serveHumaRoute(t, ws, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	for _, k := range []string{"port", "daemon_root", "workspace_root", "agent", "features"} {
		if _, ok := resp[k]; !ok {
			t.Errorf("expected %q in /api/config response", k)
		}
	}
}
