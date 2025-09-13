package api

import (
	"strings"
)

// ModelConfig holds comprehensive information about a model
type ModelConfig struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Provider      string   `json:"provider"`
	ContextLength int      `json:"context_length"`
	InputCost     float64  `json:"input_cost_per_1m"`     // Cost per 1M input tokens
	OutputCost    float64  `json:"output_cost_per_1m"`    // Cost per 1M output tokens
	CachedInputCost float64 `json:"cached_input_cost_per_1m,omitempty"` // Cached input cost
	Features      []string `json:"features,omitempty"`    // e.g., "audio", "vision", "reasoning"
	Tags          []string `json:"tags,omitempty"`        // e.g., "latest", "preview"
}

// ModelRegistry manages model configurations in a data-driven way
type ModelRegistry struct {
	models       map[string]ModelConfig
	patterns     []ModelPattern // For pattern-based matching
}

// ModelPattern represents a pattern-based model configuration
type ModelPattern struct {
	Contains     []string     `json:"contains"`      // Strings that must be present in model ID
	NotContains  []string     `json:"not_contains"`  // Strings that must not be present
	Config       ModelConfig  `json:"config"`        // Configuration for matching models
	Priority     int          `json:"priority"`      // Higher priority patterns checked first
}

var defaultRegistry *ModelRegistry

// GetModelRegistry returns the default model registry
func GetModelRegistry() *ModelRegistry {
	if defaultRegistry == nil {
		defaultRegistry = newDefaultModelRegistry()
	}
	return defaultRegistry
}

// newDefaultModelRegistry creates the registry with all model configurations
func newDefaultModelRegistry() *ModelRegistry {
	registry := &ModelRegistry{
		models:   make(map[string]ModelConfig),
		patterns: make([]ModelPattern, 0),
	}
	
	// Initialize with OpenAI models (migrated from hard-coded map)
	openAIModels := []ModelConfig{
		// GPT-5 series
		{"gpt-5", "GPT-5", "openai", 272000, 0.625, 5.0, 0.3125, []string{}, []string{"latest"}},
		{"gpt-5-2025-08-07", "GPT-5", "openai", 272000, 0.625, 5.0, 0.3125, []string{}, []string{}},
		{"gpt-5-chat-latest", "GPT-5 Chat", "openai", 272000, 0.625, 5.0, 0.3125, []string{}, []string{"latest"}},
		{"gpt-5-mini", "GPT-5 Mini", "openai", 272000, 0.125, 1.0, 0.0625, []string{}, []string{}},
		{"gpt-5-mini-2025-08-07", "GPT-5 Mini", "openai", 272000, 0.125, 1.0, 0.0625, []string{}, []string{}},
		{"gpt-5-nano", "GPT-5 Nano", "openai", 272000, 0.025, 0.2, 0.0125, []string{}, []string{}},
		{"gpt-5-nano-2025-08-07", "GPT-5 Nano", "openai", 272000, 0.025, 0.2, 0.0125, []string{}, []string{}},
		
		// O3 series
		{"o3", "O3", "openai", 200000, 1.0, 4.0, 0.25, []string{"reasoning"}, []string{}},
		{"o3-mini", "O3 Mini", "openai", 200000, 0.55, 2.2, 0.138, []string{"reasoning"}, []string{}},
		{"o4-mini", "O4 Mini", "openai", 200000, 0.55, 2.2, 0.138, []string{"reasoning"}, []string{}},
		
		// O1 series
		{"o1", "O1", "openai", 128000, 1.0, 4.0, 0.25, []string{"reasoning"}, []string{}},
		{"o1-2024-12-17", "O1", "openai", 128000, 1.0, 4.0, 0.25, []string{"reasoning"}, []string{}},
		{"o1-mini", "O1 Mini", "openai", 128000, 0.55, 2.2, 0.138, []string{"reasoning"}, []string{}},
		{"o1-mini-2024-09-12", "O1 Mini", "openai", 128000, 0.55, 2.2, 0.138, []string{"reasoning"}, []string{}},
		{"o1-pro", "O1 Pro", "openai", 128000, 3.0, 12.0, 0.75, []string{"reasoning"}, []string{}},
		{"o1-pro-2025-03-19", "O1 Pro", "openai", 128000, 3.0, 12.0, 0.75, []string{"reasoning"}, []string{}},
		
		// GPT-4o series
		{"gpt-4o", "GPT-4o", "openai", 128000, 0.005, 0.015, 0.0025, []string{"vision"}, []string{}},
		{"gpt-4o-2024-05-13", "GPT-4o", "openai", 128000, 0.005, 0.015, 0.0025, []string{"vision"}, []string{}},
		{"gpt-4o-2024-08-06", "GPT-4o", "openai", 128000, 0.0025, 0.01, 0.00125, []string{"vision"}, []string{}},
		{"gpt-4o-2024-11-20", "GPT-4o", "openai", 128000, 0.0025, 0.01, 0.00125, []string{"vision"}, []string{}},
		{"gpt-4o-mini", "GPT-4o Mini", "openai", 128000, 0.00015, 0.0006, 0.000075, []string{"vision"}, []string{}},
		{"gpt-4o-mini-2024-07-18", "GPT-4o Mini", "openai", 128000, 0.00015, 0.0006, 0.000075, []string{"vision"}, []string{}},
		
		// Audio models
		{"gpt-4o-audio-preview", "GPT-4o Audio", "openai", 128000, 0.01, 0.03, 0.005, []string{"audio", "vision"}, []string{"preview"}},
		{"gpt-4o-audio-preview-2024-10-01", "GPT-4o Audio", "openai", 128000, 0.01, 0.03, 0.005, []string{"audio", "vision"}, []string{"preview"}},
		{"gpt-4o-audio-preview-2024-12-17", "GPT-4o Audio", "openai", 128000, 0.01, 0.03, 0.005, []string{"audio", "vision"}, []string{"preview"}},
		{"gpt-4o-audio-preview-2025-06-03", "GPT-4o Audio", "openai", 128000, 0.01, 0.03, 0.005, []string{"audio", "vision"}, []string{"preview"}},
		{"gpt-4o-mini-audio-preview", "GPT-4o Mini Audio", "openai", 128000, 0.002, 0.008, 0.001, []string{"audio", "vision"}, []string{"preview"}},
		{"gpt-4o-mini-audio-preview-2024-12-17", "GPT-4o Mini Audio", "openai", 128000, 0.002, 0.008, 0.001, []string{"audio", "vision"}, []string{"preview"}},
		
		// GPT-4 series
		{"gpt-4", "GPT-4", "openai", 8192, 0.03, 0.06, 0.015, []string{}, []string{}},
		{"gpt-4-0314", "GPT-4", "openai", 8192, 0.03, 0.06, 0.015, []string{}, []string{}},
		{"gpt-4-0613", "GPT-4", "openai", 8192, 0.03, 0.06, 0.015, []string{}, []string{}},
		{"gpt-4-turbo", "GPT-4 Turbo", "openai", 128000, 0.01, 0.03, 0.005, []string{"vision"}, []string{}},
		{"gpt-4-turbo-2024-04-09", "GPT-4 Turbo", "openai", 128000, 0.01, 0.03, 0.005, []string{"vision"}, []string{}},
		{"gpt-4-turbo-preview", "GPT-4 Turbo", "openai", 128000, 0.01, 0.03, 0.005, []string{}, []string{"preview"}},
		{"gpt-4-1106-preview", "GPT-4 Turbo", "openai", 128000, 0.01, 0.03, 0.005, []string{}, []string{"preview"}},
		
		// GPT-3.5 series
		{"gpt-3.5-turbo", "GPT-3.5 Turbo", "openai", 16385, 0.002, 0.002, 0.001, []string{}, []string{}},
		{"gpt-3.5-turbo-0125", "GPT-3.5 Turbo", "openai", 16385, 0.002, 0.002, 0.001, []string{}, []string{}},
		{"gpt-3.5-turbo-1106", "GPT-3.5 Turbo", "openai", 16385, 0.002, 0.002, 0.001, []string{}, []string{}},
		{"gpt-3.5-turbo-16k", "GPT-3.5 Turbo 16K", "openai", 16385, 0.003, 0.004, 0.0015, []string{}, []string{}},
		{"gpt-3.5-turbo-instruct", "GPT-3.5 Turbo Instruct", "openai", 4097, 0.0015, 0.002, 0.00075, []string{}, []string{}},
		{"gpt-3.5-turbo-instruct-0914", "GPT-3.5 Turbo Instruct", "openai", 4097, 0.0015, 0.002, 0.00075, []string{}, []string{}},
		
		// ChatGPT models
		{"chatgpt-4o-latest", "ChatGPT-4o", "openai", 128000, 0.005, 0.015, 0.0025, []string{"vision"}, []string{"latest"}},
	}
	
	// Add all OpenAI models to registry
	for _, model := range openAIModels {
		registry.models[model.ID] = model
	}
	
	// Add pattern-based configurations for flexible matching
	registry.patterns = []ModelPattern{
		// GPT-5 patterns (highest priority)
		{[]string{"gpt-5"}, []string{}, ModelConfig{"", "", "openai", 272000, 0.625, 5.0, 0.3125, []string{}, []string{}}, 100},
		
		// O3 patterns
		{[]string{"o3-mini"}, []string{}, ModelConfig{"", "", "openai", 200000, 0.55, 2.2, 0.138, []string{"reasoning"}, []string{}}, 90},
		{[]string{"o3"}, []string{"mini"}, ModelConfig{"", "", "openai", 200000, 1.0, 4.0, 0.25, []string{"reasoning"}, []string{}}, 85},
		
		// O1 patterns  
		{[]string{"o1"}, []string{"mini", "pro"}, ModelConfig{"", "", "openai", 128000, 1.0, 4.0, 0.25, []string{"reasoning"}, []string{}}, 80},
		{[]string{"o1-mini"}, []string{}, ModelConfig{"", "", "openai", 128000, 0.55, 2.2, 0.138, []string{"reasoning"}, []string{}}, 75},
		{[]string{"o1-pro"}, []string{}, ModelConfig{"", "", "openai", 128000, 3.0, 12.0, 0.75, []string{"reasoning"}, []string{}}, 75},
		
		// GPT-4o patterns
		{[]string{"gpt-4o-mini"}, []string{}, ModelConfig{"", "", "openai", 128000, 0.00015, 0.0006, 0.000075, []string{"vision"}, []string{}}, 70},
		{[]string{"gpt-4o"}, []string{"mini"}, ModelConfig{"", "", "openai", 128000, 0.005, 0.015, 0.0025, []string{"vision"}, []string{}}, 65},
		
		// GPT-4 patterns
		{[]string{"gpt-4-turbo"}, []string{}, ModelConfig{"", "", "openai", 128000, 0.01, 0.03, 0.005, []string{}, []string{}}, 60},
		{[]string{"gpt-4"}, []string{"turbo", "o"}, ModelConfig{"", "", "openai", 8192, 0.03, 0.06, 0.015, []string{}, []string{}}, 55},
		
		// GPT-3.5 patterns
		{[]string{"gpt-3.5-turbo"}, []string{}, ModelConfig{"", "", "openai", 16385, 0.002, 0.002, 0.001, []string{}, []string{}}, 50},
		
		// ChatGPT patterns  
		{[]string{"chatgpt"}, []string{}, ModelConfig{"", "", "openai", 128000, 0.005, 0.015, 0.0025, []string{}, []string{}}, 45},
	}
	
	return registry
}

// GetModelConfig retrieves configuration for a model by exact ID match first, then pattern matching
func (r *ModelRegistry) GetModelConfig(modelID string) (ModelConfig, bool) {
	// Try exact match first
	if config, exists := r.models[modelID]; exists {
		return config, true
	}
	
	// Try pattern matching (sorted by priority, highest first)
	for _, pattern := range r.patterns {
		if r.matchesPattern(modelID, pattern) {
			// Create a copy and set the actual ID
			config := pattern.Config
			config.ID = modelID
			return config, true
		}
	}
	
	return ModelConfig{}, false
}

// GetModelPricing returns input and output costs per 1M tokens
func (r *ModelRegistry) GetModelPricing(modelID string) (inputCost, outputCost float64) {
	if config, exists := r.GetModelConfig(modelID); exists {
		return config.InputCost, config.OutputCost
	}
	return 0, 0
}

// GetModelContextLength returns the context length for a model
func (r *ModelRegistry) GetModelContextLength(modelID string) int {
	if config, exists := r.GetModelConfig(modelID); exists {
		return config.ContextLength
	}
	return 16000 // Conservative default
}

// matchesPattern checks if a model ID matches a pattern
func (r *ModelRegistry) matchesPattern(modelID string, pattern ModelPattern) bool {
	// All "contains" strings must be present
	for _, mustContain := range pattern.Contains {
		if !strings.Contains(modelID, mustContain) {
			return false
		}
	}
	
	// None of "not_contains" strings must be present
	for _, mustNotContain := range pattern.NotContains {
		if strings.Contains(modelID, mustNotContain) {
			return false
		}
	}
	
	return true
}

// AddModel adds or updates a model configuration
func (r *ModelRegistry) AddModel(config ModelConfig) {
	r.models[config.ID] = config
}

// AddPattern adds a new pattern for flexible model matching
func (r *ModelRegistry) AddPattern(pattern ModelPattern) {
	r.patterns = append(r.patterns, pattern)
}