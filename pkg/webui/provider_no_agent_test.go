//go:build !js

package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestProviderNoAgentBehavior documents that getClientAgent gracefully
// handles the case when no provider is configured.
//
// The key behavioral guarantees are:
//  1. isProviderAvailable() returns false for "editor" provider
//  2. getClientAgent() checks isProviderAvailable() before expensive agent creation
//  3. getClientAgent() returns ErrNoProviderConfigured immediately without creating agent
//  4. This prevents blocking on interactive prompts and wasted initialization
func TestProviderNoAgentBehavior(t *testing.T) {
	t.Run("isProviderAvailable returns false for editor mode", func(t *testing.T) {
		// Hermetic config: temp dir + scoped SPROUT_CONFIG so the
		// mutations below never touch the user's real config file.
		// Previously this test loaded the real config, set
		// LastUsedProvider="editor", then "restored" it to "test" —
		// which silently poisoned the runtime so /commit picked the
		// mock provider on next CLI invocation.
		mgr, cleanup := configuration.NewTestManager(t)
		defer cleanup()

		if err := mgr.UpdateConfig(func(c *configuration.Config) error {
			c.LastUsedProvider = "editor"
			return nil
		}); err != nil {
			t.Fatalf("update test config: %v", err)
		}

		if mgr.GetConfig().LastUsedProvider != "editor" {
			t.Errorf("expected provider 'editor', got %q", mgr.GetConfig().LastUsedProvider)
		}

		if isProviderAvailable() != false {
			t.Errorf("Expected isProviderAvailable() to return false, got %v", isProviderAvailable())
		}
	})

	t.Run("ErrNoProviderConfigured sentinel is properly defined", func(t *testing.T) {
		// Verify the error sentinel exists
		if ErrNoProviderConfigured == nil {
			t.Error("ErrNoProviderConfigured sentinel is not defined")
		}

		if ErrNoProviderConfigured.Error() == "" {
			t.Error("ErrNoProviderConfigured has no error message")
		}
	})
}
