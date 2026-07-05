package configuration

import (
	"time"
)

// GetModelForProvider returns the configured model for a provider.
// Returns an empty string if no model is configured for the provider; callers
// handle this by running model selection against the live provider API.
func (c *Config) GetModelForProvider(provider string) string {
	if model, exists := c.ProviderModels[provider]; exists && model != "" {
		return model
	}
	return ""
}

// SetModelForProvider sets the model for a specific provider.
// The test provider is silently rejected to prevent it from leaking
// into the persisted config via direct Config access.
func (c *Config) SetModelForProvider(provider, model string) {
	// Defense-in-depth: reject test provider at the Config level so that
	// even code that bypasses the Manager guard cannot persist it.
	if provider == "test" {
		return
	}
	if c.ProviderModels == nil {
		c.ProviderModels = make(map[string]string)
	}
	c.ProviderModels[provider] = model
	c.LastUsedProvider = provider
}

// GetMCPTimeout returns the MCP timeout as a time.Duration
func (c *Config) GetMCPTimeout() time.Duration {
	if c.MCP.Timeout == 0 {
		return 30 * time.Second
	}
	return c.MCP.Timeout
}

// GetCommitModel returns the configured model for commit message generation
// If not explicitly set, falls back to the provider's default model
func (c *Config) GetCommitModel() string {
	if c.CommitModel != "" {
		return c.CommitModel
	}
	// Use the provider for commits
	provider := c.GetCommitProvider()
	return c.GetModelForProvider(provider)
}

// GetCommitProvider returns the configured provider for commit message generation.
// Returns an empty string if no explicit commit provider is set; callers
// should surface this and offer interactive provider selection.
func (c *Config) GetCommitProvider() string {
	return c.CommitProvider
}

// SetCommitProvider sets the provider for commit message generation
func (c *Config) SetCommitProvider(provider string) {
	c.CommitProvider = provider
}

// SetCommitModel sets the model for commit message generation
func (c *Config) SetCommitModel(model string) {
	c.CommitModel = model
}

// GetReviewProvider returns the configured provider for review commands.
// Returns an empty string if no explicit review provider is set; callers
// should surface this and offer interactive provider selection.
func (c *Config) GetReviewProvider() string {
	return c.ReviewProvider
}

// GetReviewModel returns the configured model for review commands
// If not explicitly set, falls back to the provider's default model
func (c *Config) GetReviewModel() string {
	if c.ReviewModel != "" {
		return c.ReviewModel
	}
	// Use the provider for reviews
	provider := c.GetReviewProvider()
	return c.GetModelForProvider(provider)
}

// SetReviewProvider sets the provider for review commands
func (c *Config) SetReviewProvider(provider string) {
	c.ReviewProvider = provider
}

// SetReviewModel sets the model for review commands
func (c *Config) SetReviewModel(model string) {
	c.ReviewModel = model
}
