package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProxyStats_GET_returns200 tests that GET /api/proxy/stats returns 200 with valid JSON.
func TestProxyStats_GET_returns200(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/stats", nil)
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}
}

// TestProxyStats_NonGET_returns405 tests that non-GET methods return 405.
func TestProxyStats_NonGET_returns405(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/proxy/stats", nil)
			req.Header.Set(webClientIDHeader, testProxyChatClientID)
			rec := httptest.NewRecorder()

			ws.handleAPIProxyStats(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status 405 for %s, got %d", method, rec.Code)
			}
		})
	}
}

// TestProxyStats_ExpectedFields tests that the response includes expected stats fields.
func TestProxyStats_ExpectedFields(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/stats", nil)
	req.Header.Set(webClientIDHeader, testProxyChatClientID)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}

	// Verify server-level fields are present
	expectedFields := []string{
		"provider",
		"model",
		"uptime_seconds",
		"queries",
		"terminal_sessions",
		"server_time",
	}

	for _, field := range expectedFields {
		if _, ok := resp[field]; !ok {
			t.Errorf("expected field %q in response, got keys: %v", field, sortedKeys(resp))
		}
	}
}

// TestProxyStats_NoClientID_returns200 tests that stats work without a client ID header.
func TestProxyStats_NoClientID_returns200(t *testing.T) {
	ws := setupProxyChatTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/proxy/stats", nil)
	rec := httptest.NewRecorder()

	ws.handleAPIProxyStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v", err)
	}
}

// sortedKeys returns the sorted keys of a map for deterministic error messages.
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
