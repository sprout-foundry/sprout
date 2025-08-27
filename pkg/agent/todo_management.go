package agent

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// ExecutionType defines the method for executing a todo
type ExecutionType int

const (
	ExecutionTypeAnalysis     ExecutionType = iota // Analysis-only, no code changes
	ExecutionTypeDirectEdit                        // Simple file edits
	ExecutionTypeCodeCommand                       // Complex code generation
	ExecutionTypeShellCommand                      // Shell commands for filesystem operations
	ExecutionTypeContinuation                      // Continuation to next phase of complex workflow
)

// createTodos generates a list of todos based on user intent
func createTodos(ctx *SimplifiedAgentContext) error {
	// Build context-aware prompt using cached optimized template
	promptCache := GetPromptCache()
	userPromptTemplate := promptCache.GetCachedPromptWithFallback(
		"agent_todo_creation_user_optimized.txt",
		`Expert developer: break down request into actionable todos using workspace context.

Request: "{USER_REQUEST}"

## Workspace
{WORKSPACE_CONTEXT}

{ROLLOVER_CONTEXT}

GUIDELINES:
- Max 10 todos; use continuation todo #10 for complex multi-phase work
- Monorepo: focus on ONE component at a time
- Use tools to validate: read_file, grep_search, shell commands
- Include analysis todo if locations/details uncertain
- Ground in actual files, avoid speculation

JSON format:
[{"content":"Brief task","description":"Details","priority":1,"file_path":"optional/path.ext"}]

Return ONLY JSON array.`,
	)

	workspaceContext := workspace.GetProgressiveWorkspaceContext(ctx.UserIntent, ctx.Config)

	// Build rollover context
	var rolloverContext strings.Builder

	// Add rollover context from previous analysis if available
	if ctx.ContextManager != nil && ctx.PersistentCtx != nil {
		rolloverCtxData := ctx.ContextManager.GetRolloverContext(ctx.PersistentCtx)

		if recentFindings, ok := rolloverCtxData["recent_findings"]; ok {
			if findings, ok := recentFindings.([]AnalysisFinding); ok && len(findings) > 0 {
				rolloverContext.WriteString("\n\nRECENT FINDINGS:\n")
				for _, finding := range findings {
					rolloverContext.WriteString(fmt.Sprintf("- %s: %s\n", finding.Type, finding.Title))
				}
			}
		}

		if keyKnowledge, ok := rolloverCtxData["key_knowledge"]; ok {
			if knowledge, ok := keyKnowledge.([]KnowledgeItem); ok && len(knowledge) > 0 {
				rolloverContext.WriteString("\n\nKNOWLEDGE:\n")
				for _, item := range knowledge {
					rolloverContext.WriteString(fmt.Sprintf("- %s: %s\n", item.Category, item.Title))
				}
			}
		}

		if codePatterns, ok := rolloverCtxData["code_patterns"]; ok {
			if patterns, ok := codePatterns.([]CodePattern); ok && len(patterns) > 0 {
				rolloverContext.WriteString("\n\nCODE PATTERNS:\n")
				for _, pattern := range patterns {
					rolloverContext.WriteString(fmt.Sprintf("- %s: %s\n", pattern.Type, pattern.Name))
				}
			}
		}
	}

	// Substitute template variables
	prompt := strings.ReplaceAll(userPromptTemplate, "{USER_REQUEST}", ctx.UserIntent)
	prompt = strings.ReplaceAll(prompt, "{WORKSPACE_CONTEXT}", workspaceContext)
	prompt = strings.ReplaceAll(prompt, "{ROLLOVER_CONTEXT}", rolloverContext.String())

	// Load optimized system prompt from cache
	systemPrompt := promptCache.GetCachedPromptWithFallback(
		"agent_todo_creation_system_optimized.txt",
		"Create specific, actionable development todos. Ground todos in workspace context using tools (read_file, grep_search, run_shell_command) to validate assumptions. Include analysis todo if uncertain about file locations or details. Always return valid JSON.",
	)

	messages := []prompts.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: prompt},
	}

	// Try primary model with smart timeout
	smartTimeout := GetSmartTimeout(ctx.Config, ctx.Config.OrchestrationModel, "analysis")
	response, tokenUsage, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, smartTimeout)

	// If primary model fails, try with fallback model and extended timeout
	if err != nil {
		ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Primary model failed (%v), trying fallback approach", err))

		// Try with a simpler prompt and extended timeout
		fallbackMessages := []prompts.Message{
			{Role: "system", Content: "You create development todos. Keep it simple and return JSON only."},
			{Role: "user", Content: fmt.Sprintf("Create 1-2 simple todos for: %s\nReturn JSON array only.", ctx.UserIntent)},
		}

		// Use extended timeout for fallback
		fallbackTimeout := time.Duration(float64(smartTimeout) * 1.5)
		response, tokenUsage, err = llm.GetLLMResponse(ctx.Config.OrchestrationModel, fallbackMessages, "", ctx.Config, fallbackTimeout)

		if err != nil {
			ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Fallback attempt failed: %v", err))
			return fmt.Errorf("both primary and fallback attempts failed: %w", err)
		} else {
			ctx.Logger.LogProcessStep("✅ Fallback approach succeeded")
		}
	}

	// Track token usage and cost for todo generation
	trackTokenUsage(ctx, tokenUsage, ctx.Config.OrchestrationModel)

	// Parse JSON response - handle reasoning model responses that include thinking blocks
	clean, err := utils.ExtractJSON(response)
	if err != nil {
		// Try to extract JSON from the end of the response (after thinking blocks)
		// Look for the last occurrence of '[' or '{' that starts valid JSON
		if lastBracket := strings.LastIndex(response, "["); lastBracket != -1 {
			potentialJSON := response[lastBracket:]
			if json.Valid([]byte(potentialJSON)) {
				clean = potentialJSON
				ctx.Logger.LogProcessStep("✅ Successfully extracted JSON from end of response")
			} else {
				ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ JSON extraction failed. LLM Response: %s", response))
				return fmt.Errorf("failed to extract JSON from response: %w", err)
			}
		} else {
			ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ JSON extraction failed. LLM Response: %s", response))
			return fmt.Errorf("failed to extract JSON from response: %w", err)
		}
	}

	var todos []struct {
		Content     string `json:"content"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
		FilePath    string `json:"file_path"`
	}

	if err := json.Unmarshal([]byte(clean), &todos); err != nil {
		ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ JSON parsing failed, trying fallback todo creation: %v", err))

		// Create a simple fallback todo
		todos = []struct {
			Content     string `json:"content"`
			Description string `json:"description"`
			Priority    int    `json:"priority"`
			FilePath    string `json:"file_path"`
		}{
			{
				Content:     "Analyze user request: " + ctx.UserIntent,
				Description: "Analyze and understand what the user is asking for: " + ctx.UserIntent,
				Priority:    1,
				FilePath:    "",
			},
		}

		ctx.Logger.LogProcessStep("✅ Created fallback todo for analysis")
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

// executeTodoWithSmartRetry executes a todo with context-aware retry logic
func executeTodoWithSmartRetry(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	const maxRetries = 2

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := executeTodo(ctx, todo)
		if err == nil {
			return nil
		}

		// Parse failure reason and adjust approach
		if strings.Contains(err.Error(), "code review requires revisions") {
			// Switch to shell command approach for filesystem tasks on first retry
			if attempt == 0 && containsFilesystemKeywords(todo.Content) {
				ctx.Logger.LogProcessStep("🔄 Switching to shell command approach for filesystem task")
				return executeShellCommandTodo(ctx, todo)
			}
		}

		if attempt < maxRetries {
			ctx.Logger.LogProcessStep(fmt.Sprintf("🔄 Retry %d/%d: %v", attempt+1, maxRetries, err))
			// Add small delay between retries
			time.Sleep(time.Second * 2)
		}
	}

	return fmt.Errorf("failed after %d retries", maxRetries+1)
}

// executeTodo executes a todo using the optimized editing service
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
	case ExecutionTypeShellCommand:
		return executeShellCommandTodo(ctx, todo)
	case ExecutionTypeCodeCommand:
		return executeOptimizedCodeEditingTodo(ctx, todo)
	case ExecutionTypeContinuation:
		return executeContinuationTodo(ctx, todo)
	default:
		return executeOptimizedCodeEditingTodo(ctx, todo)
	}
}

// analyzeTodoExecutionType determines the best way to execute a todo
func analyzeTodoExecutionType(content, description string) ExecutionType {
	contentLower := strings.ToLower(content)
	descriptionLower := strings.ToLower(description)

	// Continuation todos - check first for workflow continuation
	continuationKeywords := []string{"continue with next phase", "continue with", "next phase of", "continue to", "proceed with next"}
	for _, keyword := range continuationKeywords {
		if strings.Contains(contentLower, keyword) || strings.Contains(descriptionLower, keyword) {
			return ExecutionTypeContinuation
		}
	}

	// Direct edit todos (simple changes, updates to documentation) - check first before shell commands
	directEditKeywords := []string{"update readme", "update documentation", "add comment", "fix typo", "update description", "add example", "update text"}
	for _, keyword := range directEditKeywords {
		if strings.Contains(contentLower, keyword) || strings.Contains(descriptionLower, keyword) {
			return ExecutionTypeDirectEdit
		}
	}

	// File creation/generation tasks - should use Edit/Write tools, not shell commands
	fileCreationPatterns := []string{
		"generate.*\\.md", "create.*\\.md", "write.*\\.md",
		"generate.*\\.txt", "create.*\\.txt", "write.*\\.txt",
		"generate.*\\.json", "create.*\\.json", "write.*\\.json",
		"generate.*\\.yaml", "create.*\\.yaml", "write.*\\.yaml",
		"generate.*\\.yml", "create.*\\.yml", "write.*\\.yml",
		"generate.*documentation", "create.*documentation", "write.*documentation",
		"generate.*api.*doc", "create.*api.*doc", "write.*api.*doc",
	}
	for _, pattern := range fileCreationPatterns {
		matched, _ := regexp.MatchString(pattern, contentLower)
		if matched {
			return ExecutionTypeDirectEdit
		}
		matched, _ = regexp.MatchString(pattern, descriptionLower)
		if matched {
			return ExecutionTypeDirectEdit
		}
	}

	// Check for documentation updates more flexibly (handle README.md, readme files, etc.)
	if strings.Contains(contentLower, "update") && (strings.Contains(contentLower, "readme") || strings.Contains(contentLower, "documentation") || strings.Contains(contentLower, "docs")) {
		return ExecutionTypeDirectEdit
	}

	// Shell command todos (filesystem operations) - after direct edit check
	shellKeywords := []string{
		"create directory", "mkdir", "create folder", "setup project", "initialize",
		"install", "setup monorepo", "create backend", "create frontend", "run", "execute command",
		"create the", "directory for", "backend directory", "frontend directory",
		"directory in", "directory called", "directory named", " directory ", "new directory",
	}
	for _, keyword := range shellKeywords {
		if strings.Contains(contentLower, keyword) || strings.Contains(descriptionLower, keyword) {
			return ExecutionTypeShellCommand
		}
	}

	// Analysis-only todos (read, explore, examine, analyze)
	analysisKeywords := []string{"analyze", "examine", "explore", "read", "review", "understand", "study", "investigate", "check", "verify", "validate", "list", "show", "display", "find", "search", "discover", "identify"}
	for _, keyword := range analysisKeywords {
		if strings.Contains(contentLower, keyword) {
			return ExecutionTypeAnalysis
		}
	}

	// Default to code command for anything involving code changes
	return ExecutionTypeCodeCommand
}

// executeAnalysisTodo handles analysis-only todos with direct LLM exploration
func executeAnalysisTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("🔍 Performing analysis (no code changes)")

	// Get progressive workspace context for analysis with smart fallbacks
	workspaceContext := workspace.GetProgressiveWorkspaceContext(ctx.UserIntent, ctx.Config)
	if workspaceContext == "" {
		workspaceContext = "Workspace context not available"
	}

	prompt := fmt.Sprintf(`You are analyzing the codebase to help with: "%s"

Context from overall task: "%s"

## Workspace Context
%s

Please analyze and provide insights on: %s

CRITICAL: Use tools to gather evidence before making any analysis or recommendations. Do not make assumptions about the codebase structure or content.

REQUIRED TOOLS - Use these in order:
1. **run_shell_command(command="find . -type f -name '*.go' | head -20")** - Get overview of Go files structure
2. **run_shell_command(command="grep -r 'relevant terms' --include='*.go' .")** - Find files containing specific terms
3. **run_shell_command(command="ls -la pkg/")** - List contents of specific directories (example: list pkg directory)
4. **run_shell_command(command="grep -r 'func.*main' .")** - Search for specific patterns (example: find main functions)
5. **read_file(file_path="main.go")** - Read specific files for detailed analysis

AFTER gathering evidence with tools, provide your analysis with:
- Concrete file references and line numbers
- Evidence-based findings, not assumptions
- Specific recommendations with implementation details
- Code examples where relevant

Remember: Always use tools first, then analyze based on actual evidence from the codebase.
`, ctx.UserIntent, todo.Content, workspaceContext, todo.Description)

	// Use the unified agent workflow pattern that works reliably with tools
	prompt = fmt.Sprintf(`Task: %s

Use available tools to complete this analysis task effectively.`, todo.Description)

	messages := []prompts.Message{
		{Role: "system", Content: llm.GetSystemMessageForInformational()},
		{Role: "user", Content: prompt},
	}

	response, tokenUsage, err := executeAgentWorkflowWithTools(ctx, messages, "analysis")
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// Track token usage and cost
	model := ctx.Config.OrchestrationModel
	if model == "" {
		model = ctx.Config.EditingModel
	}
	trackTokenUsage(ctx, tokenUsage, model)

	// Store analysis results in context for future todos to reference
	ctx.AnalysisResults[todo.ID] = response

	// Extract and store findings in context manager if available
	if ctx.ContextManager != nil && ctx.PersistentCtx != nil {
		findings := extractFindingsFromAnalysis(response, todo)
		for _, finding := range findings {
			err := ctx.ContextManager.AddFinding(ctx.PersistentCtx, finding)
			if err != nil {
				ctx.Logger.LogError(fmt.Errorf("failed to store finding in context: %w", err))
			} else {
				ctx.Logger.LogProcessStep(fmt.Sprintf("💡 Finding stored: %s", finding.Title))
			}
		}
	}

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

	// Try primary model with smart timeout
	response, tokenUsage, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, llm.GetSmartTimeout(ctx.Config, ctx.Config.OrchestrationModel, "analysis"))

	// If primary model fails, try with fallback approach
	if err != nil {
		ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Edit planning failed (%v), trying simpler approach", err))

		// Try with a much simpler prompt
		simpleMessages := []prompts.Message{
			{Role: "system", Content: "You suggest simple file edits. Return JSON with file_path and changes."},
			{Role: "user", Content: fmt.Sprintf("Suggest a simple edit for: %s\nReturn JSON: {\"file_path\":\"path/to/file\",\"changes\":\"what to change\"}", todo.Content)},
		}

		fallbackTimeout := time.Duration(float64(llm.GetSmartTimeout(ctx.Config, ctx.Config.OrchestrationModel, "analysis")) * 1.5)
		response, tokenUsage, err = llm.GetLLMResponse(ctx.Config.OrchestrationModel, simpleMessages, "", ctx.Config, fallbackTimeout)

		if err != nil {
			return fmt.Errorf("both primary and fallback edit planning failed: %w", err)
		}
	}

	// Track token usage and cost for direct edit planning
	trackTokenUsage(ctx, tokenUsage, ctx.Config.OrchestrationModel)

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

	// Use ProcessCodeGeneration for safe, targeted edits instead of the broken applyDirectEdit
	agentConfig := *ctx.Config
	agentConfig.SkipPrompt = true
	agentConfig.FromAgent = true

	// Set environment variables to ensure non-interactive mode
	os.Setenv("LEDIT_FROM_AGENT", "1")
	os.Setenv("LEDIT_SKIP_PROMPT", "1")

	// Clear any previous token usage
	agentConfig.LastTokenUsage = nil

	// Create a targeted edit prompt
	editPrompt := fmt.Sprintf(`Please make the following edit:

Task: %s
Description: %s
Overall Task: %s

Please implement this as a targeted edit to the file, not a complete file replacement.`, todo.Content, todo.Description, ctx.UserIntent)

	_, err = editor.ProcessCodeGeneration("", editPrompt, &agentConfig, "")

	// Track token usage from the editor's LLM calls
	if agentConfig.LastTokenUsage != nil {
		trackTokenUsage(ctx, agentConfig.LastTokenUsage, agentConfig.EditingModel)
		ctx.Logger.LogProcessStep(fmt.Sprintf("📊 Tracked %d tokens from editor LLM calls", agentConfig.LastTokenUsage.TotalTokens))
	}

	if err != nil {
		return fmt.Errorf("direct edit failed: %w", err)
	}

	ctx.Logger.LogProcessStep("✅ Direct edit completed successfully")
	return nil
}

// executeCodeCommandTodo handles complex code changes via the granular editing workflow
func executeCodeCommandTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("🛠️ Using granular editing workflow (complex changes)")

	// Phase 1: Exploration & Planning
	if err := executeExplorationPhase(ctx, todo); err != nil {
		return fmt.Errorf("exploration phase failed: %w", err)
	}

	// Phase 2: Detailed Planning
	if err := executePlanningPhase(ctx, todo); err != nil {
		return fmt.Errorf("planning phase failed: %w", err)
	}

	// Phase 3: Granular Execution
	if err := executeGranularEditingPhase(ctx, todo); err != nil {
		return fmt.Errorf("editing phase failed: %w", err)
	}

	// Phase 4: Verification & Review
	if err := executeVerificationPhase(ctx, todo); err != nil {
		return fmt.Errorf("verification phase failed: %w", err)
	}

	return nil
}

// executeOptimizedCodeEditingTodo handles code editing todos using the optimized editing service with rollback support
func executeOptimizedCodeEditingTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("⚡ Performing optimized code edit with rollback support")

	// Create optimized editing service
	editingService := NewOptimizedEditingService(ctx.Config, ctx.Logger)

	// Execute using the optimized editing strategy with rollback support
	result, err := editingService.ExecuteOptimizedEditWithRollback(todo, ctx)
	if err != nil {
		ctx.Logger.LogProcessStep(fmt.Sprintf("❌ Optimized edit failed: %v", err))
		return err
	}

	// Track token usage and costs from the editing service
	metrics := result.Metrics
	if metrics.TotalTokens > 0 {
		// Convert metrics to SimplifiedAgentContext tracking
		ctx.TotalTokensUsed += metrics.TotalTokens
		ctx.TotalCost += metrics.TotalCost

		// Estimate token breakdown (rough approximation for editing operations)
		// Typically editing has ~60% prompt tokens, 40% completion tokens
		estimatedPromptTokens := int(float64(metrics.TotalTokens) * 0.6)
		estimatedCompletionTokens := metrics.TotalTokens - estimatedPromptTokens
		ctx.TotalPromptTokens += estimatedPromptTokens
		ctx.TotalCompletionTokens += estimatedCompletionTokens

		ctx.Logger.LogProcessStep(fmt.Sprintf("📊 Optimized edit used %d tokens ($%.4f)",
			metrics.TotalTokens, metrics.TotalCost))
	}

	// Store result and revision IDs for potential rollback
	if result.Diff != "" {
		ctx.AnalysisResults[todo.ID+"_edit_result"] = result.Diff
		// Mark that files were modified for validation purposes
		ctx.FilesModified = true
	}
	if len(result.RevisionIDs) > 0 {
		ctx.AnalysisResults[todo.ID+"_revision_ids"] = fmt.Sprintf("%v", result.RevisionIDs)
		ctx.Logger.LogProcessStep(fmt.Sprintf("🔄 Rollback available with revision IDs: %v", result.RevisionIDs))

		// Store the editing service instance for potential rollback (could be stored in context if needed)
		// For now, log that rollback is available via revision IDs
		for _, revisionID := range result.RevisionIDs {
			ctx.Logger.LogProcessStep(fmt.Sprintf("💾 Revision stored for rollback: %s", revisionID))
		}
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Optimized edit completed using %s strategy", result.Strategy))
	return nil
}

// applyDirectEdit applies simple changes directly to files
func applyDirectEdit(filePath, newContent string, logger *utils.Logger, ctx *SimplifiedAgentContext) error {
	// Write new content
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	// Mark that files were modified for validation purposes
	ctx.FilesModified = true

	logger.LogProcessStep(fmt.Sprintf("📝 Updated %s", filePath))
	return nil
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

// scoreTodoForDynamicPriority computes a dynamic priority score for a todo.
// Higher score means higher priority to run next. It considers:
// - Original priority (lower number gets higher base score)
// - Presence of critical/high findings related to the todo's file or keywords
// - Accumulated knowledge pointing to related files
// - Urgency keywords in the todo text
func scoreTodoForDynamicPriority(ctx *SimplifiedAgentContext, todo *TodoItem) int {
	// Base score derives from static priority (1 highest). Keep within [0..100].
	base := 100 - (todo.Priority * 10)
	if base < 0 {
		base = 0
	}

	score := base

	content := strings.ToLower(todo.Content + " " + todo.Description)
	filePath := strings.TrimSpace(todo.FilePath)

	// Urgency keyword boosts
	urgencyKeywords := []string{"fix", "error", "failing", "fail", "build", "lint", "security", "vuln", "panic", "crash", "broken", "blocking"}
	for _, kw := range urgencyKeywords {
		if strings.Contains(content, kw) {
			score += 6
		}
	}

	// If we have persistent context, use findings/knowledge to boost relevance
	if ctx.ContextManager != nil && ctx.PersistentCtx != nil {
		pc := ctx.PersistentCtx

		// Recent findings: boost if file matches or title mentions todo terms
		for i := len(pc.Findings) - 1; i >= 0 && i >= len(pc.Findings)-8; i-- {
			f := pc.Findings[i]
			if filePath != "" && strings.TrimSpace(f.FilePath) != "" && strings.EqualFold(f.FilePath, filePath) {
				// Severity-weighted boost
				switch strings.ToLower(f.Severity) {
				case "critical":
					score += 20
				case "high":
					score += 12
				case "medium":
					score += 6
				default:
					score += 3
				}
			}
			// Title/content match to todo text
			ft := strings.ToLower(f.Title + " " + f.Description)
			if ft != "" && content != "" && (strings.Contains(ft, todo.Content) || strings.Contains(ft, todo.Description)) {
				score += 4
			}
		}

		// Knowledge items referencing related files
		for _, k := range pc.KnowledgeBase {
			if len(k.RelatedFiles) == 0 {
				continue
			}
			for _, rf := range k.RelatedFiles {
				if filePath != "" && strings.EqualFold(rf, filePath) {
					score += 5
				}
			}
		}
	}

	// If analysis summary references this file or todo terms, small boost
	if summary, ok := ctx.AnalysisResults["summary"]; ok {
		lsum := strings.ToLower(summary)
		if filePath != "" && strings.Contains(lsum, strings.ToLower(filePath)) {
			score += 4
		}
		if todo.Content != "" && strings.Contains(lsum, strings.ToLower(todo.Content)) {
			score += 2
		}
	}

	// Ensure a non-negative score
	if score < 0 {
		score = 0
	}
	return score
}

// selectNextTodoIndex chooses the next todo to execute based on dynamic scoring.
// Returns -1 if no pending todos remain. Also logs top candidates for transparency.
func selectNextTodoIndex(ctx *SimplifiedAgentContext) int {
	bestIdx := -1
	bestScore := -1

	// Track top 3 for logging
	type cand struct{ idx, score int }
	top := []cand{}

	for i := range ctx.Todos {
		t := &ctx.Todos[i]
		if t.Status != "pending" {
			continue
		}
		s := scoreTodoForDynamicPriority(ctx, t)
		// Maintain top 3
		inserted := false
		for j := 0; j < len(top); j++ {
			if s > top[j].score {
				top = append(top[:j], append([]cand{{idx: i, score: s}}, top[j:]...)...)
				inserted = true
				break
			}
		}
		if !inserted {
			top = append(top, cand{idx: i, score: s})
		}
		if len(top) > 3 {
			top = top[:3]
		}
		if s > bestScore || (s == bestScore && t.Priority < ctx.Todos[bestIdx].Priority) {
			bestScore = s
			bestIdx = i
		}
	}

	// Log decision
	if bestIdx != -1 {
		var b strings.Builder
		b.WriteString("🔄 Reprioritized next todo based on analysis: ")
		b.WriteString(ctx.Todos[bestIdx].Content)
		b.WriteString(" (score=")
		b.WriteString(strconv.Itoa(bestScore))
		b.WriteString(")\n")
		if len(top) > 0 {
			b.WriteString("   Top candidates: ")
			for ci, c := range top {
				if ci > 0 {
					b.WriteString(", ")
				}
				b.WriteString("[")
				b.WriteString(ctx.Todos[c.idx].Content)
				b.WriteString(" => ")
				b.WriteString(strconv.Itoa(c.score))
				b.WriteString("]")
			}
		}
		ctx.Logger.LogProcessStep(b.String())
	}

	return bestIdx
}

// extractFindingsFromAnalysis parses analysis text to extract structured findings
func extractFindingsFromAnalysis(analysisText string, todo *TodoItem) []AnalysisFinding {
	var findings []AnalysisFinding

	lines := strings.Split(analysisText, "\n")
	var currentFinding *AnalysisFinding

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Look for patterns that indicate findings
		if strings.HasPrefix(line, "Key finding:") || strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "• ") {
			// If we have a current finding, save it
			if currentFinding != nil {
				findings = append(findings, *currentFinding)
			}

			// Start new finding
			content := strings.TrimPrefix(line, "Key finding:")
			content = strings.TrimPrefix(content, "- ")
			content = strings.TrimPrefix(content, "• ")
			content = strings.TrimSpace(content)

			currentFinding = &AnalysisFinding{
				Type:        "file_analysis",
				Severity:    "medium",
				Title:       content,
				Description: content,
				TodoID:      todo.ID,
				Timestamp:   time.Now(),
			}
		} else if currentFinding != nil {
			// Continue building current finding
			currentFinding.Description += " " + line
		}
	}

	// Save the last finding
	if currentFinding != nil {
		findings = append(findings, *currentFinding)
	}

	// If no structured findings found, create a general one
	if len(findings) == 0 && len(analysisText) > 50 {
		findings = append(findings, AnalysisFinding{
			Type:        "file_analysis",
			Severity:    "low",
			Title:       "Analysis completed",
			Description: "Analysis completed for: " + todo.Content,
			TodoID:      todo.ID,
			Timestamp:   time.Now(),
		})
	}

	return findings
}

// containsFilesystemKeywords checks if a todo content contains filesystem operation keywords
func containsFilesystemKeywords(content string) bool {
	contentLower := strings.ToLower(content)
	filesystemKeywords := []string{
		"create directory", "mkdir", "create folder", "setup project", "initialize",
		"install", "setup monorepo", "create backend", "create frontend",
		"create the", "directory for", "backend directory", "frontend directory",
		"directory in", "directory called", "directory named", " directory ", "new directory",
	}

	for _, keyword := range filesystemKeywords {
		if strings.Contains(contentLower, keyword) {
			return true
		}
	}
	return false
}

// executeShellCommandTodo handles filesystem operations through shell commands
func executeShellCommandTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("🖥️ Executing shell command todo")

	// Use LLM to generate appropriate shell commands
	prompt := fmt.Sprintf(`You are an expert system administrator. Generate safe shell commands to accomplish this task:

Task: %s
Description: %s
Overall Goal: %s

Generate the appropriate shell commands to complete this task. Be very careful about:
1. Only use safe commands that won't harm the system
2. Create directories and files as needed
3. Follow standard conventions for project structure
4. Use relative paths from current directory
5. For multi-line file content, use SINGLE commands with proper heredoc syntax
6. Each array item should be ONE complete command, not broken into parts
7. For Go commands (go get, go mod, go build), always run them in the directory with go.mod
8. Use "cd directory && command" format for commands that need specific working directory

IMPORTANT: 
- When creating files with content, use: "cat > filename <<'EOF'\ncontent here\nEOF"  
- For Go module operations, use: "cd backend && go get package" or "cd backend && go mod tidy"
- Never run Go commands from root if the go.mod is in a subdirectory
- Use 'find' to locate files, 'grep' to search content, 'ls' to list directories
- Example: "find . -name '*.py' -path '*/routes*'" to find Python files in routes directories

Respond with JSON:
{
  "commands": ["command1", "command2", "command3"],
  "explanation": "What these commands accomplish",  
  "safety_notes": "Any important safety considerations"
}`, todo.Content, todo.Description, ctx.UserIntent)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at generating safe shell commands for development tasks. Always respond with valid JSON containing an array of shell commands."},
		{Role: "user", Content: prompt},
	}

	// Get LLM response for command generation
	response, tokenUsage, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, llm.GetSmartTimeout(ctx.Config, ctx.Config.OrchestrationModel, "analysis"))
	if err != nil {
		return fmt.Errorf("failed to generate shell commands: %w", err)
	}

	// Track token usage
	trackTokenUsage(ctx, tokenUsage, ctx.Config.OrchestrationModel)

	// Parse the response
	var commandPlan struct {
		Commands    []string `json:"commands"`
		Explanation string   `json:"explanation"`
		SafetyNotes string   `json:"safety_notes"`
	}

	clean, err := utils.ExtractJSON(response)
	if err != nil {
		return fmt.Errorf("failed to parse command plan: %w", err)
	}

	if err := json.Unmarshal([]byte(clean), &commandPlan); err != nil {
		return fmt.Errorf("failed to unmarshal command plan: %w", err)
	}

	// Log the plan
	ctx.Logger.LogProcessStep(fmt.Sprintf("📋 Execution plan: %s", commandPlan.Explanation))
	if commandPlan.SafetyNotes != "" {
		ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Safety notes: %s", commandPlan.SafetyNotes))
	}

	// Execute commands safely
	for i, command := range commandPlan.Commands {
		// Basic safety checks
		if containsUnsafeCommand(command) {
			return fmt.Errorf("unsafe command detected and blocked: %s", command)
		}

		// Validate shell command syntax
		if err := validateShellCommand(command); err != nil {
			ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Skipping invalid command: %s", err.Error()))
			continue
		}

		// Make command idempotent and prepare directories
		safeCommand := makeCommandIdempotent(command)
		safeCommand = prepareDirectoriesForCommand(safeCommand)

		ctx.Logger.LogProcessStep(fmt.Sprintf("🔧 Executing command %d/%d: %s", i+1, len(commandPlan.Commands), safeCommand))

		// Execute the command
		cmd := exec.Command("bash", "-c", safeCommand)
		cmd.Dir = "." // Execute in current directory

		output, err := cmd.CombinedOutput()
		if err != nil {
			ctx.Logger.LogProcessStep(fmt.Sprintf("❌ Command failed: %s", string(output)))
			return fmt.Errorf("command failed: %s - %w", command, err)
		}

		if len(output) > 0 {
			ctx.Logger.LogProcessStep(fmt.Sprintf("📤 Output: %s", string(output)))
		}
	}

	// Mark files as potentially modified since shell commands often create/modify files
	ctx.FilesModified = true

	// Store results
	ctx.AnalysisResults[todo.ID+"_shell_result"] = fmt.Sprintf("Successfully executed %d commands: %s", len(commandPlan.Commands), commandPlan.Explanation)

	ctx.Logger.LogProcessStep("✅ Shell command todo completed successfully")
	return nil
}

// prepareDirectoriesForCommand ensures directories exist before file operations
func prepareDirectoriesForCommand(command string) string {
	// Look for commands that create files in directories that might not exist
	if strings.Contains(command, "cat >") && strings.Contains(command, "/") {
		// Extract the file path from "cat > path/file.ext"
		parts := strings.Split(command, "cat >")
		if len(parts) > 1 {
			filePart := strings.TrimSpace(parts[1])
			if spaceIdx := strings.Index(filePart, " "); spaceIdx > 0 {
				filePart = filePart[:spaceIdx]
			}
			if strings.Contains(filePart, "/") {
				dirPath := filePart[:strings.LastIndex(filePart, "/")]
				return fmt.Sprintf("mkdir -p %s && %s", dirPath, command)
			}
		}
	}
	return command
}

// containsUnsafeCommand performs basic safety checks on shell commands
func containsUnsafeCommand(command string) bool {
	unsafePatterns := []string{
		"rm -rf /",
		"rm -rf ~",
		"rm -rf *",
		"sudo rm",
		"format",
		"del /",
		"> /dev/",
		"chmod 777",
		"curl.*|.*sh",
		"wget.*|.*sh",
	}

	cmdLower := strings.ToLower(strings.TrimSpace(command))
	for _, pattern := range unsafePatterns {
		if strings.Contains(cmdLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// makeCommandIdempotent modifies commands to be idempotent (safe to run multiple times)
func makeCommandIdempotent(command string) string {
	// Handle go mod init - check if go.mod already exists
	if strings.Contains(command, "go mod init") && !strings.Contains(command, "test -f") {
		parts := strings.Split(command, "&&")
		for i, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "go mod init") {
				// Make it conditional on go.mod not existing
				parts[i] = fmt.Sprintf("(test -f go.mod || %s)", part)
			}
		}
		return strings.Join(parts, " && ")
	}

	return command
}

// validateShellCommand performs additional validation on generated shell commands
func validateShellCommand(command string) error {
	// Check for common LLM mistakes
	problematicPatterns := []string{
		"package main", // Go source code being treated as shell command
		"import (",     // Go imports as shell command
		"func main",    // Go function as shell command
		"<<EOF\n",      // Malformed heredoc with newline
		"function ",    // JavaScript/TypeScript function
		"const ",       // JavaScript/TypeScript const
		"import React", // React imports
		"export ",      // ES6 exports
		"<!DOCTYPE",    // HTML
		"<html",        // HTML
		"<?xml",        // XML
	}

	for _, pattern := range problematicPatterns {
		if strings.Contains(command, pattern) {
			truncated := strings.TrimSpace(command)
			if len(truncated) > 50 {
				truncated = truncated[:50] + "..."
			}
			return fmt.Errorf("command appears to contain source code instead of shell syntax: %s", truncated)
		}
	}

	// Check for excessively long single commands (likely source code)
	if len(command) > 2000 && !strings.Contains(command, "&&") && !strings.Contains(command, "||") {
		return fmt.Errorf("command is suspiciously long and may contain source code: %d characters", len(command))
	}

	return nil
}

// executeContinuationTodo handles workflow continuation by generating the next batch of todos
func executeContinuationTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("🔄 Processing continuation todo - generating next phase")

	// If --skip-prompt is not set, prompt the user before continuing
	if !ctx.SkipPrompt {
		ctx.Logger.LogProcessStep("⏳ Requesting user approval for workflow continuation...")
		ui.Out().Printf("\n🔄 Workflow Continuation Required\n")
		ui.Out().Printf("The current phase is complete. Ready to continue with the next set of tasks?\n\n")
		ui.Out().Printf("Original request: %s\n\n", ctx.UserIntent)

		// Show completed tasks
		completedCount := 0
		for _, completedTodo := range ctx.Todos {
			if completedTodo.Status == "completed" && completedTodo.Content != todo.Content {
				completedCount++
			}
		}
		ui.Out().Printf("✅ Completed %d tasks in this phase\n\n", completedCount)

		ui.Out().Printf("Continue with next phase? (y/N): ")

		var response string
		fmt.Scanln(&response)

		if strings.ToLower(strings.TrimSpace(response)) != "y" {
			ctx.Logger.LogProcessStep("❌ User chose not to continue workflow")
			return fmt.Errorf("workflow continuation cancelled by user")
		}

		ctx.Logger.LogProcessStep("✅ User approved workflow continuation")
		ui.Out().Printf("\n🚀 Continuing with next phase...\n\n")
	}

	// Extract the workflow context from the current intent and completed todos
	completedTasks := []string{}
	for _, completedTodo := range ctx.Todos {
		if completedTodo.Content != todo.Content { // Don't include the continuation todo itself
			completedTasks = append(completedTasks, completedTodo.Content)
		}
	}

	// Create a continuation prompt that includes context about what's been done
	continuationPrompt := fmt.Sprintf(`CONTINUATION WORKFLOW

Original Request: %s

COMPLETED IN PREVIOUS PHASE:
%s

CONTINUATION TODO: %s
Description: %s

Based on the original request and what has been completed, generate the NEXT 10 todos to continue this workflow. Focus on the logical next steps that build upon the completed work.

Consider:
- What components/features are still needed?
- What files/directories need to be created next?  
- What configuration or setup steps are missing?
- What testing or validation needs to happen?

Generate todos that continue naturally from where the previous phase left off.`,
		ctx.UserIntent,
		strings.Join(completedTasks, "\n- "),
		todo.Content,
		todo.Description)

	// Use the same todo creation logic but with continuation context
	response, tokenUsage, err := llm.GetLLMResponseWithTools(
		ctx.Config.OrchestrationModel,
		[]prompts.Message{{Role: "user", Content: continuationPrompt}},
		"You are an expert project manager continuing a complex workflow. Generate the next logical set of todos.",
		ctx.Config,
		60*time.Second,
	)

	if err != nil {
		return fmt.Errorf("failed to generate continuation todos: %w", err)
	}

	// Track token usage
	trackTokenUsage(ctx, tokenUsage, ctx.Config.OrchestrationModel)

	// Parse the new todos from the response
	newTodos, err := parseTodosFromResponse(response)
	if err != nil {
		return fmt.Errorf("failed to parse continuation todos: %w", err)
	}

	// Add the new todos to the context (they'll be picked up in the next execution cycle)
	ctx.Todos = append(ctx.Todos, newTodos...)

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Generated %d continuation todos for next phase", len(newTodos)))

	// Mark this continuation todo as completed
	return nil
}

// executeParallelTodos executes a set of independent todos concurrently
// This is particularly useful for documentation and analysis tasks that don't modify the same files
func executeParallelTodos(ctx *SimplifiedAgentContext, todos []TodoItem) error {
	ctx.Logger.LogProcessStep(fmt.Sprintf("🚀 Starting parallel execution of %d todos", len(todos)))

	// Create an optimized worker pool with controlled concurrency
	maxWorkers := getOptimalWorkerCount(len(todos), ctx.Config.OrchestrationModel)
	if len(todos) < maxWorkers {
		maxWorkers = len(todos)
	}

	// Create channels for work distribution
	todosChan := make(chan TodoItem, len(todos))
	resultsChan := make(chan ParallelTodoResult, len(todos))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			executeParallelWorker(ctx, workerID, todosChan, resultsChan)
		}(i)
	}

	// Send todos to workers
	go func() {
		defer close(todosChan)
		for _, todo := range todos {
			todosChan <- todo
		}
	}()

	// Collect results
	var results []ParallelTodoResult
	for i := 0; i < len(todos); i++ {
		result := <-resultsChan
		results = append(results, result)
	}

	// Wait for all workers to complete
	wg.Wait()
	close(resultsChan)

	// Process results and update context
	var errors []error
	completedCount := 0
	for _, result := range results {
		if result.Error != nil {
			ctx.Logger.LogError(fmt.Errorf("parallel todo failed: %w", result.Error))
			errors = append(errors, result.Error)
			// Update todo status in context
			for i := range ctx.Todos {
				if ctx.Todos[i].ID == result.TodoID {
					ctx.Todos[i].Status = "failed"
					break
				}
			}
		} else {
			completedCount++
			// Update todo status and store results
			for i := range ctx.Todos {
				if ctx.Todos[i].ID == result.TodoID {
					ctx.Todos[i].Status = "completed"
					break
				}
			}
			// Store analysis results and track token usage
			if result.Output != "" {
				ctx.AnalysisResults[result.TodoID] = result.Output
			}
			if result.TokenUsage != nil {
				trackTokenUsage(ctx, result.TokenUsage, result.ModelUsed)
			}
		}
	}

	ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Parallel execution completed: %d/%d successful", completedCount, len(todos)))

	// Return error if any todos failed
	if len(errors) > 0 {
		return fmt.Errorf("parallel execution had %d failures", len(errors))
	}

	return nil
}

// ParallelTodoResult represents the result of a parallel todo execution
type ParallelTodoResult struct {
	TodoID     string
	Output     string
	Error      error
	TokenUsage *llm.TokenUsage
	ModelUsed  string
}

// executeParallelWorker is a worker function that processes todos from a channel
func executeParallelWorker(ctx *SimplifiedAgentContext, workerID int, todosChan <-chan TodoItem, resultsChan chan<- ParallelTodoResult) {
	for todo := range todosChan {
		ctx.Logger.LogProcessStep(fmt.Sprintf("🔧 Worker %d executing: %s", workerID, todo.Content))

		// Create a copy of the context for this worker to avoid race conditions
		workerCtx := *ctx
		workerCtx.CurrentTodo = &todo

		// Execute the todo
		var result ParallelTodoResult
		result.TodoID = todo.ID

		// Only execute analysis and documentation todos in parallel
		// Code modification todos should still be sequential to avoid conflicts
		executionType := analyzeTodoExecutionType(todo.Content, todo.Description)
		switch executionType {
		case ExecutionTypeAnalysis:
			err := executeAnalysisTodo(&workerCtx, &todo)
			if err != nil {
				result.Error = err
			} else {
				result.Output = workerCtx.AnalysisResults[todo.ID]
			}
		case ExecutionTypeDirectEdit:
			// For documentation files, we can execute in parallel
			if isDocumentationTodo(todo) {
				err := executeDirectEditTodo(&workerCtx, &todo)
				if err != nil {
					result.Error = err
				}
			} else {
				// Skip non-documentation edits in parallel mode
				result.Error = fmt.Errorf("todo %s skipped in parallel mode (requires sequential execution)", todo.Content)
			}
		default:
			// Skip complex todos in parallel mode
			result.Error = fmt.Errorf("todo %s skipped in parallel mode (requires sequential execution)", todo.Content)
		}

		resultsChan <- result
		ctx.Logger.LogProcessStep(fmt.Sprintf("✅ Worker %d completed: %s", workerID, todo.Content))
	}
}

// isDocumentationTodo checks if a todo is related to documentation generation
func isDocumentationTodo(todo TodoItem) bool {
	content := strings.ToLower(todo.Content + " " + todo.Description)
	docKeywords := []string{
		"documentation", "docs", "api_docs", "readme", "doc", "document",
		"generate.*md", "create.*md", "write.*md",
		"markdown", ".md",
	}

	for _, keyword := range docKeywords {
		if strings.Contains(content, keyword) {
			return true
		}
	}

	// Check file path for documentation files
	if todo.FilePath != "" {
		filePath := strings.ToLower(todo.FilePath)
		if strings.HasSuffix(filePath, ".md") || strings.Contains(filePath, "doc") || strings.Contains(filePath, "readme") {
			return true
		}
	}

	return false
}

// canExecuteInParallel determines if a set of todos can be executed in parallel
func canExecuteInParallel(todos []TodoItem) bool {
	// Check if todos are independent (don't modify the same files)
	fileMap := make(map[string]bool)
	for _, todo := range todos {
		// Skip analysis todos (they don't modify files)
		executionType := analyzeTodoExecutionType(todo.Content, todo.Description)
		if executionType == ExecutionTypeAnalysis {
			continue
		}

		// Check for file conflicts
		if todo.FilePath != "" {
			if fileMap[todo.FilePath] {
				return false // Same file targeted by multiple todos
			}
			fileMap[todo.FilePath] = true
		}

		// Only allow documentation and analysis todos in parallel
		if executionType != ExecutionTypeAnalysis && executionType != ExecutionTypeDirectEdit {
			return false
		}
		if executionType == ExecutionTypeDirectEdit && !isDocumentationTodo(todo) {
			return false
		}
	}

	return true
}

// parseTodosFromResponse extracts and parses todos from an LLM response
func parseTodosFromResponse(response string) ([]TodoItem, error) {
	// Parse JSON response - handle reasoning model responses that include thinking blocks
	clean, err := utils.ExtractJSON(response)
	if err != nil {
		// Try to extract JSON from the end of the response (after thinking blocks)
		if lastBracket := strings.LastIndex(response, "["); lastBracket != -1 {
			potentialJSON := response[lastBracket:]
			if json.Valid([]byte(potentialJSON)) {
				clean = potentialJSON
			} else {
				return nil, fmt.Errorf("failed to extract valid JSON from response")
			}
		} else {
			return nil, fmt.Errorf("no JSON array found in response")
		}
	}

	// Parse todo structures from JSON
	var todos []struct {
		Content     string `json:"content"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
		FilePath    string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(clean), &todos); err != nil {
		return nil, fmt.Errorf("failed to parse todos JSON: %w", err)
	}

	// Convert to TodoItem structures
	var todoItems []TodoItem
	for _, todo := range todos {
		todoItems = append(todoItems, TodoItem{
			ID:          generateTodoID(),
			Content:     todo.Content,
			Description: todo.Description,
			Priority:    todo.Priority,
			FilePath:    todo.FilePath,
			Status:      "pending",
		})
	}

	return todoItems, nil
}

// createDocumentationTodos creates documentation-specific todos
func createDocumentationTodos(ctx *SimplifiedAgentContext) error {
	// Build documentation-specific context prompt
	var contextInfo strings.Builder
	contextInfo.WriteString(fmt.Sprintf(`You are an expert technical writer and software developer. Create specific todos for generating comprehensive documentation.

User Request: "%s"

## Project Context Information
`, ctx.UserIntent))

	// Add project context if available
	if ctx.ProjectContext != nil {
		projectCtx := ctx.ProjectContext
		if projectCtx.Language != "" {
			contextInfo.WriteString(fmt.Sprintf("Language: %s\n", projectCtx.Language))
		}
		if projectCtx.Framework != "" {
			contextInfo.WriteString(fmt.Sprintf("Framework: %s\n", projectCtx.Framework))
		}
		if projectCtx.ProjectType != "" {
			contextInfo.WriteString(fmt.Sprintf("Project Type: %s\n", projectCtx.ProjectType))
		}
		if len(projectCtx.Patterns) > 0 {
			contextInfo.WriteString("Patterns:\n")
			for key, value := range projectCtx.Patterns {
				contextInfo.WriteString(fmt.Sprintf("- %s: %s\n", key, value))
			}
		}
	}

	contextInfo.WriteString(fmt.Sprintf(`
## Workspace Context
%s

DOCUMENTATION GUIDANCE:
- Focus on creating comprehensive, well-structured documentation
- For API projects: Document endpoints, methods, parameters, authentication, responses
- Use appropriate output formats (Markdown for most cases)
- Analyze existing code to extract accurate information
- Include examples where helpful
- Structure documentation logically
- Create new documentation files as needed

Please create a JSON array of documentation todos. Each todo should:
- Be specific to documentation generation
- Use analysis tasks to gather information first
- Create documentation files as the final step
- Include clear file paths for documentation outputs
- Be prioritized appropriately (analysis first, then creation)

Format:
[
  {
    "content": "Brief description of the todo",
    "description": "More detailed explanation of what needs to be done",
    "priority": 1,
    "file_path": "path/to/file.ext"
  }
]`, func() string {
		return workspace.GetProgressiveWorkspaceContext(ctx.UserIntent, ctx.Config)
	}()))

	return createTodosFromPrompt(ctx, contextInfo.String())
}

// createCreationTodos creates file/content creation-specific todos
func createCreationTodos(ctx *SimplifiedAgentContext) error {
	// Build creation-specific context prompt
	var contextInfo strings.Builder
	contextInfo.WriteString(fmt.Sprintf(`You are an expert software developer. Create specific todos for creating new files, directories, or project structures.

User Request: "%s"

## Project Context Information
`, ctx.UserIntent))

	// Add project context if available
	if ctx.ProjectContext != nil {
		projectCtx := ctx.ProjectContext
		if projectCtx.Language != "" {
			contextInfo.WriteString(fmt.Sprintf("Language: %s\n", projectCtx.Language))
		}
		if projectCtx.Framework != "" {
			contextInfo.WriteString(fmt.Sprintf("Framework: %s\n", projectCtx.Framework))
		}
		if projectCtx.ProjectType != "" {
			contextInfo.WriteString(fmt.Sprintf("Project Type: %s\n", projectCtx.ProjectType))
		}
	}

	contextInfo.WriteString(fmt.Sprintf(`
## Workspace Context
%s

CREATION GUIDANCE:
- Focus on creating new files, directories, and structures
- Use appropriate file naming conventions
- Follow project patterns and conventions
- Create supporting files as needed (tests, configs, etc.)
- Ensure proper directory structure
- Use shell commands for directory creation when needed

Please create a JSON array of creation todos. Each todo should:
- Be specific to creating new content
- Create one logical unit at a time
- Include proper file paths and directory structure
- Consider dependencies (create directories before files)
- Be prioritized appropriately

Format:
[
  {
    "content": "Brief description of what to create",
    "description": "More detailed explanation of the creation task",
    "priority": 1,
    "file_path": "path/to/new/file.ext"
  }
]`, func() string {
		return workspace.GetProgressiveWorkspaceContext(ctx.UserIntent, ctx.Config)
	}()))

	return createTodosFromPrompt(ctx, contextInfo.String())
}

// createAnalysisTodos creates analysis-specific todos
func createAnalysisTodos(ctx *SimplifiedAgentContext) error {
	// Build analysis-specific context prompt
	var contextInfo strings.Builder
	contextInfo.WriteString(fmt.Sprintf(`You are an expert software analyzer and code reviewer. Create specific todos for analyzing code, systems, or project structures.

User Request: "%s"

## Workspace Context
%s

ANALYSIS GUIDANCE:
- Focus on understanding and analyzing existing code/systems
- Use tools to explore the codebase thoroughly
- Identify patterns, issues, opportunities for improvement
- Provide detailed insights and findings
- Don't make code changes - analysis only
- Use grep, file reading, and exploration tools extensively

Please create a JSON array of analysis todos. Each todo should:
- Be specific to analysis tasks
- Use appropriate analysis tools and techniques
- Focus on understanding rather than modifying
- Build knowledge progressively
- Be prioritized logically

Format:
[
  {
    "content": "Brief description of analysis task",
    "description": "More detailed explanation of what to analyze",
    "priority": 1,
    "file_path": ""
  }
]`, ctx.UserIntent, func() string {
		return workspace.GetProgressiveWorkspaceContext(ctx.UserIntent, ctx.Config)
	}()))

	return createTodosFromPrompt(ctx, contextInfo.String())
}

// createTodosFromPrompt is a helper function to create todos from a given prompt
func createTodosFromPrompt(ctx *SimplifiedAgentContext, prompt string) error {
	messages := []prompts.Message{
		{Role: "user", Content: prompt},
	}

	response, tokenUsage, err := executeAgentWorkflowWithTools(ctx, messages, "todo_creation")
	if err != nil {
		return fmt.Errorf("failed to get todos response: %w", err)
	}

	// Track token usage
	if tokenUsage != nil {
		trackTokenUsage(ctx, tokenUsage, ctx.Config.OrchestrationModel)
	}

	// Extract and parse todos
	todoItems, err := parseTodosFromResponse(response)
	if err != nil {
		return fmt.Errorf("failed to parse todos from response: %w", err)
	}

	ctx.Todos = todoItems
	return nil
}

// getOptimalWorkerCount determines the optimal number of workers based on task count and model
func getOptimalWorkerCount(todoCount int, modelName string) int {
	baseWorkers := 3 // Conservative default

	// Adjust based on provider characteristics
	if strings.Contains(strings.ToLower(modelName), "groq") {
		// Groq is fast, can handle more concurrent requests
		baseWorkers = 5
	} else if strings.Contains(strings.ToLower(modelName), "deepinfra") {
		// DeepInfra has rate limits, be more conservative
		baseWorkers = 2
	} else if strings.Contains(strings.ToLower(modelName), "openai") {
		// OpenAI has good rate limits
		baseWorkers = 4
	}

	// Scale based on todo count but cap at reasonable limit
	if todoCount <= 2 {
		return min(todoCount, 2)
	} else if todoCount <= 5 {
		return min(baseWorkers, todoCount)
	} else {
		return baseWorkers // Cap at base for stability
	}
}
