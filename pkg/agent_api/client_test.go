package api

import (
	"os"
	"testing"
	"time"
)

func TestTimeoutConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{
			name:     "default timeout",
			envValue: "",
			expected: 120 * time.Second,
		},
		{
			name:     "duration string minutes",
			envValue: "10m",
			expected: 10 * time.Minute,
		},
		{
			name:     "duration string seconds",
			envValue: "120s",
			expected: 120 * time.Second,
		},
		{
			name:     "plain seconds",
			envValue: "600",
			expected: 600 * time.Second,
		},
		{
			name:     "complex duration",
			envValue: "1h30m",
			expected: 90 * time.Minute,
		},
		{
			name:     "invalid format falls back to default",
			envValue: "invalid",
			expected: 120 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.envValue != "" {
				os.Setenv("LEDIT_API_TIMEOUT", tt.envValue)
				defer os.Unsetenv("LEDIT_API_TIMEOUT")
			}

			// Create a test API key
			os.Setenv("DEEPINFRA_API_KEY", "test-key")
			defer os.Unsetenv("DEEPINFRA_API_KEY")

			client, err := NewClient()
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			// Check timeout
			if client.httpClient.Timeout != tt.expected {
				t.Errorf("Expected timeout %v, got %v", tt.expected, client.httpClient.Timeout)
			}
		})
	}
}
