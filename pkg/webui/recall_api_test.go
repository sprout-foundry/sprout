//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newTestRecallServer creates a minimal ReactWebServer for recall API tests.
func newTestRecallServer(t *testing.T) *ReactWebServer {
	t.Helper()
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// ---------------------------------------------------------------------------
// handleAPIRecall — GET /api/recall
// ---------------------------------------------------------------------------

func TestHandleAPIRecall_MissingQuery(t *testing.T) {
	ws := newTestRecallServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/recall", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIRecall(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response as JSON: %v", err)
	}
	if resp["error"] == "" {
		t.Error("expected error message in response, got none")
	}
	if !strings.Contains(resp["error"], "query") {
		t.Errorf("expected error to mention 'query', got: %s", resp["error"])
	}
}

func TestHandleAPIRecall_MethodNotAllowed(t *testing.T) {
	ws := newTestRecallServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/recall?query=test", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIRecall(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d: %s", http.StatusMethodNotAllowed, rec.Code, rec.Body.String())
	}
}

func TestHandleAPIRecall_NoAgent(t *testing.T) {
	ws := newTestRecallServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/recall?query=find+a+function", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIRecall(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp RecallResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Query != "find a function" {
		t.Errorf("expected query 'find a function', got %q", resp.Query)
	}
	if resp.Count != 0 {
		t.Errorf("expected count 0, got %d", resp.Count)
	}
	if resp.Items == nil {
		t.Error("expected items to be an empty array, got nil")
	}
	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(resp.Items))
	}
}

func TestHandleAPIRecall_WithMockedAgent(t *testing.T) {
	ws := newTestRecallServer(t)
	// Set a real (but empty) agent — Recall returns nil items when there's no embedding manager.
	ws.agent = &agent.Agent{}

	req := httptest.NewRequest(http.MethodGet, "/api/recall?query=test", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIRecall(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp RecallResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Query != "test" {
		t.Errorf("expected query 'test', got %q", resp.Query)
	}
	if resp.Items == nil {
		t.Error("expected items to be an empty array, got nil")
	}
}

func TestHandleAPIRecall_InvalidLimit(t *testing.T) {
	ws := newTestRecallServer(t)

	// Invalid limit should be ignored and default to 5.
	req := httptest.NewRequest(http.MethodGet, "/api/recall?query=test&limit=invalid", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIRecall(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp RecallResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Query != "test" {
		t.Errorf("expected query 'test', got %q", resp.Query)
	}
}

func TestHandleAPIRecall_LimitOutOfRange(t *testing.T) {
	ws := newTestRecallServer(t)

	// limit=100 should be capped to recallMaxLimit (50).
	req := httptest.NewRequest(http.MethodGet, "/api/recall?query=test&limit=100", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIRecall(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp RecallResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// With no agent, items are empty regardless of limit.
	if resp.Items == nil {
		t.Error("expected items to be an empty array, got nil")
	}

	// limit=0 should fall back to default.
	req2 := httptest.NewRequest(http.MethodGet, "/api/recall?query=test&limit=0", nil)
	req2.Header.Set(webClientIDHeader, "test-client")
	rec2 := httptest.NewRecorder()
	ws.handleAPIRecall(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status %d for limit=0, got %d: %s", http.StatusOK, rec2.Code, rec2.Body.String())
	}
}

func TestHandleAPIRecall_QueryTooLong(t *testing.T) {
	ws := newTestRecallServer(t)

	// Build a query string with 1025 characters (exceeds recallMaxQueryLen).
	longQuery := strings.Repeat("a", recallMaxQueryLen+1)
	req := httptest.NewRequest(http.MethodGet, "/api/recall?query="+longQuery, nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIRecall(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}
