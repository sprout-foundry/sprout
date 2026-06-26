//go:build !js

// Package webui tests for trusted user header extraction
package webui

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestTrustedUserHeaderParsing verifies that the trustedUserHeader field
// is correctly parsed from the SPROUT_TRUSTED_USER_HEADER environment variable.
func TestTrustedUserHeaderParsing(t *testing.T) {
	tests := []struct {
		name                string
		envValue            string
		serviceMode         bool
		expectedHeader      string
		expectedServiceMode bool
	}{
		{
			name:                "header set in service mode",
			envValue:            "X-User-ID",
			serviceMode:         true,
			expectedHeader:      "X-User-ID",
			expectedServiceMode: true,
		},
		{
			name:                "header not set",
			envValue:            "",
			serviceMode:         false,
			expectedHeader:      "",
			expectedServiceMode: false,
		},
		{
			name:                "header with whitespace trimmed",
			envValue:            "  X-User-ID  ",
			serviceMode:         true,
			expectedHeader:      "X-User-ID",
			expectedServiceMode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the environment variables
			t.Setenv("SPROUT_TRUSTED_USER_HEADER", tt.envValue)
			if tt.serviceMode {
				t.Setenv("SPROUT_SERVICE", "1")
			} else {
				t.Setenv("SPROUT_SERVICE", "")
			}

			// Create a minimal event bus for testing
			eventBus := events.NewEventBus()

			// Create the server which should parse the trusted user header
			server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
			if err != nil {
				t.Fatal(err)
			}

			// Verify the parsed values match expectations
			if server.trustedUserHeader != tt.expectedHeader {
				t.Errorf("Expected trustedUserHeader %q, got %q", tt.expectedHeader, server.trustedUserHeader)
			}
			if server.serviceMode != tt.expectedServiceMode {
				t.Errorf("Expected serviceMode %v, got %v", tt.expectedServiceMode, server.serviceMode)
			}
		})
	}
}

// TestExtractUserID verifies the ExtractUserID function correctly extracts
// the user ID from requests in service mode, and returns empty string in local mode.
func TestExtractUserID(t *testing.T) {
	tests := []struct {
		name           string
		serviceMode    bool
		headerName     string
		headerValue    string
		expectedUserID string
	}{
		{
			name:           "service mode with valid header",
			serviceMode:    true,
			headerName:     "X-User-ID",
			headerValue:    "user123",
			expectedUserID: "user123",
		},
		{
			name:           "service mode with missing header",
			serviceMode:    true,
			headerName:     "X-User-ID",
			headerValue:    "",
			expectedUserID: "",
		},
		{
			name:           "local mode ignores header (security)",
			serviceMode:    false,
			headerName:     "X-User-ID",
			headerValue:    "user123",
			expectedUserID: "", // Must be empty in local mode
		},
		{
			name:           "service mode without configured header",
			serviceMode:    true,
			headerName:     "",
			headerValue:    "user123",
			expectedUserID: "",
		},
		{
			name:           "service mode with empty header name",
			serviceMode:    true,
			headerName:     "",
			headerValue:    "",
			expectedUserID: "",
		},
		// Validation tests for Issue 3
		{
			name:           "header value with whitespace is trimmed and accepted",
			serviceMode:    true,
			headerName:     "X-User-ID",
			headerValue:    "  user123  ",
			expectedUserID: "user123",
		},
		{
			name:           "header value exceeding 256 chars is rejected",
			serviceMode:    true,
			headerName:     "X-User-ID",
			headerValue:    strings.Repeat("a", 257),
			expectedUserID: "",
		},
		{
			name:           "header value with special chars like semicolon is rejected",
			serviceMode:    true,
			headerName:     "X-User-ID",
			headerValue:    "user;123",
			expectedUserID: "",
		},
		{
			name:           "header value with script tag is rejected",
			serviceMode:    true,
			headerName:     "X-User-ID",
			headerValue:    "<script>alert('xss')</script>",
			expectedUserID: "",
		},
		{
			name:           "header value with valid special chars like @ is accepted",
			serviceMode:    true,
			headerName:     "X-User-ID",
			headerValue:    "user@example.com",
			expectedUserID: "user@example.com",
		},
		{
			name:           "header value with hyphens and dots is accepted",
			serviceMode:    true,
			headerName:     "X-User-ID",
			headerValue:    "user-123.sso",
			expectedUserID: "user-123.sso",
		},
		{
			name:           "header value with underscore and colon is accepted",
			serviceMode:    true,
			headerName:     "X-User-ID",
			headerValue:    "user_name:123",
			expectedUserID: "user_name:123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventBus := events.NewEventBus()
			server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
			if err != nil {
				t.Fatal(err)
			}
			server.serviceMode = tt.serviceMode
			server.trustedUserHeader = tt.headerName

			// Create a request with the header
			req, err := http.NewRequest("GET", "/ws", nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			if tt.headerValue != "" {
				req.Header.Set(tt.headerName, tt.headerValue)
			}

			// Extract user ID
			userID := server.ExtractUserID(req)

			if userID != tt.expectedUserID {
				t.Errorf("Expected userID %q, got %q", tt.expectedUserID, userID)
			}
		})
	}
}

// TestUserIDContextFunctions verifies the context functions work correctly.
func TestUserIDContextFunctions(t *testing.T) {
	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Test contextWithUserID and UserIDFromContext
	t.Run("context with user ID", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		req.Header.Set("X-User-ID", "test-user-123")
		server.serviceMode = true
		server.trustedUserHeader = "X-User-ID"

		ctx := server.contextWithUserID(req.Context(), req)
		userID := UserIDFromContext(ctx)

		if userID != "test-user-123" {
			t.Errorf("Expected userID %q, got %q", "test-user-123", userID)
		}
	})

	t.Run("context without user ID", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		ctx := server.contextWithUserID(req.Context(), req)
		userID := UserIDFromContext(ctx)

		if userID != "" {
			t.Errorf("Expected empty userID, got %q", userID)
		}
	})
}

// TestGetClientContextForRequestPopulatesUserID verifies that getClientContextForRequest
// correctly extracts the user ID from the request context and stores it on the client context.
func TestGetClientContextForRequestPopulatesUserID(t *testing.T) {
	eventBus := events.NewEventBus()
	server, err := NewReactWebServer(nil, eventBus, 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("UserID populated from context", func(t *testing.T) {
		// Create a request with user ID in context
		req, err := http.NewRequest("GET", "/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Set user ID in context
		ctx := context.WithValue(req.Context(), userIDContextKey, "test-user-123")
		req = req.WithContext(ctx)

		// Get client context
		clientCtx := server.getClientContextForRequest(req)

		// Assert UserID field is populated
		if clientCtx.UserID != "test-user-123" {
			t.Errorf("Expected clientCtx.UserID %q, got %q", "test-user-123", clientCtx.UserID)
		}
	})

	t.Run("UserID not overwritten on subsequent calls", func(t *testing.T) {
		// Create a request with a different user ID in context
		req, err := http.NewRequest("GET", "/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// First call sets UserID
		ctx := context.WithValue(req.Context(), userIDContextKey, "test-user-123")
		req = req.WithContext(ctx)
		clientCtx := server.getClientContextForRequest(req)

		// Second call with different user ID in context should not overwrite
		ctx2 := context.WithValue(req.Context(), userIDContextKey, "different-user")
		req2 := req.WithContext(ctx2)
		_ = server.getClientContextForRequest(req2)

		// UserID should remain the first value (same client context instance)
		if clientCtx.UserID != "test-user-123" {
			t.Errorf("Expected clientCtx.UserID %q to persist, got %q", "test-user-123", clientCtx.UserID)
		}
	})

	t.Run("UserID not set when empty in context", func(t *testing.T) {
		// Create a request without user ID in context
		req, err := http.NewRequest("GET", "/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}
		// Use a different client ID to avoid reusing cached context
		req.Header.Set("X-Sprout-Client-ID", "different-client")

		// Get client context
		clientCtx := server.getClientContextForRequest(req)

		// UserID should remain empty
		if clientCtx.UserID != "" {
			t.Errorf("Expected empty clientCtx.UserID, got %q", clientCtx.UserID)
		}
	})
}
