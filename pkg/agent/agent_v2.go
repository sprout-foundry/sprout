//go:build !agent2refactor

package agent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"os/exec"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// TodoItem represents a task to be executed
type TodoItem struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	Description string `json:"description"`
	Status      string `json:"status"` // pending, in_progress, completed, failed
	FilePath    string `json:"file_path,omitempty"`
	Priority    int    `json:"priority"`
}

// SimplifiedAgentContext holds the simplified agent state
type SimplifiedAgentContext struct {
	UserIntent      string
	Todos           []TodoItem
	Config          *config.Config
	Logger          *utils.Logger
	CurrentTodo     *TodoItem
	BuildCommand    string
	AnalysisResults map[string]string
}

// IntentType represents the type of user intent
type IntentType string

const (
	IntentTypeCodeUpdate IntentType = "code_update"
	IntentTypeQuestion   IntentType = "question"
	IntentTypeCommand    IntentType = "command"
)

// RunSimplifiedAgent: New simplified agent workflow
func RunSimplifiedAgent(userIntent string, skipPrompt bool, model string) error {
	startTime := time.Now()
	ui.Out().Print("🤖 Simplified Agent Mode\n")
	ui.Out().Printf("🎯 Intent: %s\n", userIntent)

	cfg, err := config.LoadOrInitConfig(skipPrompt)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if model != "" {
		cfg.EditingModel = model
	}
	cfg.SkipPrompt = skipPrompt
	cfg.FromAgent = true

	// Set environment variables to ensure non-interactive mode for all operations
	os.Setenv("LEDIT_FROM_AGENT", "1")
	os.Setenv("LEDIT_SKIP_PROMPT", "1")

	logger := utils.GetLogger(cfg.SkipPrompt)

	// Analyze intent type
	intentType := analyzeIntentType(userIntent, logger)

	ctx := &SimplifiedAgentContext{
		UserIntent:      userIntent,
		Config:          cfg,
		Logger:          logger,
		Todos:           []TodoItem{},
		AnalysisResults: make(map[string]string),
	}

	switch intentType {
	case IntentTypeCodeUpdate:
		return handleCodeUpdate(ctx, startTime)
	case IntentTypeQuestion:
		return handleQuestion(ctx)
	case IntentTypeCommand:
		return handleCommand(ctx)
	default:
		return fmt.Errorf("unknown intent type")
	}
}

// analyzeIntentType determines what type of request this is
func analyzeIntentType(userIntent string, logger *utils.Logger) IntentType {
	intentLower := strings.ToLower(userIntent)

	// Check for questions - be more specific to avoid false positives
	questionWords := []string{"what is", "what are", "how do", "how does", "how can", "how to", "why is", "why does", "when is", "where is", "which is", "who is", "can you explain", "can you describe"}
	for _, phrase := range questionWords {
		if strings.Contains(intentLower, phrase) {
			return IntentTypeQuestion
		}
	}

	// Also check for common question starters
	questionStarters := []string{"what ", "how ", "why ", "when ", "where ", "which ", "who "}
	for _, starter := range questionStarters {
		if strings.HasPrefix(intentLower, starter) {
			return IntentTypeQuestion
		}
	}

	// Check for commands - be more specific to avoid false positives
	commandPrefixes := []string{"run ", "execute ", "start ", "stop ", "build ", "test ", "deploy ", "install ", "uninstall "}
	for _, prefix := range commandPrefixes {
		if strings.HasPrefix(intentLower, prefix) {
			return IntentTypeCommand
		}
	}

	// Check for file extensions - if the intent mentions specific files, it's likely a code update
	if strings.Contains(intentLower, ".go") || strings.Contains(intentLower, ".py") ||
		strings.Contains(intentLower, ".js") || strings.Contains(intentLower, ".ts") {
		return IntentTypeCodeUpdate
	}

	// Check for code-related keywords that indicate code updates
	codeWords := []string{"add ", "create ", "implement ", "fix ", "update ", "change ", "modify ", "refactor ", "delete ", "remove ", "rename ", "move ", "extract ", "function", "class", "method", "variable"}
	for _, word := range codeWords {
		if strings.Contains(intentLower, word) {
			return IntentTypeCodeUpdate
		}
	}

	// Check for command-like patterns that are actually code updates
	commandLikeButCode := []string{" add", " create", " fix", " update", " change", " modify"}
	for _, phrase := range commandLikeButCode {
		if strings.Contains(intentLower, phrase) {
			return IntentTypeCodeUpdate
		}
	}

	// Default to code update for anything else
	return IntentTypeCodeUpdate
}

// handleCodeUpdate manages the code update workflow with todos
func handleCodeUpdate(ctx *SimplifiedAgentContext, startTime time.Time) error {
	ctx.Logger.LogProcessStep("🧭 Analyzing intent and creating plan...")

	// Create todos based on user intent
	err := createTodos(ctx)
	if err != nil {
		return fmt.Errorf("failed to create todos: %w", err)
	}

	if len(ctx.Todos) == 0 {
		ctx.Logger.LogProcessStep("⚠️ No actionable todos created")
		return fmt.Errorf("no actionable todos could be created")
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Created %d todos", len(ctx.Todos)))

	// Execute todos sequentially
	for i, todo := range ctx.Todos {
		ctx.Logger.LogProcessStep(fmt.Sprintf("📋 Executing todo %d/%d: %s", i+1, len(ctx.Todos), todo.Content))

		// Update todo status
		ctx.CurrentTodo = &todo
		ctx.Todos[i].Status = "in_progress"

		// Execute via code command with skip prompt
		err := executeTodo(ctx, &ctx.Todos[i])
		if err != nil {
			ctx.Todos[i].Status = "failed"
			ctx.Logger.LogError(fmt.Errorf("todo failed: %w", err))
			return fmt.Errorf("todo execution failed: %w", err)
		}

		ctx.Todos[i].Status = "completed"

		// Validate build after each todo
		err = validateBuild(ctx)
		if err != nil {
			ctx.Logger.LogError(fmt.Errorf("build validation failed after todo %d: %w", i+1, err))
			return fmt.Errorf("build validation failed: %w", err)
		}

		ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Todo %d completed and validated", i+1))
	}

	// Final summary
	duration := time.Since(startTime)
	ui.Out().Print("\n✅ Simplified Agent completed successfully\n")
	ui.Out().Printf("├─ Duration: %.2f seconds\n", duration.Seconds())
	ui.Out().Printf("├─ Todos completed: %d\n", len(ctx.Todos))
	ui.Out().Printf("└─ Status: All changes validated\n")

	return nil
}

// createTodos generates a list of todos based on user intent
func createTodos(ctx *SimplifiedAgentContext) error {
	prompt := fmt.Sprintf(`You are an expert software developer. Break down this user request into specific, actionable todos grounded in the provided workspace context.

User Request: "%s"

Workspace Context (truncated):
%s

Guidance:
- Use tools to validate and ground todos in reality (read files, search code, list files): prefer reading files (workspace_context/read_file), searching code (grep_search), and using workspace_context to find targets.
- If uncertain about exact locations or details, include an initial "analysis" todo that explicitly uses tools to gather the needed evidence before edits.
- Avoid speculative or ungrounded todos.

Please create a JSON array of todos that accomplish this request. Each todo should be:
- Specific and actionable
- Focused on a single task
- Include a clear description
- Prioritized (lower number = higher priority)
- Reference a concrete file path when applicable (use file_path)

Format:
[
  {
    "content": "Brief, actionable description",
    "description": "Detailed explanation of what this todo accomplishes",
    "priority": 1,
    "file_path": "optional/relative/path.ext"
  }
]

Focus on concrete changes that can be made to the codebase. Return ONLY the JSON array.`, ctx.UserIntent, func() string {
		wc := workspace.GetWorkspaceContext(ctx.UserIntent, ctx.Config)
		if len(wc) > 16000 {
			return wc[:16000]
		}
		return wc
	}())

	messages := []prompts.Message{
		{Role: "system", Content: "You create specific, actionable development todos. Ground todos in workspace context and prefer referencing actual files. Strongly prefer using tools (workspace_context, read_file, grep_search) to validate assumptions when planning. If uncertain, include an initial analysis todo that uses tools to gather evidence. Always return valid JSON."},
		{Role: "user", Content: prompt},
	}

	response, _, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to get todo response: %w", err)
	}

	// Parse JSON response
	clean, err := utils.ExtractJSON(response)
	if err != nil {
		return fmt.Errorf("failed to extract JSON from response: %w", err)
	}

	var todos []struct {
		Content     string `json:"content"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
		FilePath    string `json:"file_path"`
	}

	if err := json.Unmarshal([]byte(clean), &todos); err != nil {
		return fmt.Errorf("failed to parse todos JSON: %w", err)
	}

	// Convert to TodoItem slice
	for _, todo := range todos {
		ctx.Todos = append(ctx.Todos, TodoItem{
			ID:          generateTodoID(),
			Content:     todo.Content,
			Description: todo.Description,
			Status:      "pending",
			Priority:    todo.Priority,
			FilePath:    strings.TrimSpace(todo.FilePath),
		})
	}

	// Sort by priority
	sort.Slice(ctx.Todos, func(i, j int) bool {
		return ctx.Todos[i].Priority < ctx.Todos[j].Priority
	})

	return nil
}

// generateTodoID creates a unique ID for a todo
func generateTodoID() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return strconv.FormatUint(uint64(bytes[0])<<24|uint64(bytes[1])<<16|uint64(bytes[2])<<8|uint64(bytes[3]), 16)
}

// executeTodo executes a todo using the most appropriate method based on its content
func executeTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep(fmt.Sprintf("🔧 Executing: %s", todo.Content))

	// Analyze the todo to determine the best execution method
	executionType := analyzeTodoExecutionType(todo.Content, todo.Description)

	switch executionType {
	case ExecutionTypeAnalysis:
		if err := executeAnalysisTodo(ctx, todo); err != nil {
			return err
		}
		// Use analysis output to refine remaining todos
		refineTodosWithAnalysis(ctx, todo)
		return nil
	case ExecutionTypeDirectEdit:
		return executeDirectEditTodo(ctx, todo)
	case ExecutionTypeCodeCommand:
		return executeCodeCommandTodo(ctx, todo)
	default:
		return executeCodeCommandTodo(ctx, todo)
	}
}

// ExecutionType represents how a todo should be executed
type ExecutionType string

const (
	ExecutionTypeAnalysis    ExecutionType = "analysis"
	ExecutionTypeDirectEdit  ExecutionType = "direct_edit"
	ExecutionTypeCodeCommand ExecutionType = "code_command"
)

// analyzeTodoExecutionType determines the best way to execute a todo
func analyzeTodoExecutionType(content, description string) ExecutionType {
	contentLower := strings.ToLower(content)
	descriptionLower := strings.ToLower(description)

	// Analysis-only todos (read, explore, examine, analyze)
	analysisKeywords := []string{"analyze", "examine", "explore", "read", "review", "understand", "study", "investigate", "check", "verify", "validate", "list", "show", "display", "find", "search", "discover", "identify"}
	for _, keyword := range analysisKeywords {
		if strings.Contains(contentLower, keyword) {
			return ExecutionTypeAnalysis
		}
	}

	// Direct edit todos (simple changes, updates to documentation)
	directEditKeywords := []string{"update readme", "update documentation", "add comment", "fix typo", "update description", "add example", "update text"}
	for _, keyword := range directEditKeywords {
		if strings.Contains(contentLower, keyword) || strings.Contains(descriptionLower, keyword) {
			return ExecutionTypeDirectEdit
		}
	}

	// Default to code command for anything involving code changes
	return ExecutionTypeCodeCommand
}

// executeAnalysisTodo handles analysis-only todos with direct LLM exploration
func executeAnalysisTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("🔍 Performing analysis (no code changes)")

	prompt := fmt.Sprintf(`You are analyzing the codebase to help with: "%s"

Context from overall task: "%s"

Please analyze and provide insights on: %s

FIRST, use tools to ground your analysis:
- Call workspace_context with action=load_tree to understand the file structure.
- Call workspace_context with action=load_summary to get a project summary.
- Call workspace_context with action=search_keywords and a concise query to locate relevant files and function names.
- Then call read_file for the top one or two files that are most relevant.

AFTER you gather evidence, summarize your findings. Provide concrete file references (paths and function names) where applicable.
`, ctx.UserIntent, todo.Content, todo.Description)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert code analyst. Prefer using tools (workspace_context, read_file) to gather grounded evidence before answering. Provide detailed analysis without making changes."},
		{Role: "user", Content: prompt},
	}

	model := ctx.Config.OrchestrationModel
	if model == "" {
		model = ctx.Config.EditingModel
	}
	analysisCfg := *ctx.Config
	_, response, _, err := llm.CallLLMWithUnifiedInteractive(&llm.UnifiedInteractiveConfig{
		ModelName:       model,
		Messages:        messages,
		Filename:        "",
		WorkflowContext: llm.GetAgentWorkflowContext(),
		Config:          &analysisCfg,
		Timeout:         60 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// Store analysis results in context for future todos to reference
	ctx.AnalysisResults[todo.ID] = response
	ctx.Logger.LogProcessStep("📊 Analysis completed and stored")
	ui.Out().Print(fmt.Sprintf("\n📋 Analysis Result for Todo: %s\n%s\n", todo.Content, response))

	return nil
}

// executeDirectEditTodo handles simple documentation edits directly
func executeDirectEditTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("✏️ Performing direct edit (simple changes)")

	prompt := fmt.Sprintf(`You need to make a simple edit based on this todo:

Todo: %s
Description: %s
Overall Task: %s

Please provide the specific file path and the exact changes needed. Respond in JSON format:
{
  "file_path": "path/to/file",
  "changes": "description of what to change",
  "content": "the new content to use"
}`, todo.Content, todo.Description, ctx.UserIntent)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at making simple, targeted edits. Provide specific file paths and exact content changes."},
		{Role: "user", Content: prompt},
	}

	response, _, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, 45*time.Second)
	if err != nil {
		return fmt.Errorf("direct edit planning failed: %w", err)
	}

	// Parse the response to get file path and changes
	var editPlan struct {
		FilePath string `json:"file_path"`
		Changes  string `json:"changes"`
		Content  string `json:"content"`
	}

	clean, err := utils.ExtractJSON(response)
	if err != nil {
		return fmt.Errorf("failed to parse edit plan: %w", err)
	}

	if err := json.Unmarshal([]byte(clean), &editPlan); err != nil {
		return fmt.Errorf("failed to unmarshal edit plan: %w", err)
	}

	// Apply the direct edit
	if err := applyDirectEdit(editPlan.FilePath, editPlan.Content, ctx.Logger); err != nil {
		return fmt.Errorf("direct edit failed: %w", err)
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Direct edit completed: %s", editPlan.FilePath))
	return nil
}

// executeCodeCommandTodo handles complex code changes via the full code command workflow
func executeCodeCommandTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("🛠️ Using full code command workflow (complex changes)")

	instructions := fmt.Sprintf("%s\n\n%s", todo.Content, todo.Description)

	// Ensure we're in non-interactive mode for agent workflows
	agentConfig := *ctx.Config // Create a copy to avoid modifying the original
	agentConfig.SkipPrompt = true
	agentConfig.FromAgent = true

	// Set environment variables to ensure non-interactive mode
	os.Setenv("LEDIT_FROM_AGENT", "1")
	os.Setenv("LEDIT_SKIP_PROMPT", "1")

	// Use the editor directly instead of shelling out
	_, err := editor.ProcessCodeGeneration("", instructions, &agentConfig, "")
	if err != nil {
		return fmt.Errorf("code generation failed: %w", err)
	}

	return nil
}

// applyDirectEdit applies simple changes directly to files
func applyDirectEdit(filePath, newContent string, logger *utils.Logger) error {
	// Write new content
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	logger.LogProcessStep(fmt.Sprintf("📝 Updated %s", filePath))
	return nil
}

// validateBuild runs build validation after todo execution with intelligent error recovery
func validateBuild(ctx *SimplifiedAgentContext) error {
	ctx.Logger.LogProcessStep("🔍 Validating build after changes...")

	// Get build command from workspace
	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		ctx.Logger.LogProcessStep("⚠️ No workspace file found, skipping build validation")
		return nil
	}

	buildCmd := strings.TrimSpace(workspaceFile.BuildCommand)
	if buildCmd == "" {
		ctx.Logger.LogProcessStep("⚠️ No build command configured, skipping validation")
		return nil
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("🏗️ Running build: %s", buildCmd))

	cmd := exec.Command("sh", "-c", buildCmd)
	output, err := cmd.CombinedOutput()

	if err != nil {
		failureMsg := string(output)
		ctx.Logger.LogProcessStep("❌ Build failed, analyzing with LLM for recovery...")

		// Ask LLM to fix the build failure directly
		fixErr := fixBuildFailure(ctx, buildCmd, failureMsg)
		if fixErr != nil {
			ctx.Logger.LogError(fmt.Errorf("LLM fix attempt failed: %w", fixErr))
			return fmt.Errorf("build validation failed and fix attempt unsuccessful: %s", failureMsg)
		}

		// Try the build again after the fix attempt
		ctx.Logger.LogProcessStep("🔄 Retrying build after fix...")
		retryCmd := exec.Command("sh", "-c", buildCmd)
		if retryOutput, retryErr := retryCmd.CombinedOutput(); retryErr != nil {
			return fmt.Errorf("build still fails after fix attempt: %s", string(retryOutput))
		}

		ctx.Logger.LogProcessStep("✅ Build validation passed after LLM fix!")
		return nil
	}

	ctx.Logger.LogProcessStep("✅ Build validation passed")
	return nil
}

// fixBuildFailure asks the LLM to fix the build failure directly using available tools
func fixBuildFailure(ctx *SimplifiedAgentContext, buildCmd, failureMsg string) error {
	ctx.Logger.LogProcessStep("🔧 Asking LLM to fix build failure...")

	maxIterations := 12
	messages := []prompts.Message{
		{Role: "system", Content: `You are an expert software engineer troubleshooting a build failure. 

Available tools:
- read_file: {"file_path": "path/to/file"} - Read a file to understand its content
- edit_file_section: {"file_path": "path/to/file", "old_text": "text to replace", "new_text": "replacement text"} - Edit a specific part of a file
- run_shell_command: {"command": "shell command"} - Run shell commands for diagnostics or testing
- validate_file: {"file_path": "path/to/file"} - Check Go syntax of a file

Use these tools to diagnose and fix build issues. Read files to understand errors, edit files to fix syntax problems, and test your changes.`},
		{Role: "user", Content: fmt.Sprintf(`The build command '%s' failed with this error:

BUILD ERROR:
%s

Please fix this build failure by using the available tools. Read files to understand the error, edit files to fix syntax issues, and test your fixes.`, buildCmd, failureMsg)},
	}

	for iteration := 1; iteration <= maxIterations; iteration++ {
		ctx.Logger.LogProcessStep(fmt.Sprintf("🔄 Build fix attempt %d/%d", iteration, maxIterations))

		response, _, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, 60*time.Second)
		if err != nil {
			return fmt.Errorf("LLM fix request failed: %w", err)
		}

		ctx.Logger.LogProcessStep(fmt.Sprintf("LLM response: %s", response))

		// Parse and execute tool calls from the response
		toolCalls, err := llm.ParseToolCalls(response)
		if err != nil || len(toolCalls) == 0 {
			ctx.Logger.LogProcessStep("⚠️ No tool calls found in response")

			// Check if the response indicates the build is fixed
			responseLower := strings.ToLower(response)
			if strings.Contains(responseLower, "build is now fixed") ||
				strings.Contains(responseLower, "build has been successfully fixed") ||
				strings.Contains(responseLower, "build is fixed") ||
				strings.Contains(responseLower, "successfully fixed") {
				ctx.Logger.LogProcessStep("🔍 Model indicates build is fixed, testing...")

				// Test the build
				cmd := exec.Command("sh", "-c", buildCmd)
				if output, err := cmd.CombinedOutput(); err == nil {
					ctx.Logger.LogProcessStep("✅ Build confirmed working! Issue resolved.")
					return nil
				} else {
					newFailureMsg := string(output)
					ctx.Logger.LogProcessStep(fmt.Sprintf("❌ Build still failing despite model's claim: %s", newFailureMsg))
					messages = append(messages, prompts.Message{Role: "assistant", Content: response})
					messages = append(messages, prompts.Message{Role: "user", Content: fmt.Sprintf("You said the build is fixed, but it's still failing: %s\nPlease continue fixing.", newFailureMsg)})
					continue
				}
			}

			// Add the response to conversation and continue
			messages = append(messages, prompts.Message{Role: "assistant", Content: response})
			continue
		}

		// Execute the tool calls and collect results
		var toolResults []string
		allToolsSucceeded := true

		for _, toolCall := range toolCalls {
			ctx.Logger.LogProcessStep(fmt.Sprintf("🔧 Executing tool: %s", toolCall.Function.Name))

			// Execute the tool call using enhanced executor
			result, err := executeEnhancedTool(toolCall, ctx.Config, ctx.Logger)
			if err != nil {
				ctx.Logger.LogError(fmt.Errorf("tool execution failed: %w", err))
				result = fmt.Sprintf("Tool execution failed: %v", err)
				allToolsSucceeded = false
			}

			ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Tool result: %s", result))
			toolResults = append(toolResults, fmt.Sprintf("Tool %s result: %s", toolCall.Function.Name, result))
		}

		// Add tool results to conversation
		toolResultsText := strings.Join(toolResults, "\n")
		messages = append(messages, prompts.Message{Role: "assistant", Content: response})
		messages = append(messages, prompts.Message{Role: "user", Content: fmt.Sprintf("Tool execution results:\n%s\n\nIf the build is now fixed, respond with 'BUILD_FIXED'. If you need to make more changes, continue with additional tool calls.", toolResultsText)})

		// Test the build after tool execution
		if allToolsSucceeded {
			ctx.Logger.LogProcessStep("🏗️ Testing build after fixes...")
			cmd := exec.Command("sh", "-c", buildCmd)
			if output, err := cmd.CombinedOutput(); err == nil {
				ctx.Logger.LogProcessStep("✅ Build succeeded! Issue resolved.")
				return nil
			} else {
				newFailureMsg := string(output)
				ctx.Logger.LogProcessStep(fmt.Sprintf("❌ Build still failing: %s", newFailureMsg))
				messages = append(messages, prompts.Message{Role: "user", Content: fmt.Sprintf("Build still failing with: %s\nPlease continue fixing.", newFailureMsg)})
			}
		}
	}

	return fmt.Errorf("build fix failed after %d attempts", maxIterations)
}

// executeEnhancedTool executes a tool call with enhanced functionality including file editing
func executeEnhancedTool(toolCall llm.ToolCall, cfg *config.Config, logger *utils.Logger) (string, error) {
	// Debug logging
	logger.LogProcessStep(fmt.Sprintf("Debug: Tool name: %s", toolCall.Function.Name))
	logger.LogProcessStep(fmt.Sprintf("Debug: Arguments string: '%s'", toolCall.Function.Arguments))

	// Parse the arguments from JSON string
	var args map[string]interface{}
	if toolCall.Function.Arguments == "" {
		return "", fmt.Errorf("tool arguments are empty")
	}

	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w (arguments were: '%s')", err, toolCall.Function.Arguments)
	}

	switch toolCall.Function.Name {
	case "read_file":
		filePath, ok := args["file_path"].(string)
		if !ok {
			return "", fmt.Errorf("read_file requires file_path argument")
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
		}

		return fmt.Sprintf("File content of %s:\n%s", filePath, string(content)), nil

	case "edit_file_section":
		filePath, ok := args["file_path"].(string)
		if !ok {
			return "", fmt.Errorf("%s requires file_path argument", toolCall.Function.Name)
		}

		oldText, hasOld := args["old_text"].(string)
		newText, hasNew := args["new_text"].(string)

		if !hasOld || !hasNew {
			return "", fmt.Errorf("%s requires old_text and new_text arguments", toolCall.Function.Name)
		}

		// Read current file content
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
		}

		// Perform replacement
		currentContent := string(content)
		if !strings.Contains(currentContent, oldText) {
			return "", fmt.Errorf("old_text not found in file %s", filePath)
		}

		newContent := strings.Replace(currentContent, oldText, newText, 1)

		// Write back to file
		if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", filePath, err)
		}

		return fmt.Sprintf("Successfully edited %s: replaced text", filePath), nil

	case "run_shell_command":
		command, ok := args["command"].(string)
		if !ok {
			return "", fmt.Errorf("run_shell_command requires command argument")
		}

		cmd := exec.Command("sh", "-c", command)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("Command failed: %s\nOutput: %s", command, string(output)), nil
		}

		return fmt.Sprintf("Command executed successfully:\n%s", string(output)), nil

	case "validate_file":
		filePath, ok := args["file_path"].(string)
		if !ok {
			return "", fmt.Errorf("validate_file requires file_path argument")
		}

		// Run gofmt to check syntax
		cmd := exec.Command("gofmt", "-e", filePath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("File %s has syntax errors:\n%s", filePath, string(output)), nil
		}

		return fmt.Sprintf("File %s syntax is valid", filePath), nil

	default:
		return "", fmt.Errorf("tool %s is not supported in enhanced executor", toolCall.Function.Name)
	}
}

// handleQuestion responds directly to user questions
func handleQuestion(ctx *SimplifiedAgentContext) error {
	ctx.Logger.LogProcessStep("❓ Handling question directly...")

	prompt := fmt.Sprintf(`You are an expert software developer. Please answer this question:

Question: "%s"

Provide a clear, helpful answer. If this involves code or technical details, be specific and include examples where appropriate.`, ctx.UserIntent)

	messages := []prompts.Message{
		{Role: "system", Content: "You are a helpful software development assistant."},
		{Role: "user", Content: prompt},
	}

	response, _, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to get answer: %w", err)
	}

	ui.Out().Print("\n🤖 Answer:\n")
	ui.Out().Print(response + "\n")
	return nil
}

// handleCommand executes user commands directly
func handleCommand(ctx *SimplifiedAgentContext) error {
	ctx.Logger.LogProcessStep("⚡ Handling command directly...")

	// Extract command from intent
	command := extractCommandFromIntent(ctx.UserIntent)
	if command == "" {
		return fmt.Errorf("could not extract command from intent")
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("🚀 Executing: %s", command))

	cmd := exec.Command("sh", "-c", command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		ctx.Logger.LogError(fmt.Errorf("command failed: %s", string(output)))
		return fmt.Errorf("command execution failed: %s", string(output))
	}

	ui.Out().Print("\n📋 Command Output:\n")
	ui.Out().Print(string(output) + "\n")
	return nil
}

// extractCommandFromIntent extracts a command from user intent
func extractCommandFromIntent(intent string) string {
	// Simple extraction - look for commands after "run", "execute", etc.
	intentLower := strings.ToLower(intent)

	commands := []string{"run ", "execute ", "start ", "stop ", "build ", "test ", "deploy ", "install ", "uninstall "}
	for _, prefix := range commands {
		if idx := strings.Index(intentLower, prefix); idx != -1 {
			return strings.TrimSpace(intent[idx+len(prefix):])
		}
	}

	// If no prefix found, return the whole intent as a command
	return strings.TrimSpace(intent)
}

// refineTodosWithAnalysis updates remaining todos based on analysis results.
// It can add file paths discovered in analysis and optionally insert follow-up todos.
func refineTodosWithAnalysis(ctx *SimplifiedAgentContext, completedTodo *TodoItem) {
	analysis := strings.TrimSpace(ctx.AnalysisResults[completedTodo.ID])
	if analysis == "" {
		return
	}
	// Heuristic: try to extract likely file paths mentioned in the analysis output
	// This is lightweight and avoids extra dependencies; it catches patterns like pkg/.../file.go
	pathRe := regexp.MustCompile(`(?m)(?:^|\s)([\w./-]+\.[A-Za-z0-9]+)`) // basic file token with extension
	matches := pathRe.FindAllStringSubmatch(analysis, -1)
	foundFiles := map[string]bool{}
	for _, m := range matches {
		if len(m) >= 2 {
			p := strings.TrimSpace(m[1])
			if p != "" && !strings.HasSuffix(p, "/") {
				foundFiles[p] = true
			}
		}
	}
	// Update pending todos that lack file_path with discovered files when content seems related
	for i := range ctx.Todos {
		t := &ctx.Todos[i]
		if t.Status != "pending" && t.Status != "in_progress" {
			continue
		}
		if strings.TrimSpace(t.FilePath) == "" {
			for f := range foundFiles {
				// simple relevance check: mention of filename stem in todo text
				stem := f
				if idx := strings.LastIndex(stem, "/"); idx != -1 {
					stem = stem[idx+1:]
				}
				if strings.Contains(strings.ToLower(analysis), strings.ToLower(stem)) || strings.Contains(strings.ToLower(t.Content), strings.ToLower(stem)) {
					t.FilePath = f
					break
				}
			}
		}
	}
	// Optionally, if analysis suggests a clear next step and no todo exists, append a follow-up
	// Heuristic: look for phrases like "add", "implement", "update" with a file path
	if len(foundFiles) > 0 {
		suggestRe := regexp.MustCompile(`(?i)\b(add|implement|update|modify|refactor|create)\b`)
		if suggestRe.MatchString(analysis) {
			for f := range foundFiles {
				ctx.Todos = append(ctx.Todos, TodoItem{
					ID:          generateTodoID(),
					Content:     "Apply changes based on analysis",
					Description: "Implement the changes identified by the analysis for: " + f,
					Status:      "pending",
					FilePath:    f,
					Priority:    5,
				})
				break
			}
		}
	}
}
