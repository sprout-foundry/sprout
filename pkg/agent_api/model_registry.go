package api

import (
	"strings"
	"sync"
)

// ModelConfig holds comprehensive information about a model
type ModelConfig struct {
	ID              string   `json:"id"`
	Name            string   `json:"name,omitempty"`
	Provider        string   `json:"provider"`
	ContextLength   int      `json:"context_length"`
	InputCost       float64  `json:"input_cost_per_1m"`                  // Cost per 1M input tokens
	OutputCost      float64  `json:"output_cost_per_1m"`                 // Cost per 1M output tokens
	CachedInputCost float64  `json:"cached_input_cost_per_1m,omitempty"` // Cached input cost
	Features        []string `json:"features,omitempty"`                 // e.g., "audio", "vision", "reasoning"
	Tags            []string `json:"tags,omitempty"`                     // e.g., "latest", "preview"
}

// ModelRegistry manages model configurations in a data-driven way
type ModelRegistry struct {
	models   map[string]ModelConfig
	patterns []ModelPattern // For pattern-based matching
	mu       sync.RWMutex   // Protects concurrent access
}

// ModelPattern represents a pattern-based model configuration
type ModelPattern struct {
	Contains    []string    `json:"contains"`     // Strings that must be present in model ID
	NotContains []string    `json:"not_contains"` // Strings that must not be present
	Config      ModelConfig `json:"config"`       // Configuration for matching models
	Priority    int         `json:"priority"`     // Higher priority patterns checked first
}

var (
	defaultRegistry *ModelRegistry
	registryOnce    sync.Once
)

// ModelNotFoundError is returned when a model is not found in the registry
type ModelNotFoundError struct {
	ModelID string
}

func (e *ModelNotFoundError) Error() string {
	return "model not found in registry: " + e.ModelID
}

// GetModelRegistry returns the default model registry (thread-safe singleton)
func GetModelRegistry() *ModelRegistry {
	registryOnce.Do(func() {
		defaultRegistry = NewModelRegistry()
	})
	return defaultRegistry
}

// NewModelRegistry creates a new model registry instance
// This allows for dependency injection in tests and better isolation
func NewModelRegistry() *ModelRegistry {
	return newDefaultModelRegistry()
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

	// Add DeepSeek models
	deepSeekModels := []ModelConfig{
		{"deepseek-chat", "DeepSeek Chat", "deepseek", 128000, 0.27, 1.1, 0, []string{"tools"}, []string{}},
		{"deepseek-chat-v3.1", "DeepSeek Chat V3.1", "deepseek", 128000, 0.27, 1.1, 0, []string{"tools"}, []string{"latest"}},
		{"deepseek-v3", "DeepSeek V3", "deepseek", 128000, 0.27, 1.1, 0, []string{"tools"}, []string{}},
		{"deepseek-v3.1", "DeepSeek V3.1", "deepseek", 128000, 0.27, 1.1, 0, []string{"tools"}, []string{}},
		{"deepseek-r1", "DeepSeek R1", "deepseek", 685000, 1.88, 5.88, 0, []string{"reasoning", "tools"}, []string{}},
		{"deepseek-r1-turbo", "DeepSeek R1 Turbo", "deepseek", 128000, 0.55, 2.2, 0, []string{"reasoning", "tools"}, []string{}},
	}
	for _, model := range deepSeekModels {
		registry.models[model.ID] = model
	}

	// Add DeepInfra models
	deepInfraModels := []ModelConfig{
		{"Qwen/Qwen3-Coder-480B-A35B-Instruct-Turbo", "Qwen3 Coder 480B", "deepinfra", 256000, 2.0, 2.0, 0, []string{"tools"}, []string{}},
		{"deepseek-ai/DeepSeek-V3.1", "DeepSeek V3.1", "deepinfra", 128000, 0.27, 1.1, 0, []string{"tools"}, []string{}},
		{"meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8", "Llama 4 Maverick", "deepinfra", 256000, 0.5, 0.5, 0, []string{"tools"}, []string{}},
		{"meta-llama/Llama-3.2-11B-Vision-Instruct", "Llama 3.2 Vision", "deepinfra", 128000, 0.35, 0.35, 0, []string{"vision", "tools"}, []string{}},
		{"meta-llama/Llama-3.3-70B-Instruct-Turbo", "Llama 3.3 70B", "deepinfra", 128000, 0.6, 0.6, 0, []string{"tools"}, []string{}},
		{"openai/gpt-oss-20b", "GPT OSS 20B", "deepinfra", 120000, 0.4, 0.4, 0, []string{"tools"}, []string{}},
	}
	for _, model := range deepInfraModels {
		registry.models[model.ID] = model
	}

	// Add OpenRouter-specific models
	openRouterModels := []ModelConfig{
		{"deepseek/deepseek-chat-v3.1:free", "DeepSeek Chat V3.1 (Free)", "openrouter", 128000, 0, 0, 0, []string{"tools"}, []string{"free"}},
		{"qwen/qwen3-coder:free", "Qwen3 Coder (Free)", "openrouter", 32000, 0, 0, 0, []string{"tools"}, []string{"free"}},
		{"qwen/qwen3-coder-30b-a3b-instruct", "Qwen3 Coder 30B", "openrouter", 32000, 0.4, 0.4, 0, []string{"tools"}, []string{}},
		{"mistralai/codestral-2508", "Codestral 2508", "openrouter", 256000, 0.5, 0.5, 0, []string{"tools"}, []string{}},
		{"x-ai/grok-code-fast-1", "Grok Code Fast", "openrouter", 131072, 0.5, 0.5, 0, []string{"tools"}, []string{}},
	}
	for _, model := range openRouterModels {
		registry.models[model.ID] = model
	}

	// Add Ollama models
	ollamaModels := []ModelConfig{
		{"gpt-oss:20b", "GPT OSS 20B (Local)", "ollama", 120000, 0, 0, 0, []string{"tools"}, []string{"local"}},
		{"qwen3-coder", "Qwen3 Coder (Local)", "ollama", 32000, 0, 0, 0, []string{"tools"}, []string{"local"}},
	}
	for _, model := range ollamaModels {
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

		// DeepSeek patterns
		{[]string{"deepseek-r1"}, []string{}, ModelConfig{"", "", "deepseek", 685000, 1.88, 5.88, 0, []string{"reasoning", "tools"}, []string{}}, 40},
		{[]string{"deepseek-v3"}, []string{}, ModelConfig{"", "", "deepseek", 128000, 0.27, 1.1, 0, []string{"tools"}, []string{}}, 35},
		{[]string{"deepseek"}, []string{}, ModelConfig{"", "", "deepseek", 32000, 0.14, 0.28, 0, []string{"tools"}, []string{}}, 30},

		// Llama patterns
		{[]string{"llama-4"}, []string{}, ModelConfig{"", "", "", 256000, 0.5, 0.5, 0, []string{"tools"}, []string{}}, 25},
		{[]string{"llama-3.3-70b"}, []string{}, ModelConfig{"", "", "", 128000, 0.6, 0.6, 0, []string{"tools"}, []string{}}, 20},
		{[]string{"llama-3"}, []string{}, ModelConfig{"", "", "", 32000, 0.4, 0.4, 0, []string{"tools"}, []string{}}, 15},
		{[]string{"llama"}, []string{}, ModelConfig{"", "", "", 8192, 0.2, 0.2, 0, []string{}, []string{}}, 10},

		// Qwen patterns
		{[]string{"qwen3-coder-480b"}, []string{}, ModelConfig{"", "", "", 256000, 2.0, 2.0, 0, []string{"tools"}, []string{}}, 25},
		{[]string{"qwen3"}, []string{}, ModelConfig{"", "", "", 128000, 0.4, 0.4, 0, []string{"tools"}, []string{}}, 20},
		{[]string{"qwen"}, []string{}, ModelConfig{"", "", "", 32000, 0.3, 0.3, 0, []string{"tools"}, []string{}}, 15},

		// Claude patterns (for future compatibility)
		{[]string{"claude"}, []string{}, ModelConfig{"", "", "", 200000, 3.0, 15.0, 0, []string{"tools"}, []string{}}, 25},

		// Gemini patterns (for future compatibility)
		{[]string{"gemini-2.5"}, []string{}, ModelConfig{"", "", "", 1000000, 1.0, 3.0, 0, []string{"vision", "tools"}, []string{}}, 20},
		{[]string{"gemini"}, []string{}, ModelConfig{"", "", "", 128000, 0.5, 1.5, 0, []string{"vision", "tools"}, []string{}}, 15},
	}

	return registry
}

// GetModelConfig retrieves configuration for a model by exact ID match first, then pattern matching
func (r *ModelRegistry) GetModelConfig(modelID string) (ModelConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Try exact match first
	if config, exists := r.models[modelID]; exists {
		return config, nil
	}

	// Try pattern matching (sorted by priority, highest first)
	for _, pattern := range r.patterns {
		if r.matchesPattern(modelID, pattern) {
			// Create a copy and set the actual ID
			config := pattern.Config
			config.ID = modelID
			return config, nil
		}
	}

	return ModelConfig{}, &ModelNotFoundError{ModelID: modelID}
}

// GetModelPricing returns input and output costs per 1M tokens
func (r *ModelRegistry) GetModelPricing(modelID string) (inputCost, outputCost float64, err error) {
	config, err := r.GetModelConfig(modelID)
	if err != nil {
		return 0, 0, err
	}
	return config.InputCost, config.OutputCost, nil
}

// GetModelContextLength returns the context length for a model
func (r *ModelRegistry) GetModelContextLength(modelID string) (int, error) {
	config, err := r.GetModelConfig(modelID)
	if err != nil {
		// Return conservative default with error for logging
		return 16000, err
	}
	return config.ContextLength, nil
}

// GetModelContextLengthWithDefault returns context length with fallback (for backward compatibility)
// DEPRECATED: Use GetModelContextLength instead and handle errors properly
func (r *ModelRegistry) GetModelContextLengthWithDefault(modelID string, defaultLength int) int {
	length, err := r.GetModelContextLength(modelID)
	if err != nil {
		return defaultLength
	}
	return length
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
func (r *ModelRegistry) AddModel(config ModelConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Comprehensive validation
	if err := r.validateModelConfig(config); err != nil {
		return err
	}

	r.models[config.ID] = config
	return nil
}

// validateModelConfig performs comprehensive validation on a model configuration
func (r *ModelRegistry) validateModelConfig(config ModelConfig) error {
	// ID validation
	if config.ID == "" {
		return &ModelValidationError{Field: "ID", Message: "model ID cannot be empty"}
	}

	// Provider validation
	if config.Provider == "" {
		return &ModelValidationError{Field: "Provider", Message: "provider cannot be empty"}
	}

	// Context length validation
	if config.ContextLength <= 0 {
		return &ModelValidationError{Field: "ContextLength", Message: "context length must be positive"}
	}

	// Cost validation
	if config.InputCost < 0 {
		return &ModelValidationError{Field: "InputCost", Message: "input cost cannot be negative"}
	}

	if config.OutputCost < 0 {
		return &ModelValidationError{Field: "OutputCost", Message: "output cost cannot be negative"}
	}

	if config.CachedInputCost < 0 {
		return &ModelValidationError{Field: "CachedInputCost", Message: "cached input cost cannot be negative"}
	}

	// Logical validation
	if config.CachedInputCost > config.InputCost {
		return &ModelValidationError{Field: "CachedInputCost", Message: "cached input cost should not exceed regular input cost"}
	}

	return nil
}

// AddPattern adds a new pattern for flexible model matching
func (r *ModelRegistry) AddPattern(pattern ModelPattern) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate pattern
	if err := r.validateModelPattern(pattern); err != nil {
		return err
	}

	r.patterns = append(r.patterns, pattern)

	// Sort patterns by priority (highest first) for consistent matching
	r.sortPatternsByPriority()

	return nil
}

// validateModelPattern validates a model pattern
func (r *ModelRegistry) validateModelPattern(pattern ModelPattern) error {
	if len(pattern.Contains) == 0 && len(pattern.NotContains) == 0 {
		return &ModelValidationError{Field: "Pattern", Message: "pattern must have at least one contains or not_contains rule"}
	}

	// Validate the embedded config (but allow empty ID since it's set dynamically)
	configCopy := pattern.Config
	configCopy.ID = "test" // Temporary ID for validation
	if err := r.validateModelConfig(configCopy); err != nil {
		return &ModelValidationError{Field: "Pattern.Config", Message: "invalid pattern config: " + err.Error()}
	}

	// Priority validation
	if pattern.Priority < 0 {
		return &ModelValidationError{Field: "Pattern.Priority", Message: "priority cannot be negative"}
	}

	return nil
}

// sortPatternsByPriority sorts patterns by priority (highest first)
func (r *ModelRegistry) sortPatternsByPriority() {
	// Simple insertion sort since patterns list is typically small
	for i := 1; i < len(r.patterns); i++ {
		j := i
		for j > 0 && r.patterns[j].Priority > r.patterns[j-1].Priority {
			r.patterns[j], r.patterns[j-1] = r.patterns[j-1], r.patterns[j]
			j--
		}
	}
}

// ModelValidationError is returned when model or pattern validation fails
type ModelValidationError struct {
	Field   string
	Message string
}

func (e *ModelValidationError) Error() string {
	return "model validation error in " + e.Field + ": " + e.Message
}
