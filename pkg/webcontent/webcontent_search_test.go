package webcontent

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestGetSearchResults_FallbackToDuckDuckGo(t *testing.T) {
	// Create a config manager without Jina API key
	cfg, err := configuration.NewManager()
	assert.NoError(t, err, "Should create config manager successfully")

	// Test search with no API key configured
	results, err := GetSearchResults("golang programming", cfg)

	assert.NoError(t, err, "Search should not fail when falling back to DuckDuckGo")
	assert.NotNil(t, results, "Results should not be nil")
	assert.Greater(t, len(results), 0, "Should have at least one result")

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
	logger := utils.GetLogger(false)

	results, err := performDuckDuckGoSearch("golang programming", logger)

	assert.NoError(t, err, "DuckDuckGo search should not fail")
	assert.GreaterOrEqual(t, len(results), 1, "Should return at least one result")

	result := results[0]
	assert.NotEmpty(t, result.Title, "Title should not be empty")
	assert.NotEmpty(t, result.URL, "URL should not be empty")
	assert.NotEmpty(t, result.Description, "Description should not be empty")
}
