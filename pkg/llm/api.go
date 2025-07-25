package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"ledit/pkg/config"
	"ledit/pkg/prompts" // Import the new prompts package
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	// DefaultTokenLimit is the default token limit for API calls
	DefaultTokenLimit = 30000
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
}

type GeminiContent struct {
	Role  string `json:"role"`
	Parts []struct {
		Text string `json:"text"`
	} `json:"parts"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content GeminiContent `json:"content"`
	} `json:"candidates"`
}

// --- API Callers ---

func callOpenAICompatibleStream(apiURL, apiKey, model string, messages []prompts.Message, timeout time.Duration, writer io.Writer) error {
	reqBody, err := json.Marshal(OpenAIRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0.0,
		Stream:      true,
	})
	if err != nil {
		fmt.Printf(prompts.RequestMarshalError(err)) // Use prompt
		return err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Printf(prompts.RequestCreationError(err)) // Use prompt
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf(prompts.HTTPRequestError(err)) // Use prompt
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf(prompts.APIError(string(body), resp.StatusCode)) // Use prompt
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
		fmt.Printf(prompts.ResponseBodyError(err)) // Use prompt
		return err
	}

	return nil
}

func callGeminiAPI(model string, messages []prompts.Message, timeout time.Duration) (string, error) {
	apiKey, err := GetAPIKey("gemini")
	if err != nil {
		fmt.Printf(prompts.APIKeyError(err)) // Use prompt
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

	reqBody, err := json.Marshal(GeminiRequest{
		Contents: geminiContents,
		GenerationConfig: struct {
			Temperature float64 `json:"temperature"`
		}{
			Temperature: 0.0,
		},
	})
	if err != nil {
		fmt.Printf(prompts.RequestMarshalError(err)) // Use prompt
		return "", err
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(apiURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Printf(prompts.HTTPRequestError(err)) // Use prompt
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf(prompts.ResponseBodyError(err)) // Use prompt
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf(prompts.APIError(string(body), resp.StatusCode)) // Use prompt
		return "", fmt.Errorf(prompts.APIError(string(body), resp.StatusCode))
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		fmt.Printf(prompts.ResponseUnmarshalError(err)) // Use prompt
		return "", err
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		return strings.TrimSpace(geminiResp.Candidates[0].Content.Parts[0].Text), nil
	}

	fmt.Println(prompts.NoGeminiContent()) // Use prompt
	return "", fmt.Errorf("no content in response")
}

// --- Main Dispatcher ---

func GetOrchestrationPlan(cfg *config.Config, prompt, workspaceContext string) (string, error) {
	messages := prompts.BuildOrchestrationMessages(prompt, workspaceContext)
	// Using a longer timeout for planning
	modelName := cfg.OrchestrationModel
	if modelName == "" {
		modelName = cfg.EditingModel
		fmt.Printf(prompts.NoOrchestrationModel(modelName)) // Use prompt
	}
	_, response, err := GetLLMResponse(modelName, messages, "", cfg, 5*time.Minute)
	if err != nil {
		return "", err
	}

	// The response might be inside a code block, let's be robust.
	if strings.Contains(response, "```json") {
		parts := strings.SplitN(response, "```json", 2)
		if len(parts) > 1 {
			response = strings.Split(parts[1], "```")[0]
		}
	} else if strings.HasPrefix(response, "```") && strings.HasSuffix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		response = strings.TrimSuffix(response, "```")
	}

	return strings.TrimSpace(response), nil
}

func GetLLMResponseStream(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, writer io.Writer) (string, error) {
	var totalInputTokens int
	for _, msg := range messages {
		totalInputTokens += EstimateTokens(msg.Content)
	}
	fmt.Printf(prompts.TokenEstimate(totalInputTokens, modelName)) // Use prompt
	if totalInputTokens > DefaultTokenLimit && !cfg.SkipPrompt {
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf(prompts.TokenLimitWarning(totalInputTokens, DefaultTokenLimit)) // Use prompt
		confirm, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		if strings.TrimSpace(confirm) != "y" {
			fmt.Println(prompts.OperationCancelled()) // Use prompt
			return modelName, nil
		}
		fmt.Printf(prompts.ContinuingRequest()) // Use prompt

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
		apiKey, err := GetAPIKey("openai")
		if err != nil {
			fmt.Printf(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.openai.com/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "groq":
		apiKey, err := GetAPIKey("groq")
		if err != nil {
			fmt.Printf(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.groq.com/openai/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "gemini":
		// Gemini streaming not implemented, using non-streaming call and writing the whole response.
		var content string
		content, err = callGeminiAPI(model, messages, timeout)
		if err == nil && content != "" {
			content = removeThinkTags(content)
			_, err = writer.Write([]byte(content))
		}
	case "lambda-ai":
		apiKey, err := GetAPIKey("lambda-ai")
		if err != nil {
			fmt.Printf(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.lambda.ai/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "cerebras":
		apiKey, err := GetAPIKey("cerebras")
		if err != nil {
			fmt.Printf(prompts.APIKeyError(err)) // Use prompt
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.cerebras.ai/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "deepseek":
		apiKey, err := GetAPIKey("deepseek")
		if err != nil {
			fmt.Printf(prompts.APIKeyError(err)) // Use prompt
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

func GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration) (string, string, error) {
	var contentBuffer strings.Builder
	// GetLLMResponseStream handles the token limit prompt and provider logic
	newModelName, err := GetLLMResponseStream(modelName, messages, filename, cfg, timeout, &contentBuffer)
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

// GetScriptRiskAnalysis sends a shell script to the summary model for risk analysis.
func GetScriptRiskAnalysis(cfg *config.Config, scriptContent string) (string, error) {
	messages := prompts.BuildScriptRiskAnalysisMessages(scriptContent)
	modelName := cfg.SummaryModel // Use the summary model for this task
	if modelName == "" {
		// Fallback if summary model is not configured
		modelName = cfg.EditingModel
		fmt.Printf(prompts.NoSummaryModelFallback(modelName)) // New prompt
	}

	_, response, err := GetLLMResponse(modelName, messages, "", cfg, 1*time.Minute) // Use a shorter timeout for analysis
	if err != nil {
		return "", fmt.Errorf("failed to get script risk analysis from LLM: %w", err)
	}

	return strings.TrimSpace(response), nil
}
