package types

// TokenUsage represents actual token usage from an API response
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ModelPricing represents cost per 1K tokens for different models
type ModelPricing struct {
	InputCostPer1K  float64 // Cost per 1K input tokens
	OutputCostPer1K float64 // Cost per 1K output tokens
}

// PricingTable holds per-model pricing that can be loaded from disk
type PricingTable struct {
	Models map[string]ModelPricing `json:"models"`
}
