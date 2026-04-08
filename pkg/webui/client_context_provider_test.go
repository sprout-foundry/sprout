package webui

import (
	"errors"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/events"
)

func TestIsProviderConfigError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "ErrNoProviderConfigured returns true",
			err:      ErrNoProviderConfigured,
			expected: true,
		},
		{
			name:     "nil error returns false",
			err:      nil,
			expected: false,
		},
		{
			name:     "provider recovery failed returns true",
			err:      testError("provider recovery failed"),
			expected: true,
		},
		{
			name:     "failed to initialize provider returns true",
			err:      testError("failed to initialize provider"),
			expected: true,
		},
		{
			name:     "provider_not_configured returns true",
			err:      testError("provider_not_configured"),
			expected: true,
		},
		{
			name:     "other errors return false",
			err:      testError("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isProviderConfigError(tt.err)
			if result != tt.expected {
				t.Errorf("isProviderConfigError() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// testError is a simple error for testing
type testError string

func (e testError) Error() string {
	return string(e)
}

func TestGetClientAgentReturnsNoProviderError(t *testing.T) {
	// Save original config to restore later
	originalCfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("Failed to load original config: %v", err)
	}
	defer func() {
		if err := originalCfg.Save(); err != nil {
			t.Logf("Failed to restore original config: %v", err)
		}
	}()

	t.Run("returns ErrNoProviderConfigured when provider is editor", func(t *testing.T) {
		cfg, err := configuration.Load()
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		cfg.LastUsedProvider = "editor"
		if err := cfg.Save(); err != nil {
			t.Fatalf("Failed to save test config: %v", err)
		}

		// Create a test server
		ws := NewReactWebServer(nil, events.NewEventBus(), 0)

		// Try to get a client agent - it should fail with ErrNoProviderConfigured
		agentInst, err := ws.getClientAgent("test-client")
		if err == nil {
			t.Error("Expected error when provider is 'editor', got nil")
		}
		if !errors.Is(err, ErrNoProviderConfigured) && !isProviderConfigError(err) {
			t.Errorf("Expected ErrNoProviderConfigured or provider config error, got: %v", err)
		}
		if agentInst != nil {
			t.Error("Expected agent to be nil when provider is 'editor'")
		}
	})
}
