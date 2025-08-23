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

	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// createTodos generates a list of todos based on user intent
func createTodos(ctx *SimplifiedAgentContext) error {
	// Build context-aware prompt
	var contextInfo strings.Builder
	contextInfo.WriteString(fmt.Sprintf(`You are an expert software developer. Break down this user request into specific, actionable todos grounded in the provided workspace context.

User Request: "%s"

## Workspace Context
%s`, ctx.UserIntent, func() string {
		// Use minimal workspace context for cleaner, more focused prompts
		minimalContext := workspace.GetMinimalWorkspaceContext(ctx.UserIntent, ctx.Config)
		if minimalContext == "" {
			// Fallback to basic workspace context if minimal context fails
			wc := workspace.GetWorkspaceContext(ctx.UserIntent, ctx.Config)
			if len(wc) > 8000 {
				return wc[:8000]
			}
			return wc
		}
		return minimalContext
	}()))

	// Add rollover context from previous analysis if available
	if ctx.ContextManager != nil && ctx.PersistentCtx != nil {
		rolloverContext := ctx.ContextManager.GetRolloverContext(ctx.PersistentCtx)

		if recentFindings, ok := rolloverContext["recent_findings"]; ok {
			if findings, ok := recentFindings.([]AnalysisFinding); ok && len(findings) > 0 {
				contextInfo.WriteString("\n\nRECENT ANALYSIS FINDINGS:\n")
				for _, finding := range findings {
					contextInfo.WriteString(fmt.Sprintf("- %s: %s\n", finding.Type, finding.Title))
				}
			}
		}

		if keyKnowledge, ok := rolloverContext["key_knowledge"]; ok {
			if knowledge, ok := keyKnowledge.([]KnowledgeItem); ok && len(knowledge) > 0 {
				contextInfo.WriteString("\n\nACCUMULATED KNOWLEDGE:\n")
				for _, item := range knowledge {
					contextInfo.WriteString(fmt.Sprintf("- %s: %s\n", item.Category, item.Title))
				}
			}
		}

		if codePatterns, ok := rolloverContext["code_patterns"]; ok {
			if patterns, ok := codePatterns.([]CodePattern); ok && len(patterns) > 0 {
				contextInfo.WriteString("\n\nIDENTIFIED CODE PATTERNS:\n")
				for _, pattern := range patterns {
					contextInfo.WriteString(fmt.Sprintf("- %s: %s\n", pattern.Type, pattern.Name))
				}
			}
		}
	}

	contextInfo.WriteString(`

Guidance:
- Use tools to validate and ground todos in reality (read files, search code, list files): prefer reading files (workspace_context/read_file), searching code (grep_search), and using workspace_context to find targets.
- If uncertain about exact locations or details, include an initial "analysis" todo that explicitly uses tools to gather the needed evidence before edits.
- Avoid speculative or ungrounded todos.
- Consider the recent analysis findings and accumulated knowledge when creating todos.

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

Focus on concrete changes that can be made to the codebase. Return ONLY the JSON array.`)

	prompt := contextInfo.String()

	messages := []prompts.Message{
		{Role: "system", Content: "You create specific, actionable development todos. Ground todos in workspace context and prefer referencing actual files. Strongly prefer using tools (workspace_context, read_file, grep_search) to validate assumptions when planning. If uncertain, include an initial analysis todo that uses tools to gather evidence. Always return valid JSON."},
		{Role: "user", Content: prompt},
	}

	// Try primary model with smart timeout
	response, tokenUsage, err := llm.GetLLMResponse(ctx.Config.OrchestrationModel, messages, "", ctx.Config, llm.GetSmartTimeout(ctx.Config, ctx.Config.OrchestrationModel, "analysis"))

	// If primary model fails, try with fallback model and extended timeout
	if err != nil {
		ctx.Logger.LogProcessStep(fmt.Sprintf("⚠️ Primary model failed (%v), trying fallback approach", err))

		// Try with a simpler prompt and extended timeout
		fallbackMessages := []prompts.Message{
			{Role: "system", Content: "You create development todos. Keep it simple and return JSON only."},
			{Role: "user", Content: fmt.Sprintf("Create 1-2 simple todos for: %s\nReturn JSON array only.", ctx.UserIntent)},
		}

		// Use extended timeout for fallback
		fallbackTimeout := time.Duration(float64(llm.GetSmartTimeout(ctx.Config, ctx.Config.OrchestrationModel, "analysis")) * 1.5)
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

	// Check for documentation updates more flexibly (handle README.md, readme files, etc.)
	if strings.Contains(contentLower, "update") && (strings.Contains(contentLower, "readme") || strings.Contains(contentLower, "documentation") || strings.Contains(contentLower, "docs")) {
		return ExecutionTypeDirectEdit
	}

	// Default to code command for anything involving code changes
	return ExecutionTypeCodeCommand
}

// executeAnalysisTodo handles analysis-only todos with direct LLM exploration
func executeAnalysisTodo(ctx *SimplifiedAgentContext, todo *TodoItem) error {
	ctx.Logger.LogProcessStep("🔍 Performing analysis (no code changes)")

	// Get minimal workspace context for analysis
	minimalContext := workspace.GetMinimalWorkspaceContext(ctx.UserIntent, ctx.Config)
	if minimalContext == "" {
		minimalContext = "Workspace context not available"
	}

	prompt := fmt.Sprintf(`You are analyzing the codebase to help with: "%s"

Context from overall task: "%s"

## Workspace Context
%s

Please analyze and provide insights on: %s

CRITICAL: Use tools to gather evidence before making any analysis or recommendations. Do not make assumptions about the codebase structure or content.

REQUIRED TOOLS - Use these in order:
1. **workspace_context(action="load_tree")** - Get complete file/directory structure
2. **workspace_context(action="search_keywords", query="relevant terms")** - Find files containing specific terms
3. **run_shell_command(command="ls -la pkg/")** - List contents of specific directories (example: list pkg directory)
4. **run_shell_command(command="grep -r 'func.*main' .")** - Search for specific patterns (example: find main functions)
5. **read_file(file_path="main.go")** - Read specific files for detailed analysis

AFTER gathering evidence with tools, provide your analysis with:
- Concrete file references and line numbers
- Evidence-based findings, not assumptions
- Specific recommendations with implementation details
- Code examples where relevant

Remember: Always use tools first, then analyze based on actual evidence from the codebase.
`, ctx.UserIntent, todo.Content, minimalContext, todo.Description)

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

// applyDirectEdit applies simple changes directly to files
func applyDirectEdit(filePath, newContent string, logger *utils.Logger) error {
	// Write new content
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

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
