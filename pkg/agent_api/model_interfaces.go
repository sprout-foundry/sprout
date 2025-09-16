package api

// ModelStore defines the interface for model configuration storage
type ModelStore interface {
	// GetModelConfig retrieves configuration for a model by ID
	GetModelConfig(modelID string) (ModelConfig, error)

	// AddModel adds or updates a model configuration
	AddModel(config ModelConfig) error

	// GetAllModels returns all registered models
	GetAllModels() map[string]ModelConfig
}

// PricingProvider defines the interface for model pricing operations
type PricingProvider interface {
	// GetModelPricing returns input and output costs per 1M tokens
	GetModelPricing(modelID string) (inputCost, outputCost float64, err error)

	// CalculateCost calculates the cost for given token counts
	CalculateCost(modelID string, inputTokens, outputTokens int) (float64, error)
}

// ContextService defines the interface for model context operations
type ContextService interface {
	// GetModelContextLength returns the context length for a model
	GetModelContextLength(modelID string) (int, error)

	// ValidateContextFit checks if content fits within model context
	ValidateContextFit(modelID string, estimatedTokens int) error
}

// PatternMatcher defines the interface for pattern-based model matching
type PatternMatcher interface {
	// MatchModel finds a model configuration using pattern matching
	MatchModel(modelID string) (ModelConfig, error)

	// AddPattern adds a pattern for model matching
	AddPattern(pattern ModelPattern) error
}

// ModelValidator defines the interface for model configuration validation
type ModelValidator interface {
	// ValidateModelConfig validates a model configuration
	ValidateModelConfig(config ModelConfig) error

	// ValidateModelPattern validates a model pattern
	ValidateModelPattern(pattern ModelPattern) error
}
