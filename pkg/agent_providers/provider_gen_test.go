package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLMStudio_ProviderRequiresAPIKey verifies that LM Studio is correctly
// identified as a local provider that does NOT require an API key.
// Regression test for SP-022-4a: auth.type was changed from "bearer" to "none"
// in the LM Studio provider config, and this function must reflect that change.
func TestLMStudio_ProviderRequiresAPIKey(t *testing.T) {
	assert.False(t, ProviderRequiresAPIKey("lmstudio"),
		"lmstudio should not require an API key")

	// Verify case-insensitivity
	assert.False(t, ProviderRequiresAPIKey("LMStudio"),
		"LMStudio (mixed case) should not require an API key")
	assert.False(t, ProviderRequiresAPIKey(" LMSTUDIO "),
		"LMSTUDIO (with whitespace) should not require an API key")
}

// TestLMStudio_ProviderEnvVar verifies that LM Studio returns an empty env var
// name, consistent with it being a local provider that doesn't need credentials.
// Regression test for SP-022-4a: ProviderEnvVar previously returned
// "LMSTUDIO_API_KEY" but should now return "" since auth.type is "none".
func TestLMStudio_ProviderEnvVar(t *testing.T) {
	assert.Empty(t, ProviderEnvVar("lmstudio"),
		"lmstudio should return an empty env var name")

	// Verify case-insensitivity
	assert.Empty(t, ProviderEnvVar("LMStudio"),
		"LMStudio (mixed case) should return empty env var")
	assert.Empty(t, ProviderEnvVar("  lmstudio  "),
		"lmstudio (with whitespace) should return empty env var")
}

// TestLMStudio_Consistency verifies that ProviderRequiresAPIKey and ProviderEnvVar
// are consistent for lmstudio: if the provider doesn't require an API key,
// it should also have an empty env var name.
func TestLMStudio_Consistency(t *testing.T) {
	requiresKey := ProviderRequiresAPIKey("lmstudio")
	envVar := ProviderEnvVar("lmstudio")

	assert.False(t, requiresKey, "lmstudio should not require an API key")
	assert.Empty(t, envVar, "lmstudio should have an empty env var when it doesn't require a key")

	if requiresKey && envVar == "" {
		t.Error("inconsistent state: provider requires API key but has no env var name")
	}
}

// TestLMStudio_ClientType verifies that the lmstudio client type constant
// and conversion functions work correctly.
func TestLMStudio_ClientType(t *testing.T) {
	// Verify the constant value
	assert.Equal(t, "lmstudio", LmstudioClientType)

	// Verify StringToClientType
	ct, err := StringToClientType("lmstudio")
	assert.NoError(t, err)
	assert.Equal(t, "lmstudio", ct)

	// Verify case-insensitivity
	ct, err = StringToClientType("LMStudio")
	assert.NoError(t, err)
	assert.Equal(t, "lmstudio", ct)

	// Verify ClientTypeToString
	assert.Equal(t, "lmstudio", ClientTypeToString("lmstudio"))
}

// TestLMStudio_InAllProviderNames verifies that lmstudio appears in the
// list of all provider names.
func TestLMStudio_InAllProviderNames(t *testing.T) {
	names := AllProviderNames()
	found := false
	for _, name := range names {
		if name == "lmstudio" {
			found = true
			break
		}
	}
	assert.True(t, found, "lmstudio should be in AllProviderNames()")
}

// TestLMStudio_DisplayName verifies the display name mapping for lmstudio.
func TestLMStudio_DisplayName(t *testing.T) {
	displayNames := ProviderDisplayNames()
	assert.Equal(t, "LM Studio", displayNames["lmstudio"],
		"lmstudio display name should be 'LM Studio'")
}

// TestLMStudio_KnownProvider verifies that lmstudio is listed as a known provider.
func TestLMStudio_KnownProvider(t *testing.T) {
	known := KnownProviders()
	found := false
	for _, name := range known {
		if name == "lmstudio" {
			found = true
			break
		}
	}
	assert.True(t, found, "lmstudio should be a known provider")
}
