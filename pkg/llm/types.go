package llm

import (
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/types"
)

// ModelPricing represents cost per 1K tokens for different models
type ModelPricing = types.ModelPricing

// TokenUsage represents token usage from LLM responses
type TokenUsage = types.TokenUsage

// PricingTable holds per-model pricing that can be loaded from disk
type PricingTable = types.PricingTable

// OpenAIRequest represents a request to OpenAI-compatible APIs
type OpenAIRequest struct {
	Model       string            `json:"model"`
	Messages    []prompts.Message `json:"messages"`
	Temperature float64           `json:"temperature"`
	Stream      bool              `json:"stream"`
}

// OpenAIResponse represents a streaming response from OpenAI-compatible APIs
type OpenAIResponse struct {
	Choices []struct {
		Delta struct {
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"` // For streaming, content is always a string
		} `json:"delta"`
	} `json:"choices"`
}

// OpenAIUsageResponse represents the final response with usage information
type OpenAIUsageResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage TokenUsage `json:"usage"`
}

// GeminiRequest represents a request to Gemini API
type GeminiRequest struct {
	Contents         []GeminiContent `json:"contents"`
	GenerationConfig struct {
		Temperature     float64  `json:"temperature"`
		MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
		TopP            float64  `json:"topP,omitempty"`
		StopSequences   []string `json:"stopSequences,omitempty"`
	} `json:"generationConfig"`
	Tools []GeminiTool `json:"tools,omitempty"` // Add this for search grounding
}

// GeminiContent represents content in a Gemini request/response
type GeminiContent struct {
	Role  string `json:"role"`
	Parts []struct {
		Text string `json:"text"`
	} `json:"parts"`
}

// GeminiTool defines the structure for enabling tools, specifically Google Search Retrieval.
type GeminiTool struct {
	GoogleSearch struct{} `json:"googleSearch"`
}

// GeminiResponse defines the overall structure of the Gemini API response.
type GeminiResponse struct {
	Candidates []struct {
		Content           GeminiContent            `json:"content"`
		GroundingMetadata *GeminiGroundingMetadata `json:"groundingMetadata,omitempty"` // Added for grounding info
		CitationMetadata  *GeminiCitationMetadata  `json:"citationMetadata,omitempty"`  // Added for citations
	} `json:"candidates"`
	UsageMetadata *GeminiUsageMetadata `json:"usageMetadata,omitempty"` // Add usage metadata
}

// GeminiUsageMetadata holds token usage information from Gemini API
type GeminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// GeminiGroundingMetadata holds information about the web search queries and retrieved chunks.
type GeminiGroundingMetadata struct {
	WebSearchQueries []string `json:"webSearchQueries,omitempty"`
	GroundingChunks  []struct {
		URI   string `json:"uri"`
		Title string `json:"title"`
	} `json:"groundingChunks,omitempty"`
}

// GeminiCitationMetadata holds citation details from grounded responses.
type GeminiCitationMetadata struct {
	Citations []struct {
		StartIndex int    `json:"startIndex"`
		EndIndex   int    `json:"endIndex"`
		URI        string `json:"uri"`
		Title      string `json:"title"`
	} `json:"citations"`
}
