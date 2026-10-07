package webcontent

import (
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSearchResults_FallbackToDuckDuckGo(t *testing.T) {
	// Live DuckDuckGo call: not run in CI, where third-party throttling makes
	// it fail at random. Run it locally to check the real fallback.
	if testing.Short() || os.Getenv("SKIP_NETWORK_TESTS") != "" || os.Getenv("CI") != "" {
		t.Skip("skipping live network test (short mode, SKIP_NETWORK_TESTS or CI)")
	}
	// Create a config manager without Jina API key
	cfg, err := configuration.NewManager()
	assert.NoError(t, err, "Should create config manager successfully")

	// Test search with no API key configured
	results, err := GetSearchResults("golang programming", cfg)

	require.NoError(t, err, "Search should not fail when falling back to DuckDuckGo")
	require.NotEmpty(t, results, "Should have at least one result")

	// Verify the fallback result structure
	result := results[0]
	assert.NotEmpty(t, result.Title, "Result title should not be empty")
	assert.NotEmpty(t, result.URL, "Result URL should not be empty")
	assert.NotNil(t, result.Description, "Result description should not be nil")
}

func TestSearchProviderInterface(t *testing.T) {
	// Test JinaSearchProvider
	jinaProvider := &JinaSearchProvider{}
	assert.Equal(t, "Jina AI", jinaProvider.Name())

	// Test DuckDuckGoSearchProvider
	ddgProvider := &DuckDuckGoSearchProvider{}
	assert.Equal(t, "DuckDuckGo", ddgProvider.Name())
}

func TestDuckDuckGoSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent test in short mode")
	}
	logger := utils.GetLogger(false)

	results, err := performDuckDuckGoSearch("golang programming", logger)

	assert.NoError(t, err, "DuckDuckGo search should not fail")
	assert.GreaterOrEqual(t, len(results), 1, "Should return at least one result")

	result := results[0]
	assert.NotEmpty(t, result.Title, "Title should not be empty")
	assert.NotEmpty(t, result.URL, "URL should not be empty")
	assert.NotEmpty(t, result.Description, "Description should not be empty")
}
