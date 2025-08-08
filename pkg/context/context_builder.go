package context // Changed from contexthandler to context

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
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
		logger.Log("Taking non-interactive path with tool calling support")
		// Create a wrapper to convert between context request types
		contextHandlerWrapper := func(llmRequests []llm.ContextRequest, cfg *config.Config) (string, error) {
			// Convert llm.ContextRequest to local ContextRequest
			var localRequests []ContextRequest
			for _, req := range llmRequests {
				localRequests = append(localRequests, ContextRequest{
					Type:  req.Type,
					Query: req.Query,
				})
			}
			return handleContextRequest(localRequests, cfg)
		}

		// Use the new tool calling system even for non-interactive mode
		response, err := llm.CallLLMWithInteractiveContext(modelName, messages, filename, cfg, 6*time.Minute, contextHandlerWrapper)
		if err != nil {
			logger.Log(fmt.Sprintf("Non-interactive LLM call failed: %v", err))
			return modelName, "", err
		}
		logger.Log(fmt.Sprintf("Non-interactive response length: %d chars", len(response)))
		logger.Log("=== End GetLLMCodeResponse Debug ===")
		return modelName, response, nil
	}

	logger.Log("Taking interactive path with enhanced tool calling support")

	// Create a wrapper to convert between context request types
	contextHandlerWrapper := func(llmRequests []llm.ContextRequest, cfg *config.Config) (string, error) {
		// Convert llm.ContextRequest to local ContextRequest
		var localRequests []ContextRequest
		for _, req := range llmRequests {
			localRequests = append(localRequests, ContextRequest{
				Type:  req.Type,
				Query: req.Query,
			})
		}
		return handleContextRequest(localRequests, cfg)
	}

	// Use the new tool calling system for interactive mode
	response, err := llm.CallLLMWithInteractiveContext(modelName, messages, filename, cfg, 6*time.Minute, contextHandlerWrapper)
	if err != nil {
		logger.Log(fmt.Sprintf("Interactive LLM call failed: %v", err))
		return modelName, "", err
	}

	logger.Log(fmt.Sprintf("Interactive response length: %d chars", len(response)))
	logger.Log("=== End GetLLMCodeResponse Debug ===")
	return modelName, response, nil
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
