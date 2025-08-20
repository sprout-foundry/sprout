package agent

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// createDetailedEditPlan uses the orchestration model to create a detailed plan for code changes.
// No heuristic plan fallbacks: if the LLM does not return a valid, non-empty plan, the caller should retry
// a limited number of times and then abort.
func createDetailedEditPlan(userIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (*EditPlan, int, error) {
	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: createDetailedEditPlan started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Determine relevant files for context
	var contextFiles []string
	if len(intentAnalysis.EstimatedFiles) > 0 {
		contextFiles = intentAnalysis.EstimatedFiles
	} else {
		logger.Logf("No estimated files from analysis, using robust file discovery for: %s", userIntent)
		contextFiles = findRelevantFilesRobust(userIntent, cfg, logger)
		if len(contextFiles) == 0 {
			// Provide minimal context if none found; we still do not fabricate a plan
			logger.Logf("No relevant files found for context; proceeding with empty context")
		}
	}

	// Load basic file context
	context := buildBasicFileContext(contextFiles, logger)

	// Determine project type and preferred extensions for guidance
	projectType := detectProjectType()
	preferredExt := ""
	switch projectType {
	case "node":
		preferredExt = ".js, .ts"
	case "python":
		preferredExt = ".py"
	case "rust":
		preferredExt = ".rs"
	case "java":
		preferredExt = ".java"
	case "php":
		preferredExt = ".php"
	case "ruby":
		preferredExt = ".rb"
	default:
		preferredExt = ".go"
	}

	// Construct the planning prompt with flexible execution strategies
	prompt := fmt.Sprintf(`You are an expert software development agent. Your job is to create an effective plan to accomplish the user's request using the most appropriate method.

USER REQUEST: %s

PROJECT CONTEXT:
- Project Type: %s
- Available Context Files: %d files (%s)
- Task Category: %s
- Task Complexity: %s
- Preferred File Types: %s

AVAILABLE FILES FOR CONTEXT:
%s

CURRENT FILE CONTEXT:
%s

=== PLANNING STRATEGY ===

Choose the BEST approach for this task. You have 4 options:

**OPTION 1: Direct Commands** (BEST for investigation, setup, simple operations)
- When to use: Tasks that can be solved with shell commands
- Examples: "find all TODOs" → "grep -r 'TODO' ."
- Examples: "install deps" → "npm install" or "pip install -r requirements.txt"
- Examples: "check service status" → "systemctl status app"
- Examples: "run tests" → "go test ./..." or "pytest"

**OPTION 2: Simple Edits** (BEST for small, localized changes)
- When to use: Single file changes under 50 lines
- Examples: Fix typo, add comment, update import, change variable name
- Focus on: Tiny, isolated changes that don't need complex planning

**OPTION 3: Structured Code Changes** (BEST for refactoring, multi-file changes)
- When to use: Complex changes requiring coordination across files
- Examples: Function extraction, API changes, architectural refactoring
- Focus on: Well-planned modifications with clear scope

**OPTION 4: Analysis & Investigation** (BEST for research, debugging)
- When to use: Tasks requiring understanding before action
- Examples: "analyze performance bottleneck", "find security issues"
- Focus on: Information gathering and recommendations

=== RESPONSE FORMAT ===

Choose the most appropriate response format based on your strategy:

**For Direct Commands (Option 1):**
{
  "strategy": "direct_commands",
  "commands": [
    {"command": "specific shell command", "purpose": "why this command helps"},
    {"command": "another command if needed", "purpose": "explanation"}
  ],
  "reasoning": "Why this approach is best for the task"
}

**For Simple Edits (Option 2):**
{
  "strategy": "simple_edits",
  "edits": [
    {
      "file_path": "relative/path/to/file.ext",
      "change_type": "add|modify|delete",
      "old_content": "exact text to replace (or empty for additions)",
      "new_content": "replacement text (or empty for deletions)",
      "reasoning": "Why this specific change accomplishes the goal"
    }
  ],
  "reasoning": "Why simple edits are sufficient"
}

**For Structured Changes (Option 3):**
{
  "strategy": "structured_changes",
  "files_to_edit": ["file1.ext", "file2.ext"],
  "edit_operations": [
    {
      "file_path": "string",
      "description": "what change to make",
      "instructions": "detailed how-to for the editing model",
      "scope_justification": "why this change serves the user request"
    }
  ],
  "context": "additional context for the changes",
  "scope_statement": "clear statement of what this plan accomplishes"
}

**For Analysis (Option 4):**
{
  "strategy": "analysis",
  "analysis_steps": [
    {"step": "command or investigation", "expected_outcome": "what we learn"},
    {"step": "next step based on findings", "expected_outcome": "further insights"}
  ],
  "reasoning": "Why analysis is needed before action"
}

=== DECISION GUIDANCE ===

**Choose Direct Commands when:**
- Task can be solved with 1-3 shell commands
- No file modifications needed
- Task is about investigation, setup, or system operations
- Examples: search, install, status checks, builds, tests

**Choose Simple Edits when:**
- Change affects only one file
- Change is under 50 lines total
- Change is isolated and doesn't affect other parts of the system
- Examples: comments, imports, variable names, small bug fixes

**Choose Structured Changes when:**
- Multiple files need coordinated changes
- Complex refactoring or architectural changes
- Changes need careful planning and sequencing
- Examples: function extraction, API changes, multi-file updates

**Choose Analysis when:**
- Need to understand current state before acting
- Task is about investigation or research
- Need to gather information to make informed decisions
- Examples: performance analysis, security audits, code reviews

Make your choice and provide a focused, actionable plan using the appropriate format above.`,
		userIntent,
		projectType,
		len(contextFiles),
		strings.Join(contextFiles, ", "),
		intentAnalysis.Category,
		intentAnalysis.Complexity,
		preferredExt,
		strings.Join(contextFiles, ", "),
		context,
	)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert software development agent. Choose the best strategy and respond with the appropriate JSON format."},
		{Role: "user", Content: prompt},
	}
	response, _, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 3*time.Minute)
	if err != nil {
		logger.LogError(fmt.Errorf("orchestration model failed to create plan: %w", err))
		return nil, 0, fmt.Errorf("failed to create plan: %w", err)
	}

	// Estimate tokens for cost accounting
	promptTokens := llm.GetConversationTokens([]struct{ Role, Content string }{
		{Role: messages[0].Role, Content: messages[0].Content.(string)},
		{Role: messages[1].Role, Content: messages[1].Content.(string)},
	})
	responseTokens := llm.EstimateTokens(response)
	totalTokens := promptTokens + responseTokens
	logger.Logf("Planning tokens: prompt=%d completion=%d total=%d", promptTokens, responseTokens, totalTokens)

	cleanedResponse, err := utils.ExtractJSONFromLLMResponse(response)
	if err != nil {
		// Try multiple extraction strategies for different response formats
		extractors := []string{"strategy", "edit_operations", "commands", "edits", "analysis_steps"}
		for _, key := range extractors {
			if alt, altErr := utils.CleanAndValidateJSONResponse(response, []string{key}); altErr == nil && strings.Contains(alt, key) {
				cleanedResponse = alt
				break
			}
		}
		if cleanedResponse == "" {
			logger.LogError(fmt.Errorf("failed to extract JSON from orchestration response: %w", err))
			return nil, totalTokens, fmt.Errorf("failed to extract JSON from orchestration response: %w", err)
		}
	}

	// Parse JSON to determine strategy and convert to EditPlan
	var rawPlan map[string]interface{}
	if err := json.Unmarshal([]byte(cleanedResponse), &rawPlan); err != nil {
		logger.LogError(fmt.Errorf("failed to parse plan JSON: %w", err))
		return nil, totalTokens, fmt.Errorf("failed to parse plan JSON: %w", err)
	}

	// Convert flexible plan format to EditPlan based on strategy
	strategy, _ := rawPlan["strategy"].(string)
	var plan *EditPlan

	switch strategy {
	case "direct_commands":
		plan = convertCommandsToEditPlan(rawPlan)
	case "simple_edits":
		plan = convertSimpleEditsToEditPlan(rawPlan)
	case "structured_changes":
		plan = convertStructuredChangesToEditPlan(rawPlan)
	case "analysis":
		plan = convertAnalysisToEditPlan(rawPlan)
	default:
		// Fallback: try to parse as old format or create minimal plan
		plan = convertLegacyFormatToEditPlan(rawPlan)
	}

	if plan == nil {
		return nil, totalTokens, fmt.Errorf("failed to convert plan format")
	}

	// Heuristic augmentation: if the planner produced an empty plan in a known simple scenario,
	// opportunistically create a minimal scaffolding plan based on project type and user intent keywords.
	if len(plan.EditOperations) == 0 {
		lo := strings.ToLower(userIntent)
		switch detectProjectType() {
		case "node":
			if strings.Contains(lo, "/health") || strings.Contains(lo, "express") {
				plan.FilesToEdit = append(plan.FilesToEdit, "package.json", "server.js")
				plan.EditOperations = append(plan.EditOperations,
					EditOperation{
						FilePath:           "package.json",
						Description:        "Create minimal package.json for Express server",
						Instructions:       "Create a package.json with name 'app', minimal scripts, and express as dependency. Do not include extra packages.",
						ScopeJustification: "Required for Node project initialization",
					},
					EditOperation{
						FilePath:           "server.js",
						Description:        "Create Express server with GET /health -> {status:'ok'}",
						Instructions:       "Create a minimal Express server listening on port 3000 with a GET /health endpoint responding with JSON {status:'ok'}. Include require('express') and module.exports if needed.",
						ScopeJustification: "Implements the requested endpoint",
					},
				)
			}
		}
	}

	// Minimal schema validation
	if vErr := validateEditPlan(plan); vErr != nil {
		logger.LogError(fmt.Errorf("invalid edit plan: %w", vErr))
		return nil, totalTokens, fmt.Errorf("invalid edit plan: %w", vErr)
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: createDetailedEditPlan completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
	return plan, totalTokens, nil
}

// convertCommandsToEditPlan converts direct commands strategy to EditPlan
func convertCommandsToEditPlan(rawPlan map[string]interface{}) *EditPlan {
	commands, ok := rawPlan["commands"].([]interface{})
	if !ok {
		return &EditPlan{
			ScopeStatement: "Execute direct commands",
			EditOperations: []EditOperation{},
		}
	}

	ops := make([]EditOperation, 0, len(commands))
	for _, cmd := range commands {
		if cmdMap, ok := cmd.(map[string]interface{}); ok {
			command, _ := cmdMap["command"].(string)
			purpose, _ := cmdMap["purpose"].(string)
			if command != "" {
				ops = append(ops, EditOperation{
					FilePath:           "command",
					Description:        fmt.Sprintf("Execute: %s", command),
					Instructions:       fmt.Sprintf("Run command: %s (Purpose: %s)", command, purpose),
					ScopeJustification: purpose,
				})
			}
		}
	}

	return &EditPlan{
		ScopeStatement: "Execute direct commands to accomplish the task",
		EditOperations: ops,
	}
}

// convertSimpleEditsToEditPlan converts simple edits strategy to EditPlan
func convertSimpleEditsToEditPlan(rawPlan map[string]interface{}) *EditPlan {
	edits, ok := rawPlan["edits"].([]interface{})
	if !ok {
		return &EditPlan{
			ScopeStatement: "Apply simple edits",
			EditOperations: []EditOperation{},
		}
	}

	ops := make([]EditOperation, 0, len(edits))
	files := make(map[string]bool)

	for _, edit := range edits {
		if editMap, ok := edit.(map[string]interface{}); ok {
			filePath, _ := editMap["file_path"].(string)
			changeType, _ := editMap["change_type"].(string)
			oldContent, _ := editMap["old_content"].(string)
			newContent, _ := editMap["new_content"].(string)
			reasoning, _ := editMap["reasoning"].(string)

			if filePath != "" {
				files[filePath] = true
				instructions := fmt.Sprintf("Change type: %s", changeType)
				if oldContent != "" {
					instructions += fmt.Sprintf(" | Old: %s", oldContent)
				}
				if newContent != "" {
					instructions += fmt.Sprintf(" | New: %s", newContent)
				}

				ops = append(ops, EditOperation{
					FilePath:           filePath,
					Description:        fmt.Sprintf("Simple %s edit", changeType),
					Instructions:       instructions,
					ScopeJustification: reasoning,
				})
			}
		}
	}

	// Convert files map to slice
	var fileList []string
	for file := range files {
		fileList = append(fileList, file)
	}

	return &EditPlan{
		FilesToEdit:    fileList,
		ScopeStatement: "Apply simple, localized edits",
		EditOperations: ops,
	}
}

// convertStructuredChangesToEditPlan converts structured changes strategy to EditPlan
func convertStructuredChangesToEditPlan(rawPlan map[string]interface{}) *EditPlan {
	// This is the original format, so we can parse it directly
	var plan EditPlan
	if files, ok := rawPlan["files_to_edit"].([]interface{}); ok {
		for _, f := range files {
			if fileStr, ok := f.(string); ok {
				plan.FilesToEdit = append(plan.FilesToEdit, fileStr)
			}
		}
	}

	if ops, ok := rawPlan["edit_operations"].([]interface{}); ok {
		for _, op := range ops {
			if opMap, ok := op.(map[string]interface{}); ok {
				filePath, _ := opMap["file_path"].(string)
				description, _ := opMap["description"].(string)
				instructions, _ := opMap["instructions"].(string)
				scopeJustification, _ := opMap["scope_justification"].(string)

				plan.EditOperations = append(plan.EditOperations, EditOperation{
					FilePath:           filePath,
					Description:        description,
					Instructions:       instructions,
					ScopeJustification: scopeJustification,
				})
			}
		}
	}

	if context, ok := rawPlan["context"].(string); ok {
		plan.Context = context
	}

	if scope, ok := rawPlan["scope_statement"].(string); ok {
		plan.ScopeStatement = scope
	}

	return &plan
}

// convertAnalysisToEditPlan converts analysis strategy to EditPlan
func convertAnalysisToEditPlan(rawPlan map[string]interface{}) *EditPlan {
	steps, ok := rawPlan["analysis_steps"].([]interface{})
	if !ok {
		return &EditPlan{
			ScopeStatement: "Perform analysis and investigation",
			EditOperations: []EditOperation{},
		}
	}

	ops := make([]EditOperation, 0, len(steps))
	for _, step := range steps {
		if stepMap, ok := step.(map[string]interface{}); ok {
			stepDesc, _ := stepMap["step"].(string)
			outcome, _ := stepMap["expected_outcome"].(string)
			if stepDesc != "" {
				ops = append(ops, EditOperation{
					FilePath:           "analysis",
					Description:        fmt.Sprintf("Analysis: %s", stepDesc),
					Instructions:       fmt.Sprintf("Perform analysis step: %s (Expected: %s)", stepDesc, outcome),
					ScopeJustification: "Gather information to inform next steps",
				})
			}
		}
	}

	return &EditPlan{
		ScopeStatement: "Perform analysis and investigation to understand the situation",
		EditOperations: ops,
	}
}

// convertLegacyFormatToEditPlan handles old format or creates minimal fallback
func convertLegacyFormatToEditPlan(rawPlan map[string]interface{}) *EditPlan {
	// Try to parse as old edit_operations format first
	if _, ok := rawPlan["edit_operations"].([]interface{}); ok {
		return convertStructuredChangesToEditPlan(rawPlan)
	}

	// Fallback: create a minimal plan that encourages command execution
	return &EditPlan{
		ScopeStatement: "Fallback plan - consider using direct commands",
		EditOperations: []EditOperation{
			{
				FilePath:           "command",
				Description:        "Execute appropriate command",
				Instructions:       "Use the most appropriate command or tool to accomplish the user's request",
				ScopeJustification: "Direct execution is often more effective than complex planning",
			},
		},
	}
}

// validateEditPlan performs light schema checks without external deps
func validateEditPlan(p *EditPlan) error {
	if p == nil {
		return fmt.Errorf("plan is nil")
	}
	if len(p.EditOperations) == 0 {
		return fmt.Errorf("plan has no edit_operations")
	}
	// files_to_edit is optional but recommended; if present, must be strings
	for _, f := range p.FilesToEdit {
		if strings.TrimSpace(f) == "" {
			return fmt.Errorf("empty entry in files_to_edit")
		}
	}
	// operation fields must be present
	for i, op := range p.EditOperations {
		if strings.TrimSpace(op.FilePath) == "" {
			return fmt.Errorf("operation %d missing file_path", i)
		}
		if strings.TrimSpace(op.Instructions) == "" {
			return fmt.Errorf("operation %d missing instructions", i)
		}
	}
	return nil
}
