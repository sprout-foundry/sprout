package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupTestServer(t *testing.T) (string, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("failed to create models dir: %v", err)
	}

	providerData := map[string]interface{}{
		"updated_at": "2024-01-01T00:00:00Z",
		"models": []map[string]interface{}{
			{"id": "model-1", "name": "Model One", "context_length": 128000},
			{"id": "model-2", "name": "Model Two", "context_length": 64000},
		},
	}
	data, _ := json.MarshalIndent(providerData, "", "  ")
	if err := os.WriteFile(filepath.Join(modelsDir, "openrouter.json"), data, 0o644); err != nil {
		t.Fatalf("failed to write provider file: %v", err)
	}

	return dir, httptest.NewServer(newHandler(dir))
}

func TestServer_ModelEndpoint(t *testing.T) {
	dir, srv := setupTestServer(t)
	defer srv.Close()

	t.Logf("test dir: %s", dir)

	resp, err := http.Get(srv.URL + "/models/openrouter.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc == "" {
		t.Error("expected Cache-Control header to be set")
	}
	if origin := resp.Header.Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("expected CORS Access-Control-Allow-Origin *, got %q", origin)
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	models := payload["models"].([]interface{})
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
}

func TestServer_ETag(t *testing.T) {
	_, srv := setupTestServer(t)
	defer srv.Close()

	resp1, err := http.Get(srv.URL + "/models/openrouter.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp1.Body.Close()

	etag := resp1.Header.Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header to be set")
	}
	// ETag should be a SHA-256 hash (64 hex chars in quotes)
	if len(etag) < 10 {
		t.Errorf("ETag too short: %q", etag)
	}

	// Same content should produce same ETag
	resp2, err := http.Get(srv.URL + "/models/openrouter.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp2.Body.Close()

	etag2 := resp2.Header.Get("ETag")
	if etag != etag2 {
		t.Errorf("expected same ETag for same content: %q vs %q", etag, etag2)
	}
}

func TestServer_NotFound(t *testing.T) {
	_, srv := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/models/nonexistent.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServer_HealthEndpoint(t *testing.T) {
	_, srv := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if payload["status"] != "ok" {
		t.Errorf("expected status ok, got %q", payload["status"])
	}
}

func TestServer_IndexEndpoint(t *testing.T) {
	_, srv := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestServer_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	os.MkdirAll(modelsDir, 0o755)
	os.WriteFile(filepath.Join(modelsDir, "bad.json"), []byte("not-json"), 0o644)

	srv := httptest.NewServer(newHandler(dir))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/models/bad.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 for invalid JSON, got %d", resp.StatusCode)
	}
}

func TestServer_PathTraversal(t *testing.T) {
	_, srv := setupTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/models/../main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for path traversal, got %d", resp.StatusCode)
	}
}

func TestServer_PathTraversalVariants(t *testing.T) {
	_, srv := setupTestServer(t)
	defer srv.Close()

	testPaths := []string{
		"/models/..%2fmain.go",
		"/models/../../etc/passwd",
		"/models/....//main.go",
		"/models/nonexistent/../etc/passwd",
	}

	for _, path := range testPaths {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("expected 404 for path traversal attempt %q, got %d", path, resp.StatusCode)
			}
		})
	}
}

func TestServer_FilenameEdgeCases(t *testing.T) {
	_, srv := setupTestServer(t)
	defer srv.Close()

	testCases := []struct {
		path string
		want int
	}{
		{"/models/", http.StatusNotFound},
		{"/models/a.txt", http.StatusNotFound},      // wrong extension
		{"/models/.json", http.StatusNotFound},      // just extension
		{"/models/a.json.bak", http.StatusNotFound}, // wrong extension
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tc.path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("expected %d for %q, got %d", tc.want, tc.path, resp.StatusCode)
			}
		})
	}
}

func TestServer_OPTIONSRequest(t *testing.T) {
	_, srv := setupTestServer(t)
	defer srv.Close()

	req, _ := http.NewRequest("OPTIONS", srv.URL+"/models/openrouter.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	// CORS headers should be set even on OPTIONS
	if origin := resp.Header.Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("expected CORS header on OPTIONS, got %q", origin)
	}
}
