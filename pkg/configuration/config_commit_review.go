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

// GetCommitModel returns the configured model for commit message generation.
// An explicit commit_model wins; otherwise the commit role's model
// (the commit settings alias the
// commit role) when set; otherwise falls back to the provider's default
// model.
func (c *Config) GetCommitModel() string {
	if c.CommitModel != "" {
		return c.CommitModel
	}
	if rc := c.GetRole(RoleCommit); rc.Model != "" {
		return rc.Model
	}
	// Use the provider for commits
	provider := c.GetCommitProvider()
	return c.GetModelForProvider(provider)
}

// GetCommitProvider returns the configured provider for commit message
// generation. An explicit commit_provider wins; otherwise the commit role's
// provider when set; otherwise
// falls back to the last-used provider (the documented default for the
// CommitProvider field), so the commit model resolves to the last-used
// provider's model. Returns an empty string only when none of the three
// sources is set; callers should surface this and offer interactive
// provider selection where a prompt is possible.
func (c *Config) GetCommitProvider() string {
	if c.CommitProvider != "" {
		return c.CommitProvider
	}
	if rc := c.GetRole(RoleCommit); rc.Provider != "" {
		return rc.Provider
	}
	return c.LastUsedProvider
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
// An explicit review_provider wins; otherwise the reviewer role's provider
// (the review settings alias
// the reviewer role) when set. No last-used fallback is applied (the
// pre-role shape is preserved); returns an empty string when neither is
// set, and callers should surface this and offer interactive provider
// selection.
func (c *Config) GetReviewProvider() string {
	if c.ReviewProvider != "" {
		return c.ReviewProvider
	}
	if rc := c.GetRole(RoleReviewer); rc.Provider != "" {
		return rc.Provider
	}
	return ""
}

// GetReviewModel returns the configured model for review commands. An
// explicit review_model wins; otherwise the reviewer role's model
// when set; otherwise falls
// back to the provider's default model.
func (c *Config) GetReviewModel() string {
	if c.ReviewModel != "" {
		return c.ReviewModel
	}
	if rc := c.GetRole(RoleReviewer); rc.Model != "" {
		return rc.Model
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

// GetCompletionProvider returns the configured provider for code completions.
// An explicit completion_provider wins; otherwise the coder role's provider
// (inline completion is code
// generation and the built-in role set has no completion-specific role, so
// the completion settings alias the coder role) when set. No last-used
// fallback is introduced (the pre-role shape is preserved); returns an empty
// string when neither is set, and callers fall back to the main provider.
func (c *Config) GetCompletionProvider() string {
	if c.CompletionProvider != "" {
		return c.CompletionProvider
	}
	if rc := c.GetRole(RoleCoder); rc.Provider != "" {
		return rc.Provider
	}
	return ""
}

// GetCompletionModel returns the configured model for code completions. An
// explicit completion_model wins; otherwise the coder role's model
// when set; otherwise falls back
// to the provider's default model.
func (c *Config) GetCompletionModel() string {
	if c.CompletionModel != "" {
		return c.CompletionModel
	}
	if rc := c.GetRole(RoleCoder); rc.Model != "" {
		return rc.Model
	}
	// Use the provider for completions
	provider := c.GetCompletionProvider()
	return c.GetModelForProvider(provider)
}

// SetCompletionProvider sets the provider for code completions
func (c *Config) SetCompletionProvider(provider string) {
	c.CompletionProvider = provider
}

// SetCompletionModel sets the model for code completions
func (c *Config) SetCompletionModel(model string) {
	c.CompletionModel = model
}
