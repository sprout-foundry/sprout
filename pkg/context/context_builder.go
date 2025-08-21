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
	logger := utils.GetLogger(cfg.SkipPrompt)
	var responses []string
	for _, req := range reqs {
		logger.Log(prompts.LLMContextRequest(req.Type, req.Query))
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
			logger.Log(prompts.LLMUserQuestion(req.Query))
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("failed to read user input: %w", err)
			}
			responses = append(responses, fmt.Sprintf("The user responded: %s", strings.TrimSpace(answer)))
		case "file":
			logger.Log(prompts.LLMFileRequest(req.Query))
			content, err := os.ReadFile(req.Query)
			if err != nil {
				return "", fmt.Errorf("failed to read file '%s': %w", req.Query, err)
			}
			responses = append(responses, fmt.Sprintf("Here is the content of the file `%s`:\n\n%s", req.Query, string(content)))
		case "edit_file_section":
			// Handle the edit_file_section context request
			// Parse the query parameters: file_path|instructions|target_section
			parts := strings.Split(req.Query, "|")
			var filePath, instructions, targetSection string

			for _, part := range parts {
				if strings.HasPrefix(part, "file_path=") {
					filePath = strings.TrimPrefix(part, "file_path=")
				} else if strings.HasPrefix(part, "instructions=") {
					instructions = strings.TrimPrefix(part, "instructions=")
				} else if strings.HasPrefix(part, "target_section=") {
					targetSection = strings.TrimPrefix(part, "target_section=")
				}
			}

			if strings.TrimSpace(filePath) == "" || strings.TrimSpace(instructions) == "" {
				responses = append(responses, "Error: edit_file_section requires both file_path and instructions parameters")
				break
			}

			// Try partial edit first, then fall back to full file edit
			logger := utils.GetLogger(cfg.SkipPrompt)
			logger.Logf("Processing edit_file_section context request: %s", filePath)

			var err error
			// Use simplified approach: direct LLM request with clear instructions
			var llmInstructions string
			if strings.TrimSpace(targetSection) != "" {
				llmInstructions = fmt.Sprintf("Edit the %s section with these instructions: %s", targetSection, instructions)
			} else {
				llmInstructions = instructions
			}

			// Use the standard LLM approach for all editing tasks
			messages := prompts.BuildPatchMessages("", llmInstructions, filePath, cfg.Interactive)
			_, _, err = llm.GetLLMResponse(cfg.EditingModel, messages, filePath, cfg, 6*time.Minute)

			if err != nil {
				responses = append(responses, fmt.Sprintf("Failed to edit file %s: %v", filePath, err))
			} else {
				responses = append(responses, fmt.Sprintf("Successfully edited file %s", filePath))
			}

		case "shell":
			shouldExecute := false
			if cfg.SkipPrompt {
				logger.Log(prompts.LLMShellSkippingPrompt())
				riskAnalysis, err := GetScriptRiskAnalysis(cfg, req.Query) // Call to GetScriptRiskAnalysis remains unqualified as it's now in the same package
				if err != nil {
					responses = append(responses, fmt.Sprintf("Failed to get script risk analysis: %v. User denied execution.", err))
					logger.Log(prompts.LLMScriptAnalysisFailed(err))
					continue
				}

				// Define what "not risky" means. For now, a simple string check.
				// A more robust solution might involve a structured JSON response from the summary model.
				if strings.Contains(strings.ToLower(riskAnalysis), "not risky") || strings.Contains(strings.ToLower(riskAnalysis), "safe") {
					logger.Log(prompts.LLMScriptNotRisky())
					shouldExecute = true
				} else {
					logger.Log(prompts.LLMScriptRisky(riskAnalysis))
					// If risky, fall through to prompt the user
				}
			}

			if !shouldExecute { // If not already decided to execute (either skipPrompt was false, or it was risky)
				logger.Log(prompts.LLMShellWarning())
				logger.Log(prompts.LLMShellConfirmation())
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

func GetLLMCodeResponse(cfg *config.Config, code, instructions, filename, imagePath string) (string, string, *llm.TokenUsage, error) {
	// Debug: Log function entry
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.Logf("DEBUG: GetLLMCodeResponse called with model: %s", cfg.EditingModel)
	logger.Logf("DEBUG: OrchestrationModel: %s", cfg.OrchestrationModel)
	logger.Logf("DEBUG: Interactive: %t", cfg.Interactive)
	logger.Logf("DEBUG: CodeToolsEnabled: %t", cfg.CodeToolsEnabled)

	// Routing: select models by task type and approx size

	modelName := cfg.EditingModel
	reason := "direct routing"
	logger.Log(prompts.UsingModel(modelName))
	logger.Log("=== GetLLMCodeResponse Debug ===")
	logger.Log(fmt.Sprintf("Model: %s", modelName))
	logger.Log(fmt.Sprintf("Filename: %s", filename))
	logger.Log(fmt.Sprintf("Routing: approxSize=%d editing=%s reason=%s", len(code), modelName, reason))
	logger.Log(fmt.Sprintf("Interactive: %t", cfg.Interactive))
	logger.Log(fmt.Sprintf("Instructions length: %d chars", len(instructions)))
	logger.Log(fmt.Sprintf("Code length: %d chars", len(code)))
	logger.Log(fmt.Sprintf("ImagePath: %s", imagePath))

	messages := prompts.BuildCodeMessagesWithFormat(code, instructions, filename, cfg.Interactive, true)
	logger.Log(fmt.Sprintf("Built %d messages", len(messages)))

	// Add image to the user message if provided
	if imagePath != "" {
		// Find the last user message and add the image to it
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				if err := llm.AddImageToMessage(&messages[i], imagePath); err != nil {
					return modelName, "", nil, fmt.Errorf("failed to add image to message: %w. Please ensure the image file exists and is in a supported format (JPEG, PNG, GIF, WebP)", err)
				}
				logger.Logf("Added image to message. Note: If the model doesn't support vision, the request may fail. Consider using a vision-capable model like 'openai:gpt-4o', 'gemini:gemini-1.5-flash', or 'anthropic:claude-3-sonnet'.")
				break
			}
		}
	}

	logger.Logf("DEBUG: Finished image handling")
	logger.Log(fmt.Sprintf("DEBUG: Interactive=%t, CodeToolsEnabled=%t", cfg.Interactive, cfg.CodeToolsEnabled))
	logger.Logf("DEBUG: About to check condition: !%t || !%t = %t", cfg.Interactive, cfg.CodeToolsEnabled, !cfg.Interactive || !cfg.CodeToolsEnabled)
	if !cfg.Interactive || !cfg.CodeToolsEnabled {
		if !cfg.Interactive {
			logger.Log("Taking non-interactive path without tool calling (cost optimization)")
		} else {
			logger.Log("Tools disabled for code flow. Ignoring any tool_calls; returning code only.")
		}
		// For non-interactive mode (like agent mode), use the standard LLM response without tool calling
		// This prevents expensive context requests and forces the model to provide code directly
		response, tokenUsage, err := llm.GetLLMResponse(modelName, messages, filename, cfg, 6*time.Minute)
		if err != nil {
			logger.Log(fmt.Sprintf("Non-interactive LLM call failed: %v", err))
			return modelName, "", nil, err
		}
		// Strip tool_calls blocks if present
		response = prompts.StripToolCallsIfPresent(response)
		logger.Log(fmt.Sprintf("Non-interactive response length: %d chars", len(response)))
		logger.Log("=== End GetLLMCodeResponse Debug ===")
		return modelName, response, tokenUsage, nil
	}

	logger.Log("Taking interactive path with enhanced tool calling support")
	logger.Logf("DEBUG: Taking interactive path - about to call new function")

	// Check if this is an agent workflow by looking for environment variable
	isAgentMode := os.Getenv("LEDIT_FROM_AGENT") == "1"
	logger.Logf("DEBUG: Environment variable LEDIT_FROM_AGENT = '%s', isAgentMode = %t", os.Getenv("LEDIT_FROM_AGENT"), isAgentMode)

	if isAgentMode {
		logger.Logf("DEBUG: Using unified interactive LLM handler for agent workflow")

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

		// Set the global context handler for tool execution
		llm.SetGlobalContextHandler(contextHandlerWrapper)

		// Use code-editing workflow context for code command
		workflowContext := llm.GetCodeEditingWorkflowContext()
		workflowContext.ContextHandler = contextHandlerWrapper

		// Create unified interactive config
		unifiedConfig := &llm.UnifiedInteractiveConfig{
			ModelName:       cfg.EditingModel,
			Messages:        messages,
			Filename:        filename,
			WorkflowContext: workflowContext,
			Config:          cfg,
			Timeout:         6 * time.Minute,
		}

		var response string
		var tokenUsage *llm.TokenUsage
		var err error
		_, response, tokenUsage, err = llm.CallLLMWithUnifiedInteractive(unifiedConfig)
		logger.Logf("DEBUG: Unified interactive call completed")
		if err != nil {
			logger.Log(fmt.Sprintf("Interactive LLM call failed: %v", err))
			return modelName, "", nil, err
		}
		logger.Log(fmt.Sprintf("Interactive response length: %d chars", len(response)))
		logger.Log("=== End GetLLMCodeResponse Debug ===")
		return modelName, response, tokenUsage, nil
	} else {
		logger.Logf("DEBUG: Using unified interactive approach for code workflow")

		// Extract instructions from the last user message
		if instructions == "" {
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "user" {
					if content, ok := messages[i].Content.(string); ok {
						instructions = content
						break
					}
				}
			}
		}

		if instructions == "" {
			return modelName, "", nil, fmt.Errorf("no instructions found in messages")
		}

		// Use the unified interactive approach for both agent and regular modes
		// This ensures tool calling works consistently across both modes
		logger.Logf("🎯 Using unified interactive approach for %s (with tool support)", filename)

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

		// Set the global context handler for tool execution
		llm.SetGlobalContextHandler(contextHandlerWrapper)

		// Use code-editing workflow context for code command
		workflowContext := llm.GetCodeEditingWorkflowContext()
		workflowContext.ContextHandler = contextHandlerWrapper

		// Create unified interactive config
		unifiedConfig := &llm.UnifiedInteractiveConfig{
			ModelName:       cfg.EditingModel,
			Messages:        messages,
			Filename:        filename,
			WorkflowContext: workflowContext,
			Config:          cfg,
			Timeout:         6 * time.Minute,
		}

		var response string
		var tokenUsage *llm.TokenUsage
		var err error
		_, response, tokenUsage, err = llm.CallLLMWithUnifiedInteractive(unifiedConfig)
		logger.Logf("DEBUG: Unified interactive call completed")
		if err != nil {
			logger.Log(fmt.Sprintf("Interactive LLM call failed: %v", err))
			return modelName, "", nil, err
		}
		logger.Logf("DEBUG: Direct code editing call completed")
		logger.Log(fmt.Sprintf("Interactive response length: %d chars", len(response)))
		logger.Log("=== End GetLLMCodeResponse Debug ===")
		return modelName, response, tokenUsage, nil
	}
}

// GetScriptRiskAnalysis sends a shell script to the summary model for risk analysis.
func GetScriptRiskAnalysis(cfg *config.Config, scriptContent string) (string, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	messages := prompts.BuildScriptRiskAnalysisMessages(scriptContent)
	modelName := cfg.SummaryModel // Use the summary model for this task
	if modelName == "" {
		// Fallback if summary model is not configured
		modelName = cfg.EditingModel
		logger.Log(prompts.NoSummaryModelFallback(modelName)) // New prompt
	}

	response, _, err := llm.GetLLMResponse(modelName, messages, "", cfg, 1*time.Minute) // Analysis does not use search grounding
	if err != nil {
		return "", fmt.Errorf("failed to get script risk analysis from LLM: %w", err)
	}

	return strings.TrimSpace(response), nil
}
