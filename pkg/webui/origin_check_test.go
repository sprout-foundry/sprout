//go:build !js

// Package webui tests for origin checking middleware
package webui

import (
	"net/http"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestAllowedOriginsParsing verifies that the allowedOrigins field
// is correctly parsed from the SPROUT_ALLOWED_ORIGINS environment variable.
func TestAllowedOriginsParsing(t *testing.T) {
	tests := []struct {
		name            string
		envValue        string
		expectedOrigins []string
	}{
		{
			name:            "single origin",
			envValue:        "https://example.com",
			expectedOrigins: []string{"https://example.com"},
		},
		{
			name:            "multiple origins comma-separated",
			envValue:        "https://example.com,https://test.com",
			expectedOrigins: []string{"https://example.com", "https://test.com"},
		},
		{
			name:            "multiple origins with spaces",
			envValue:        "https://example.com, https://test.com , https://another.com",
			expectedOrigins: []string{"https://example.com", "https://test.com", "https://another.com"},
		},
		{
			name:            "origins with ports",
			envValue:        "https://example.com:3000,http://localhost:8080",
			expectedOrigins: []string{"https://example.com:3000", "http://localhost:8080"},
		},
		{
			name:            "empty string",
			envValue:        "",
			expectedOrigins: []string{},
		},
		{
			name:            "only whitespace",
			envValue:        "   ",
			expectedOrigins: []string{},
		},
		{
			name:            "mixed case origins",
			envValue:        "HTTPS://Example.com,http://LOCALHOST:8080",
			expectedOrigins: []string{"https://example.com", "http://localhost:8080"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the environment variable
			t.Setenv("SPROUT_ALLOWED_ORIGINS", tt.envValue)

			// Create a minimal event bus for testing
			eventBus := events.NewEventBus()

			// Create the server which should parse the allowedOrigins
			server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
			if err != nil {
				t.Fatal(err)
			}

			// Verify the parsed origins match expectations
			if len(server.normalizedAllowedOrigins) != len(tt.expectedOrigins) {
				t.Errorf("Expected %d origins, got %d", len(tt.expectedOrigins), len(server.normalizedAllowedOrigins))
			}

			for i, expected := range tt.expectedOrigins {
				if i < len(server.normalizedAllowedOrigins) {
					if server.normalizedAllowedOrigins[i] != expected {
						t.Errorf("Origin %d: expected %q, got %q", i, expected, server.normalizedAllowedOrigins[i])
					}
				}
			}
		})
	}
}

// TestCheckOrigin_AllowedOrigins verifies the CheckOrigin function correctly
// validates origins against the allowedOrigins list.
func TestCheckOrigin_AllowedOrigins(t *testing.T) {
	// Set up allowed origins
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "https://example.com,https://test.com:3000,http://app.internal")

	// Create server with localhost binding (not 0.0.0.0)
	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "empty origin header - should allow",
			originHeader: "",
			shouldAllow:  true,
		},
		{
			name:         "localhost - should allow",
			originHeader: "http://localhost:3000",
			shouldAllow:  true,
		},
		{
			name:         "127.0.0.1 - should allow",
			originHeader: "http://127.0.0.1:8080",
			shouldAllow:  true,
		},
		{
			name:         "origin in allowlist - should allow",
			originHeader: "https://example.com",
			shouldAllow:  true,
		},
		{
			name:         "origin in allowlist with port - should allow",
			originHeader: "https://test.com:3000",
			shouldAllow:  true,
		},
		{
			name:         "origin in allowlist with http - should allow",
			originHeader: "http://app.internal",
			shouldAllow:  true,
		},
		{
			name:         "origin in allowlist different case - should allow",
			originHeader: "HTTPS://EXAMPLE.COM",
			shouldAllow:  true,
		},
		{
			name:         "origin not in allowlist - should reject",
			originHeader: "https://malicious.com",
			shouldAllow:  false,
		},
		{
			name:         "default port 443 matches base https origin",
			originHeader: "https://example.com:443",
			shouldAllow:  true,
		},
		{
			name:         "non-default port not in allowlist - should reject",
			originHeader: "https://example.com:8443",
			shouldAllow:  false,
		},
		{
			name:         "similar but different domain - should reject",
			originHeader: "https://example.com.evil.com",
			shouldAllow:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a request with the origin header
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			// Call CheckOrigin
			allowed := server.upgrader.CheckOrigin(req)

			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}

// TestCheckOrigin_EmptyAllowedOrigins verifies that when no allowed origins
// are configured, the existing behavior is preserved.
func TestCheckOrigin_EmptyAllowedOrigins(t *testing.T) {
	// Ensure no allowed origins env var is set
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "")

	// Test with localhost binding
	eventBus := events.NewEventBus()
	serverLocalhost, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "empty origin header",
			originHeader: "",
			shouldAllow:  true,
		},
		{
			name:         "localhost",
			originHeader: "http://localhost:3000",
			shouldAllow:  true,
		},
		{
			name:         "127.0.0.1",
			originHeader: "http://127.0.0.1:8080",
			shouldAllow:  true,
		},
		{
			name:         "non-localhost domain",
			originHeader: "https://example.com",
			shouldAllow:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" (localhost binding)", func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := serverLocalhost.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}

	// Test with 0.0.0.0 binding - should allow any origin
	t.Setenv("SPROUT_AUTH_TOKEN", "test-token-for-origin-check")
	serverAllInterfaces, err := NewReactWebServer(nil, eventBus, 0, "0.0.0.0", "", "")
	if err != nil {
		t.Fatal(err)
	}

	allInterfaceTests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "localhost with 0.0.0.0 binding",
			originHeader: "http://localhost:3000",
			shouldAllow:  true,
		},
		{
			name:         "arbitrary domain with 0.0.0.0 binding",
			originHeader: "https://any-domain.com",
			shouldAllow:  true,
		},
		{
			name:         "another arbitrary domain with 0.0.0.0 binding",
			originHeader: "https://cloud-service.app",
			shouldAllow:  true,
		},
	}

	for _, tt := range allInterfaceTests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := serverAllInterfaces.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}

// TestCheckOrigin_AllowedOriginsWithZeroZeroZeroZero verifies that when
// allowed origins are set AND the server is bound to 0.0.0.0, any origin
// is still allowed (0.0.0.0 takes precedence for backwards compatibility).
func TestCheckOrigin_AllowedOriginsWithZeroZeroZeroZero(t *testing.T) {
	// Set allowed origins
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "https://example.com")
	t.Setenv("SPROUT_AUTH_TOKEN", "test-token-for-origin-check")

	// Create server with 0.0.0.0 binding
	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "0.0.0.0", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "origin in allowlist",
			originHeader: "https://example.com",
			shouldAllow:  true,
		},
		{
			name:         "origin NOT in allowlist but 0.0.0.0 binding",
			originHeader: "https://not-in-allowlist.com",
			shouldAllow:  true, // 0.0.0.0 allows any origin
		},
		{
			name:         "localhost with 0.0.0.0 binding",
			originHeader: "http://localhost:3000",
			shouldAllow:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := server.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}

// TestCheckOrigin_IPV6Binding verifies origin checking with IPv6 binding (::).
func TestCheckOrigin_IPV6Binding(t *testing.T) {
	// Ensure no allowed origins env var is set
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "")
	t.Setenv("SPROUT_AUTH_TOKEN", "test-token-for-origin-check")

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "::", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "localhost with IPv6 binding",
			originHeader: "http://localhost:3000",
			shouldAllow:  true,
		},
		{
			name:         "arbitrary domain with IPv6 binding",
			originHeader: "https://any-domain.com",
			shouldAllow:  true, // :: allows any origin like 0.0.0.0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := server.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}

// TestCheckOrigin_InvalidOrigin verifies graceful handling of malformed origins.
func TestCheckOrigin_InvalidOrigin(t *testing.T) {
	// Set allowed origins
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "https://example.com")

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "malformed URL",
			originHeader: "not-a-url",
			shouldAllow:  false,
		},
		{
			name:         "missing scheme",
			originHeader: "example.com",
			shouldAllow:  false,
		},
		{
			name:         "origin with invalid characters",
			originHeader: "https://ex ample.com",
			shouldAllow:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := server.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}

// TestCheckOrigin_CaseInsensitive verifies that origin comparison is case-insensitive.
func TestCheckOrigin_CaseInsensitive(t *testing.T) {
	// Set allowed origins with mixed case
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "HTTPS://Example.COM:3000,http://LOCALHOST:8080")

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "exact match",
			originHeader: "HTTPS://Example.COM:3000",
			shouldAllow:  true,
		},
		{
			name:         "lowercase",
			originHeader: "https://example.com:3000",
			shouldAllow:  true,
		},
		{
			name:         "uppercase",
			originHeader: "HTTPS://EXAMPLE.COM:3000",
			shouldAllow:  true,
		},
		{
			name:         "mixed case",
			originHeader: "HtTpS://ExAmPlE.cOm:3000",
			shouldAllow:  true,
		},
		{
			name:         "scheme lowercase",
			originHeader: "http://localhost:8080",
			shouldAllow:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := server.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}

// TestCheckOrigin_IPv6Localhost verifies that IPv6 localhost (::1) is
// accepted as a local connection, just like 127.0.0.1.
func TestCheckOrigin_IPv6Localhost(t *testing.T) {
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "")

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "IPv6 localhost ::1",
			originHeader: "http://[::1]:3000",
			shouldAllow:  true,
		},
		{
			name:         "IPv6 localhost without port",
			originHeader: "http://[::1]",
			shouldAllow:  true,
		},
		{
			name:         "IPv4 127.0.0.1 still works",
			originHeader: "http://127.0.0.1:8080",
			shouldAllow:  true,
		},
		{
			name:         "localhost hostname still works",
			originHeader: "http://localhost:3000",
			shouldAllow:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := server.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}

// TestCheckOrigin_DefaultPortNormalization verifies that default ports
// (80 for HTTP, 443 for HTTPS) are normalized during comparison so that
// configuring "https://example.com:443" still matches a browser sending
// "Origin: https://example.com" (browsers strip default ports).
func TestCheckOrigin_DefaultPortNormalization(t *testing.T) {
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "https://example.com,http://test.com")

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "exact match https no port",
			originHeader: "https://example.com",
			shouldAllow:  true,
		},
		{
			name:         "browser sends :443 but config has no port",
			originHeader: "https://example.com:443",
			shouldAllow:  true,
		},
		{
			name:         "exact match http no port",
			originHeader: "http://test.com",
			shouldAllow:  true,
		},
		{
			name:         "browser sends :80 but config has no port",
			originHeader: "http://test.com:80",
			shouldAllow:  true,
		},
		{
			name:         "non-default port must match exactly",
			originHeader: "https://example.com:8443",
			shouldAllow:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := server.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}

// TestCheckOrigin_DefaultPortNormalization_ConfigWithPort verifies the
// reverse: configuring with default port matches browser without port.
func TestCheckOrigin_DefaultPortNormalization_ConfigWithPort(t *testing.T) {
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "https://example.com:443,http://test.com:80")

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "config has :443, browser sends no port",
			originHeader: "https://example.com",
			shouldAllow:  true,
		},
		{
			name:         "config has :443, browser also sends :443",
			originHeader: "https://example.com:443",
			shouldAllow:  true,
		},
		{
			name:         "config has :80, browser sends no port",
			originHeader: "http://test.com",
			shouldAllow:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := server.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}

// TestCheckOrigin_TrailingSlash verifies that trailing slashes in
// the config are handled correctly. Browser Origin headers never
// include trailing slashes, so a config with a trailing slash
// should still match.
func TestCheckOrigin_TrailingSlash(t *testing.T) {
	t.Setenv("SPROUT_ALLOWED_ORIGINS", "https://example.com/,https://test.com")

	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		originHeader string
		shouldAllow  bool
	}{
		{
			name:         "config has trailing slash, browser sends no slash",
			originHeader: "https://example.com",
			shouldAllow:  true,
		},
		{
			name:         "config no trailing slash, browser no trailing slash",
			originHeader: "https://test.com",
			shouldAllow:  true,
		},
		{
			name:         "unknown domain rejected",
			originHeader: "https://unknown.com",
			shouldAllow:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}

			allowed := server.upgrader.CheckOrigin(req)
			if allowed != tt.shouldAllow {
				t.Errorf("CheckOrigin(%q): expected %v, got %v", tt.originHeader, tt.shouldAllow, allowed)
			}
		})
	}
}
