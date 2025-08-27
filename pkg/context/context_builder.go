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
		startTool := time.Now()
		switch strings.ToLower(req.Type) {
		case "read_file":
			if content, err := os.ReadFile(req.Query); err == nil {
				responses = append(responses, fmt.Sprintf("Contents of %s:\n%s", req.Query, string(content)))
			} else {
				responses = append(responses, fmt.Sprintf("Failed to read file %s: %v", req.Query, err))
			}
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
			fileContent, readErr := os.ReadFile(filePath)
			if readErr != nil {
				responses = append(responses, fmt.Sprintf("Error: Failed to read file %s for editing: %v", filePath, readErr))
				break
			}
			fileContentStr := string(fileContent)
			messages := prompts.BuildPatchMessages(fileContentStr, llmInstructions, filePath, true)
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
		logger.Logf("TOOL DONE ← %s in %s", req.Type, time.Since(startTool))
	}
	return strings.Join(responses, "\n"), nil
}

func GetLLMCodeResponse(cfg *config.Config, code, instructions, filename, imagePath string) (string, string, *llm.TokenUsage, error) {
	// Debug: Log function entry
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.Logf("DEBUG: GetLLMCodeResponse called with model: %s", cfg.EditingModel)
	logger.Logf("DEBUG: OrchestrationModel: %s", cfg.OrchestrationModel)
	// Interactive tools forced globally
	logger.Log(fmt.Sprintf("DEBUG: Code length: %d chars", len(code)))
	logger.Log(fmt.Sprintf("ImagePath: %s", imagePath))

	// For agent workflow, use patch format but without interactive tools to avoid confusion
	_ = os.Getenv("LEDIT_FROM_AGENT") == "1"

	// Use quality-aware messages if quality level is set
	var messages []prompts.Message
	if cfg.QualityLevel > 0 {
		messages = buildQualityAwareCodeMessages(code, instructions, filename, true, true, cfg.QualityLevel)
	} else {
		messages = prompts.BuildCodeMessagesWithFormat(code, instructions, filename, true, true)
	}
	logger.Log(fmt.Sprintf("Built %d messages", len(messages)))

	// Add image to the user message if provided
	if imagePath != "" {
		// Find the last user message and add the image to it
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				if err := llm.AddImageToMessage(&messages[i], imagePath); err != nil {
					return cfg.EditingModel, "", nil, fmt.Errorf("failed to add image to message: %w. Please ensure the image file exists and is in a supported format (JPEG, PNG, GIF, WebP)", err)
				}
				logger.Logf("Added image to message. Note: If the model doesn't support vision, the request may fail. Consider using a vision-capable model like 'openai:gpt-4o', 'gemini:gemini-1.5-flash', or 'anthropic:claude-3-sonnet'.")
				break
			}
		}
	}

	logger.Logf("DEBUG: Finished image handling")
	// Interactive tools forced globally
	logger.Log("Checking if interactive path should be used")

	// Check if this is an agent workflow by looking for environment variable
	isAgentMode := os.Getenv("LEDIT_FROM_AGENT") == "1"
	logger.Logf("DEBUG: Environment variable LEDIT_FROM_AGENT = '%s', isAgentMode = %t", os.Getenv("LEDIT_FROM_AGENT"), isAgentMode)

	// Strategy: Always try direct LLM first for non-agent cases, then fall back to interactive
	if !isAgentMode {
		logger.Log("Always trying direct LLM first for non-agent workflow")
		logger.Logf("DEBUG: Code parameter length: %d chars", len(code))
		logger.Logf("DEBUG: Instructions: %s", instructions)
		logger.Logf("DEBUG: Filename: %s", filename)

		// Always try direct LLM call first
		modelName, response, tokenUsage, err := callLLMDirectly(cfg, code, instructions, filename, imagePath)
		if err == nil && response != "" {
			logger.Log("Direct LLM call succeeded")
			return modelName, response, tokenUsage, nil
		}

		logger.Logf("Direct LLM call failed or returned empty, falling back to interactive workflow: %v", err)
		// Fall back to interactive workflow
	} else {
		logger.Log("Using interactive path for agent workflow")
	}

	// For complex cases where direct LLM failed, use the enhanced interactive workflow
	logger.Log("Using enhanced interactive path with tool calling support")

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
			return cfg.EditingModel, "", nil, err
		}
		logger.Log(fmt.Sprintf("Interactive response length: %d chars", len(response)))
		logger.Log("=== End GetLLMCodeResponse Debug ===")
		return cfg.EditingModel, response, tokenUsage, nil
	} else {
		logger.Logf("DEBUG: Using unified interactive approach for code workflow (forced tools)")

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
			return cfg.EditingModel, "", nil, fmt.Errorf("no instructions found in messages")
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
			return cfg.EditingModel, "", nil, err
		}
		logger.Logf("DEBUG: Direct code editing call completed")
		logger.Log(fmt.Sprintf("Interactive response length: %d chars", len(response)))
		logger.Log("=== End GetLLMCodeResponse Debug ===")
		return cfg.EditingModel, response, tokenUsage, nil
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

// callLLMDirectly handles simple code generation without interactive tools
func callLLMDirectly(cfg *config.Config, code, instructions, filename, imagePath string) (string, string, *llm.TokenUsage, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.Log("DEBUG: Using direct LLM call path")

	var userContent string
	var systemPrompt string

	if code != "" {
		// Handle existing code modification
		logger.Log("DEBUG: Handling existing code modification")
		systemPromptText, err := prompts.LoadPromptFromFile("code_modification_system.txt")
		if err != nil {
			return "", "", nil, fmt.Errorf("failed to load code modification prompt: %w", err)
		}
		systemPrompt = systemPromptText

		userContent = fmt.Sprintf(`Instructions: %s

Existing code to modify:
%s

Target file: %s

Generate the complete modified file content.`, instructions, code, filename)
	} else {
		// Handle new file creation
		logger.Log("DEBUG: Handling new file creation")
		systemPromptText, err := prompts.LoadPromptFromFile("code_generation_system.txt")
		if err != nil {
			return "", "", nil, fmt.Errorf("failed to load code generation prompt: %w", err)
		}
		systemPrompt = systemPromptText

		// New file generation
		if len(code) == 0 {
			userContent = instructions
		} else {
			userContent = fmt.Sprintf("Analyze the following code and make the requested changes:\n\n---\n\n%s\n\n---\n\n%s", code, instructions)
		}
	}

	// Build simple messages without tool support
	messages := []prompts.Message{
		{
			Role:    "system",
			Content: systemPrompt,
		},
		{
			Role:    "user",
			Content: userContent,
		},
	}

	// Call LLM directly without tools
	llmResponse, tokenUsage, err := llm.GetLLMResponse(cfg.EditingModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		return "", "", nil, fmt.Errorf("direct LLM call failed: %w", err)
	}

	logger.Log(fmt.Sprintf("DEBUG: Direct LLM response length: %d chars", len(llmResponse)))

	// Basic validation - check if response looks like valid JSON
	if !strings.Contains(llmResponse, `"file_path"`) || !strings.Contains(llmResponse, `"file_content"`) {
		return "", "", nil, fmt.Errorf("LLM response does not contain expected JSON structure")
	}

	return cfg.EditingModel, llmResponse, tokenUsage, nil
}

// buildQualityAwareCodeMessages creates code generation messages with quality-enhanced system prompts
func buildQualityAwareCodeMessages(code, instructions, filename string, interactive bool, usePatchFormat bool, qualityLevel int) []prompts.Message {
	var messages []prompts.Message

	// Use quality-aware system prompt instead of base system prompt
	systemPrompt := prompts.GetQualityAwareCodeGenSystemMessage(qualityLevel, usePatchFormat)

	if interactive {
		// For interactive mode, still use the interactive prompt but enhance it with quality level
		var interactivePrompt string
		var err error
		if qualityLevel >= 2 { // Production level
			interactivePrompt, err = prompts.LoadPromptFromFile("interactive_code_generation_quality_enhanced.txt")
			if err != nil {
				// Fallback to standard interactive prompt
				interactivePrompt, _ = prompts.LoadPromptFromFile("interactive_code_generation.txt")
			}
		} else {
			interactivePrompt, _ = prompts.LoadPromptFromFile("interactive_code_generation.txt")
		}
		systemPrompt = strings.Replace(interactivePrompt, "{INSTRUCTIONS}", instructions, 1)
	}

	// Inject dynamic guidance when a specific filename is targeted
	if filename != "" {
		systemPrompt = systemPrompt + "\nSINGLE-FILE TARGETING:\n" +
			"- A specific filename was provided (" + filename + "). Focus your edits primarily on that file.\n" +
			"- Only create or modify other files if absolutely necessary dependencies are required for the requested change to work.\n" +
			"MINIMALITY:\n" +
			"- Make the smallest possible changes to satisfy the request. Do not add unrelated features, refactors, or formatting changes.\n"
	}

	messages = append(messages, prompts.Message{Role: "system", Content: systemPrompt})

	if code != "" {
		if usePatchFormat {
			messages = append(messages, prompts.Message{Role: "user", Content: fmt.Sprintf("Here is the current content of `%s`:\n\n```\n%s\n```\n\nInstructions: %s", filename, code, instructions)})
		} else {
			// Get language from filename for syntax highlighting
			lang := getLanguageFromFilename(filename)
			messages = append(messages, prompts.Message{Role: "user", Content: fmt.Sprintf("Here is the current content of `%s`:\n\n```%s\n%s\n```\n\nInstructions: %s", filename, lang, code, instructions)})
		}
	} else {
		messages = append(messages, prompts.Message{Role: "user", Content: instructions})
	}

	return messages
}

// getLanguageFromFilename extracts language identifier from filename for syntax highlighting
func getLanguageFromFilename(filename string) string {
	if filename == "" {
		return ""
	}

	ext := strings.ToLower(filename[strings.LastIndex(filename, "."):])
	switch ext {
	case ".go":
		return "go"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".c":
		return "c"
	case ".cs":
		return "csharp"
	case ".php":
		return "php"
	case ".rb":
		return "ruby"
	case ".rs":
		return "rust"
	case ".sh":
		return "bash"
	case ".sql":
		return "sql"
	case ".html":
		return "html"
	case ".css":
		return "css"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".xml":
		return "xml"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}
