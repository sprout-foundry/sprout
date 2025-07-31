package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/apikeys" // New import
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts" // Import the new prompts package
	"github.com/alantheprice/ledit/pkg/utils"   // New import for EstimateTokens
)

var (
	// DefaultTokenLimit is the default token limit for API calls
	DefaultTokenLimit = prompts.DefaultTokenLimit
)

// --- Request/Response Structs for APIs ---

type OpenAIRequest struct {
	Model       string            `json:"model"`
	Messages    []prompts.Message `json:"messages"`
	Temperature float64           `json:"temperature"`
	Stream      bool              `json:"stream"`
}

type OpenAIResponse struct {
	Choices []struct {
		Delta prompts.Message `json:"delta"`
	} `json:"choices"`
}

type GeminiRequest struct {
	Contents         []GeminiContent `json:"contents"`
	GenerationConfig struct {
		Temperature float64 `json:"temperature"`
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

// --- API Callers ---

func callGeminiAPI(model string, messages []prompts.Message, timeout time.Duration, useSearchGrounding bool) (string, error) {
	apiKey, err := apikeys.GetAPIKey("gemini") // Use apikeys package
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
		// Note: For grounding to work, you should typically use a Gemini 1.5 model
		// (e.g., "gemini-1.5-flash" or "gemini-1.5-pro").
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

	// fmt.Printf("--- DEBUG: Sending Request ---\nURL: %s\nBody: %s\n---------------------------\n", apiURL, string(reqBody))

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

func callOpenAICompatibleStream(apiURL, apiKey, model string, messages []prompts.Message, timeout time.Duration, writer io.Writer) error {
	reqBody, err := json.Marshal(OpenAIRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0.0,
		Stream:      true,
	})
	if err != nil {
		fmt.Print(prompts.RequestMarshalError(err)) // Use prompt
		return err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Print(prompts.RequestCreationError(err)) // Use prompt
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Print(prompts.HTTPRequestError(err)) // Use prompt
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Print(prompts.APIError(string(body), resp.StatusCode)) // Use prompt
		return fmt.Errorf(prompts.APIError(string(body), resp.StatusCode))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			line = line[6:]
		}
		if line == "[DONE]" {
			break
		}

		var openAIResp OpenAIResponse
		if err := json.Unmarshal([]byte(line), &openAIResp); err != nil {
			// Don't print error for every line, just continue
			continue
		}

		if len(openAIResp.Choices) > 0 {
			content := openAIResp.Choices[0].Delta.Content
			if _, err := writer.Write([]byte(content)); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Print(prompts.ResponseBodyError(err)) // Use prompt
		return err
	}

	return nil
}

// --- Main Dispatcher ---

func GetOrchestrationPlan(cfg *config.Config, prompt, workspaceContext string) (string, error) {
	messages := prompts.BuildOrchestrationMessages(prompt, workspaceContext)
	// Using a longer timeout for planning
	modelName := cfg.OrchestrationModel
	if modelName == "" {
		modelName = cfg.EditingModel
		fmt.Print(prompts.NoOrchestrationModel(modelName)) // Use prompt
	}
	// Orchestration planning does not use search grounding
	_, response, err := GetLLMResponse(modelName, messages, "", cfg, 5*time.Minute, false)
	if err != nil {
		return "", err
	}

	// The response might be inside a code block, let's be robust.
	if strings.Contains(response, "```json") {
		parts := strings.SplitN(response, "```json", 2)
		if len(parts) > 1 {
			response = strings.Split(parts[1], "```")[0]
		} else if strings.HasPrefix(response, "```") && strings.HasSuffix(response, "```") {
			response = strings.TrimPrefix(response, "```")
			response = strings.TrimSuffix(response, "```")
		}
	}

	return strings.TrimSpace(response), nil
}

func GetLLMResponseStream(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, writer io.Writer, useSearchGrounding bool) (string, error) {
	var totalInputTokens int
	for _, msg := range messages {
		totalInputTokens += utils.EstimateTokens(msg.Content) // Use utils.EstimateTokens
	}
	fmt.Print(prompts.TokenEstimate(totalInputTokens, modelName)) // Use prompt
	if totalInputTokens > DefaultTokenLimit && !cfg.SkipPrompt {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print(prompts.TokenLimitWarning(totalInputTokens, DefaultTokenLimit)) // Use prompt
		confirm, err := reader.ReadString('\n')
		if err != nil {
			fmt.Print(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		if strings.TrimSpace(confirm) != "y" {
			fmt.Println(prompts.OperationCancelled()) // Use prompt
			return modelName, nil
		}
		fmt.Print(prompts.ContinuingRequest()) // Use prompt

		// User confirmed, continue with the request
	}

	var err error

	parts := strings.SplitN(modelName, ":", 2)
	provider := parts[0]
	model := ""
	if len(parts) > 1 {
		model = parts[1]
	}

	ollamaUrl := fmt.Sprintf("%s/v1/chat/completions", cfg.OllamaServerURL)

	switch provider {
	case "openai":
		apiKey, err := apikeys.GetAPIKey("openai") // Use apikeys package
		if err != nil {
			fmt.Print(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.openai.com/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "groq":
		apiKey, err := apikeys.GetAPIKey("groq") // Use apikeys package
		if err != nil {
			fmt.Print(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.groq.com/openai/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "gemini":
		// Gemini streaming not implemented, using non-streaming call and writing the whole response.
		var content string
		content, err = callGeminiAPI(model, messages, timeout, useSearchGrounding)
		if err == nil && content != "" {
			content = removeThinkTags(content)
			_, err = writer.Write([]byte(content))
		}
	case "lambda-ai":
		apiKey, err := apikeys.GetAPIKey("lambda-ai") // Use apikeys package
		if err != nil {
			fmt.Print(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.lambda.ai/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "cerebras":
		apiKey, err := apikeys.GetAPIKey("cerebras") // Use apikeys package
		if err != nil {
			fmt.Print(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.cerebras.ai/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "deepseek":
		apiKey, err := apikeys.GetAPIKey("deepseek") // Use apikeys package
		if err != nil {
			fmt.Print(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.deepseek.com/v1/chat/completions", apiKey, model, messages, timeout, writer)

	case "ollama":
		err = callOpenAICompatibleStream(ollamaUrl, "ollama", model, messages, timeout, writer)
	default:
		// Fallback to Ollama
		fmt.Println(prompts.ProviderNotRecognized()) // Use prompt
		modelName = cfg.LocalModel
		err = callOpenAICompatibleStream(ollamaUrl, "ollama", modelName, messages, timeout, writer)
	}

	if err != nil {
		fmt.Printf(prompts.LLMResponseError(err)) // Use prompt
		return modelName, err
	}

	return modelName, nil
}

func GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, useSearchGrounding bool) (string, string, error) {
	var contentBuffer strings.Builder
	// GetLLMResponseStream handles the token limit prompt and provider logic
	newModelName, err := GetLLMResponseStream(modelName, messages, filename, cfg, timeout, &contentBuffer, useSearchGrounding)
	if err != nil {
		// GetLLMResponseStream already prints the error if it happens
		return newModelName, "", err
	}

	// This can happen if user cancels at the prompt in GetLLMResponseStream
	if contentBuffer.Len() == 0 {
		return newModelName, "", nil
	}

	content := contentBuffer.String()

	// Remove any think tags before returning the content
	content = removeThinkTags(content)

	return newModelName, content, nil
}

// GenerateSearchQuery uses an LLM to generate a concise search query based on the provided context.
func GenerateSearchQuery(cfg *config.Config, context string) ([]string, error) {
	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at generating concise search queries to resolve software development issues. Your output should be a JSON array of 1 to 2 concise search queries (2-15 words each), based on the provided context. For example: `[\"query one\", \"query two\"]`"},
		{Role: "user", Content: fmt.Sprintf("Generate search queries based on the following context: %s", context)},
	}

	modelName := cfg.EditingModel // Use the editing model for generating search queries

	// Use a short timeout for generating a search query
	_, queryResponse, err := GetLLMResponse(modelName, messages, "", cfg, 30*time.Second, false) // Query generation does not use search grounding
	if err != nil {
		return nil, fmt.Errorf("failed to generate search query from LLM: %w", err)
	}

	// The response might be inside a code block, let's be robust.
	if strings.Contains(queryResponse, "```json") {
		parts := strings.SplitN(queryResponse, "```json", 2)
		if len(parts) > 1 {
			queryResponse = strings.Split(parts[1], "```")[0]
		} else if strings.HasPrefix(queryResponse, "```") && strings.HasSuffix(queryResponse, "```") {
			queryResponse = strings.TrimPrefix(queryResponse, "```")
			queryResponse = strings.TrimSuffix(queryResponse, "```")
		}
	}

	var searchQueries []string
	if err := json.Unmarshal([]byte(queryResponse), &searchQueries); err != nil {
		return nil, fmt.Errorf("failed to parse search queries from LLM response: %w, response: %s", err, queryResponse)
	}

	return searchQueries, nil
}
