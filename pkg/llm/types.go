package llm

import (
	"github.com/alantheprice/ledit/pkg/prompts"
)

type OpenAIRequest struct {
	Model       string            `json:"model"`
	Messages    []prompts.Message `json:"messages"`
	Temperature float64           `json:"temperature"`
	Stream      bool              `json:"stream"`
}

type OpenAIResponse struct {
	Choices []struct {
		Delta struct {
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"` // For streaming, content is always a string
		} `json:"delta"`
	} `json:"choices"`
}

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
