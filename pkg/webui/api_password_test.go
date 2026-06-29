//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// ---------------------------------------------------------------------------
// handleAPIPasswordRespond
// ---------------------------------------------------------------------------

func TestHandleAPIPasswordRespond_Success(t *testing.T) {
	ws, _ := newTestWebServer(t)

	// Pre-register a request in the broker so RespondToPasswordRequest returns true.
	requestID := "pwd_test_success"
	ch := agent.RegisterPasswordRequestForTest(requestID)
	defer agent.CleanupPasswordRequestForTest(requestID)

	// Drain the channel so the broker doesn't block.
	go func() { <-ch }()

	ws.agent = &agent.Agent{}

	body := `{"password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/password/"+requestID+"/respond", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIPasswordRoutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["responded"] != true {
		t.Errorf("expected responded=true, got: %v", resp["responded"])
	}
	if resp["request_id"] != requestID {
		t.Errorf("expected request_id=%s, got: %v", requestID, resp["request_id"])
	}
}

func TestHandleAPIPasswordRespond_NotFound(t *testing.T) {
	ws, _ := newTestWebServer(t)
	ws.agent = &agent.Agent{}

	body := `{"password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/password/nonexistent/respond", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIPasswordRoutes(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIPasswordRespond_MethodNotAllowed(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/password/pwd_1/respond", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIPasswordRoutes(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIPasswordRespond_InvalidJSON(t *testing.T) {
	ws, _ := newTestWebServer(t)
	ws.agent = &agent.Agent{}

	req := httptest.NewRequest(http.MethodPost, "/api/password/pwd_1/respond", strings.NewReader("not json"))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIPasswordRoutes(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIPasswordRespond_PasswordNotLogged(t *testing.T) {
	// Verify the password value does NOT appear in the response body.
	ws, _ := newTestWebServer(t)

	requestID := "pwd_test_no_log"
	ch := agent.RegisterPasswordRequestForTest(requestID)
	defer agent.CleanupPasswordRequestForTest(requestID)
	go func() { <-ch }()

	ws.agent = &agent.Agent{}

	secret := "super_secret_password_123"
	body := `{"password":"` + secret + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/password/"+requestID+"/respond", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIPasswordRoutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Error("password value must not appear in response body")
	}
}

func TestHandleAPIPasswordRespond_NoAgent(t *testing.T) {
	ws, _ := newTestWebServer(t)
	// ws.agent is nil — resolveEditAgent returns nil.

	body := `{"password":"secret"}`
	req := httptest.NewRequest(http.MethodPost, "/api/password/pwd_1/respond", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIPasswordRoutes(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no agent, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIPasswordRoutes_NotFound(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/password/something-else", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIPasswordRoutes(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown password route, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// extractPasswordIDFromPath
// ---------------------------------------------------------------------------

func TestExtractPasswordIDFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/api/password/pwd_1/respond", "pwd_1"},
		{"/api/password/pwd_42/respond", "pwd_42"},
		{"/api/password/my-request-id/respond", "my-request-id"},
		{"/api/password/pwd_1", "pwd_1"},
		{"/api/edits/edit_1/decision", ""},
		{"/api/password/respond", ""},
		{"/api/password/", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := extractPasswordIDFromPath(tt.path)
		if result != tt.expected {
			t.Errorf("extractPasswordIDFromPath(%q) = %q, want %q", tt.path, result, tt.expected)
		}
	}
}
