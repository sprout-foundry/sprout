package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// newTestSemanticServer creates a minimal ReactWebServer for semantic search tests.
// Unlike newTestWebServer, it does not set up a workspace or terminal manager,
// because the semantic search endpoint only needs the embedding manager (which is nil here).
func newTestSemanticServer(t *testing.T) *ReactWebServer {
	t.Helper()
	return NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1")
}

// ---------------------------------------------------------------------------
// handleAPISemanticSearch — GET /api/search/semantic
// ---------------------------------------------------------------------------

func TestHandleAPISemanticSearch_MissingQuery(t *testing.T) {
	ws := newTestSemanticServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

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

func TestHandleAPISemanticSearch_MethodNotAllowed(t *testing.T) {
	ws := newTestSemanticServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/search/semantic?query=test", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d: %s", http.StatusMethodNotAllowed, rec.Code, rec.Body.String())
	}
}

func TestHandleAPISemanticSearch_NoAgent(t *testing.T) {
	ws := newTestSemanticServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=find+a+function", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Query != "find a function" {
		t.Errorf("expected query 'find a function', got %q", resp.Query)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
	if resp.Results == nil {
		t.Error("expected results to be an empty array, got nil")
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(resp.Results))
	}
	if resp.Duration == "" {
		t.Error("expected duration to be set, got empty string")
	}
}

func TestHandleAPISemanticSearch_InvalidTopK(t *testing.T) {
	ws := newTestSemanticServer(t)

	// Invalid top_k should be ignored and default to 10.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&top_k=invalid", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("expected total 0 (no agent), got %d", resp.Total)
	}
	if resp.Query != "test" {
		t.Errorf("expected query 'test', got %q", resp.Query)
	}
}

func TestHandleAPISemanticSearch_InvalidThreshold(t *testing.T) {
	ws := newTestSemanticServer(t)

	// Invalid threshold should be ignored and default to 0.75.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&threshold=invalid", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Total != 0 {
		t.Errorf("expected total 0 (no agent), got %d", resp.Total)
	}
	if resp.Query != "test" {
		t.Errorf("expected query 'test', got %q", resp.Query)
	}
}

func TestHandleAPISemanticSearch_TopKOutOfRange(t *testing.T) {
	ws := newTestSemanticServer(t)

	// top_k=0 (below minimum) should be ignored.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&top_k=0", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	// top_k=51 (above maximum) should be ignored.
	req = httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&top_k=51", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

func TestHandleAPISemanticSearch_ThresholdOutOfRange(t *testing.T) {
	ws := newTestSemanticServer(t)

	// threshold=-0.1 (below 0) should be ignored.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&threshold=-0.1", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	// threshold=1.5 (above 1) should be ignored.
	req = httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test&threshold=1.5", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec = httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

func TestHandleAPISemanticSearch_NoClientID(t *testing.T) {
	ws := newTestSemanticServer(t)

	// No client ID header set — should still return empty results gracefully.
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
}

func TestHandleAPISemanticSearch_ValidTopKAndThreshold(t *testing.T) {
	ws := newTestSemanticServer(t)

	// Valid top_k and threshold should be accepted (but still return empty without agent).
	req := httptest.NewRequest(http.MethodGet, "/api/search/semantic?query=hello&top_k=5&threshold=0.8", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPISemanticSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp SemanticSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Query != "hello" {
		t.Errorf("expected query 'hello', got %q", resp.Query)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0 (no agent), got %d", resp.Total)
	}
}
