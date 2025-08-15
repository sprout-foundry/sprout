package context

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webcontent"
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
		ui.Out().Print(prompts.LLMContextRequest(req.Type, req.Query))
		switch req.Type {
		case "search":
			// Gate external web search behind config flag to avoid ungrounded context by default
			if !cfg.UseSearchGrounding {
				responses = append(responses, fmt.Sprintf("Web search disabled by configuration. Skipping search for '%s'.", req.Query))
				break
			}
			searchResult, err := webcontent.FetchContextFromSearch(req.Query, cfg)
			if err != nil {
				responses = append(responses, fmt.Sprintf("Failed to perform web search for '%s': %v", req.Query, err))
			} else if searchResult == "" {
				responses = append(responses, fmt.Sprintf("No relevant content found for search query: '%s'", req.Query))
			} else {
				responses = append(responses, fmt.Sprintf("Here are the search results for '%s':\n\n%s", req.Query, searchResult))
			}
		case "user_prompt":
			ui.Out().Print(prompts.LLMUserQuestion(req.Query))
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read user input: %w", err)
			}
			responses = append(responses, fmt.Sprintf("The user responded: %s", strings.TrimSpace(answer)))
		case "file":
			ui.Out().Print(prompts.LLMFileRequest(req.Query))
			content, err := os.ReadFile(req.Query)
			if err != nil {
				return "", fmt.Errorf("failed to read file '%s': %w", req.Query, err)
			}
			responses = append(responses, fmt.Sprintf("Here is the content of the file `%s`:\n\n%s", req.Query, string(content)))
		case "shell":
			shouldExecute := false
			if cfg.SkipPrompt {
				ui.Out().Print(prompts.LLMShellSkippingPrompt() + "\n")
				riskAnalysis, err := GetScriptRiskAnalysis(cfg, req.Query) // Call to GetScriptRiskAnalysis remains unqualified as it's now in the same package
				if err != nil {
					responses = append(responses, fmt.Sprintf("Failed to get script risk analysis: %v. User denied execution.", err))
					ui.Out().Print(prompts.LLMScriptAnalysisFailed(err) + "\n")
					continue
				}

				// Define what "not risky" means. For now, a simple string check.
				// A more robust solution might involve a structured JSON response from the summary model.
				if strings.Contains(strings.ToLower(riskAnalysis), "not risky") || strings.Contains(strings.ToLower(riskAnalysis), "safe") {
					ui.Out().Print(prompts.LLMScriptNotRisky() + "\n")
					shouldExecute = true
				} else {
					ui.Out().Print(prompts.LLMScriptRisky(riskAnalysis) + "\n")
					// If risky, fall through to prompt the user
				}
			}

			if !shouldExecute { // If not already decided to execute (either skipPrompt was false, or it was risky)
				ui.Out().Print(prompts.LLMShellWarning() + "\n")
				ui.Out().Print(prompts.LLMShellConfirmation())
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
	// Routing: select models by task type and approx size
	category := "code"
	lower := strings.ToLower(instructions)
	if strings.Contains(lower, "comment") || strings.Contains(lower, "summary") || strings.Contains(lower, "header") || strings.Contains(lower, "docs") {
		category = "docs"
	}
	// Use orchestration model for control turns, editing model for code output
	controlModel := cfg.OrchestrationModel
	if controlModel == "" {
		controlModel = cfg.EditingModel
	}
	modelName := cfg.EditingModel
	reason := "direct routing"
	ui.Out().Print(prompts.UsingModel(modelName))

	// Log key parameters
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.Log("=== GetLLMCodeResponse Debug ===")
	logger.Log(fmt.Sprintf("Model: %s", modelName))
	logger.Log(fmt.Sprintf("Filename: %s", filename))
	logger.Log(fmt.Sprintf("Routing: category=%s approxSize=%d control=%s editing=%s reason=%s", category, len(code), controlModel, modelName, reason))
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
				ui.Out().Printf("Added image to message. Note: If the model doesn't support vision, the request may fail. Consider using a vision-capable model like 'openai:gpt-4o', 'gemini:gemini-1.5-flash', or 'anthropic:claude-3-sonnet'.\n")
				break
			}
		}
	}

	if !cfg.Interactive || !cfg.CodeToolsEnabled {
		if !cfg.Interactive {
			logger.Log("Taking non-interactive path without tool calling (cost optimization)")
		} else {
			logger.Log("Tools disabled for code flow. Ignoring any tool_calls; returning code only.")
		}
		// For non-interactive mode (like agent mode), use the standard LLM response without tool calling
		// This prevents expensive context requests and forces the model to provide code directly
		response, _, err := llm.GetLLMResponse(modelName, messages, filename, cfg, 6*time.Minute)
		if err != nil {
			logger.Log(fmt.Sprintf("Non-interactive LLM call failed: %v", err))
			return modelName, "", err
		}
		// Strip tool_calls blocks if present
		response = prompts.StripToolCallsIfPresent(response)
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
	// Use small/fast model for control turns in interactive loop
	// Use routed control model for interactive loop
	response, err := llm.CallLLMWithInteractiveContext(controlModel, messages, filename, cfg, 6*time.Minute, contextHandlerWrapper)
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
		ui.Out().Print(prompts.NoSummaryModelFallback(modelName)) // New prompt
	}

	response, _, err := llm.GetLLMResponse(modelName, messages, "", cfg, 1*time.Minute) // Analysis does not use search grounding
	if err != nil {
		return "", fmt.Errorf("failed to get script risk analysis from LLM: %w", err)
	}

	return strings.TrimSpace(response), nil
}
