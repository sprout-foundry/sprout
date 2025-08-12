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

// createDetailedEditPlan uses the orchestration model to create a detailed plan for code changes
func createDetailedEditPlan(userIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (*EditPlan, int, error) {
	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: createDetailedEditPlan started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Get basic context for the files we need to understand
	var contextFiles []string
	if len(intentAnalysis.EstimatedFiles) > 0 {
		contextFiles = intentAnalysis.EstimatedFiles
	} else {
		// Use our new robust file finding approach
		logger.Logf("No estimated files from analysis, using robust file discovery for: %s", userIntent)
		contextFiles = findRelevantFilesRobust(userIntent, cfg, logger)

		// If still no files found, try content search as additional fallback
		if len(contextFiles) == 0 {
			logger.Logf("Robust discovery found no files, trying content search as final fallback...")
			contextFiles = findRelevantFilesByContent(userIntent, logger)
		}
	}

	// Load context for the files
	context := buildBasicFileContext(contextFiles, logger)

	// Log what context is being provided to the orchestration model
	logger.LogProcessStep("🎯 ORCHESTRATION MODEL CONTEXT:")
	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.LogProcessStep(fmt.Sprintf("User Intent: %s", userIntent))
	logger.LogProcessStep(fmt.Sprintf("Context Files Count: %d", len(contextFiles)))
	logger.LogProcessStep(fmt.Sprintf("Context Size: %d characters", len(context)))
	if len(contextFiles) > 0 {
		logger.LogProcessStep("Files included in orchestration context:")
		for i, file := range contextFiles {
			logger.LogProcessStep(fmt.Sprintf("  %d. %s", i+1, file))
		}
	}
	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Analyze workspace patterns for intelligent task decomposition
	workspacePatterns := analyzeWorkspacePatterns(logger)
	refactoringGuidance := ""

	// Check if this is a large file refactoring task
	if isLargeFileRefactoringTask(userIntent, contextFiles, logger) {
		refactoringGuidance = generateRefactoringStrategy(userIntent, contextFiles, workspacePatterns, logger)

		// For large file refactoring, also analyze source file functions
		sourceFile := extractSourceFileFromIntent(userIntent, contextFiles)
		if sourceFile != "" {
			functionInventory := analyzeFunctionsInFile(sourceFile, logger)
			refactoringGuidance += "\n\nFUNCTION INVENTORY FOR EXTRACTION:\n" + functionInventory
		}
	} else {
		refactoringGuidance = "Standard single-file or small-scope changes"
	}

	prompt := fmt.Sprintf(`You are an expert Go software architect and developer. Create a MINIMAL and FOCUSED edit plan for this Go codebase.

USER REQUEST: %s

WORKSPACE ANALYSIS:
Project Type: Go
Total Context Files: %d
Workspace Patterns: Average file size: %d lines, Modularity: %s

TASK ANALYSIS:
- Category: %s
- Complexity: %s
- Context Files: %s

CURRENT GO CODEBASE CONTEXT:
%s

CRITICAL WORKSPACE VALIDATION:
- This is a PROJECT - You must ONLY work with source files
- ALL files in "files_to_edit" must be EXISTING .go files from the context above
- Do NOT create new files unless explicitly requested by user
- Do NOT suggest .ts, .js, .py, or any non-Go files
- File paths must be relative to project root
- Verify each file exists in the provided context before including it

INTELLIGENT REFACTORING GUIDANCE:
%s

FOR LARGE FILE REFACTORING (when extracting functions to new files):
1. **ANALYZE SOURCE**: Examine the function inventory above to understand what needs extraction
2. **CREATE SPECIFIC OPERATIONS**: Instead of generic "refactor file", create separate operations for:
   - Creating each new target file (pkg/agent/agent.go, pkg/agent/orchestrator.go, etc.)
   - Moving specific functions from source to target files
   - Updating the source file to remove extracted functions and add imports
3. **FUNCTION-LEVEL INSTRUCTIONS**: For each operation, specify exactly which functions/types to move:
   Example: "Move functions NewAgent, (*Agent).Run, and AgentContext type from cmd/agent.go to pkg/agent/agent.go"
4. **DEPENDENCY ORDER**: Create files in order - types first, then functions that use them
5. **IMPORT MANAGEMENT**: Ensure each operation includes proper import statements

REFACTORING OPERATION TEMPLATE FOR LARGE FILES:
When refactoring large files, create multiple granular operations like:
- Operation 1: "Create pkg/agent/agent.go with core Agent types and constructor"
- Operation 2: "Move specific orchestration functions to pkg/agent/orchestrator.go"  
- Operation 3: "Update cmd/agent.go to use extracted packages and remove moved functions"

AVAILABLE TOOLS AND CAPABILITIES:
The agent has access to the following tools during the editing process:
1. File editing (primary capability) - both full file and partial section editing
2. Terminal command execution - can run shell commands when needed
3. File system operations (read, write, check existence)
4. Compilation and testing tools via the shell
5. Git operations for version control
6. File validation tools (syntax checking, compilation verification)
7. Automatic issue fixing capabilities

INTELLIGENT TOOL USAGE:
The orchestration model should leverage these tools strategically:
- Use fix_validation_issues tool to automatically resolve common problems
- Use run_shell_command tool for build verification and testing
- Use read_file tool to examine current state before making changes
- Use write_file tool to apply changes with proper error checking

RESPONSE FORMAT:
Respond with STRICT JSON using this schema:
{
  "files_to_edit": ["list", "of", "existing", "go", "files"],
  "edit_operations": [
    {
      "file_path": "string",
      "description": "string",
      "instructions": "string",
      "scope_justification": "string"
    }
  ],
  "context": "string",
  "scope_statement": "string"
}`,
		userIntent,
		len(contextFiles),
		workspacePatterns.AverageFileSize,
		workspacePatterns.ModularityLevel,
		intentAnalysis.Category,
		intentAnalysis.Complexity,
		strings.Join(contextFiles, ", "),
		context,
		refactoringGuidance,
	)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert software development orchestrator. Respond only with valid JSON conforming to the requested schema."},
		{Role: "user", Content: prompt},
	}
	response, _, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 3*time.Minute)
	if err != nil {
		logger.LogError(fmt.Errorf("orchestration model failed to create edit plan: %w", err))
		return nil, 0, fmt.Errorf("failed to create detailed edit plan: %w", err)
	}

	// Estimate tokens used (attribute to Planning) and record split (caller applies totals)
	promptTokens := utils.EstimateTokens(prompt)
	responseTokens := utils.EstimateTokens(response)
	totalTokens := promptTokens + responseTokens
	logger.Logf("Planning tokens: prompt=%d completion=%d total=%d", promptTokens, responseTokens, totalTokens)

	cleanedResponse, err := utils.ExtractJSONFromLLMResponse(response)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to extract JSON from orchestration response: %w", err))
		return nil, totalTokens, fmt.Errorf("failed to extract JSON from orchestration response: %w", err)
	}

	var plan EditPlan
	if err := json.Unmarshal([]byte(cleanedResponse), &plan); err != nil {
		logger.LogError(fmt.Errorf("failed to parse edit plan JSON: %w", err))
		return nil, totalTokens, fmt.Errorf("failed to parse edit plan JSON: %w", err)
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: createDetailedEditPlan completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
	return &plan, totalTokens, nil
}
