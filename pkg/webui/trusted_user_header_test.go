// Package webui tests for trusted user header extraction
package webui

import (
	"net/http"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestTrustedUserHeaderParsing verifies that the trustedUserHeader field
// is correctly parsed from the SPROUT_TRUSTED_USER_HEADER environment variable.
func TestTrustedUserHeaderParsing(t *testing.T) {
	tests := []struct {
		name               string
		envValue           string
		serviceMode        bool
		expectedHeader     string
		expectedServiceMode bool
	}{
		{
			name:               "header set in service mode",
			envValue:           "X-User-ID",
			serviceMode:        true,
			expectedHeader:     "X-User-ID",
			expectedServiceMode: true,
		},
		{
			name:               "header not set",
			envValue:           "",
			serviceMode:        false,
			expectedHeader:     "",
			expectedServiceMode: false,
		},
		{
			name:               "header with whitespace trimmed",
			envValue:           "  X-User-ID  ",
			serviceMode:        true,
			expectedHeader:     "X-User-ID",
			expectedServiceMode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the environment variables
			os.Setenv("SPROUT_TRUSTED_USER_HEADER", tt.envValue)
			if tt.serviceMode {
				os.Setenv("SPROUT_SERVICE", "1")
			} else {
				os.Unsetenv("SPROUT_SERVICE")
			}
			defer func() {
				os.Unsetenv("SPROUT_TRUSTED_USER_HEADER")
				os.Unsetenv("SPROUT_SERVICE")
			}()

			// Create a minimal event bus for testing
			eventBus := events.NewEventBus()

			// Create the server which should parse the trusted user header
			server := NewReactWebServer(nil, eventBus, 0, "127.0.0.1")

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
		name          string
		serviceMode   bool
		headerName    string
		headerValue   string
		expectedUserID string
	}{
		{
			name:          "service mode with valid header",
			serviceMode:   true,
			headerName:    "X-User-ID",
			headerValue:   "user123",
			expectedUserID: "user123",
		},
		{
			name:          "service mode with missing header",
			serviceMode:   true,
			headerName:    "X-User-ID",
			headerValue:   "",
			expectedUserID: "",
		},
		{
			name:          "local mode ignores header (security)",
			serviceMode:   false,
			headerName:    "X-User-ID",
			headerValue:   "user123",
			expectedUserID: "", // Must be empty in local mode
		},
		{
			name:          "service mode without configured header",
			serviceMode:   true,
			headerName:    "",
			headerValue:   "user123",
			expectedUserID: "",
		},
		{
			name:          "service mode with empty header name",
			serviceMode:   true,
			headerName:    "",
			headerValue:   "",
			expectedUserID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventBus := events.NewEventBus()
			server := NewReactWebServer(nil, eventBus, 0, "127.0.0.1")
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
	server := NewReactWebServer(nil, eventBus, 0, "127.0.0.1")

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
