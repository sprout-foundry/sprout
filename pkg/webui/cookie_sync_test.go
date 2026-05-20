//go:build !js

// Package webui provides React web server with embedded assets

package webui

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCookieSyncMiddleware_SetsCookieFromHeader(t *testing.T) {
	mw := cookieSyncMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(webClientIDHeader, "test-client-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != clientIDCookieName {
		t.Errorf("cookie name = %q; want %q", c.Name, clientIDCookieName)
	}
	if c.Value != "test-client-123" {
		t.Errorf("cookie value = %q; want %q", c.Value, "test-client-123")
	}
	if c.Path != "/" {
		t.Errorf("cookie path = %q; want /", c.Path)
	}
	if c.HttpOnly {
		t.Error("cookie must NOT be HttpOnly — JS must be able to read it for same-origin recovery")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite = %v (same-origin, no Origin header); want %v", c.SameSite, http.SameSiteLaxMode)
	}
	if c.Secure {
		t.Error("cookie should NOT be Secure for same-origin requests")
	}

	// Verify X-Sprout-Client-ID response header is set (for cross-origin sync)
	respClientID := w.Result().Header.Get(clientIDResponseHeader)
	if respClientID != "test-client-123" {
		t.Errorf("X-Sprout-Client-ID header = %q; want %q", respClientID, "test-client-123")
	}
}

func TestCookieSyncMiddleware_SameSiteNoneForCrossOriginHTTPS(t *testing.T) {
	mw := cookieSyncMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://api.sprout.dev/api/stats", nil)
	req.TLS = &tls.ConnectionState{} // simulate HTTPS
	req.Header.Set(webClientIDHeader, "cross-origin-client")
	req.Header.Set("Origin", "https://pages.sprout.dev")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.SameSite != http.SameSiteNoneMode {
		t.Errorf("cookie SameSite = %v (cross-origin HTTPS); want %v", c.SameSite, http.SameSiteNoneMode)
	}
	if !c.Secure {
		t.Error("cookie must be Secure for cross-origin HTTPS (SameSite=None requires Secure)")
	}

	// Verify response header is set for cross-origin client sync
	respClientID := w.Result().Header.Get(clientIDResponseHeader)
	if respClientID != "cross-origin-client" {
		t.Errorf("X-Sprout-Client-ID header = %q; want %q", respClientID, "cross-origin-client")
	}
}

func TestCookieSyncMiddleware_SameSiteLaxForCrossOriginHTTP(t *testing.T) {
	// Cross-origin on HTTP (local dev) must NOT use SameSite=None since Secure
	// can't be set on HTTP — the cookie would be silently dropped.
	mw := cookieSyncMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://localhost:56000/api/stats", nil)
	req.Header.Set(webClientIDHeader, "local-dev-client")
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite = %v (cross-origin HTTP); want %v", c.SameSite, http.SameSiteLaxMode)
	}
	if c.Secure {
		t.Error("cookie must NOT be Secure for HTTP (would be silently dropped)")
	}
}

func TestCookieSyncMiddleware_SameSiteLaxForSameOrigin(t *testing.T) {
	mw := cookieSyncMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://api.sprout.dev/api/stats", nil)
	req.TLS = &tls.ConnectionState{} // simulate HTTPS
	req.Header.Set(webClientIDHeader, "same-origin-client")
	req.Header.Set("Origin", "https://api.sprout.dev")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite = %v (same-origin HTTPS); want %v", c.SameSite, http.SameSiteLaxMode)
	}
	if c.Secure {
		t.Error("cookie should NOT be Secure for same-origin requests")
	}
}

func TestCookieSyncMiddleware_NoHeaderUsesDefault(t *testing.T) {
	mw := cookieSyncMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Value != defaultWebClientID {
		t.Errorf("cookie value = %q; want %q", c.Value, defaultWebClientID)
	}

	// Response header should also carry the default
	respClientID := w.Result().Header.Get(clientIDResponseHeader)
	if respClientID != defaultWebClientID {
		t.Errorf("X-Sprout-Client-ID header = %q; want %q", respClientID, defaultWebClientID)
	}
}

func TestCookieSyncMiddleware_ReadsCookieAsFallback(t *testing.T) {
	mw := cookieSyncMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request with cookie but no header (simulates browser after page reload)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  clientIDCookieName,
		Value: "persisted-client-456",
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Value != "persisted-client-456" {
		t.Errorf("cookie value = %q; want %q (should persist from existing cookie)", c.Value, "persisted-client-456")
	}

	// Response header should echo the persisted client ID
	respClientID := w.Result().Header.Get(clientIDResponseHeader)
	if respClientID != "persisted-client-456" {
		t.Errorf("X-Sprout-Client-ID header = %q; want %q", respClientID, "persisted-client-456")
	}
}

func TestCookieSyncMiddleware_HeaderOverridesCookie(t *testing.T) {
	mw := cookieSyncMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request with both header and cookie — header wins
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(webClientIDHeader, "new-client-789")
	req.AddCookie(&http.Cookie{
		Name:  clientIDCookieName,
		Value: "old-client-123",
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Value != "new-client-789" {
		t.Errorf("cookie value = %q; want %q (header should override cookie)", c.Value, "new-client-789")
	}

	// Response header should echo the header value (header wins)
	respClientID := w.Result().Header.Get(clientIDResponseHeader)
	if respClientID != "new-client-789" {
		t.Errorf("X-Sprout-Client-ID header = %q; want %q", respClientID, "new-client-789")
	}
}

func TestCookieSyncMiddleware_SanitizesCookieValue(t *testing.T) {
	mw := cookieSyncMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Cookie value with path traversal — should be sanitized
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(webClientIDHeader, "../../etc/passwd")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	// sanitizeClientID removes ".." and "/", so "../../etc/passwd" becomes "etcpasswd"
	if c.Value == "../../etc/passwd" {
		t.Errorf("cookie value was NOT sanitized: got %q, expected sanitized value", c.Value)
	}
	if c.Value == "" {
		t.Error("cookie value should not be empty after sanitization")
	}

	// Response header should also be sanitized
	respClientID := w.Result().Header.Get(clientIDResponseHeader)
	if respClientID == "../../etc/passwd" {
		t.Errorf("X-Sprout-Client-ID header was NOT sanitized: got %q", respClientID)
	}
}

func TestIsCrossOriginRequest(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{"no origin", "", "localhost:3000", false},
		{"same origin", "https://api.sprout.dev", "api.sprout.dev", false},
		{"different origin", "https://pages.sprout.dev", "api.sprout.dev", true},
		{"with port", "https://pages.sprout.dev", "api.sprout.dev:443", true},
		{"localhost same", "http://localhost:3000", "localhost:3000", false},
		{"localhost different port", "http://localhost:3000", "localhost:56000", true},
		{"empty origin", "", "api.sprout.dev", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCrossOriginRequest(tt.origin, tt.host)
			if got != tt.want {
				t.Errorf("isCrossOriginRequest(%q, %q) = %v; want %v", tt.origin, tt.host, got, tt.want)
			}
		})
	}
}

func TestExtractHostFromOrigin(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com", "example.com"},
		{"https://example.com:443", "example.com:443"},
		{"http://localhost:3000", "localhost:3000"},
		{"", ""},
		{"not-an-origin", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractHostFromOrigin(tt.input)
			if got != tt.want {
				t.Errorf("extractHostFromOrigin(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
