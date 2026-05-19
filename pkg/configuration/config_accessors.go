package configuration

import (
	"fmt"
	"strings"
	"time"
)

// GetModelForProvider returns the configured model for a provider
func (c *Config) GetModelForProvider(provider string) string {
	if model, exists := c.ProviderModels[provider]; exists && model != "" {
		return model
	}

	// Return default from NewConfig if not set
	defaults := NewConfig()
	if defaultModel, exists := defaults.ProviderModels[provider]; exists {
		return defaultModel
	}

	return ""
}

// SetModelForProvider sets the model for a specific provider.
// The test provider is silently rejected to prevent it from leaking
// into the persisted config via direct Config access.
func (c *Config) SetModelForProvider(provider, model string) {
	if provider == "test" {
		return
	}
	if c.ProviderModels == nil {
		c.ProviderModels = make(map[string]string)
	}
	c.ProviderModels[provider] = model
	c.LastUsedProvider = provider
}

// NormalizeSelfReviewGateMode validates and normalizes self-review gate mode.
func NormalizeSelfReviewGateMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", SelfReviewGateModeOff:
		return SelfReviewGateModeOff, true
	case SelfReviewGateModeCode:
		return SelfReviewGateModeCode, true
	case SelfReviewGateModeAlways:
		return SelfReviewGateModeAlways, true
	default:
		return "", false
	}
}

// GetSelfReviewGateMode returns the effective self-review gate mode.
func (c *Config) GetSelfReviewGateMode() string {
	mode, ok := NormalizeSelfReviewGateMode(c.SelfReviewGateMode)
	if !ok {
		return SelfReviewGateModeOff
	}
	return mode
}

// SetSelfReviewGateMode sets the self-review gate mode.
func (c *Config) SetSelfReviewGateMode(mode string) error {
	normalized, ok := NormalizeSelfReviewGateMode(mode)
	if !ok {
		return fmt.Errorf("invalid self-review gate mode %q (allowed: off, code, always)", mode)
	}
	c.SelfReviewGateMode = normalized
	return nil
}

// GetMCPTimeout returns the MCP timeout as a time.Duration
func (c *Config) GetMCPTimeout() time.Duration {
	if c.MCP.Timeout == 0 {
		return 30 * time.Second
	}
	return c.MCP.Timeout
}

// GetSubagentProvider returns the configured provider for subagents
// If not explicitly set, falls back to the last used provider
func (c *Config) GetSubagentProvider() string {
	if c.SubagentProvider != "" {
		return c.SubagentProvider
	}
	if c.LastUsedProvider != "" {
		return c.LastUsedProvider
	}
	if len(c.ProviderPriority) > 0 {
		return c.ProviderPriority[0]
	}
	return "ollama-local"
}

// GetSubagentModel returns the configured model for subagents
// If not explicitly set, falls back to the provider's default model
func (c *Config) GetSubagentModel() string {
	if c.SubagentModel != "" {
		return c.SubagentModel
	}
	provider := c.GetSubagentProvider()
	return c.GetModelForProvider(provider)
}

// SetSubagentProvider sets the provider for subagents
func (c *Config) SetSubagentProvider(provider string) {
	c.SubagentProvider = provider
}

// SetSubagentModel sets the model for subagents
func (c *Config) SetSubagentModel(model string) {
	c.SubagentModel = model
}

// GetCommitProvider returns the configured provider for commit message generation
// If not explicitly set, falls back to the last used provider
func (c *Config) GetCommitProvider() string {
	if c.CommitProvider != "" {
		return c.CommitProvider
	}
	if c.LastUsedProvider != "" {
		return c.LastUsedProvider
	}
	if len(c.ProviderPriority) > 0 {
		return c.ProviderPriority[0]
	}
	return "ollama-local"
}

// GetCommitModel returns the configured model for commit message generation
// If not explicitly set, falls back to the provider's default model
func (c *Config) GetCommitModel() string {
	if c.CommitModel != "" {
		return c.CommitModel
	}
	provider := c.GetCommitProvider()
	return c.GetModelForProvider(provider)
}

// SetCommitProvider sets the provider for commit message generation
func (c *Config) SetCommitProvider(provider string) {
	c.CommitProvider = provider
}

// SetCommitModel sets the model for commit message generation
func (c *Config) SetCommitModel(model string) {
	c.CommitModel = model
}

// GetReviewProvider returns the configured provider for review commands
// If not explicitly set, falls back to the last used provider
func (c *Config) GetReviewProvider() string {
	if c.ReviewProvider != "" {
		return c.ReviewProvider
	}
	if c.LastUsedProvider != "" {
		return c.LastUsedProvider
	}
	if len(c.ProviderPriority) > 0 {
		return c.ProviderPriority[0]
	}
	return "ollama-local"
}

// GetReviewModel returns the configured model for review commands
// If not explicitly set, falls back to the provider's default model
func (c *Config) GetReviewModel() string {
	if c.ReviewModel != "" {
		return c.ReviewModel
	}
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

// GetSubagentMaxParallel returns the maximum number of parallel subagents
// Defaults to 2 if not configured or set to 0
func (c *Config) GetSubagentMaxParallel() int {
	if c.SubagentMaxParallel > 0 {
		return c.SubagentMaxParallel
	}
	return 2
}

// GetSubagentParallelEnabled returns whether parallel subagent execution is enabled
// Defaults to true if not explicitly set (nil pointer)
func (c *Config) GetSubagentParallelEnabled() bool {
	if c.SubagentParallelEnabled == nil {
		return true
	}
	return *c.SubagentParallelEnabled
}

// GetSubagentMaxDepth returns the maximum subagent nesting depth.
// Defaults to 2 if not configured or set to 0.
func (c *Config) GetSubagentMaxDepth() int {
	if c.SubagentMaxDepth > 0 {
		return c.SubagentMaxDepth
	}
	return 2
}

// GetEAMode returns the EA startup mode. Defaults to "interactive".
func (c *Config) GetEAMode() string {
	if c == nil || c.EAMode == "" {
		return "interactive"
	}
	return c.EAMode
}

// GetPersistentContextConfig returns the persistent context configuration,
// initializing defaults if not set.
func (c *Config) GetPersistentContextConfig() *PersistentContextConfig {
	if c.PersistentContext == nil {
		return &PersistentContextConfig{
			ProactiveContextEnabled:  func() *bool { b := true; return &b }(),
			MaxContextualResults:     5,
			MinRelevanceScore:        0.50,
			MaxContextChars:          4000,
			WorkspaceScopedRetrieval: false,
			DriftDetectionEnabled:    func() *bool { b := true; return &b }(),
			DriftThreshold:           0.60,
			DriftCheckInterval:       5,
		}
	}
	result := *c.PersistentContext
	if result.MaxContextualResults == 0 {
		result.MaxContextualResults = 5
	}
	if result.MinRelevanceScore == 0 {
		result.MinRelevanceScore = 0.50
	}
	if result.MaxContextChars == 0 {
		result.MaxContextChars = 4000
	}
	if result.ProactiveContextEnabled == nil {
		result.ProactiveContextEnabled = func() *bool { b := true; return &b }()
	}
	if result.DriftDetectionEnabled == nil {
		result.DriftDetectionEnabled = func() *bool { b := true; return &b }()
	}
	if result.DriftThreshold <= 0 {
		result.DriftThreshold = 0.60
	}
	if result.DriftCheckInterval <= 0 {
		result.DriftCheckInterval = 5
	}
	return &result
}
