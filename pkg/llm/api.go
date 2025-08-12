package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/apikeys"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

var (
	// DefaultTokenLimit is the default token limit for API calls
	DefaultTokenLimit = prompts.DefaultTokenLimit
)

// GetLLMResponseWithTools makes an LLM call with tool calling support
func GetLLMResponseWithTools(modelName string, messages []prompts.Message, systemPrompt string, cfg *config.Config, timeout time.Duration) (string, error) {
	// Import the orchestration package functions here to avoid circular import
	// This is a temporary solution - in a full implementation, you'd refactor the architecture
	response, _, err := GetLLMResponse(modelName, messages, systemPrompt, cfg, timeout)
	return response, err
}

// --- Main Dispatcher ---

func GetLLMResponseStream(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, writer io.Writer, imagePath ...string) (*TokenUsage, error) {
	var totalInputTokens int
	for _, msg := range messages {
		totalInputTokens += EstimateTokens(GetMessageText(msg.Content)) // Use GetMessageText helper
	}
	fmt.Print(prompts.TokenEstimate(totalInputTokens, modelName))
	if totalInputTokens > DefaultTokenLimit && !cfg.SkipPrompt {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print(prompts.TokenLimitWarning(totalInputTokens, DefaultTokenLimit))
		confirm, err := reader.ReadString('\n')
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		if strings.TrimSpace(confirm) != "y" {
			fmt.Println(prompts.OperationCancelled())
			return nil, nil
		}
		fmt.Print(prompts.ContinuingRequest())

		// User confirmed, continue with the request
	}

	var err error
	var tokenUsage *TokenUsage

	parts := strings.SplitN(modelName, ":", 3) // Changed from 2 to 3
	provider := parts[0]
	model := ""
	if len(parts) > 1 {
		model = parts[1]
	}
	if len(parts) > 2 { // If there are 3 parts, the last one is the model
		model = parts[2]
	}

	ollamaUrl := fmt.Sprintf("%s/v1/chat/completions", cfg.OllamaServerURL)

	switch provider {
	case "openai":
		apiKey, err := apikeys.GetAPIKey("openai", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.openai.com/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "groq":
		apiKey, err := apikeys.GetAPIKey("groq", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.groq.com/openai/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "gemini":
		// Gemini streaming not implemented, using non-streaming call and writing the whole response.
		var content string
		content, err = callGeminiAPI(model, messages, timeout, false) // Removed undefined useSearchGrounding variable
		if err == nil && content != "" {
			logger := utils.GetLogger(cfg.SkipPrompt)
			logger.Log(fmt.Sprintf("Gemini API response: %s", content)) // Log the response
			content = removeThinkTags(content)
			_, err = writer.Write([]byte(content))
		}
		// Estimate token usage for Gemini
		if err == nil {
			tokenUsage = estimateUsageFromMessages(messages)
		}
	case "lambda-ai":
		apiKey, err := apikeys.GetAPIKey("lambda-ai", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.lambda.ai/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "cerebras":
		apiKey, err := apikeys.GetAPIKey("cerebras", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.cerebras.ai/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "deepseek":
		apiKey, err := apikeys.GetAPIKey("deepseek", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.deepseek.com/openai/v1/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "deepinfra":
		apiKey, err := apikeys.GetAPIKey("deepinfra", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream("https://api.deepinfra.com/v1/openai/chat/completions", apiKey, model, messages, cfg, timeout, writer)
	case "custom": // New case for custom provider:url:model
		var endpointURL string

		customParts := strings.SplitN(modelName, ":", 4)

		if len(customParts) == 4 {
			// Format: custom:base_url:path_suffix:model
			endpointURL = customParts[1] + customParts[2]
			model = customParts[3]
		} else if len(customParts) == 3 {
			// Format: custom:full_url:model
			endpointURL = customParts[1]
			model = customParts[2]
		} else {
			err = fmt.Errorf("invalid model name format for 'custom' provider. Expected 'custom:base_url:path_suffix:model' or 'custom:full_url:model', got '%s'", modelName)
			fmt.Print(prompts.LLMResponseError(err))
			return nil, err
		}

		apiKey, err := apikeys.GetAPIKey("custom", cfg.Interactive) // Use "custom" as the provider for API key lookup
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return nil, err
		}
		tokenUsage, err = callOpenAICompatibleStream(endpointURL, apiKey, model, messages, cfg, timeout, writer)
	case "ollama":
		tokenUsage, err = callOllamaAPI(model, messages, cfg, timeout, writer)
	default:
		// Fallback to openai-compatible ollama api
		fmt.Println(prompts.ProviderNotRecognized())
		modelName = cfg.LocalModel
		tokenUsage, err = callOpenAICompatibleStream(ollamaUrl, "ollama", modelName, messages, cfg, timeout, writer)
	}

	if err != nil {
		fmt.Print(prompts.LLMResponseError(err))
		return tokenUsage, err
	}

	return tokenUsage, nil
}

func GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, imagePath ...string) (string, *TokenUsage, error) {
	var contentBuffer strings.Builder
	// GetLLMResponseStream handles the token limit prompt and provider logic
	tokenUsage, err := GetLLMResponseStream(modelName, messages, filename, cfg, timeout, &contentBuffer, imagePath...)
	if err != nil {
		// GetLLMResponseStream already prints the error if it happens
		return modelName, tokenUsage, err
	}

	// This can happen if user cancels at the prompt in GetLLMResponseStream
	if contentBuffer.Len() == 0 {
		return modelName, tokenUsage, nil
	}

	content := contentBuffer.String()

	// Remove any think tags before returning the content
	content = removeThinkTags(content)

	return content, tokenUsage, nil
}

// GenerateSearchQuery uses an LLM to generate a concise search query based on the provided context.
func GenerateSearchQuery(cfg *config.Config, context string) ([]string, error) {
	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at generating concise search queries to resolve software development issues. Your output should be a JSON array of 1 to 2 concise search queries (2-15 words each), based on the provided context. For example: `[\"query one\", \"query two\"]`"},
		{Role: "user", Content: fmt.Sprintf("Generate search queries based on the following context: %s", context)},
	}

	modelName := cfg.EditingModel // Use the editing model for generating search queries

	// Use a short timeout for generating a search query
	queryResponse, _, err := GetLLMResponse(modelName, messages, "", cfg, 30*time.Second) // Query generation does not use search grounding
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
	if err := json.Unmarshal([]byte([]byte(queryResponse)), &searchQueries); err != nil {
		return nil, fmt.Errorf("failed to parse search queries from LLM response: %w, response: %s", err, queryResponse)
	}

	return searchQueries, nil
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

	response, _, err := GetLLMResponse(modelName, messages, "", cfg, 1*time.Minute) // Analysis does not use search grounding
	if err != nil {
		return "", fmt.Errorf("failed to get script risk analysis from LLM: %w", err)
	}

	return strings.TrimSpace(response), nil
}

// GetChangesForRequirement asks the LLM to break down a high-level requirement into file-specific changes.
func GetChangesForRequirement(cfg *config.Config, requirementInstruction string, workspaceContext string) ([]types.OrchestrationChange, error) {

	modelName := cfg.OrchestrationModel
	if modelName == "" {
		modelName = cfg.EditingModel // Fallback to editing model if orchestration model is not configured
		fmt.Print(prompts.UsingModel(modelName))
	}
	fmt.Print(prompts.UsingModel(modelName))

	messages := prompts.BuildChangesForRequirementMessages(requirementInstruction, workspaceContext, cfg.Interactive)

	// Use a longer timeout for this, as it's a planning step
	response, _, err := GetLLMResponse(modelName, messages, "", cfg, 3*time.Minute) // No search grounding for this planning step
	if err != nil {
		return nil, fmt.Errorf("failed to get changes for requirement from LLM: %w", err)
	}

	if response == "" {
		return nil, fmt.Errorf("LLM returned an empty response for changes")
	}

	// Try to extract JSON from response (handles both raw JSON and code block JSON)
	var jsonStr string
	if strings.Contains(response, "```json") {
		// Handle code block JSON
		parts := strings.Split(response, "```json")
		if len(parts) > 1 {
			jsonPart := parts[1]
			end := strings.Index(jsonPart, "```")
			if end > 0 {
				jsonStr = strings.TrimSpace(jsonPart[:end])
			} else {
				jsonStr = strings.TrimSpace(jsonPart)
			}
		}
	} else if strings.Contains(response, `"changes"`) { // Heuristic to detect raw JSON
		jsonStr = response
	}

	if jsonStr == "" {
		return nil, fmt.Errorf("LLM response did not contain expected JSON for changes: %s", response)
	}

	var changesList types.OrchestrationChangesList
	if err := json.Unmarshal([]byte(jsonStr), &changesList); err != nil {
		return nil, fmt.Errorf("failed to parse changes JSON from LLM response: %w\nResponse was: %s", err, response)
	}

	return changesList.Changes, nil
}

// GetCodeReview asks the LLM to review a combined diff of changes against the original prompt.
func GetCodeReview(cfg *config.Config, combinedDiff, originalPrompt, workspaceContext string) (*types.CodeReviewResult, error) {
	// Use a dedicated CodeReviewModel if available, otherwise fall back to EditingModel
	modelName := cfg.CodeReviewModel
	if modelName == "" {
		modelName = cfg.EditingModel
	}

	messages := prompts.BuildCodeReviewMessages(combinedDiff, originalPrompt, workspaceContext)

	response, _, err := GetLLMResponse(modelName, messages, "", cfg, 3*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to get code review from LLM: %w", err)
	}

	if response == "" {
		return nil, fmt.Errorf("LLM returned an empty response for code review")
	}

	// Try to extract JSON from response (handles both raw JSON and code block JSON)
	var jsonStr string
	if strings.Contains(response, "```json") {
		parts := strings.Split(response, "```json")
		if len(parts) > 1 {
			jsonPart := parts[1]
			end := strings.Index(jsonPart, "```")
			if end > 0 {
				jsonStr = strings.TrimSpace(jsonPart[:end])
			} else {
				jsonStr = strings.TrimSpace(jsonPart)
			}
		}
	} else {
		// Simple heuristic to find the start of a JSON object
		start := strings.Index(response, "{")
		end := strings.LastIndex(response, "}")
		if start != -1 && end != -1 && end > start {
			jsonStr = response[start : end+1]
		} else {
			jsonStr = response
		}
	}

	// Extract JSON from response with improved error handling
	jsonStr, extractErr := extractJSONFromResponse(response)

	// Validate that we have a non-empty JSON string
	if jsonStr == "" {
		if extractErr != nil {
			return nil, fmt.Errorf("failed to extract JSON from LLM response: %w. Full response: %s", extractErr, response)
		}
		return nil, fmt.Errorf("failed to extract JSON from LLM response. Full response: %s", response)
	}

	// Add debug logging for JSON parsing issues
	if os.Getenv("DEBUG_JSON_PARSING") == "true" {
		fmt.Printf("DEBUG: Extracted JSON string: %s\n", jsonStr)
		fmt.Printf("DEBUG: JSON length: %d\n", len(jsonStr))
	}

	var reviewResult types.CodeReviewResult
	if err := json.Unmarshal([]byte(jsonStr), &reviewResult); err != nil {
		return nil, fmt.Errorf("failed to parse code review JSON from LLM response: %w\nExtracted JSON was: %s\nFull response was: %s", err, jsonStr, response)
	}

	return &reviewResult, nil
}

// GetStagedCodeReview performs a code review on staged Git changes using a human-readable prompt.
// This is specifically designed for the review-staged command.
func GetStagedCodeReview(cfg *config.Config, stagedDiff, reviewPrompt, workspaceContext string) (*types.CodeReviewResult, error) {
	modelName := cfg.EditingModel
	if modelName == "" {
		return nil, fmt.Errorf("no editing model specified in config")
	}

	// Build messages for the staged code review
	var messages []prompts.Message

	// Add system message with the review prompt
	messages = append(messages, prompts.Message{
		Role:    "system",
		Content: reviewPrompt,
	})

	// Add user message with the staged diff and optional workspace context
	userContent := fmt.Sprintf("Please review the following staged Git changes:\n\n```diff\n%s\n```", stagedDiff)
	if strings.TrimSpace(workspaceContext) != "" {
		userContent = fmt.Sprintf("Workspace Context:\n%s\n\n%s", workspaceContext, userContent)
	}

	messages = append(messages, prompts.Message{
		Role:    "user",
		Content: userContent,
	})

	response, _, err := GetLLMResponse(modelName, messages, "", cfg, 3*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to get staged code review from LLM: %w", err)
	}

	if response == "" {
		return nil, fmt.Errorf("LLM returned an empty response for staged code review")
	}

	// Parse the response to extract status and feedback
	// Since we're using a human-readable prompt, we need to parse the text response
	return parseStagedCodeReviewResponse(response)
}

// parseStagedCodeReviewResponse parses the human-readable code review response
func parseStagedCodeReviewResponse(response string) (*types.CodeReviewResult, error) {
	result := &types.CodeReviewResult{}

	// Look for status indicators in the response
	responseLower := strings.ToLower(response)

	if strings.Contains(responseLower, "status") && strings.Contains(responseLower, "approved") {
		result.Status = "approved"
	} else if strings.Contains(responseLower, "status") && strings.Contains(responseLower, "needs_revision") {
		result.Status = "needs_revision"
	} else if strings.Contains(responseLower, "status") && strings.Contains(responseLower, "rejected") {
		result.Status = "rejected"
	} else {
		// Default to needs_revision if we can't determine status
		result.Status = "needs_revision"
	}

	// The entire response is the feedback
	result.Feedback = strings.TrimSpace(response)

	// For rejected status, suggest a new prompt (this is a simple implementation)
	if result.Status == "rejected" {
		result.NewPrompt = "Please address the issues identified in the code review and resubmit the changes."
	}

	return result, nil
}

// extractJSONFromResponse extracts JSON from an LLM response that may contain markdown formatting
func extractJSONFromResponse(response string) (string, error) {
	// First try to extract from markdown code blocks
	if strings.Contains(response, "```json") {
		parts := strings.Split(response, "```json")
		if len(parts) > 1 {
			jsonPart := parts[1]
			end := strings.Index(jsonPart, "```")
			if end > 0 {
				jsonStr := strings.TrimSpace(jsonPart[:end])
				if jsonStr != "" {
					return jsonStr, nil
				}
			}
		}
	}

	// Try to find JSON object boundaries
	response = strings.TrimSpace(response)

	// Look for first opening brace
	start := strings.Index(response, "{")
	if start == -1 {
		return "", fmt.Errorf("no JSON object found (no opening brace)")
	}

	// Look for matching closing brace from the end
	end := strings.LastIndex(response, "}")
	if end == -1 || end <= start {
		return "", fmt.Errorf("no matching closing brace found")
	}

	// Extract the JSON substring
	jsonStr := strings.TrimSpace(response[start : end+1])

	// Validate it's not empty
	if jsonStr == "" {
		return "", fmt.Errorf("extracted JSON is empty")
	}

	// Quick validation - try to parse as JSON
	var test interface{}
	if err := json.Unmarshal([]byte(jsonStr), &test); err != nil {
		return "", fmt.Errorf("extracted string is not valid JSON: %w", err)
	}

	return jsonStr, nil
}