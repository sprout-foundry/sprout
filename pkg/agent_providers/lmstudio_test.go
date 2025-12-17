package providers

import (
	"testing"
)

func TestLMStudioConnectionNoAuth(t *testing.T) {
	// Test that LM Studio provider can connect without API key for local instances
	testCases := []struct {
		name     string
		endpoint string
	}{
		{"127.0.0.1", "http://127.0.0.1:1234"},
		{"localhost", "http://localhost:1234"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &ProviderConfig{
				Name:     "lmstudio",
				Endpoint: tc.endpoint,
				Defaults: RequestDefaults{
					Model: "qwen3-coder:30b",
				},
				Models: ModelConfig{
					DefaultModel:       "qwen3-coder:30b",
					DefaultContextLimit: 4096, // Add required context limit
					AvailableModels:    []string{"qwen3-coder:30b"},
				},
				Auth: AuthConfig{
					Type:   "bearer",
					EnvVar: "", // Empty env var for local LM Studio
				},
			}

			provider, err := NewGenericProvider(config)
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			// This should not fail due to auth issues for local instances
			// It may fail due to connection issues if LM Studio is not running, but not due to auth
			err = provider.CheckConnection()
			if err != nil {
				// Check if it's an auth error - if so, the fix didn't work
				if contains(err.Error(), "authentication") || contains(err.Error(), "auth") || contains(err.Error(), "token") {
					t.Fatalf("Connection failed due to auth error (fix didn't work): %v", err)
				}
				// Otherwise it's a connection error (LM Studio not running), which is expected
				t.Logf("Connection failed (expected if LM Studio not running): %v", err)
			} else {
				t.Logf("✅ Connection successful - LM Studio is running and accessible")
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		 indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}