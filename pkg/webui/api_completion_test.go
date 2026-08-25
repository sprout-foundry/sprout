//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleAPICompletionMethodNotAllowed(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodGet, "/api/completion", nil)
	rec := httptest.NewRecorder()
	ws.handleAPICompletion(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleAPICompletionInvalidJSON(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/completion", strings.NewReader("bad"))
	rec := httptest.NewRecorder()
	ws.handleAPICompletion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp["code"] != "invalid_json" {
		t.Fatalf("expected code invalid_json, got %v", resp["code"])
	}
}

func TestHandleAPICompletionEmptyPrefix(t *testing.T) {
	ws := &ReactWebServer{}
	req := httptest.NewRequest(http.MethodPost, "/api/completion", strings.NewReader(`{"prefix":""}`))
	rec := httptest.NewRecorder()
	ws.handleAPICompletion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if resp["code"] != "prefix_required" {
		t.Fatalf("expected code prefix_required, got %v", resp["code"])
	}
}
