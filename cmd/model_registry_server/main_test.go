// Command model_registry_server tests
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateRegistryDir(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	tests := []struct {
		name    string
		setup   func(string) error
		wantErr bool
	}{
		{
			name: "valid directory with models subdirectory",
			setup: func(dir string) error {
				return os.MkdirAll(filepath.Join(dir, "models"), 0755)
			},
			wantErr: false,
		},
		{
			name: "valid directory without models subdirectory (creates it)",
			setup: func(dir string) error {
				// Create the directory but not the models subdirectory
				// validateRegistryDir will create the models subdirectory
				return os.Mkdir(dir, 0755)
			},
			wantErr: false,
		},
		{
			name: "directory does not exist",
			setup: func(dir string) error {
				// t.TempDir() already created testDir, so we don't need to create it again
				return nil
			},
			wantErr: true,
		},
		{
			name: "path is not a directory",
			setup: func(dir string) error {
				return os.WriteFile(dir, []byte("not a directory"), 0644)
			},
			wantErr: true,
		},
		{
			name: "models subdirectory is a file",
			setup: func(dir string) error {
				if err := os.Mkdir(dir, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(dir, "models"), []byte("not a directory"), 0644)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := filepath.Join(tempDir, tt.name)
			if tt.setup != nil {
				if err := tt.setup(testDir); err != nil {
					t.Fatalf("setup failed: %v", err)
				}
			}

			// Override registryDir for this test
			originalDir := *registryDir
			defer func() { *registryDir = originalDir }()
			*registryDir = testDir

			err := validateRegistryDir(testDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRegistryDir() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHandleRoot(t *testing.T) {
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

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
}

func TestHandleRootNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	w := httptest.NewRecorder()

	handleRoot(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestHandleHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var data map[string]string
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if data["status"] != "ok" {
		t.Errorf("expected status 'ok', got %s", data["status"])
	}
}

func TestHandleModels(t *testing.T) {
	// Create a temporary registry directory
	tempDir := t.TempDir()
	modelsDir := filepath.Join(tempDir, "models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("failed to create models directory: %v", err)
	}

	// Create a test provider JSON file
	testProvider := "openrouter"
	testData := map[string]interface{}{
		"updated_at": time.Now().UTC().Format(time.RFC3339),
		"models": []map[string]interface{}{
			{
				"id":              "test-model-1",
				"name":            "Test Model 1",
				"context_length":  128000,
				"input_cost":      0.15,
				"output_cost":     0.60,
				"description":     "A test model",
				"provider":        "openrouter",
				"tags":            []string{"test", "demo"},
			},
		},
	}
	jsonData, err := json.MarshalIndent(testData, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal test data: %v", err)
	}
	testFile := filepath.Join(modelsDir, testProvider+".json")
	if err := os.WriteFile(testFile, jsonData, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Override registryDir for this test
	originalDir := *registryDir
	defer func() { *registryDir = originalDir }()
	*registryDir = tempDir

	tests := []struct {
		name       string
		path       string
		method     string
		wantStatus int
		checkBody  bool
	}{
		{
			name:       "valid provider JSON",
			path:       "/models/" + testProvider + ".json",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			checkBody:  true,
		},
		{
			name:       "provider not found",
			path:       "/models/nonexistent.json",
			method:     http.MethodGet,
			wantStatus: http.StatusNotFound,
			checkBody:  false,
		},
		{
			name:       "missing .json extension",
			path:       "/models/openrouter",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
			checkBody:  false,
		},
		{
			name:       "invalid provider ID with special characters",
			path:       "/models/invalid$provider.json",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
			checkBody:  false,
		},
		{
			name:       "method not allowed",
			path:       "/models/openrouter.json",
			method:     http.MethodPost,
			wantStatus: http.StatusMethodNotAllowed,
			checkBody:  false,
		},
		{
			name:       "empty provider ID",
			path:       "/models/.json",
			method:     http.MethodGet,
			wantStatus: http.StatusBadRequest,
			checkBody:  false,
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

			if tt.checkBody {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("failed to read response body: %v", err)
				}

				var data map[string]interface{}
				if err := json.Unmarshal(body, &data); err != nil {
					t.Fatalf("failed to unmarshal JSON: %v", err)
				}

				if data["models"] == nil {
					t.Error("expected 'models' field in response")
				}

				// Verify Cache-Control header is set
				cacheControl := resp.Header.Get("Cache-Control")
				if cacheControl != "public, max-age=3600" {
					t.Errorf("expected Cache-Control 'public, max-age=3600', got %q", cacheControl)
				}

				// Verify security headers are set
				nosniff := resp.Header.Get("X-Content-Type-Options")
				if nosniff != "nosniff" {
					t.Errorf("expected X-Content-Type-Options 'nosniff', got %q", nosniff)
				}

				frameOptions := resp.Header.Get("X-Frame-Options")
				if frameOptions != "DENY" {
					t.Errorf("expected X-Frame-Options 'DENY', got %q", frameOptions)
				}
			}
		})
	}
}

func TestIsValidProviderID(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		// Valid IDs
		{"openai", true},
		{"openrouter", true},
		{"ollama-local", true},
		{"zai", true},
		{"deepinfra", true},
		{"lm_studio", true},
		{"provider123", true},
		{"test-provider_name", true},
		{"a", true},
		{"123", true},

		// Invalid IDs
		{"", false},
		{"OpenAI", false},          // uppercase
		{"openAI", false},          // mixed case
		{"open.ai", false},         // contains dot
		{"open$router", false},      // contains special character
		{"open router", false},      // contains space
		{"openrouter/", false},     // contains slash
		{"openrouter\\", false},    // contains backslash
		{"openrouter!", false},     // contains exclamation
		{"openrouter@", false},     // contains at sign
		{"openrouter#", false},     // contains hash
		{"openrouter%", false},     // contains percent
		{"openrouter^", false},     // contains caret
		{"openrouter&", false},     // contains ampersand
		{"openrouter*", false},     // contains asterisk
		{"openrouter(", false},     // contains parenthesis
		{"openrouter)", false},     // contains parenthesis
		{"openrouter+", false},     // contains plus
		{"openrouter=", false},     // contains equals
		{"openrouter[", false},     // contains bracket
		{"openrouter]", false},     // contains bracket
		{"openrouter{", false},     // contains brace
		{"openrouter}", false},     // contains brace
		{"openrouter|", false},     // contains pipe
		{"openrouter:", false},     // contains colon
		{"openrouter;", false},     // contains semicolon
		{"openrouter'", false},     // contains single quote
		{"openrouter\"", false},    // contains double quote
		{"openrouter<", false},     // contains less than
		{"openrouter>", false},     // contains greater than
		{"openrouter,", false},     // contains comma
		{"openrouter.", false},     // contains dot
		{"openrouter?", false},     // contains question mark
		{"openrouter~", false},     // contains tilde
		{"openrouter`", false},     // contains backtick
		{strings.Repeat("a", 129), false}, // too long
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result := isValidProviderID(tt.id)
			if result != tt.valid {
				t.Errorf("isValidProviderID(%q) = %v, want %v", tt.id, result, tt.valid)
			}
		})
	}
}

func TestMainIntegration(t *testing.T) {
	// Create a temporary registry directory
	tempDir := t.TempDir()
	modelsDir := filepath.Join(tempDir, "models")
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		t.Fatalf("failed to create models directory: %v", err)
	}

	// Create test provider files
	testProviders := []string{"openai", "openrouter", "ollama-local"}
	for _, provider := range testProviders {
		testData := map[string]interface{}{
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"models": []map[string]interface{}{
				{
					"id":             provider + "-model-1",
					"name":           "Model 1",
					"context_length": 128000,
					"input_cost":     0.15,
					"output_cost":    0.60,
					"description":    "A test model",
					"provider":       provider,
				},
			},
		}
		jsonData, err := json.MarshalIndent(testData, "", "  ")
		if err != nil {
			t.Fatalf("failed to marshal test data: %v", err)
		}
		testFile := filepath.Join(modelsDir, provider+".json")
		if err := os.WriteFile(testFile, jsonData, 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
	}

	// Override registryDir for this test
	originalDir := *registryDir
	originalPort := *port
	defer func() {
		*registryDir = originalDir
		*port = originalPort
	}()
	*registryDir = tempDir
	*port = 0 // Use random port

	// Start server in a goroutine
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		ReadTimeout:  defaultReadTimeout,
		WriteTimeout: defaultWriteTimeout,
		IdleTimeout:  defaultIdleTimeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/healthz", handleHealth)
	mux.HandleFunc("/models/", handleModels)
	server.Handler = mux

	// Use a listener to get the actual port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("server error: %v", err)
		}
	}()
	defer server.Close()

	// Give the server time to start
	time.Sleep(100 * time.Millisecond)

	baseURL := "http://" + listener.Addr().String()

	// Test root endpoint
	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("failed to GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / returned status %d", resp.StatusCode)
	}

	// Test health endpoint
	resp, err = http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("failed to GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz returned status %d", resp.StatusCode)
	}

	// Test model endpoints
	for _, provider := range testProviders {
		resp, err = http.Get(baseURL + "/models/" + provider + ".json")
		if err != nil {
			t.Fatalf("failed to GET /models/%s.json: %v", provider, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /models/%s.json returned status %d", provider, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if data["models"] == nil {
			t.Errorf("expected 'models' field in response for %s", provider)
		}
	}

	// Test 404 for non-existent provider
	resp, err = http.Get(baseURL + "/models/nonexistent.json")
	if err != nil {
		t.Fatalf("failed to GET /models/nonexistent.json: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /models/nonexistent.json returned status %d, expected 404", resp.StatusCode)
	}
}
