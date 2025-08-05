package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/apikeys" // Import the apikeys package
	"github.com/alantheprice/ledit/pkg/prompts"
)

func callGeminiAPI(model string, messages []prompts.Message, timeout time.Duration, useSearchGrounding bool) (string, error) {
	// Pass 'false' for interactive, as API calls should typically not prompt the user directly.
	apiKey, err := apikeys.GetAPIKey("gemini", false) // Use apikeys package and pass false for interactive
	if err != nil {
		fmt.Print(prompts.APIKeyError(err)) // Use prompt
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
				{Text: msg.Content},
			},
		})
	}

	reqBodyStruct := GeminiRequest{
		Contents: geminiContents,
		GenerationConfig: struct {
			Temperature float64 `json:"temperature"`
		}{
			Temperature: 0.0,
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
		fmt.Print(prompts.RequestMarshalError(err)) // Use prompt
		return "", err
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Print(prompts.HTTPRequestError(err)) // Use prompt
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Print(prompts.ResponseBodyError(err)) // Use prompt
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Print(prompts.APIError(string(body), resp.StatusCode)) // Use prompt
		return "", fmt.Errorf(prompts.APIError(string(body), resp.StatusCode))
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		fmt.Print(prompts.ResponseUnmarshalError(err)) // Use prompt
		return "", err
	}

	if len(geminiResp.Candidates) > 0 {
		candidate := geminiResp.Candidates[0] // Assuming we process the first candidate

		// Print grounding metadata if available
		if candidate.GroundingMetadata != nil {
			if len(candidate.GroundingMetadata.WebSearchQueries) > 0 {
				fmt.Println("\n--- Grounding Details (Web Search Queries) ---")
				for _, query := range candidate.GroundingMetadata.WebSearchQueries {
					fmt.Printf("  Query: %s\n", query)
				}
			}
			if len(candidate.GroundingMetadata.GroundingChunks) > 0 {
				fmt.Println("\n--- Grounding Details (Sources) ---")
				for i, chunk := range candidate.GroundingMetadata.GroundingChunks {
					fmt.Printf("  [%d] Title: %s, URI: %s\n", i+1, chunk.Title, chunk.URI)
				}
			}
		}

		// Print citation metadata if available
		if candidate.CitationMetadata != nil && len(candidate.CitationMetadata.Citations) > 0 {
			fmt.Println("\n--- Citations ---")
			for _, citation := range candidate.CitationMetadata.Citations {
				fmt.Printf("  Text Span: %d-%d, Title: %s, URI: %s\n",
					citation.StartIndex, citation.EndIndex, citation.Title, citation.URI)
			}
		}
		// Add a separator for clarity after grounding/citation details
		if candidate.GroundingMetadata != nil || (candidate.CitationMetadata != nil && len(candidate.CitationMetadata.Citations) > 0) {
			fmt.Println("----------------------------------------")
		}

		if len(candidate.Content.Parts) > 0 {
			return strings.TrimSpace(candidate.Content.Parts[0].Text), nil
		}
	}

	fmt.Println(prompts.NoGeminiContent()) // Use prompt
	return "", fmt.Errorf("no content in response")
}