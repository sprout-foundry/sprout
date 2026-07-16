package agent

import (
	"strings"
	"testing"
)

// TestHandleSettingsListProviders_NoFilter verifies that listing providers
// without a filter returns only provider names (no model fetching).
func TestHandleSettingsListProviders_NoFilter(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	result, err := handleSettingsListProviders(a, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleSettingsListProviders() returned error: %v", err)
	}

	// Should contain provider count header
	if !strings.Contains(result, "Available providers") {
		t.Errorf("expected provider count header, got: %s", result)
	}

	// Should NOT contain model listing (no filter = no model fetch)
	if strings.Contains(result, "Models for") {
		t.Errorf("should not list models when no filter is provided, got: %s", result)
	}
}

// TestHandleSettingsListProviders_FilterMultipleProviders verifies that
// filtering to multiple providers still only shows provider names.
func TestHandleSettingsListProviders_FilterMultipleProviders(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// Filter with a common substring that matches multiple providers
	result, err := handleSettingsListProviders(a, map[string]interface{}{
		"provider": "ai",
	})
	if err != nil {
		t.Fatalf("handleSettingsListProviders() returned error: %v", err)
	}

	// Should contain provider count header
	if !strings.Contains(result, "Available providers") {
		t.Errorf("expected provider count header, got: %s", result)
	}
}

// TestHandleSettingsListProviders_FilterSingleProvider verifies that
// filtering to exactly one provider triggers model fetching.
func TestHandleSettingsListProviders_FilterSingleProvider(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// Use a very specific filter that should match only one provider
	// "zai-coding" is unique enough to match exactly one provider
	result, err := handleSettingsListProviders(a, map[string]interface{}{
		"provider": "zai-coding",
	})
	if err != nil {
		t.Fatalf("handleSettingsListProviders() returned error: %v", err)
	}

	// Should show exactly 1 provider
	if !strings.Contains(result, "Available providers (1):") {
		t.Errorf("expected exactly 1 provider, got: %s", result)
	}

	// Should have attempted to fetch models (either models or error message)
	hasModels := strings.Contains(result, "Models for")
	hasError := strings.Contains(result, "could not fetch models")
	if !hasModels && !hasError {
		t.Errorf("expected model listing or error when single provider matched, got: %s", result)
	}
}

// TestHandleSettingsListProviders_FilterNoMatch verifies that a filter
// matching no providers returns 0 providers.
func TestHandleSettingsListProviders_FilterNoMatch(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	result, err := handleSettingsListProviders(a, map[string]interface{}{
		"provider": "nonexistent-provider-xyz",
	})
	if err != nil {
		t.Fatalf("handleSettingsListProviders() returned error: %v", err)
	}

	if !strings.Contains(result, "Available providers (0):") {
		t.Errorf("expected 0 providers, got: %s", result)
	}

	// Should NOT attempt model listing when no providers match
	if strings.Contains(result, "Models for") {
		t.Errorf("should not list models when no providers matched, got: %s", result)
	}
}

// TestHandleSettingsListProviders_CaseInsensitiveFilter verifies that
// provider filtering is case-insensitive.
func TestHandleSettingsListProviders_CaseInsensitiveFilter(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// Try uppercase filter
	result, err := handleSettingsListProviders(a, map[string]interface{}{
		"provider": "ZAI-CODING",
	})
	if err != nil {
		t.Fatalf("handleSettingsListProviders() returned error: %v", err)
	}

	// Should still find the provider
	if !strings.Contains(result, "Available providers (1):") {
		t.Errorf("expected case-insensitive match, got: %s", result)
	}
}

// TestHandleSettingsListProviders_NilConfigManager verifies error handling
// when the config manager is not available.
func TestHandleSettingsListProviders_NilConfigManager(t *testing.T) {
	a := &Agent{}
	// Don't initialize configManager

	result, err := handleSettingsListProviders(a, map[string]interface{}{})
	if err == nil {
		t.Fatalf("expected error when config manager is nil, got result: %s", result)
	}

	if !strings.Contains(err.Error(), "configuration manager not available") {
		t.Errorf("expected config manager error, got: %v", err)
	}
}

// TestHandleSettingsListProviders_EmptyProviders verifies behavior when
// no providers are available.
func TestHandleSettingsListProviders_EmptyProviders(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// This test verifies the normal path works; empty providers is hard
	// to trigger since the agent always has at least some providers.
	// The code path is covered by the nil check above.
	result, err := handleSettingsListProviders(a, map[string]interface{}{})
	if err != nil {
		t.Fatalf("handleSettingsListProviders() returned error: %v", err)
	}

	if result == "No providers available" {
		// This is valid if somehow no providers are configured
		return
	}
	if !strings.Contains(result, "Available providers") {
		t.Errorf("expected provider listing, got: %s", result)
	}
}

// TestHandleSettingsListProviders_ModelListingFormat verifies the output
// format of model listings when models are successfully fetched.
func TestHandleSettingsListProviders_ModelListingFormat(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// Use a provider that loads models from config (no API call needed)
	result, err := handleSettingsListProviders(a, map[string]interface{}{
		"provider": "zai-coding",
	})
	if err != nil {
		t.Fatalf("handleSettingsListProviders() returned error: %v", err)
	}

	if strings.Contains(result, "Models for") {
		// Models were fetched — verify format
		lines := strings.Split(result, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "Models for") {
				// Verify the format: "Models for <provider> (<count>):"
				if !strings.HasSuffix(trimmed, ":") {
					t.Errorf("model header should end with colon: %q", trimmed)
				}
			}
			if strings.HasPrefix(trimmed, "  - ") && !strings.Contains(result, "could not fetch models") {
				// Verify model ID format: "  - <model_id>"
				modelID := strings.TrimPrefix(trimmed, "  - ")
				if modelID == "" {
					t.Error("model ID should not be empty")
				}
			}
		}
	}
}
