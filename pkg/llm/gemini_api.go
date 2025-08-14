package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/apikeys"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
)

func callGeminiAPI(model string, messages []prompts.Message, timeout time.Duration, useSearchGrounding bool) (string, error) {
	// Pass 'false' for interactive, as API calls should typically not prompt the user directly.
	apiKey, err := apikeys.GetAPIKey("gemini", false)
	if err != nil {
		ui.Out().Print(prompts.APIKeyError(err))
		return "", err
	}
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)

	var geminiContents []GeminiContent
	for _, msg := range messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		geminiContents = append(geminiContents, GeminiContent{
			Role: role,
			Parts: []struct {
				Text string `json:"text"`
			}{
				{Text: GetMessageText(msg.Content)},
			},
		})
	}

	reqBodyStruct := GeminiRequest{
		Contents: geminiContents,
		GenerationConfig: struct {
			Temperature     float64  `json:"temperature"`
			MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
			TopP            float64  `json:"topP,omitempty"`
			StopSequences   []string `json:"stopSequences,omitempty"`
		}{
			Temperature:     0.1,                                  // Very low for consistency
			MaxOutputTokens: 4096,                                 // Limit output length
			TopP:            0.9,                                  // Focus on high-probability tokens
			StopSequences:   []string{"\n\n\n", "```\n\n", "END"}, // Stop sequences
		},
	}

	// Enable Google Search Retrieval tool if useSearchGrounding is true
	if useSearchGrounding {
		reqBodyStruct.Tools = []GeminiTool{
			{
				GoogleSearch: struct{}{},
			},
		}
	}

	reqBody, err := json.Marshal(reqBodyStruct)
	if err != nil {
		ui.Out().Print(prompts.RequestMarshalError(err))
		return "", err
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		ui.Out().Print(prompts.HTTPRequestError(err))
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ui.Out().Print(prompts.ResponseBodyError(err))
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		msg := prompts.APIError(string(body), resp.StatusCode)
		ui.Out().Print(msg)
		return "", fmt.Errorf("%s", msg)
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		ui.Out().Print(prompts.ResponseUnmarshalError(err))
		return "", err
	}

	if len(geminiResp.Candidates) > 0 {
		candidate := geminiResp.Candidates[0] // Assuming we process the first candidate

		// Print grounding metadata if available
		if candidate.GroundingMetadata != nil {
			if len(candidate.GroundingMetadata.WebSearchQueries) > 0 {
				ui.Out().Print("\n--- Grounding Details (Web Search Queries) ---\n")
				for _, query := range candidate.GroundingMetadata.WebSearchQueries {
					ui.Out().Printf("  Query: %s\n", query)
				}
			}
			if len(candidate.GroundingMetadata.GroundingChunks) > 0 {
				ui.Out().Print("\n--- Grounding Details (Sources) ---\n")
				for i, chunk := range candidate.GroundingMetadata.GroundingChunks {
					ui.Out().Printf("  [%d] Title: %s, URI: %s\n", i+1, chunk.Title, chunk.URI)
				}
			}
		}

		// Print citation metadata if available
		if candidate.CitationMetadata != nil && len(candidate.CitationMetadata.Citations) > 0 {
			ui.Out().Print("\n--- Citations ---\n")
			for _, citation := range candidate.CitationMetadata.Citations {
				ui.Out().Printf("  Text Span: %d-%d, Title: %s, URI: %s\n",
					citation.StartIndex, citation.EndIndex, citation.Title, citation.URI)
			}
		}
		// Add a separator for clarity after grounding/citation details
		if candidate.GroundingMetadata != nil || (candidate.CitationMetadata != nil && len(candidate.CitationMetadata.Citations) > 0) {
			ui.Out().Print("----------------------------------------\n")
		}

		if len(candidate.Content.Parts) > 0 {
			return strings.TrimSpace(candidate.Content.Parts[0].Text), nil
		}
	}

	ui.Out().Print(prompts.NoGeminiContent() + "\n")
	return "", fmt.Errorf("no content in response")
}
