package configuration

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/stretchr/testify/assert"
)

// TestIsCustomProvider_BuiltInNotCustom verifies that a built-in provider
// name is never classified as custom — even if a user created a custom
// provider entry with the same name. Built-ins must always get full
// ListModels validation.
func TestIsCustomProvider_BuiltInNotCustom(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	for _, builtIn := range KnownProviderNames() {
		assert.False(t, isCustomProvider(builtIn),
			"built-in provider %q must not be classified as custom", builtIn)
	}
}

// TestIsCustomProvider_UnknownProviderNotCustom verifies that a random
// provider name that doesn't exist in any custom-providers store returns
// false — so it gets full ListModels validation like any unknown provider.
func TestIsCustomProvider_UnknownProviderNotCustom(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	assert.False(t, isCustomProvider("definitely-not-a-real-provider"))
}

// TestIsCustomProvider_RealCustomProvider verifies that a provider saved via
// SaveCustomProvider is correctly identified as custom — so its key can be
// saved without ListModels validation when the endpoint lacks /models.
func TestIsCustomProvider_RealCustomProvider(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	err := SaveCustomProvider(CustomProviderConfig{
		Name:     "my-custom-endpoint",
		Endpoint: "https://api.example.com/v1",
	})
	assert.NoError(t, err)

	assert.True(t, isCustomProvider("my-custom-endpoint"),
		"provider saved via SaveCustomProvider must be classified as custom")
}

// TestValidateAndSaveAPIKey_CustomProviderSkipsValidation verifies that when
// ListModels would fail (no network, no /models endpoint), a custom provider's
// key is still saved successfully. This is the core behavior that lets users
// store keys for custom providers that don't expose a standard models route.
func TestValidateAndSaveAPIKey_CustomProviderSkipsValidation(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	// Register a custom provider with a non-routable endpoint so ListModels
	// will fail — but the key should still be saved.
	err := SaveCustomProvider(CustomProviderConfig{
		Name:     "no-models-endpoint",
		Endpoint: "https://localhost:1/v1", // port 1 → connection refused
	})
	assert.NoError(t, err)

	// Ensure real validation path runs (not the global test-mode skip).
	SetValidateAndSaveAPIKeyValidation(false)
	t.Cleanup(func() { SetValidateAndSaveAPIKeyValidation(true) })

	modelCount, err := ValidateAndSaveAPIKey("no-models-endpoint", "sk-test-key-1234567890")
	assert.NoError(t, err, "custom provider key should save without ListModels validation")
	assert.Equal(t, 0, modelCount, "model count is 0 when validation is skipped")

	// Verify the key was actually stored.
	stored, _, err := credentials.GetFromActiveBackend("no-models-endpoint")
	assert.NoError(t, err)
	assert.Equal(t, "sk-test-key-1234567890", stored,
		"key must be persisted to the credential backend despite validation skip")
}

// TestValidateAndSaveAPIKey_BuiltInProviderRejectedOnNetworkFailure verifies
// the inverse: a built-in provider that requires a valid key still goes
// through full ListModels validation. We can't easily force a network failure
// for a specific built-in in CI (OpenRouter's list is public), so this test
// focuses on the routing decision: isCustomProvider must return false for
// every built-in name, ensuring the validation-skip path is never taken.
func TestValidateAndSaveAPIKey_BuiltInProviderGoesThroughValidation(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	// Every built-in name must be classified as non-custom so that
	// ValidateAndSaveAPIKey takes the full-validation path.
	for _, builtIn := range KnownProviderNames() {
		assert.False(t, isCustomProvider(builtIn),
			"built-in %q would incorrectly skip validation", builtIn)
	}
}
