package context // Changed from contexthandler to context

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"    // Import the new prompts package
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

func GetLLMCodeResponse(cfg *config.Config, code, instructions, filename string, useGeminiSearchGrounding bool) (string, string, error) {
	modelName := cfg.EditingModel
	fmt.Print(prompts.UsingModel(modelName))

	messages := prompts.BuildCodeMessages(code, instructions, filename, cfg.Interactive)

	if !cfg.Interactive {
		_, response, err := llm.GetLLMResponse(modelName, messages, filename, cfg, 6*time.Minute, useGeminiSearchGrounding)
		if err != nil {
			return modelName, "", err
		}
		return modelName, response, nil
	}

	const maxContextRequests = 5
	contextRequestCount := 0

	for {
		if contextRequestCount >= maxContextRequests {
			fmt.Println(prompts.LLMMaxContextRequestsReached()) // Use prompt
			// Reset the system message to the base code generation prompt, forcing the LLM to generate code.
			messages[0] = prompts.Message{Role: "system", Content: prompts.GetBaseCodeGenSystemMessage()}
		}

		// Default timeout for code generation is 6 minutes
		_, response, err := llm.GetLLMResponse(modelName, messages, filename, cfg, 6*time.Minute, useGeminiSearchGrounding)
		if err != nil {
			return modelName, "", err
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
		} else if strings.Contains(response, "context_requests") {
			// Handle raw JSON
			jsonStr = response
		}

		if jsonStr != "" {
			// Clean up the JSON string by removing any remaining backticks or whitespace
			jsonStr = strings.TrimSpace(jsonStr)
			jsonStr = strings.Trim(jsonStr, "`")

			var contextResponse ContextResponse
			if err := json.Unmarshal([]byte(jsonStr), &contextResponse); err != nil {
				fmt.Print(prompts.LLMContextParseError(err, response)) // Use prompt
				return modelName, response, nil                        // Return the raw response if parsing fails
			}
			if len(contextResponse.ContextRequests) == 0 {
				fmt.Println(prompts.LLMNoContextRequests()) // Use prompt
				return modelName, response, nil             // No context requests, return the response
			}
			fmt.Print(prompts.LLMContextRequestsFound(len(contextResponse.ContextRequests))) // Use prompt
			contextRequestCount++
			additionalContext, err := handleContextRequest(contextResponse.ContextRequests, cfg)
			if err != nil {
				fmt.Print(prompts.LLMContextRequestError(err)) // Use prompt
				messages = append(messages, prompts.Message{
					Role:    "assistant",
					Content: response,
				}, prompts.Message{
					Role:    "user",
					Content: fmt.Sprintf("There was an error handling your request: %v. Please try a different request or generate the code.", err),
				})
			} else {
				fmt.Print(prompts.LLMAddingContext(additionalContext)) // Use prompt
				messages = append(messages, prompts.Message{
					Role:    "assistant",
					Content: response,
				}, prompts.Message{
					Role:    "user",
					Content: additionalContext,
				})
			}
			continue // Loop to ask LLM again with new context
		} else {
			return modelName, response, nil // No context requests, return the response
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
		fmt.Printf(prompts.NoSummaryModelFallback(modelName)) // New prompt
	}

	_, response, err := llm.GetLLMResponse(modelName, messages, "", cfg, 1*time.Minute, false) // Analysis does not use search grounding
	if err != nil {
		return "", fmt.Errorf("failed to get script risk analysis from LLM: %w", err)
	}

	return strings.TrimSpace(response), nil
}
