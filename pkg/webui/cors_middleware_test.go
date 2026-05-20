//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCorsMiddleware_NoOrigin(t *testing.T) {
	// When there's no Origin header, CORS headers must not be set
	// (same-origin request or non-browser client).
	handler := corsMiddleware("127.0.0.1", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	// No Origin header set
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin header, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Credentials header, got %q", got)
	}
	if code := w.Code; code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, code)
	}
}

func TestCorsMiddleware_Localhost(t *testing.T) {
	handler := corsMiddleware("127.0.0.1", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("expected Access-Control-Allow-Origin: http://localhost:5173, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials: true, got %q", got)
	}
}

func TestCorsMiddleware_AllowedOrigin(t *testing.T) {
	allowedOrigins := []string{"https://pages.sprout.dev", "https://app.sprout.dev"}
	handler := corsMiddleware("0.0.0.0", allowedOrigins)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "https://pages.sprout.dev")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://pages.sprout.dev" {
		t.Errorf("expected Access-Control-Allow-Origin: https://pages.sprout.dev, got %q", got)
	}
}

func TestCorsMiddleware_DeniedOrigin_WithAllowlist(t *testing.T) {
	allowedOrigins := []string{"https://pages.sprout.dev"}
	handler := corsMiddleware("0.0.0.0", allowedOrigins)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin header for denied origin, got %q", got)
	}
	// The request itself is still served (just without CORS headers)
	if code := w.Code; code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, code)
	}
}

func TestCorsMiddleware_OpenBinding_NoAllowlist(t *testing.T) {
	// When binding to 0.0.0.0 with no allowlist, accept any origin.
	handler := corsMiddleware("0.0.0.0", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "https://any-origin.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://any-origin.example.com" {
		t.Errorf("expected Access-Control-Allow-Origin: https://any-origin.example.com, got %q", got)
	}
}

func TestCorsMiddleware_LocalhostBinding_DeniesUnknown(t *testing.T) {
	// When binding to 127.0.0.1 (localhost only) with no allowlist,
	// only localhost/loopback origins are allowed.
	handler := corsMiddleware("127.0.0.1", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "https://external.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin header for unknown origin, got %q", got)
	}
}

func TestCorsMiddleware_OptionsPreflight(t *testing.T) {
	allowedOrigins := []string{"https://pages.sprout.dev"}
	handler := corsMiddleware("0.0.0.0", allowedOrigins)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// This should NOT be called for preflight — the middleware
			// should intercept and return 204 before reaching the handler.
			t.Error("handler should not be called for OPTIONS preflight")
		},
	))

	req := httptest.NewRequest("OPTIONS", "/api/query", nil)
	req.Header.Set("Origin", "https://pages.sprout.dev")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if code := w.Code; code != http.StatusNoContent {
		t.Errorf("expected status %d for OPTIONS preflight, got %d", http.StatusNoContent, code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://pages.sprout.dev" {
		t.Errorf("expected Access-Control-Allow-Origin: https://pages.sprout.dev, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials: true, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("expected Access-Control-Allow-Methods header to be set for preflight")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("expected Access-Control-Allow-Headers header to be set for preflight")
	}
}

func TestCorsMiddleware_AllowedHeaders(t *testing.T) {
	handler := corsMiddleware("0.0.0.0", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("POST", "/api/query", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	expected := "Accept, Authorization, Content-Type, Content-Length, X-Sprout-Client-ID"
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != expected {
		t.Errorf("expected Access-Control-Allow-Headers: %s, got %q", expected, got)
	}
}

func TestCorsMiddleware_AllowedMethods(t *testing.T) {
	handler := corsMiddleware("0.0.0.0", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("PUT", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	expected := "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS"
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != expected {
		t.Errorf("expected Access-Control-Allow-Methods: %s, got %q", expected, got)
	}
}

func TestCorsMiddleware_LoopbackIP(t *testing.T) {
	handler := corsMiddleware("127.0.0.1", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:8080" {
		t.Errorf("expected Access-Control-Allow-Origin: http://127.0.0.1:8080, got %q", got)
	}
}

func TestCorsMiddleware_Ipv6Loopback(t *testing.T) {
	handler := corsMiddleware("127.0.0.1", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://[::1]:8080")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://[::1]:8080" {
		t.Errorf("expected Access-Control-Allow-Origin: http://[::1]:8080, got %q", got)
	}
}

func TestCorsMiddleware_PortNormalization(t *testing.T) {
	// When allowed origins are normalized at startup, port 443 for HTTPS
	// and port 80 for HTTP are stripped. The middleware should still match.
	allowedOrigins := []string{"https://pages.sprout.dev"}
	handler := corsMiddleware("0.0.0.0", allowedOrigins)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	// Origin includes explicit port 443 — should still match the normalized entry
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "https://pages.sprout.dev:443")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://pages.sprout.dev:443" {
		t.Errorf("expected Access-Control-Allow-Origin: https://pages.sprout.dev:443, got %q", got)
	}
}

func TestCorsMiddleware_MalformedOrigin(t *testing.T) {
	handler := corsMiddleware("0.0.0.0", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "not-a-valid-url-!!!")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin for malformed origin, got %q", got)
	}
}

func TestCorsMiddleware_Iv6Binding_Open(t *testing.T) {
	// When binding to :: (IPv6 all interfaces) with no allowlist, accept any origin.
	handler := corsMiddleware("::", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "https://any-origin.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://any-origin.example.com" {
		t.Errorf("expected Access-Control-Allow-Origin: https://any-origin.example.com, got %q", got)
	}
}

func TestCorsMiddleware_ExposeHeaders(t *testing.T) {
	handler := corsMiddleware("0.0.0.0", nil)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "https://pages.sprout.dev")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	exposed := w.Header().Get("Access-Control-Expose-Headers")
	if exposed == "" {
		t.Error("expected Access-Control-Expose-Headers header to be set for allowed origin")
	}
	// Verify it does NOT include Set-Cookie (browser handles that via credentials: 'include')
	if strings.Contains(exposed, "Set-Cookie") {
		t.Error("Access-Control-Expose-Headers should NOT include Set-Cookie")
	}
	// Verify it includes X-Sprout-Client-ID
	if !strings.Contains(exposed, "X-Sprout-Client-ID") {
		t.Error("Access-Control-Expose-Headers should include X-Sprout-Client-ID")
	}
}
