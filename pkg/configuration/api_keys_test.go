package configuration

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
)

// TestValidateAndSaveAPIKey_NewKey tests that a new API key can be validated and saved
func TestValidateAndSaveAPIKey_NewKey(t *testing.T) {
	// Skip if no API key is configured for test providers
	if !HasProviderAuth("test") {
		t.Skip("No test provider credential available, skipping test")
	}

	// Read the current keys to restore later
	keys, err := LoadAPIKeys()
	if err != nil {
		t.Fatalf("Failed to load API keys: %v", err)
	}

	// Save the old test key if it exists
	oldTestKey := ""
	if keys != nil {
		if val, ok := (*keys)["test"]; ok && val != "" {
			oldTestKey = val
		}
	}

	// Use a valid test key from environment or skip
	testKey := os.Getenv("TEST_API_KEY")
	if testKey == "" {
		t.Skip("TEST_API_KEY not set, skipping test")
	}

	// Validate and save the key
	modelCount, err := ValidateAndSaveAPIKey("test", testKey)
	if err != nil {
		t.Fatalf("Failed to validate and save test API key: %v", err)
	}

	if modelCount <= 0 {
		t.Errorf("Expected positive model count, got %d", modelCount)
	}

	// Restore the old key
	if oldTestKey != "" {
		_ = SaveAPIKeys(&APIKeys{"test": oldTestKey})
	}
}

// TestValidateAndSaveAPIKey_InvalidKey tests that an invalid API key is rejected
func TestValidateAndSaveAPIKey_InvalidKey(t *testing.T) {
	// Get the current key for restoration
	keys, err := LoadAPIKeys()
	if err != nil {
		t.Fatalf("Failed to load API keys: %v", err)
	}

	// Save the old test key if it exists
	oldTestKey := ""
	if keys != nil {
		if val, ok := (*keys)["test"]; ok && val != "" {
			oldTestKey = val
		}
	}

	// Use an obviously invalid key
	invalidKey := "invalid-key-that-does-not-work"

	// Try to validate and save the key - should fail
	_, err = ValidateAndSaveAPIKey("test", invalidKey)
	if err == nil {
		t.Error("Expected validation to fail for invalid key, but it succeeded")
	}

	// Restore the old key if it existed
	if oldTestKey != "" {
		err = SaveAPIKeys(&APIKeys{"test": oldTestKey})
		if err != nil {
			t.Errorf("Failed to restore old key: %v", err)
		}
	}
}

// TestValidateAndSaveAPIKey_NoOldKey tests that a new key can be saved when no old key exists
func TestValidateAndSaveAPIKey_NoOldKey(t *testing.T) {
	// Skip if no test provider credential exists
	if HasProviderAuth("test") {
		t.Skip("Test provider already has a credential, skipping test")
	}

	// Use a valid test key from environment
	testKey := os.Getenv("TEST_API_KEY")
	if testKey == "" {
		t.Skip("TEST_API_KEY not set, skipping test")
	}

	// Validate and save the key
	modelCount, err := ValidateAndSaveAPIKey("test", testKey)
	if err != nil {
		t.Fatalf("Failed to validate and save test API key: %v", err)
	}

	if modelCount <= 0 {
		t.Errorf("Expected positive model count, got %d", modelCount)
	}

	// Clean up - delete the test key
	_ = credentials.DeleteFromActiveBackend("test")
}

// TestValidateAndSaveAPIKey_UnsupportedProvider tests error for unknown provider
func TestValidateAndSaveAPIKey_UnsupportedProvider(t *testing.T) {
	// Unknown providers are accepted as ClientType, but validation will fail
	// since there's no real API to validate against
	_, err := ValidateAndSaveAPIKey("totally-fake-provider-xyz", "some-key")
	if err == nil {
		t.Fatal("Expected error for unsupported provider")
	}
	// The error should indicate validation failure (not "unsupported provider")
	// since ParseProviderName accepts any string
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("Expected 'validation failed' error, got: %v", err)
	}
}

// TestSanitizeValidationError tests error message sanitization
func TestSanitizeValidationError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"401 error", "status 401 unauthorized", "Invalid API key"},
		{"403 error", "forbidden", "Access forbidden"},
		{"429 error", "status 429 rate limit exceeded", "Rate limit exceeded"},
		{"500 error", "internal server error 500", "Service temporarily unavailable"},
		{"timeout", "request timed out deadline exceeded", "Request timed out"},
		{"network", "network dial tcp", "Network error"},
		{"generic", "some random error", "Validation failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := sanitizeValidationError(errors.New(tc.input))
			if !strings.Contains(result, tc.expected) {
				t.Errorf("sanitizeValidationError(%q) = %q, want substring %q", tc.input, result, tc.expected)
			}
		})
	}
}
