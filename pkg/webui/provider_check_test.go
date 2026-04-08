package webui

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

func TestIsProviderAvailable(t *testing.T) {
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

	tests := []struct {
		name              string
		provider          string
		expectedAvailable bool
	}{
		{
			name:              "editor mode should return false",
			provider:          "editor",
			expectedAvailable: false,
		},
		{
			name:              "empty provider should return true (auto-select)",
			provider:          "",
			expectedAvailable: true,
		},
		{
			name:              "real provider should return true",
			provider:          "openrouter",
			expectedAvailable: true,
		},
		{
			name:              "test provider should return true",
			provider:          "test",
			expectedAvailable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := configuration.Load()
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}
			cfg.LastUsedProvider = tt.provider
			if err := cfg.Save(); err != nil {
				t.Fatalf("Failed to save test config: %v", err)
			}

			available := isProviderAvailable()
			if available != tt.expectedAvailable {
				t.Errorf("isProviderAvailable() = %v, want %v", available, tt.expectedAvailable)
			}
		})
	}
}
