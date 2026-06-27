//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/search"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testSearchIndexPath returns the search index path for the given temp root.
// It mirrors search.DefaultIndexPath() but relative to root instead of $HOME.
func testSearchIndexPath(root string) string {
	return filepath.Join(root, ".sprout", "sessions", "search-index.json")
}

// makeTestIndex builds a SessionIndex with three entries for testing.
func makeTestIndex() *search.SessionIndex {
	return &search.SessionIndex{
		Version: 1,
		BuiltAt: time.Now(),
		Sessions: map[string]search.SessionIndexEntry{
			"sess-embed": {
				SessionID:    "sess-embed",
				Name:         "Embedding Migration",
				WorkingDir:   "/tmp/test-project",
				LastUpdated:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
				TotalCost:    0.05,
				MessageCount: 1,
				Tokens:       map[string][]int{"sess-embed:0": {0, len("the embedding index was rebuilt after the schema migration")}},
				Text:         "the embedding index was rebuilt after the schema migration",
			},
			"sess-auth": {
				SessionID:    "sess-auth",
				Name:         "Auth Debugging",
				WorkingDir:   "/home/user/project",
				LastUpdated:  time.Date(2025, 6, 20, 10, 0, 0, 0, time.UTC),
				TotalCost:    0.03,
				MessageCount: 1,
				Tokens:       map[string][]int{"sess-auth:0": {0, len("openai auth was returning 401 on the api key validation")}},
				Text:         "openai auth was returning 401 on the api key validation",
			},
			"sess-test": {
				SessionID:    "sess-test",
				Name:         "Testing Workflow",
				WorkingDir:   "/tmp/test-project",
				LastUpdated:  time.Date(2025, 7, 1, 10, 0, 0, 0, time.UTC),
				TotalCost:    0.10,
				MessageCount: 1,
				Tokens:       map[string][]int{"sess-test:0": {0, len("testing the search endpoint with integration tests")}},
				Text:         "testing the search endpoint with integration tests",
			},
		},
	}
}

// setupSearchTest creates a ReactWebServer with a fake search index in a temp
// HOME directory. Sets HOME before creating the server so that any
// initialization (e.g., search.DefaultIndexPath) resolves against the temp
// directory instead of the real $HOME. Returns the web server and temp root.
func setupSearchTest(t *testing.T) (*ReactWebServer, string) {
	t.Helper()
	root := t.TempDir()

	// Set HOME BEFORE creating the web server so that any initialization
	// that resolves search.DefaultIndexPath() uses the temp directory.
	t.Setenv("HOME", root)

	// Write the test index before creating the server (server init may read it).
	idx := makeTestIndex()
	idxPath := testSearchIndexPath(root)
	if err := search.SaveIndex(idxPath, idx); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	// Create the web server after HOME is set.
	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatalf("NewReactWebServer: %v", err)
	}

	return ws, root
}

// ---------------------------------------------------------------------------
// Acceptance criterion 1: GET /api/sessions/search?q=embedding returns 200
// with JSON containing query, total, and results.
// ---------------------------------------------------------------------------

func TestHandleAPISessionsSearch_ResponseBodyShape(t *testing.T) {
	ws, _ := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=embedding", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Query   string                   `json:"query"`
		Total   int                      `json:"total"`
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Query != "embedding" {
		t.Errorf("expected query 'embedding', got %q", resp.Query)
	}
	if resp.Total != 1 {
		t.Errorf("expected total 1 (only sess-embed has 'embedding'), got %d", resp.Total)
	}
	if len(resp.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(resp.Results))
	}
}

// ---------------------------------------------------------------------------
// Acceptance criterion 2: Each result has all required fields.
// ---------------------------------------------------------------------------

func TestHandleAPISessionsSearch_ResultFields(t *testing.T) {
	ws, _ := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=testing", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected at least one result")
	}

	r := resp.Results[0]
	requiredFields := []string{"session_id", "name", "working_directory", "last_updated", "total_cost", "excerpt", "match_score"}
	for _, f := range requiredFields {
		if _, ok := r[f]; !ok {
			t.Errorf("result missing required field %q", f)
		}
	}
	if r["session_id"] != "sess-test" {
		t.Errorf("expected session_id 'sess-test', got %v", r["session_id"])
	}
	if r["name"] != "Testing Workflow" {
		t.Errorf("expected name 'Testing Workflow', got %v", r["name"])
	}
}

// ---------------------------------------------------------------------------
// Acceptance criterion 3: Missing q returns 400 with error message.
// ---------------------------------------------------------------------------

func TestHandleAPISessionsSearch_MissingQuery(t *testing.T) {
	ws, _ := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "missing q parameter" {
		t.Errorf("expected error 'missing q parameter', got %v", resp["error"])
	}
}

// ---------------------------------------------------------------------------
// Acceptance criterion 4: cwd filter is honored.
// ---------------------------------------------------------------------------

func TestHandleAPISessionsSearch_CwdFilter(t *testing.T) {
	ws, _ := setupSearchTest(t)

	// "the" appears in all 3 entries.  With cwd=/tmp/test-project only
	// sess-embed and sess-test qualify (sess-auth is /home/user/project).
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=the&cwd=/tmp/test-project", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Total   int                      `json:"total"`
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected 2 results with cwd=/tmp/test-project, got %d", resp.Total)
	}
	for _, r := range resp.Results {
		if r["working_directory"] != "/tmp/test-project" {
			t.Errorf("expected all results in /tmp/test-project, got %v", r["working_directory"])
		}
	}
}

// ---------------------------------------------------------------------------
// Acceptance criterion 5: since/until date filters are honored.
// ---------------------------------------------------------------------------

func TestHandleAPISessionsSearch_DateFilters(t *testing.T) {
	ws, _ := setupSearchTest(t)

	// since=2025-06-21 should exclude sess-embed (June 15) and sess-auth (June 20),
	// leaving only sess-test (July 1).
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=the&since=2025-06-21", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Total   int                      `json:"total"`
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("expected 1 result with since=2025-06-21, got %d", resp.Total)
	}
	if len(resp.Results) > 0 && resp.Results[0]["session_id"] != "sess-test" {
		t.Errorf("expected sess-test, got %v", resp.Results[0]["session_id"])
	}

	// until=2025-06-19 should only include sess-embed (June 15).
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=the&until=2025-06-19", nil)
	rec = httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("expected 1 result with until=2025-06-19, got %d", resp.Total)
	}
	if len(resp.Results) > 0 && resp.Results[0]["session_id"] != "sess-embed" {
		t.Errorf("expected sess-embed, got %v", resp.Results[0]["session_id"])
	}

	// RFC3339 format for since.
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=the&since=2025-07-01T00:00:00Z", nil)
	rec = httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("expected 1 result with RFC3339 since, got %d", resp.Total)
	}
}

// ---------------------------------------------------------------------------
// Acceptance criterion 6: limit=N caps results.
// ---------------------------------------------------------------------------

func TestHandleAPISessionsSearch_LimitCap(t *testing.T) {
	ws, _ := setupSearchTest(t)

	// "the" matches all 3 entries; limit=1 should return only 1.
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=the&limit=1", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Total   int                      `json:"total"`
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("expected total 1 with limit=1, got %d", resp.Total)
	}
	if len(resp.Results) != 1 {
		t.Errorf("expected 1 result with limit=1, got %d", len(resp.Results))
	}

	// limit=2 should return 2.
	req = httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=the&limit=2", nil)
	rec = httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected total 2 with limit=2, got %d", resp.Total)
	}
}

// ---------------------------------------------------------------------------
// Acceptance criterion 7: reindex=true triggers a full rebuild.
// ---------------------------------------------------------------------------

func TestHandleAPISessionsSearch_ReindexTrue(t *testing.T) {
	ws, root := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=the&reindex=true", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected 0 results after reindex=true with no session files, got %d", resp.Total)
	}
	_ = root
}

func TestHandleAPISessionsSearch_ReindexOne(t *testing.T) {
	ws, root := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=the&reindex=1", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected 0 results after reindex=1, got %d", resp.Total)
	}
	_ = root
}

// ---------------------------------------------------------------------------
// Acceptance criterion 8: Empty query (q=) returns empty results, total=0,
// no error.
// ---------------------------------------------------------------------------

func TestHandleAPISessionsSearch_EmptyQuery(t *testing.T) {
	ws, _ := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty query, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Total   int                      `json:"total"`
		Results []search.SearchResult    `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0 for empty query, got %d", resp.Total)
	}
	if len(resp.Results) != 0 {
		t.Errorf("expected no results for empty query, got %d", len(resp.Results))
	}
}

// ---------------------------------------------------------------------------
// Additional edge-case tests
// ---------------------------------------------------------------------------

func TestHandleAPISessionsSearch_MethodNotAllowed(t *testing.T) {
	ws, _ := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/search?q=foo", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPISessionsSearch_InvalidSinceDate(t *testing.T) {
	ws, _ := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=foo&since=not-a-date", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp["error"].(string), "invalid since date") {
		t.Errorf("expected 'invalid since date' in error, got %v", resp["error"])
	}
}

func TestHandleAPISessionsSearch_InvalidUntilDate(t *testing.T) {
	ws, _ := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=foo&until=bad-date", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp["error"].(string), "invalid until date") {
		t.Errorf("expected 'invalid until date' in error, got %v", resp["error"])
	}
}

func TestHandleAPISessionsSearch_InvalidLimit(t *testing.T) {
	ws, _ := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=foo&limit=abc", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp["error"].(string), "invalid limit") {
		t.Errorf("expected 'invalid limit' in error, got %v", resp["error"])
	}
}

func TestHandleAPISessionsSearch_NoIndexFile(t *testing.T) {
	ws, root := setupSearchTest(t)

	// Delete the index file — LoadIndex should return an empty index,
	// and the handler should auto-build (BuildIndex on empty scoped dir).
	idxPath := testSearchIndexPath(root)
	if err := os.Remove(idxPath); err != nil {
		t.Fatalf("remove index: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=foo", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (auto-build from empty), got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// No sessions on disk → 0 results after build.
	if resp.Total != 0 {
		t.Errorf("expected 0 results when no index and no session files, got %d", resp.Total)
	}
}

func TestHandleAPISessionsSearch_ContentType(t *testing.T) {
	ws, _ := setupSearchTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/search?q=embedding", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISessionsSearch(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", contentType)
	}
}

// ---------------------------------------------------------------------------
// parseSearchDateWebUI
// ---------------------------------------------------------------------------

func TestParseSearchDateWebUI_RFC3339(t *testing.T) {
	parsed, err := parseSearchDateWebUI("2025-06-15T10:30:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Year() != 2025 {
		t.Errorf("expected year 2025, got %d", parsed.Year())
	}
}

func TestParseSearchDateWebUI_YMD(t *testing.T) {
	parsed, err := parseSearchDateWebUI("2025-06-15")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Year() != 2025 || parsed.Month() != 6 || parsed.Day() != 15 {
		t.Errorf("expected 2025-06-15, got %s", parsed.Format("2006-01-02"))
	}
}

func TestParseSearchDateWebUI_Invalid(t *testing.T) {
	_, err := parseSearchDateWebUI("not-a-date")
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
	if !strings.Contains(err.Error(), "invalid date") {
		t.Errorf("expected 'invalid date' in error, got %v", err)
	}
}
