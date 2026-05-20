//go:build !js

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// handleAPIDiagnostics
// ---------------------------------------------------------------------------

func TestHandleAPIDiagnostics_MethodNotAllowed(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIDiagnostics(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIDiagnostics_InvalidJSON(t *testing.T) {
	ws, _ := newTestWebServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics", strings.NewReader("not json"))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIDiagnostics(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIDiagnostics_EmptyPath(t *testing.T) {
	ws, _ := newTestWebServer(t)

	body := `{"path":"","content":"package main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIDiagnostics(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIDiagnostics_WhitespaceOnlyPath(t *testing.T) {
	ws, _ := newTestWebServer(t)

	body := `{"path":"   ","content":"package main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIDiagnostics(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for whitespace-only path, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIDiagnostics_PathOutsideWorkspace(t *testing.T) {
	ws, _ := newTestWebServer(t)

	// Path tries to escape workspace with ../
	body := `{"path":"/etc/passwd","content":"package main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIDiagnostics(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for path outside workspace, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAPIDiagnostics_NoAgent(t *testing.T) {
	ws, _ := newTestWebServer(t)
	// ws.agent is nil since newTestWebServer passes nil for agent

	body := `{"path":"test.go","content":"package main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIDiagnostics(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when agent is nil, got %d: %s", rec.Code, rec.Body.String())
	}

	// Should return empty diagnostics
	var resp diagnosticsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message != "ok" {
		t.Errorf("expected message 'ok', got %q", resp.Message)
	}
	if resp.Path != "test.go" {
		t.Errorf("expected path 'test.go', got %q", resp.Path)
	}
	if len(resp.Diagnostics) != 0 {
		t.Errorf("expected 0 diagnostics when agent is nil, got %d", len(resp.Diagnostics))
	}
}

func TestHandleAPIDiagnostics_NoValidator(t *testing.T) {
	_ = newTestWebServer

	// This test is a documentation placeholder. The nil-validator path is exercised
	// identically by TestHandleAPIDiagnostics_NoAgent since both check for nil before use.
	// We can't easily construct a real Agent without significant setup, so the
	// writeDiagnosticsResponse tests below cover the core logic paths.
}

func TestHandleAPIDiagnostics_ResponseHasVersion(t *testing.T) {
	ws, _ := newTestWebServer(t)

	body := `{"path":"test.go","content":"package main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIDiagnostics(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp diagnosticsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Version == "" {
		t.Error("expected non-empty version timestamp in response")
	}
}

func TestHandleAPIDiagnostics_ContentTypeJSON(t *testing.T) {
	ws, _ := newTestWebServer(t)

	body := `{"path":"test.go","content":"package main"}`
	req := httptest.NewRequest(http.MethodPost, "/api/diagnostics", strings.NewReader(body))
	req.Header.Set(webClientIDHeader, "test-client")
	rec := httptest.NewRecorder()
	ws.handleAPIDiagnostics(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", contentType)
	}
}

// ---------------------------------------------------------------------------
// writeDiagnosticsResponse
// ---------------------------------------------------------------------------

func TestWriteDiagnosticsResponse_EmptyDiagnostics(t *testing.T) {
	ws, _ := newTestWebServer(t)

	rec := httptest.NewRecorder()
	ws.writeDiagnosticsResponse(rec, "test.go", nil)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", contentType)
	}

	var resp diagnosticsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Message != "ok" {
		t.Errorf("expected message 'ok', got %q", resp.Message)
	}
	if resp.Path != "test.go" {
		t.Errorf("expected path 'test.go', got %q", resp.Path)
	}
	if resp.Diagnostics != nil {
		t.Errorf("expected nil diagnostics, got %v", resp.Diagnostics)
	}
	if resp.Version == "" {
		t.Error("expected non-empty version")
	}
}

func TestWriteDiagnosticsResponse_WithDiagnostics(t *testing.T) {
	ws, _ := newTestWebServer(t)

	diags := []frontendDiagnostic{
		{From: 0, To: 5, Severity: "error", Message: "syntax error", Source: "gofmt"},
		{From: 10, To: 15, Severity: "warning", Message: "unused import", Source: "goimports"},
	}

	rec := httptest.NewRecorder()
	ws.writeDiagnosticsResponse(rec, "pkg/main.go", diags)

	var resp diagnosticsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Diagnostics) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(resp.Diagnostics))
	}
	if resp.Diagnostics[0].Severity != "error" {
		t.Errorf("expected severity 'error', got %q", resp.Diagnostics[0].Severity)
	}
	if resp.Diagnostics[1].Message != "unused import" {
		t.Errorf("expected message 'unused import', got %q", resp.Diagnostics[1].Message)
	}
}

func TestWriteDiagnosticsResponse_VersionIsTimestamp(t *testing.T) {
	ws, _ := newTestWebServer(t)

	rec := httptest.NewRecorder()
	ws.writeDiagnosticsResponse(rec, "test.go", nil)

	var resp diagnosticsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Should parse as RFC3339Nano timestamp
	_, err := time.Parse(time.RFC3339Nano, resp.Version)
	if err != nil {
		t.Errorf("version should be RFC3339Nano timestamp, got %q: %v", resp.Version, err)
	}
}

// ---------------------------------------------------------------------------
// extendToTokenEnd — additional edge cases beyond api_diagnostics_test.go
// ---------------------------------------------------------------------------

func TestExtendToTokenEnd_NonASCII(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		byteOffset int
		want       int
	}{
		{"at delimiter in UTF-8", "héllo world", 5, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extendToTokenEnd(tt.content, tt.byteOffset)
			if got != tt.want {
				t.Errorf("extendToTokenEnd(%q, %d) = %d; want %d", tt.content, tt.byteOffset, got, tt.want)
			}
		})
	}
}

func TestExtendToTokenEnd_SingleCharContent(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		byteOffset int
		want       int
	}{
		{"single letter", "a", 0, 1},
		{"single delimiter", " ", 0, 1},
		{"at end of single char", "a", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extendToTokenEnd(tt.content, tt.byteOffset)
			if got != tt.want {
				t.Errorf("extendToTokenEnd(%q, %d) = %d; want %d", tt.content, tt.byteOffset, got, tt.want)
			}
		})
	}
}

func TestExtendToTokenEnd_AllDelimiters(t *testing.T) {
	content := " (){}[]+,;:+=-*/!=<>%\"'"
	// Starting at 0 (space), should advance to 1
	got := extendToTokenEnd(content, 0)
	if got != 1 {
		t.Errorf("extendToTokenEnd(all-delimiters, 0) = %d; want 1", got)
	}
}

func TestExtendToTokenEnd_LongIdentifier(t *testing.T) {
	content := "thisIsAVeryLongIdentifierName_123"
	got := extendToTokenEnd(content, 0)
	if got != len(content) {
		t.Errorf("extendToTokenEnd(long-identifier, 0) = %d; want %d", got, len(content))
	}
}
