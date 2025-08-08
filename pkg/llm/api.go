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

// --- Main Dispatcher ---

func GetLLMResponseStream(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, writer io.Writer, imagePath ...string) (string, error) {
	var totalInputTokens int
	for _, msg := range messages {
		totalInputTokens += utils.EstimateTokens(GetMessageText(msg.Content)) // Use GetMessageText helper
	}
	fmt.Print(prompts.TokenEstimate(totalInputTokens, modelName))
	if totalInputTokens > DefaultTokenLimit && !cfg.SkipPrompt {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print(prompts.TokenLimitWarning(totalInputTokens, DefaultTokenLimit))
		confirm, err := reader.ReadString('\n')
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return modelName, err
		}
		if strings.TrimSpace(confirm) != "y" {
			fmt.Println(prompts.OperationCancelled())
			return modelName, nil
		}
		fmt.Print(prompts.ContinuingRequest())

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
		apiKey, err := apikeys.GetAPIKey("openai", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.openai.com/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "groq":
		apiKey, err := apikeys.GetAPIKey("groq", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.groq.com/openai/v1/chat/completions", apiKey, model, messages, timeout, writer)
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
	case "lambda-ai":
		apiKey, err := apikeys.GetAPIKey("lambda-ai", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.lambda.ai/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "cerebras":
		apiKey, err := apikeys.GetAPIKey("cerebras", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.cerebras.ai/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "deepseek":
		apiKey, err := apikeys.GetAPIKey("deepseek", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.deepseek.com/v1/chat/completions", apiKey, model, messages, timeout, writer)
	case "deepinfra":
		apiKey, err := apikeys.GetAPIKey("deepinfra", cfg.Interactive) // Pass cfg.Interactive
		if err != nil {
			fmt.Print(prompts.APIKeyError(err))
			return modelName, err
		}
		err = callOpenAICompatibleStream("https://api.deepinfra.com/v1/openai/chat/completions", apiKey, model, messages, timeout, writer)

	case "ollama":
		err = callOllamaAPI(model, messages, cfg, timeout, writer)
	default:
		// Fallback to openai-compatible ollama api
		fmt.Println(prompts.ProviderNotRecognized())
		modelName = cfg.LocalModel
		err = callOpenAICompatibleStream(ollamaUrl, "ollama", modelName, messages, timeout, writer)
	}

	if err != nil {
		fmt.Printf(prompts.LLMResponseError(err))
		return modelName, err
	}

	return modelName, nil
}

func GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, imagePath ...string) (string, string, error) {
	var contentBuffer strings.Builder
	// GetLLMResponseStream handles the token limit prompt and provider logic
	newModelName, err := GetLLMResponseStream(modelName, messages, filename, cfg, timeout, &contentBuffer, imagePath...)
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
	_, queryResponse, err := GetLLMResponse(modelName, messages, "", cfg, 30*time.Second) // Query generation does not use search grounding
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

	_, response, err := GetLLMResponse(modelName, messages, "", cfg, 1*time.Minute) // Analysis does not use search grounding
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
	_, response, err := GetLLMResponse(modelName, messages, "", cfg, 3*time.Minute) // No search grounding for this planning step
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
	modelName := cfg.OrchestrationModel
	if modelName == "" {
		modelName = cfg.EditingModel // Fallback
	}

	messages := prompts.BuildCodeReviewMessages(combinedDiff, originalPrompt, workspaceContext)

	_, response, err := GetLLMResponse(modelName, messages, "", cfg, 3*time.Minute)
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

	var reviewResult types.CodeReviewResult
	if err := json.Unmarshal([]byte(jsonStr), &reviewResult); err != nil {
		return nil, fmt.Errorf("failed to parse code review JSON from LLM response: %w\nResponse was: %s", err, response)
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

	_, response, err := GetLLMResponse(modelName, messages, "", cfg, 3*time.Minute)
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
