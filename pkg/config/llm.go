package config

// LLMConfig contains all Large Language Model related configuration
type LLMConfig struct {
	// Model Selection
	EditingModel       string `json:"editing_model"`       // Primary model for code editing
	SummaryModel       string `json:"summary_model"`       // Model for summarization tasks
	OrchestrationModel string `json:"orchestration_model"` // Model for orchestration tasks
	WorkspaceModel     string `json:"workspace_model"`     // Model for workspace analysis
	EmbeddingModel     string `json:"embedding_model"`     // Model for embeddings
	CodeReviewModel    string `json:"code_review_model"`   // Model for code review tasks
	LocalModel         string `json:"local_model"`         // Local model configuration
	SearchModel        string `json:"search_model"`        // Model for search tasks

	// Generation Parameters
	Temperature      float64 `json:"temperature"`       // LLM temperature (0.0-1.0)
	MaxTokens        int     `json:"max_tokens"`        // Maximum output tokens
	TopP             float64 `json:"top_p"`             // Nucleus sampling parameter
	PresencePenalty  float64 `json:"presence_penalty"`  // Presence penalty
	FrequencyPenalty float64 `json:"frequency_penalty"` // Frequency penalty

	// Infrastructure
	OllamaServerURL string `json:"ollama_server_url"` // Ollama server endpoint
}

// DefaultLLMConfig returns sensible defaults for LLM configuration
func DefaultLLMConfig() *LLMConfig {
	return &LLMConfig{
		EditingModel:       "gpt-4",
		SummaryModel:       "gpt-3.5-turbo",
		OrchestrationModel: "gpt-4",
		WorkspaceModel:     "gpt-4",
		EmbeddingModel:     "text-embedding-3-small",
		CodeReviewModel:    "gpt-4",
		LocalModel:         "",
		SearchModel:        "gpt-4",

		Temperature:      0.7,
		MaxTokens:        4096,
		TopP:             1.0,
		PresencePenalty:  0.0,
		FrequencyPenalty: 0.0,

		OllamaServerURL: "http://localhost:11434",
	}
}

// Validate checks if the LLM configuration is valid
func (c *LLMConfig) Validate() error {
	if c.EditingModel == "" {
		return NewValidationError("editing_model", "cannot be empty")
	}

	if c.Temperature < 0.0 || c.Temperature > 2.0 {
		return NewValidationError("temperature", "must be between 0.0 and 2.0")
	}

	if c.MaxTokens < 1 || c.MaxTokens > 32768 {
		return NewValidationError("max_tokens", "must be between 1 and 32768")
	}

	if c.TopP < 0.0 || c.TopP > 1.0 {
		return NewValidationError("top_p", "must be between 0.0 and 1.0")
	}

	return nil
}

// GetPrimaryModel returns the most commonly used model for general operations
func (c *LLMConfig) GetPrimaryModel() string {
	if c.EditingModel != "" {
		return c.EditingModel
	}
	return "gpt-4" // fallback
}

// IsLocalModel returns true if the primary model is a local model
func (c *LLMConfig) IsLocalModel() bool {
	primary := c.GetPrimaryModel()
	return primary == c.LocalModel || c.OllamaServerURL != ""
}
