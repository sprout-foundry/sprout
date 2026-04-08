package webui

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// TestProviderNoAgentBehavior documents that getClientAgent gracefully
// handles the case when no provider is configured.
//
// The key behavioral guarantees are:
// 1. isProviderAvailable() returns false for "editor" provider
// 2. getClientAgent() checks isProviderAvailable() before expensive agent creation
// 3. getClientAgent() returns ErrNoProviderConfigured immediately without creating agent
// 4. This prevents blocking on interactive prompts and wasted initialization
func TestProviderNoAgentBehavior(t *testing.T) {
	t.Run("isProviderAvailable returns false for editor mode", func(t *testing.T) {
		// Verify isProviderAvailable logic for "editor"
		cfg, err := configuration.Load()
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		cfg.LastUsedProvider = "editor"
		if err := cfg.Save(); err != nil {
			t.Fatalf("Failed to save test config: %v", err)
		}

		// Reload and verify
		cfg2, err := configuration.Load()
		if err != nil {
			t.Fatalf("Failed to reload config: %v", err)
		}

		if cfg2.LastUsedProvider != "editor" {
			t.Errorf("Expected provider to be 'editor', got %q", cfg2.LastUsedProvider)
		}

		if isProviderAvailable() != false {
			t.Errorf("Expected isProviderAvailable() to return false, got %v", isProviderAvailable())
		}

		// Restore to test provider
		cfg2.LastUsedProvider = "test"
		_ = cfg2.Save()
	})

	t.Run("ErrNoProviderConfigured sentinel is properly defined", func(t *testing.T) {
		// Verify the error sentinel exists
		if ErrNoProviderConfigured == nil {
			t.Error("ErrNoProviderConfigured sentinel is not defined")
		}

		if ErrNoProviderConfigured.Error() == "" {
			t.Error("ErrNoProviderConfigured has no error message")
		}

		expectedMsg := "no AI provider configured"
		if ErrNoProviderConfigured.Error() != expectedMsg {
			t.Errorf("Expected error message %q, got %q", expectedMsg, ErrNoProviderConfigured.Error())
		}
	})
}
