package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEnv_PrefersSproutOverLedit(t *testing.T) {
	t.Setenv("SPROUT_TESTENV_X", "sprout-val")
	t.Setenv("LEDIT_TESTENV_X", "ledit-val")

	result := GetEnv("SPROUT_TESTENV_X", "LEDIT_TESTENV_X")
	assert.Equal(t, "sprout-val", result)
}

func TestGetEnv_FallsBackToLedit(t *testing.T) {
	// Only set LEDIT, not SPROUT
	t.Setenv("LEDIT_TESTENV_Y", "ledit-fallback")

	result := GetEnv("SPROUT_TESTENV_Y", "LEDIT_TESTENV_Y")
	assert.Equal(t, "ledit-fallback", result)
}

func TestGetEnvSimple_SproutFirst(t *testing.T) {
	t.Setenv("SPROUT_TESTSIMPLE_A", "sprout")
	t.Setenv("LEDIT_TESTSIMPLE_A", "ledit")

	result := GetEnvSimple("TESTSIMPLE_A")
	assert.Equal(t, "sprout", result)
}

func TestGetEnvSimple_LeditFallback(t *testing.T) {
	// Only set LEDIT
	t.Setenv("LEDIT_TESTSIMPLE_B", "ledit-val")

	result := GetEnvSimple("TESTSIMPLE_B")
	assert.Equal(t, "ledit-val", result)
}

func TestSetEnv_SetsBoth(t *testing.T) {
	suffix := "TESTSETVAR_" + t.Name()
	err := SetEnv(suffix, "myvalue")
	require.NoError(t, err)

	assert.Equal(t, "myvalue", GetEnvSimple(suffix))
}

func TestLookupEnv_Found(t *testing.T) {
	t.Setenv("SPROUT_TESTLOOKUP_FOUND", "found")

	val, ok := LookupEnv("TESTLOOKUP_FOUND")
	assert.True(t, ok)
	assert.Equal(t, "found", val)
}

func TestUnsetEnv_RemovesBoth(t *testing.T) {
	suffix := "TESTUNSET_" + t.Name()
	t.Setenv("SPROUT_"+suffix, "a")
	t.Setenv("LEDIT_"+suffix, "b")

	UnsetEnv(suffix)

	_, ok := LookupEnv(suffix)
	assert.False(t, ok)
}

func TestInitialize_DebugPrintConfig(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	config := NewConfig()
	config.LastUsedProvider = "openrouter"
	config.Version = "2.0"

	apiKeys, err := LoadAPIKeys()
	require.NoError(t, err)

	// This just prints to stdout - verify it doesn't panic
	assert.NotPanics(t, func() {
		DebugPrintConfig(config, apiKeys)
	})
}

func TestInitialize_GetAvailableProviders(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	providers := GetAvailableProviders()
	assert.NotEmpty(t, providers, "should have at least some providers")

	// Should include known built-in providers
	providerSet := make(map[string]bool)
	for _, p := range providers {
		providerSet[p] = true
	}
	assert.True(t, providerSet["ollama-local"], "should include ollama-local")
	assert.True(t, providerSet["editor"], "should include editor")

	// The "test" client type is an in-process mock sentinel (api.TestClientType).
	// It must NOT appear in GetAvailableProviders — if it reaches disk as
	// LastUsedProvider, the next session silently routes to a no-op mock.
	assert.False(t, providerSet["test"], "test must not be a selectable provider")
}

func TestInitialize_LoadOrInitConfig_SkipPrompt(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CONFIG", t.TempDir())

	// With skipPrompt=true, should return default config if none exists
	config, err := LoadOrInitConfig(true)
	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Equal(t, "2.0", config.Version)
}

func TestInitialize_ValidateProviderSetup_Editor(t *testing.T) {
	err := validateProviderSetup("editor")
	assert.NoError(t, err, "editor mode should always validate")
}

func TestInitialize_ValidateProviderSetup_Empty(t *testing.T) {
	err := validateProviderSetup("")
	assert.Error(t, err, "empty provider should fail validation")
}

func TestInitialize_ValidateProviderSetup_Test(t *testing.T) {
	t.Setenv("CI", "1")
	defer t.Setenv("CI", "")

	err := validateProviderSetup("test")
	assert.NoError(t, err, "test provider should validate")
}

func TestInitialize_ShowWelcomeMessage(t *testing.T) {
	assert.NotPanics(t, func() {
		ShowWelcomeMessage()
	})
}

func TestInitialize_ShowNextSteps(t *testing.T) {
	assert.NotPanics(t, func() {
		ShowNextSteps("editor", "/tmp/test-config")
	})
	assert.NotPanics(t, func() {
		ShowNextSteps("openrouter", "/tmp/test-config")
	})
}

func TestInitialize_CIEnvironment(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("CI", "1")

	config, apiKeys, err := Initialize()
	require.NoError(t, err)
	require.NotNil(t, config)
	require.NotNil(t, apiKeys)
}

func TestHasProviderAuth_LocalProviders(t *testing.T) {
	// Local providers should always have auth
	assert.True(t, HasProviderAuth("ollama-local"))
	assert.True(t, HasProviderAuth("test"))
	assert.True(t, HasProviderAuth("editor"))
}
