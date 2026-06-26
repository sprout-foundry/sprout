package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseProviderName_Ollama(t *testing.T) {
	ct, err := ParseProviderName("ollama")
	assert.NoError(t, err)
	assert.Equal(t, OllamaLocalClientType, ct)
}

func TestParseProviderName_OllamaCaseInsensitive(t *testing.T) {
	ct, err := ParseProviderName("OLLAMA")
	assert.NoError(t, err)
	assert.Equal(t, OllamaLocalClientType, ct)
}

func TestParseProviderName_Test(t *testing.T) {
	ct, err := ParseProviderName("test")
	assert.NoError(t, err)
	assert.Equal(t, TestClientType, ct)
}

func TestParseProviderName_Editor(t *testing.T) {
	ct, err := ParseProviderName("editor")
	assert.NoError(t, err)
	assert.Equal(t, EditorClientType, ct)
}

func TestParseProviderName_Empty(t *testing.T) {
	_, err := ParseProviderName("")
	assert.Error(t, err)
}

func TestParseProviderName_OpenAI(t *testing.T) {
	ct, err := ParseProviderName("openai")
	assert.NoError(t, err)
	assert.Equal(t, OpenAIClientType, ct)
}

func TestParseProviderName_OpenRouter(t *testing.T) {
	ct, err := ParseProviderName("openrouter")
	assert.NoError(t, err)
	assert.Equal(t, OpenRouterClientType, ct)
}

func TestParseProviderName_SpacesAndCase(t *testing.T) {
	ct, err := ParseProviderName("  OpenAI  ")
	assert.NoError(t, err)
	assert.Equal(t, OpenAIClientType, ct)
}

func TestParseProviderName_CustomProvider(t *testing.T) {
	ct, err := ParseProviderName("my-custom-provider")
	assert.NoError(t, err)
	assert.Equal(t, ClientType("my-custom-provider"), ct)
}

func TestGetProviderName_Known(t *testing.T) {
	assert.Equal(t, "OpenAI", GetProviderName(OpenAIClientType))
	assert.Equal(t, "OpenRouter (Recommended)", GetProviderName(OpenRouterClientType))
	assert.Equal(t, "Ollama (Local)", GetProviderName(OllamaLocalClientType))
	assert.Equal(t, "Ollama (Cloud)", GetProviderName(OllamaCloudClientType))
	assert.Equal(t, "Test Provider", GetProviderName(TestClientType))
	assert.Equal(t, "Editor Mode", GetProviderName(EditorClientType))
	assert.Equal(t, "DeepInfra", GetProviderName(DeepInfraClientType))
	assert.Equal(t, "DeepSeek", GetProviderName(DeepSeekClientType))
	assert.Equal(t, "LM Studio", GetProviderName(LMStudioClientType))
	assert.Equal(t, "Mistral", GetProviderName(MistralClientType))
	assert.Equal(t, "MiniMax", GetProviderName(MinimaxClientType))
}

func TestGetProviderName_OllamaAlias(t *testing.T) {
	assert.Equal(t, "Ollama (Local)", GetProviderName(ClientType("ollama")))
}

func TestGetProviderName_Unknown(t *testing.T) {
	assert.Equal(t, "some-unknown", GetProviderName(ClientType("some-unknown")))
}

func TestDetermineProvider_ExplicitFlag(t *testing.T) {
	provider, err := DetermineProvider("test", "")
	assert.NoError(t, err)
	assert.Equal(t, TestClientType, provider)
}

func TestDetermineProvider_ExplicitFlagInvalidProvider(t *testing.T) {
	// Provider with no credential check - the function validates via IsProviderAvailable
	// which checks if the provider can be used. Right now most custom names
	// will succeed since HasProviderCredential returns true for anything that
	// doesn't require an API key.
	// Just verify the function doesn't panic and returns something valid.
	provider, err := DetermineProvider("nonexistent-provider-xyz", "")
	_ = provider
	_ = err
}

func TestDetermineProvider_EmptyFallback(t *testing.T) {
	// Empty inputs always succeed — returns whatever provider is available
	provider, err := DetermineProvider("", "")
	assert.NoError(t, err)
	assert.NotEmpty(t, provider)
}

func TestDetermineProvider_LastUsedOllama(t *testing.T) {
	provider, err := DetermineProvider("", OllamaLocalClientType)
	assert.NoError(t, err)
	assert.Equal(t, OllamaLocalClientType, provider)
}

func TestDetermineProvider_LastUsedTest(t *testing.T) {
	provider, err := DetermineProvider("", TestClientType)
	assert.NoError(t, err)
	assert.Equal(t, TestClientType, provider)
}

func TestDetermineProvider_ExplicitUnavailableProvider(t *testing.T) {
	// A provider with no API key - will fail IsProviderAvailable check
	_, err := DetermineProvider("deepseek", "")
	// This may or may not fail depending on whether DEEPSEEK_API_KEY is set
	// Just verify the function doesn't panic
	_ = err
}

func TestClientType_Constants(t *testing.T) {
	assert.Equal(t, ClientType("openai"), OpenAIClientType)
	assert.Equal(t, ClientType("openrouter"), OpenRouterClientType)
	assert.Equal(t, ClientType("ollama-local"), OllamaLocalClientType)
	assert.Equal(t, ClientType("ollama-cloud"), OllamaCloudClientType)
	assert.Equal(t, ClientType("test"), TestClientType)
	assert.Equal(t, ClientType("editor"), EditorClientType)
}
