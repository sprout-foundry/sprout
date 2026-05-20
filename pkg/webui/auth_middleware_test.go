//go:build !js

// Package webui provides React web server with embedded assets
package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthTokenMiddleware_GetPassesThrough tests that GET requests pass through
// without auth even when token is configured.
func TestAuthTokenMiddleware_GetPassesThrough(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for GET request")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_HeadPassesThrough tests that HEAD requests pass through
// without auth even when token is configured.
func TestAuthTokenMiddleware_HeadPassesThrough(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodHead, "/api/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for HEAD request")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_OptionsPassesThrough tests that OPTIONS requests pass through
// without auth even when token is configured.
func TestAuthTokenMiddleware_OptionsPassesThrough(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for OPTIONS request")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_PostNoToken tests that POST requests without auth
// are rejected when token is configured.
func TestAuthTokenMiddleware_PostNoToken(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if nextCalled {
		t.Error("Expected next handler NOT to be called for unauthorized POST request")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	// Check error response body
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}
	if resp["error"] != "unauthorized" {
		t.Errorf("Expected error 'unauthorized', got '%s'", resp["error"])
	}
	if resp["message"] != "valid auth token required" {
		t.Errorf("Expected message 'valid auth token required', got '%s'", resp["message"])
	}
}

// TestAuthTokenMiddleware_PostWrongToken tests that POST requests with wrong auth
// are rejected when token is configured.
func TestAuthTokenMiddleware_PostWrongToken(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if nextCalled {
		t.Error("Expected next handler NOT to be called for POST request with wrong token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}
	if resp["error"] != "unauthorized" {
		t.Errorf("Expected error 'unauthorized', got '%s'", resp["error"])
	}
}

// TestAuthTokenMiddleware_PostCorrectToken tests that POST requests with correct
// Bearer token pass through when token is configured.
func TestAuthTokenMiddleware_PostCorrectToken(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for POST request with correct token")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_EmptyToken tests that when authToken is empty,
// all requests pass through regardless of method.
func TestAuthTokenMiddleware_EmptyToken(t *testing.T) {
	var token string = ""
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	// Test POST without auth (should pass through)
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called when token is empty")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_PutRequiresAuth tests that PUT requests require auth.
func TestAuthTokenMiddleware_PutRequiresAuth(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodPut, "/api/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if nextCalled {
		t.Error("Expected next handler NOT to be called for unauthorized PUT request")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_DeleteRequiresAuth tests that DELETE requests require auth.
func TestAuthTokenMiddleware_DeleteRequiresAuth(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodDelete, "/api/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if nextCalled {
		t.Error("Expected next handler NOT to be called for unauthorized DELETE request")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_PatchRequiresAuth tests that PATCH requests require auth.
func TestAuthTokenMiddleware_PatchRequiresAuth(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodPatch, "/api/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if nextCalled {
		t.Error("Expected next handler NOT to be called for unauthorized PATCH request")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_WebSocketEndpointPassesThrough tests that WebSocket
// upgrade endpoints (/ws, /terminal, /api/lsp/ws) pass through even on POST
// without auth.
func TestAuthTokenMiddleware_WebSocketEndpointPassesThrough(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	// Test /ws endpoint
	req := httptest.NewRequest(http.MethodPost, "/ws", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for /ws endpoint")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test /terminal endpoint
	nextCalled = false
	req = httptest.NewRequest(http.MethodPost, "/terminal", nil)
	w = httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for /terminal endpoint")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test /api/lsp/ws endpoint
	nextCalled = false
	req = httptest.NewRequest(http.MethodPost, "/api/lsp/ws", nil)
	w = httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for /api/lsp/ws endpoint")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_WebSocketEndpointRequiresAuthForNonWebSocketPaths tests
// that paths containing WebSocket endpoints as substrings but not starting with /api/
// pass through with the new /api/-only auth logic.
func TestAuthTokenMiddleware_WebSocketEndpointRequiresAuthForNonWebSocketPaths(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	// Test /ws/api endpoint - should pass through since it doesn't start with /api/
	// (This changed from the old behavior where it would require auth)
	req := httptest.NewRequest(http.MethodPost, "/ws/api", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for /ws/api endpoint (non-API path)")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test /terminal/new endpoint - should pass through since it doesn't start with /api/
	// (This changed from the old behavior where it would require auth)
	nextCalled = false
	req = httptest.NewRequest(http.MethodPost, "/terminal/new", nil)
	w = httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for /terminal/new endpoint (non-API path)")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test that paths starting with /api/ still require auth (regardless of what else they contain)
	nextCalled = false
	req = httptest.NewRequest(http.MethodPost, "/api/create", nil)
	w = httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if nextCalled {
		t.Error("Expected next handler NOT to be called for /api/create endpoint (starts with /api/)")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_ErrorResponseBodyStructure tests that the 401 response
// body is valid JSON with the expected structure.
func TestAuthTokenMiddleware_ErrorResponseBodyStructure(t *testing.T) {
	token := "test-token-12345"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	// Check Content-Type header
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", ct)
	}

	// Check status code
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	// Parse and validate JSON body
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	// Check required fields
	if _, ok := resp["error"]; !ok {
		t.Error("Expected 'error' field in response")
	}
	if _, ok := resp["message"]; !ok {
		t.Error("Expected 'message' field in response")
	}

	// Check field values
	if resp["error"] != "unauthorized" {
		t.Errorf("Expected error='unauthorized', got '%s'", resp["error"])
	}
	if resp["message"] != "valid auth token required" {
		t.Errorf("Expected message='valid auth token required', got '%s'", resp["message"])
	}
}

// TestAuthTokenMiddleware_TokenCaseSensitivity tests that token comparison
// is case-sensitive.
func TestAuthTokenMiddleware_TokenCaseSensitivity(t *testing.T) {
	token := "MySecretToken"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	// Test with lowercase (should fail)
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer mysecrettoken")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if nextCalled {
		t.Error("Expected next handler NOT to be called with wrong case token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_TokenWithSpaces tests that tokens with spaces
// are handled correctly.
func TestAuthTokenMiddleware_TokenWithSpaces(t *testing.T) {
	token := "secret token with spaces"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called with correct token containing spaces")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// TestAuthTokenMiddleware_NonAPIPathsPassThrough tests that POST to non-API paths
// all pass through without auth even when token is configured.
func TestAuthTokenMiddleware_NonAPIPathsPassThrough(t *testing.T) {
	token := "test-token-12345"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	nonAPIPaths := []string{
		"/",
		"/health",
		"/static/bundle.js",
		"/assets/main.js",
		"/favicon.ico",
		"/ssh/session-key/api/query",
	}

	for _, path := range nonAPIPaths {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		w := httptest.NewRecorder()

		middleware.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 for POST to %s, got %d", path, w.Code)
		}
	}
}

// TestAuthTokenMiddleware_APIWriteEndpointsRequireAuth tests that POST to API
// endpoints are rejected without auth when token is configured.
func TestAuthTokenMiddleware_APIWriteEndpointsRequireAuth(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	apiWritePaths := []string{
		"/api/query",
		"/api/create",
		"/api/delete",
		"/api/update",
		"/api/execute",
	}

	for _, path := range apiWritePaths {
		nextCalled = false
		req := httptest.NewRequest(http.MethodPost, path, nil)
		w := httptest.NewRecorder()

		middleware.ServeHTTP(w, req)

		if nextCalled {
			t.Errorf("Expected next handler NOT to be called for POST to %s without auth", path)
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401 for POST to %s without auth, got %d", path, w.Code)
		}
	}
}

// TestAuthTokenMiddleware_APIReadEndpointsPassThrough tests that GET to API
// endpoints pass through even when token is configured.
func TestAuthTokenMiddleware_APIReadEndpointsPassThrough(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	apiReadPaths := []string{
		"/api/query/status",
		"/api/stats",
		"/api/health",
		"/api/data",
	}

	for _, path := range apiReadPaths {
		nextCalled = false
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		middleware.ServeHTTP(w, req)

		if !nextCalled {
			t.Errorf("Expected next handler to be called for GET to %s", path)
		}
		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 for GET to %s, got %d", path, w.Code)
		}
	}
}

// TestAuthTokenMiddleware_APILspWebSocketEndpointPassesThrough tests that the
// /api/lsp/ws WebSocket endpoint passes through even though it's under /api/.
func TestAuthTokenMiddleware_APILspWebSocketEndpointPassesThrough(t *testing.T) {
	token := "test-token-12345"
	nextCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := authTokenMiddleware(token)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/lsp/ws", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	if !nextCalled {
		t.Error("Expected next handler to be called for /api/lsp/ws endpoint")
	}
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}
