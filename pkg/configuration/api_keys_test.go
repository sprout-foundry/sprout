package configuration

import (
	"os"
	"strings"
	"testing"
)

func TestPopulateFromEnvironment(t *testing.T) {
	// Get all known provider environment variables
	var allEnvVars []string
	for _, name := range knownProviderNames {
		metadata, err := GetProviderAuthMetadata(name)
		if err != nil || !metadata.RequiresAPIKey || metadata.EnvVar == "" {
			continue
		}
		allEnvVars = append(allEnvVars, metadata.EnvVar)
	}

	tests := []struct {
		name             string
		envVars          map[string]string
		expectedPopulated bool
		expectedKeys     []string
	}{
		{
			name: "single environment variable",
			envVars: map[string]string{"OPENAI_API_KEY": "sk-test123"},
			expectedPopulated: true,
			expectedKeys: []string{"openai"},
		},
		{
			name: "multiple environment variables",
			envVars: map[string]string{
				"OPENAI_API_KEY":    "sk-openai",
				"DEEPINFRA_API_KEY": "sk-deepinfra",
			},
			expectedPopulated: true,
			expectedKeys: []string{"openai", "deepinfra"},
		},
		{
			name: "no environment variables",
			envVars: map[string]string{},
			expectedPopulated: false,
			expectedKeys: []string{},
		},
		{
			name: "empty environment variable value",
			envVars: map[string]string{"OPENAI_API_KEY": ""},
			expectedPopulated: false,
			expectedKeys: []string{},
		},
		{
			name: "whitespace-only environment variable",
			envVars: map[string]string{"OPENAI_API_KEY": "   "},
			expectedPopulated: false,
			expectedKeys: []string{},
		},
		{
			name: "jinaai environment variable",
			envVars: map[string]string{"JINA_API_KEY": "test-jina-key-12345"},
			expectedPopulated: true,
			expectedKeys: []string{"jinaai"},
		},
		{
			name: "mixed valid and empty environment variables",
			envVars: map[string]string{
				"OPENAI_API_KEY":    "sk-openai",
				"DEEPINFRA_API_KEY": "",
				"JINA_API_KEY":      "test-jina-key",
			},
			expectedPopulated: true,
			expectedKeys: []string{"openai", "jinaai"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original environment for ALL known provider env vars
			originalEnv := make(map[string]string)
			for _, envVar := range allEnvVars {
				if v, exists := os.LookupEnv(envVar); exists {
					originalEnv[envVar] = v
				}
			}

			// Clear ALL known provider environment variables first
			for _, envVar := range allEnvVars {
				os.Unsetenv(envVar)
			}

			defer func() {
				// Restore original environment
				for k, v := range originalEnv {
					if v == "" {
						os.Unsetenv(k)
					} else {
						os.Setenv(k, v)
					}
				}
			}()

			// Set up test environment variables
			for k, v := range tt.envVars {
				if v == "" {
					os.Unsetenv(k)
				} else {
					os.Setenv(k, v)
				}
			}

			keys := make(APIKeys)
			result := keys.PopulateFromEnvironment()

			if result != tt.expectedPopulated {
				t.Errorf("PopulateFromEnvironment() = %v, want %v", result, tt.expectedPopulated)
			}

			for _, provider := range tt.expectedKeys {
				if key := keys.GetAPIKey(provider); key == "" {
					t.Errorf("Expected API key for %q not found", provider)
				}
			}

			// Verify unexpected providers don't have keys
			for provider, expectedValue := range tt.envVars {
				if expectedValue != "" && strings.TrimSpace(expectedValue) != "" {
					continue // Already checked above
				}
				// If we set an empty/whitespace value, verify no key was stored
				key := keys.GetAPIKey(provider)
				if key != "" {
					t.Errorf("Expected no API key for %q with empty value", provider)
				}
			}
		})
	}
}