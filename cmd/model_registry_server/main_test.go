// Command model_registry_server tests
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestIsValidProviderID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		// Valid IDs
		{"lowercase letters", "openrouter", true},
		{"single word", "openai", true},
		{"short", "zai", true},
		{"with hyphen", "ollama-local", true},
		{"with hyphen and number", "my-provider-v2", true},
		{"with underscore", "lm_studio", true},
		{"with hyphen and underscore", "test-provider_name", true},
		{"alphanumeric", "a1", true},
		{"starts with number", "123provider", true},
		{"only numbers", "123", true},
		{"max length (128 chars)", strings.Repeat("a", 128), true},

		// Invalid IDs
		{"empty string", "", false},
		{"uppercase", "OpenRouter", false},
		{"mixed case", "openAI", false},
		{"with slash", "open/router", false},
		{"with dot", "open.router", false},
		{"with special chars", "has!special", false},
		{"with space", "open router", false},
		{"with dollar", "provider$name", false},
		{"with at sign", "provider@test", false},
		{"too long (129 chars)", strings.Repeat("a", 129), false},
		{"with percent", "prov%der", false},
		{"with ampersand", "prov&der", false},
		{"with asterisk", "prov*der", false},
		{"with plus", "prov+der", false},
		{"with equals", "prov=der", false},
		{"with brackets", "prov[der]", false},
		{"with braces", "prov{der}", false},
		{"with pipe", "prov|der", false},
		{"with colon", "prov:der", false},
		{"with semicolon", "prov;der", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidProviderID(tt.id)
			if got != tt.want {
				t.Errorf("isValidProviderID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestListAvailableProviders(t *testing.T) {
	tempDir := t.TempDir()

	// Create valid provider JSON files
	validFiles := []string{"openai.json", "openrouter.json", "ollama-local.json", "zai.json"}
	for _, name := range validFiles {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte("{}"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	// Create files that should be ignored
	if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte(""), 0644); err != nil {
		t.Fatalf("failed to create README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "InvalidProvider.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create invalid provider file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "bad@provider.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create bad provider file: %v", err)
	}

	// Create a subdirectory (should be ignored)
	if err := os.Mkdir(filepath.Join(tempDir, "subdir"), 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	providers, err := listAvailableProviders(tempDir)
	if err != nil {
		t.Fatalf("listAvailableProviders() error = %v", err)
	}

	expectedProviders := []string{"openai", "openrouter", "ollama-local", "zai"}
	if len(providers) != len(expectedProviders) {
		t.Errorf("got %d providers, want %d", len(providers), len(expectedProviders))
	}

	providerMap := make(map[string]bool)
	for _, p := range providers {
		providerMap[p] = true
	}
	for _, exp := range expectedProviders {
		if !providerMap[exp] {
			t.Errorf("missing expected provider: %s", exp)
		}
	}
}

func TestHandleHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	body, _ := io.ReadAll(resp.Body)
	var data map[string]string
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if data["status"] != "ok" {
		t.Errorf("expected status 'ok', got %s", data["status"])
	}
}

func TestHandleRoot(t *testing.T) {
	tempDir := t.TempDir()

	for _, name := range []string{"openai.json", "openrouter.json"} {
		if err := os.WriteFile(filepath.Join(tempDir, name), []byte(`{"models":[]}`), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	originalDir := *registryDir
	defer func() { *registryDir = originalDir }()
	*registryDir = tempDir

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	body, _ := io.ReadAll(resp.Body)
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if data["name"] == nil {
		t.Error("expected 'name' field in response")
	}
	if data["version"] == nil {
		t.Error("expected 'version' field in response")
	}

	providers, ok := data["providers"].([]interface{})
	if !ok {
		t.Fatal("expected 'providers' to be an array")
	}
	if len(providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(providers))
	}
}

func TestHandleRoot_NotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestHandleModels(t *testing.T) {
	tempDir := t.TempDir()

	testData := map[string]interface{}{
		"updated_at": "2024-01-01T00:00:00Z",
		"models": []map[string]interface{}{
			{
				"id":             "test-model",
				"name":           "Test Model",
				"context_length": 128000,
			},
		},
	}
	jsonData, _ := json.Marshal(testData)
	if err := os.WriteFile(filepath.Join(tempDir, "openrouter.json"), jsonData, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	originalDir := *registryDir
	defer func() { *registryDir = originalDir }()
	*registryDir = tempDir

	tests := []struct {
		name         string
		path         string
		method       string
		wantStatus   int
		checkHeaders bool
		checkBody    bool
	}{
		{
			name:         "valid provider",
			path:         "/models/openrouter.json",
			method:       http.MethodGet,
			wantStatus:   http.StatusOK,
			checkHeaders: true,
			checkBody:    true,
		},
		{
			name:       "non-existent provider",
			path:       "/models/nonexistent.json",
			method:     http.MethodGet,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "uppercase provider ID",
			path:       "/models/OpenRouter.json",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "special characters in provider ID",
			path:       "/models/provider$test.json",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "POST request",
			path:       "/models/openrouter.json",
			method:     http.MethodPost,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "missing .json extension",
			path:       "/models/openrouter",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty provider ID",
			path:       "/models/.json",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "path traversal attempt",
			path:       "/models/../otherfile.json",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			handleModels(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}

			if tt.checkHeaders {
				cacheControl := resp.Header.Get("Cache-Control")
				if cacheControl != "public, max-age=300" {
					t.Errorf("Cache-Control = %q, want %q", cacheControl, "public, max-age=300")
				}

				nosniff := resp.Header.Get("X-Content-Type-Options")
				if nosniff != "nosniff" {
					t.Errorf("X-Content-Type-Options = %q, want nosniff", nosniff)
				}

				frameOpts := resp.Header.Get("X-Frame-Options")
				if frameOpts != "DENY" {
					t.Errorf("X-Frame-Options = %q, want DENY", frameOpts)
				}

				contentType := resp.Header.Get("Content-Type")
				if !strings.HasPrefix(contentType, "application/json") {
					t.Errorf("Content-Type = %q, want application/json", contentType)
				}
			}

			if tt.checkBody {
				body, _ := io.ReadAll(resp.Body)
				var data map[string]interface{}
				if err := json.Unmarshal(body, &data); err != nil {
					t.Fatalf("failed to unmarshal JSON: %v", err)
				}
				if data["models"] == nil {
					t.Error("expected 'models' field in response")
				}
			}
		})
	}
}

func TestHandleModels_SymlinkOutside(t *testing.T) {
	tempDir := t.TempDir()

	// Create a file outside the registry directory
	secretFile := filepath.Join(tempDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("secret data"), 0644); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// Create registry directory
	modelsDir := filepath.Join(tempDir, "models")
	if err := os.Mkdir(modelsDir, 0755); err != nil {
		t.Fatalf("failed to create models dir: %v", err)
	}

	// Create symlink pointing outside the registry directory
	symlinkPath := filepath.Join(modelsDir, "openrouter.json")
	if err := os.Symlink(secretFile, symlinkPath); err != nil {
		t.Skipf("symlinks not supported on this filesystem: %v", err)
	}

	originalDir := *registryDir
	defer func() { *registryDir = originalDir }()
	*registryDir = modelsDir

	req := httptest.NewRequest(http.MethodGet, "/models/openrouter.json", nil)
	w := httptest.NewRecorder()

	handleModels(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Symlinks pointing outside the registry directory must be blocked.
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for symlink pointing outside registry, got %d (body: %s)", resp.StatusCode, string(body))
	}
}

func TestHandleModels_SymlinkInside(t *testing.T) {
	tempDir := t.TempDir()

	// Create a real JSON file inside the registry directory.
	realData := `{"updated_at":"2024-01-01T00:00:00Z","models":[{"id":"symlinked-model"}]}`
	realFile := filepath.Join(tempDir, "real-data.json")
	if err := os.WriteFile(realFile, []byte(realData), 0644); err != nil {
		t.Fatalf("failed to create real data file: %v", err)
	}

	// Create a symlink pointing to the real file.
	symlinkPath := filepath.Join(tempDir, "openrouter.json")
	if err := os.Symlink(realFile, symlinkPath); err != nil {
		t.Skipf("symlinks not supported on this filesystem: %v", err)
	}

	originalDir := *registryDir
	defer func() { *registryDir = originalDir }()
	*registryDir = tempDir

	req := httptest.NewRequest(http.MethodGet, "/models/openrouter.json", nil)
	w := httptest.NewRecorder()

	handleModels(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for symlink pointing inside registry, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if data["models"] == nil {
		t.Error("expected 'models' field in response")
	}
}

func TestHandleModels_ConcurrentRequests(t *testing.T) {
	tempDir := t.TempDir()

	testData := map[string]interface{}{"models": []string{"test"}}
	jsonData, _ := json.Marshal(testData)
	if err := os.WriteFile(filepath.Join(tempDir, "openrouter.json"), jsonData, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	originalDir := *registryDir
	defer func() { *registryDir = originalDir }()
	*registryDir = tempDir

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/models/openrouter.json", nil)
			w := httptest.NewRecorder()
			handleModels(w, req)
			if w.Code != http.StatusOK {
				errors <- fmt.Errorf("got status %d, want 200", w.Code)
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestSanitizeForLog(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal path", "/models/openrouter.json", "/models/openrouter.json"},
		{"with newline", "/models/test\ninjected", "/models/testinjected"},
		{"with carriage return", "/models/test\rfake", "/models/testfake"},
		{"with tab", "/models/test\tfile.json", "/models/test\tfile.json"},
		{"with null byte", "/models/\x00secret", "/models/secret"},
		{"with control chars", "/models/\x01\x02\x03", "/models/"},
		{"empty string", "", ""},
		{"only control chars", "\x00\x01\x02", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeForLog(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeForLog(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	middleware := loggingMiddleware(handleHealth)
	middleware(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var data map[string]string
	json.Unmarshal(body, &data)
	if data["status"] != "ok" {
		t.Error("middleware did not call handler correctly")
	}
}

func TestLoggingResponseWriter(t *testing.T) {
	t.Run("captures explicit status code", func(t *testing.T) {
		w := httptest.NewRecorder()
		lw := &loggingResponseWriter{ResponseWriter: w}

		if lw.status != 0 {
			t.Errorf("initial status should be 0, got %d", lw.status)
		}

		lw.WriteHeader(http.StatusNotFound)

		if lw.status != http.StatusNotFound {
			t.Errorf("status after WriteHeader = %d, want %d", lw.status, http.StatusNotFound)
		}

		if w.Code != http.StatusNotFound {
			t.Errorf("underlying writer status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})

	t.Run("defaults to 200 on Write without WriteHeader", func(t *testing.T) {
		w := httptest.NewRecorder()
		lw := &loggingResponseWriter{ResponseWriter: w}

		lw.Write([]byte("hello"))

		if lw.status != http.StatusOK {
			t.Errorf("status after Write = %d, want %d", lw.status, http.StatusOK)
		}
	})

	t.Run("Write called without WriteHeader defaults to 200 on subsequent reads", func(t *testing.T) {
		w := httptest.NewRecorder()
		lw := &loggingResponseWriter{ResponseWriter: w}

		// Simulate http.ServeFile behavior: write body without explicit WriteHeader
		n, err := lw.Write([]byte("response body"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != len("response body") {
			t.Errorf("expected %d bytes written, got %d", len("response body"), n)
		}
		if lw.status != http.StatusOK {
			t.Errorf("status after Write = %d, want %d", lw.status, http.StatusOK)
		}
	})
}
