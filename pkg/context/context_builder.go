package context // Changed from contexthandler to context

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp" // Added import for regexp package
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"    // Import the new prompts package
	"github.com/alantheprice/ledit/pkg/utils"      // Import utils for logging
	"github.com/alantheprice/ledit/pkg/webcontent" // Import webcontent package
)

// --- Message Structs ---

type ContextRequest struct {
	Type  string `json:"type"`
	Query string `json:"query"`
}

type ContextResponse struct {
	ContextRequests []ContextRequest `json:"context_requests"`
}

func handleContextRequest(reqs []ContextRequest, cfg *config.Config) (string, error) {
	var responses []string
	for _, req := range reqs {
		fmt.Print(prompts.LLMContextRequest(req.Type, req.Query)) // Use prompt
		switch req.Type {
		case "search":
			searchResult, err := webcontent.FetchContextFromSearch(req.Query, cfg)
			if err != nil {
				responses = append(responses, fmt.Sprintf("Failed to perform web search for '%s': %v", req.Query, err))
			} else if searchResult == "" {
				responses = append(responses, fmt.Sprintf("No relevant content found for search query: '%s'", req.Query))
			} else {
				responses = append(responses, fmt.Sprintf("Here are the search results for '%s':\n\n%s", req.Query, searchResult))
			}
		case "user_prompt":
			fmt.Print(prompts.LLMUserQuestion(req.Query)) // Use prompt
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read user input: %w", err)
			}
			responses = append(responses, fmt.Sprintf("The user responded: %s", strings.TrimSpace(answer)))
		case "file":
			fmt.Print(prompts.LLMFileRequest(req.Query))
			content, err := os.ReadFile(req.Query)
			if err != nil {
				return "", fmt.Errorf("failed to read file '%s': %w", req.Query, err)
			}
			responses = append(responses, fmt.Sprintf("Here is the content of the file `%s`:\n\n%s", req.Query, string(content)))
		case "shell":
			shouldExecute := false
			if cfg.SkipPrompt {
				fmt.Println(prompts.LLMShellSkippingPrompt())
				riskAnalysis, err := GetScriptRiskAnalysis(cfg, req.Query) // Call to GetScriptRiskAnalysis remains unqualified as it's now in the same package
				if err != nil {
					responses = append(responses, fmt.Sprintf("Failed to get script risk analysis: %v. User denied execution.", err))
					fmt.Println(prompts.LLMScriptAnalysisFailed(err))
					continue
				}

				// Define what "not risky" means. For now, a simple string check.
				// A more robust solution might involve a structured JSON response from the summary model.
				if strings.Contains(strings.ToLower(riskAnalysis), "not risky") || strings.Contains(strings.ToLower(riskAnalysis), "safe") {
					fmt.Println(prompts.LLMScriptNotRisky())
					shouldExecute = true
				} else {
					fmt.Println(prompts.LLMScriptRisky(riskAnalysis))
					// If risky, fall through to prompt the user
				}
			}

			if !shouldExecute { // If not already decided to execute (either skipPrompt was false, or it was risky)
				fmt.Println(prompts.LLMShellWarning())    // Use prompt
				fmt.Print(prompts.LLMShellConfirmation()) // Use prompt
				reader := bufio.NewReader(os.Stdin)
				confirm, _ := reader.ReadString('\n')
				if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
					responses = append(responses, "User denied execution of shell command.")
					continue
				}
				shouldExecute = true
			}

			if shouldExecute {
				cmd := exec.Command("sh", "-c", req.Query)
				output, err := cmd.CombinedOutput()
				if err != nil {
					responses = append(responses, fmt.Sprintf("Shell command failed with error: %v\nOutput:\n%s", err, string(output)))
				} else {
					responses = append(responses, fmt.Sprintf("The shell command `%s` produced the following output:\n\n%s", req.Query, string(output)))
				}
			}
		default:
			return "", fmt.Errorf("unknown context request type: %s", req.Type)
		}
	}
	return strings.Join(responses, "\n"), nil
}

func GetLLMCodeResponse(cfg *config.Config, code, instructions, filename, imagePath string) (string, string, error) {
	modelName := cfg.EditingModel
	fmt.Print(prompts.UsingModel(modelName))

	// Log key parameters
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.Log(fmt.Sprintf("=== GetLLMCodeResponse Debug ==="))
	logger.Log(fmt.Sprintf("Model: %s", modelName))
	logger.Log(fmt.Sprintf("Filename: %s", filename))
	logger.Log(fmt.Sprintf("Interactive: %t", cfg.Interactive))
	logger.Log(fmt.Sprintf("Instructions length: %d chars", len(instructions)))
	logger.Log(fmt.Sprintf("Code length: %d chars", len(code)))
	logger.Log(fmt.Sprintf("ImagePath: %s", imagePath))

	messages := prompts.BuildCodeMessages(code, instructions, filename, cfg.Interactive)
	logger.Log(fmt.Sprintf("Built %d messages", len(messages)))

	// Add image to the user message if provided
	if imagePath != "" {
		// Find the last user message and add the image to it
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				if err := llm.AddImageToMessage(&messages[i], imagePath); err != nil {
					return modelName, "", fmt.Errorf("failed to add image to message: %w. Please ensure the image file exists and is in a supported format (JPEG, PNG, GIF, WebP)", err)
				}
				fmt.Printf("Added image to message. Note: If the model doesn't support vision, the request may fail. Consider using a vision-capable model like 'openai:gpt-4o', 'gemini:gemini-1.5-flash', or 'anthropic:claude-3-sonnet'.\n")
				break
			}
		}
	}

	if !cfg.Interactive {
		logger.Log("Taking non-interactive path")
		_, response, err := llm.GetLLMResponse(modelName, messages, filename, cfg, 6*time.Minute, imagePath)
		if err != nil {
			logger.Log(fmt.Sprintf("Non-interactive LLM call failed: %v", err))
			return modelName, "", err
		}
		logger.Log(fmt.Sprintf("Non-interactive response length: %d chars", len(response)))
		logger.Log("=== End GetLLMCodeResponse Debug ===")
		return modelName, response, nil
	}

	logger.Log("Taking interactive path")
	const maxContextRequests = 5
	contextRequestCount := 0

	for {
		logger.Log(fmt.Sprintf("Interactive loop iteration %d", contextRequestCount))

		if contextRequestCount >= maxContextRequests {
			logger.Log("Maximum context requests reached, forcing code generation")
			fmt.Println(prompts.LLMMaxContextRequestsReached()) // Use prompt
			// Reset the system message to the base code generation prompt, forcing the LLM to generate code.
			messages[0] = prompts.Message{Role: "system", Content: prompts.GetBaseCodeGenSystemMessage()}
		}

		// Default timeout for code generation is 6 minutes
		logger.Log("Calling LLM for response...")
		_, response, err := llm.GetLLMResponse(modelName, messages, filename, cfg, 6*time.Minute, imagePath)
		if err != nil {
			logger.Log(fmt.Sprintf("LLM call failed: %v", err))
			return modelName, "", err
		}
		logger.Log(fmt.Sprintf("Got LLM response, length: %d chars", len(response)))

		var allContextRequests []ContextRequest
		var rawResponseForReturn string = response // Store the original response to return if no context requests

		logger.Log("Parsing response for context requests...")
		// Regex to find JSON objects, potentially preceded by [TOOL_CALLS] or within ```json
		// This regex tries to capture a JSON object. It's not perfect for nested structures but good for top-level.
		// It looks for { ... }
		jsonRegex := regexp.MustCompile("(?s)(?:\\[TOOL_CALLS\\]|```json\\s*)?({(?:[^{}]|{(?:[^{}]|{[^{}]*})*})*})")

		// Find all matches
		matches := jsonRegex.FindAllStringSubmatch(response, -1)
		logger.Log(fmt.Sprintf("Found %d potential JSON matches", len(matches)))

		for _, match := range matches {
			if len(match) > 1 {
				jsonCandidate := strings.TrimSpace(match[1])
				var tempContextResponse ContextResponse
				if err := json.Unmarshal([]byte(jsonCandidate), &tempContextResponse); err == nil {
					logger.Log(fmt.Sprintf("Successfully parsed context requests: %d requests", len(tempContextResponse.ContextRequests)))
					allContextRequests = append(allContextRequests, tempContextResponse.ContextRequests...)
					// If we successfully parsed a context request, the original response was a tool call,
					// so we should not return it as code.
					rawResponseForReturn = ""
				} else {
					logger.Log(fmt.Sprintf("Failed to parse JSON candidate: %v", err))
				}
			}
		}

		// If no specific JSON blocks were found by regex, try to parse the whole response as raw JSON
		if len(allContextRequests) == 0 && strings.Contains(response, "context_requests") {
			logger.Log("Trying to parse entire response as JSON...")
			var tempContextResponse ContextResponse
			if err := json.Unmarshal([]byte(strings.TrimSpace(response)), &tempContextResponse); err == nil {
				logger.Log(fmt.Sprintf("Successfully parsed entire response: %d requests", len(tempContextResponse.ContextRequests)))
				allContextRequests = append(allContextRequests, tempContextResponse.ContextRequests...)
				rawResponseForReturn = ""
			} else {
				logger.Log(fmt.Sprintf("Failed to parse entire response as JSON: %v", err))
			}
		}

		logger.Log(fmt.Sprintf("Total context requests found: %d", len(allContextRequests)))
		if len(allContextRequests) > 0 {
			fmt.Print(prompts.LLMContextRequestsFound(len(allContextRequests))) // Use prompt
			contextRequestCount++
			additionalContext, err := handleContextRequest(allContextRequests, cfg) // Pass the aggregated requests
			if err != nil {
				fmt.Print(prompts.LLMContextRequestError(err)) // Use prompt
				messages = append(messages, prompts.Message{
					Role:    "assistant",
					Content: response, // Original response from LLM
				}, prompts.Message{
					Role:    "user",
					Content: fmt.Sprintf("There was an error handling your request: %v. Please try a different request or generate the code.", err),
				})
			} else {
				fmt.Print(prompts.LLMAddingContext(additionalContext)) // Use prompt
				messages = append(messages, prompts.Message{
					Role:    "assistant",
					Content: response, // Original response from LLM
				}, prompts.Message{
					Role:    "user",
					Content: additionalContext,
				})
			}
			continue // Loop to ask LLM again with new context
		} else {
			logger.Log(fmt.Sprintf("No context requests found, returning response (length: %d)", len(rawResponseForReturn)))
			logger.Log("=== End GetLLMCodeResponse Debug ===")
			return modelName, rawResponseForReturn, nil // No context requests, return the (potentially empty) raw response
		}
	}
}

// GetScriptRiskAnalysis sends a shell script to the summary model for risk analysis.
func GetScriptRiskAnalysis(cfg *config.Config, scriptContent string) (string, error) {
	messages := prompts.BuildScriptRiskAnalysisMessages(scriptContent)
	modelName := cfg.SummaryModel // Use the summary model for this task
	if modelName == "" {
		// Fallback if summary model is not configured
		modelName = cfg.EditingModel
		fmt.Print(prompts.NoSummaryModelFallback(modelName)) // New prompt
	}

	_, response, err := llm.GetLLMResponse(modelName, messages, "", cfg, 1*time.Minute) // Analysis does not use search grounding
	if err != nil {
		return "", fmt.Errorf("failed to get script risk analysis from LLM: %w", err)
	}

	return strings.TrimSpace(response), nil
}
