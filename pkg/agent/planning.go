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

	// Workspace patterns and guidance to help the LLM output focused edits
	workspacePatterns := analyzeWorkspacePatterns(logger)
	refactoringGuidance := generateRefactoringStrategy(userIntent, contextFiles, workspacePatterns, logger)

	// Construct the planning prompt
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

	// Estimate tokens for cost accounting
	promptTokens := utils.EstimateTokens(prompt)
	responseTokens := utils.EstimateTokens(response)
	totalTokens := promptTokens + responseTokens
	logger.Logf("Planning tokens: prompt=%d completion=%d total=%d", promptTokens, responseTokens, totalTokens)

	cleanedResponse, err := utils.ExtractJSONFromLLMResponse(response)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to extract JSON from orchestration response: %w", err))
		return nil, totalTokens, fmt.Errorf("failed to extract JSON from orchestration response: %w", err)
	}

	// Parse JSON to EditPlan
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
