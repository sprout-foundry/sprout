// Agent command implementation
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime" // Import runtime for memory stats
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/orchestration"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
	"github.com/spf13/cobra"
)

var (
	agentSkipPrompt bool
)

func init() {
	agentCmd.Flags().BoolVar(&agentSkipPrompt, "skip-prompt", false, "Skip user prompt for applying changes")
}

// agentCmd represents the agent command
var agentCmd = &cobra.Command{
	Use:   "agent [intent]",
	Short: "AI agent mode - analyzes intent and autonomously decides what actions to take",
	Long: `Agent mode allows the LLM to analyze your intent and autonomously decide what actions to take.
Instead of using specific commands like 'code' or 'process', the agent will:

1. Analyze your intent
2. Decide what tools and processes are needed
3. Execute the appropriate sequence of actions

Examples:
  ledit agent "Add better error handling to the main function"
  ledit agent "Refactor the user authentication system"
  ledit agent "Fix the bug where users can't login"
  ledit agent "Add unit tests for the payment processing"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		userIntent := strings.Join(args, " ")
		return runAgentMode(userIntent)
	},
}

// runAgentMode runs the new agent-driven mode where the LLM decides what actions to take
func runAgentMode(userIntent string) error {
	fmt.Printf("🤖 Agent mode: Analyzing your intent...\n")

	// Log the original user prompt
	utils.LogUserPrompt(userIntent)

	// Load configuration
	cfg, err := config.LoadOrInitConfig(agentSkipPrompt)
	if err != nil {
		logger := utils.GetLogger(false) // Get a logger even if config fails
		logger.LogError(fmt.Errorf("failed to load config: %w", err))
		return fmt.Errorf("failed to load config: %w", err)
	}
	cfg.SkipPrompt = agentSkipPrompt

	fmt.Printf("🎯 Intent: %s\n", userIntent)

	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.LogProcessStep("🚀 Starting agent execution with escalation fallback...")

	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runAgentMode started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Initialize token usage tracking
	tokenUsage := &TokenUsage{}

	// Try optimized agent first
	logger.LogProcessStep("🔄 Phase 1: Attempting optimized agent execution...")
	err = runOptimizedAgent(userIntent, cfg, logger, tokenUsage)

	if err != nil {
		logger.LogProcessStep("⚠️ Phase 1 failed, checking if escalation is needed...")

		// Check if this is a validation failure with required steps failing
		if shouldEscalateToOrchestration(err, logger) {
			logger.LogProcessStep("🚀 Phase 2: Escalating to full orchestration model...")

			// Use full orchestration for complex cases
			err = runFullOrchestration(userIntent, cfg, logger)
			if err != nil {
				logger.LogError(fmt.Errorf("both optimized agent and orchestration failed: %w", err))
				return fmt.Errorf("both optimized agent and orchestration failed: %w", err)
			}
		} else {
			logger.LogError(fmt.Errorf("agent execution failed: %w", err))
			return fmt.Errorf("agent execution failed: %w", err)
		}
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runAgentMode completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Print token usage summary
	printTokenUsageSummary(tokenUsage, duration)

	fmt.Printf("✅ Agent execution completed in %v\n", duration)
	return nil
}

// runOptimizedAgent runs the agent with minimal context to reduce costs
func runOptimizedAgent(userIntent string, cfg *config.Config, logger *utils.Logger, tokenUsage *TokenUsage) error {
	logger.LogProcessStep("CHECKPOINT: Starting runOptimizedAgent")
	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runOptimizedAgent started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	logger.Logf("DEBUG: Starting optimized agent execution at %s for intent: %s", time.Now().Format(time.RFC3339), userIntent)
	// Phase 1: Intent analysis with minimal context
	logger.LogProcessStep("📋 Phase 1: Analyzing intent and determining scope...")

	intentAnalysis, intentTokens, err := analyzeIntentWithMinimalContext(userIntent, cfg, logger)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to analyze intent: %w", err))
		return fmt.Errorf("failed to analyze intent: %w", err)
	}

	// Track intent analysis tokens
	tokenUsage.IntentAnalysis = intentTokens

	logger.LogProcessStep(fmt.Sprintf("Intent Category: %s", intentAnalysis.Category))
	logger.LogProcessStep(fmt.Sprintf("Complexity: %s", intentAnalysis.Complexity))
	logger.LogProcessStep(fmt.Sprintf("Estimated Files: %d", len(intentAnalysis.EstimatedFiles)))
	logger.LogProcessStep("CHECKPOINT: After estimated files log")
	if len(intentAnalysis.EstimatedFiles) > 0 {
		logger.LogProcessStep(fmt.Sprintf("Files: %s", strings.Join(intentAnalysis.EstimatedFiles, ", ")))
	}

	// Phase 2: Use orchestration model to create detailed edit plan
	logger.LogProcessStep("🎯 Phase 2: Creating detailed edit plan with orchestration model...")
	editPlan, planTokens, err := createDetailedEditPlan(userIntent, intentAnalysis, cfg, logger)
	if err != nil {
		// Check if this is an escalation request
		if strings.Contains(err.Error(), "escalation required") {
			logger.Logf("Escalating to full orchestration due to edit plan creation failure")
			return runFullOrchestration(userIntent, cfg, logger)
		}
		logger.LogError(fmt.Errorf("failed to create edit plan: %w", err))
		return fmt.Errorf("failed to create edit plan: %w", err)
	}
	tokenUsage.CodeGeneration += planTokens

	// Phase 3: Context loading is now handled by edit plan (files from orchestration model)
	logger.LogProcessStep("🔍 Phase 3: Edit plan specifies target files...")

	// TEMPORARY: Force all complexities to use agent workflow until orchestration is optimized
	switch intentAnalysis.Complexity {
	case "simple":
		logger.LogProcessStep(fmt.Sprintf("Using %d files from edit plan for simple task", len(editPlan.FilesToEdit)))
	case "moderate":
		logger.LogProcessStep(fmt.Sprintf("Using %d files from edit plan for moderate task", len(editPlan.FilesToEdit)))
	case "complex":
		// TEMPORARY: Use agent workflow instead of orchestration for complex tasks
		logger.LogProcessStep("� Complex task detected, but using agent workflow instead of orchestration (temporary)")
		logger.LogProcessStep(fmt.Sprintf("Using %d files from edit plan for complex task", len(editPlan.FilesToEdit)))
	}

	// Phase 4: Execute edit plan with fast editing model
	logger.LogProcessStep("⚡ Phase 4: Executing edit plan with fast editing model...")
	codeGenTokens, err := executeEditPlan(editPlan, cfg, logger)
	if err != nil {
		return err
	}

	// Phase 5: Validate changes with iterative fixing
	logger.LogProcessStep("🔍 Phase 5: Validating changes...")
	validationTokens, err := validateChangesWithIteration(intentAnalysis, userIntent, cfg, logger, tokenUsage)
	if err != nil {
		logger.LogError(fmt.Errorf("validation failed after iterations: %w", err))
		return fmt.Errorf("validation failed: %w", err)
	}

	// Track all token usage
	tokenUsage.Planning = planTokens
	tokenUsage.CodeGeneration = codeGenTokens
	tokenUsage.Validation = validationTokens
	tokenUsage.Total = tokenUsage.IntentAnalysis + tokenUsage.Planning + tokenUsage.CodeGeneration + tokenUsage.Validation

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: runOptimizedAgent completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
	return nil
}

// TokenUsage tracks token consumption throughout agent execution
type TokenUsage struct {
	IntentAnalysis int
	Planning       int // Tokens used by orchestration model for detailed planning
	CodeGeneration int
	Validation     int
	Total          int
}

// IntentAnalysis represents the analysis of user intent
type IntentAnalysis struct {
	Category        string   // "code", "fix", "docs", "test", "review"
	Complexity      string   // "simple", "moderate", "complex"
	EstimatedFiles  []string // Files likely to be involved
	RequiresContext bool     // Whether workspace context is needed
}

// EditPlan represents a detailed plan for code changes created by the orchestration model
type EditPlan struct {
	FilesToEdit    []string        // Files that need to be modified
	EditOperations []EditOperation // Specific operations to perform
	Context        string          // Additional context for the edits
	ScopeStatement string          // Clear statement of what this plan addresses
}

// EditOperation represents a single file edit operation
type EditOperation struct {
	FilePath           string // Path to the file to edit
	Description        string // What change to make
	Instructions       string // Detailed instructions for the editing model
	ScopeJustification string // Explanation of how this change serves the user request
}

// analyzeIntentWithMinimalContext analyzes user intent with proper workspace context
func analyzeIntentWithMinimalContext(userIntent string, cfg *config.Config, logger *utils.Logger) (*IntentAnalysis, int, error) {
	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: analyzeIntentWithMinimalContext started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// STEP 1: Load workspace file for embeddings (create if it doesn't exist)
	logger.Logf("STEP 1: Loading workspace file...")
	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		if os.IsNotExist(err) {
			logger.Logf("STEP 1: No workspace file found, creating and populating workspace...")
			// Ensure .ledit directory exists
			if err := os.MkdirAll(".ledit", os.ModePerm); err != nil {
				logger.LogError(fmt.Errorf("failed to create .ledit directory: %w", err))
				return nil, 0, fmt.Errorf("failed to create workspace directory: %w", err)
			}
			// Use GetWorkspaceContext to trigger workspace creation and population
			_ = workspace.GetWorkspaceContext("", cfg)
			// Now try to load the workspace file again
			workspaceFile, err = workspace.LoadWorkspaceFile()
			if err != nil {
				logger.LogError(fmt.Errorf("failed to load workspace after creation: %w", err))
				return nil, 0, fmt.Errorf("failed to load workspace after creation: %w", err)
			}
		} else {
			logger.LogError(fmt.Errorf("failed to load workspace file: %w", err))
			return nil, 0, fmt.Errorf("failed to load workspace: %w", err)
		}
	}
	logger.Logf("STEP 1: Successfully loaded workspace with %d files", len(workspaceFile.Files))

	// STEP 1.5: Build basic workspace analysis BEFORE any LLM decisions
	logger.Logf("STEP 1.5: Analyzing workspace structure...")
	workspaceAnalysis, err := buildWorkspaceStructure(logger)
	if err != nil {
		logger.Logf("Warning: Could not build workspace analysis: %v", err)
		workspaceAnalysis = &WorkspaceInfo{
			ProjectType: "go", // Default assumption
			AllFiles:    []string{},
		}
	}
	logger.Logf("STEP 1.5: Detected project type: %s with %d files", workspaceAnalysis.ProjectType, len(workspaceAnalysis.AllFiles))

	// STEP 2: Use embeddings to find relevant files
	logger.Logf("STEP 2: Starting embedding search for intent: %s", userIntent)
	logger.Logf("STEP 2: About to call GetFilesForContextUsingEmbeddings...")

	fullContextFiles, summaryContextFiles, err := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFile, cfg, logger)
	logger.Logf("STEP 2: GetFilesForContextUsingEmbeddings returned: full=%d, summary=%d", len(fullContextFiles), len(summaryContextFiles))

	if err != nil {
		logger.LogError(fmt.Errorf("embedding search failed: %w", err))
		// Fallback to basic file listing
		logger.Logf("STEP 2: Falling back to basic file discovery...")
		fullContextFiles, summaryContextFiles = []string{}, []string{}
	}

	// Combine full context and summary files
	relevantFiles := append(fullContextFiles, summaryContextFiles...)
	logger.Logf("Found %d full context files: %v", len(fullContextFiles), fullContextFiles)
	logger.Logf("Found %d summary context files: %v", len(summaryContextFiles), summaryContextFiles)
	logger.Logf("Total relevant files (%d): %v", len(relevantFiles), relevantFiles)

	// STEP 2.5: If embeddings found few/no files, try workspace model rewording
	if len(relevantFiles) < 3 {
		logger.Logf("STEP 2.5: Few files found (%d), trying workspace model to reword prompt...", len(relevantFiles))
		rewordedIntent, rewordErr := rewordPromptForBetterSearch(userIntent, workspaceAnalysis, cfg, logger)
		if rewordErr == nil && rewordedIntent != userIntent {
			logger.Logf("STEP 2.5: Reworded intent: '%s' -> '%s'", userIntent, rewordedIntent)

			// Try embeddings again with reworded intent
			fullContextFiles2, summaryContextFiles2, err2 := workspace.GetFilesForContextUsingEmbeddings(rewordedIntent, workspaceFile, cfg, logger)
			if err2 == nil && len(fullContextFiles2)+len(summaryContextFiles2) > len(relevantFiles) {
				logger.Logf("STEP 2.5: Reworded search found more files! Using new results.")
				fullContextFiles = fullContextFiles2
				summaryContextFiles = summaryContextFiles2
				relevantFiles = append(fullContextFiles, summaryContextFiles...)
			}
		}
	}

	// STEP 2.7: If still few files, try shell commands to find files
	if len(relevantFiles) < 2 {
		logger.Logf("STEP 2.7: Still few files (%d), trying shell commands to find relevant files...", len(relevantFiles))
		shellFoundFiles := findFilesUsingShellCommands(userIntent, workspaceAnalysis, logger)
		if len(shellFoundFiles) > 0 {
			logger.Logf("STEP 2.7: Shell commands found %d additional files: %v", len(shellFoundFiles), shellFoundFiles)
			relevantFiles = append(relevantFiles, shellFoundFiles...)
		}
	}

	// If still no files found, try content-based search as final fallback
	if len(relevantFiles) == 0 {
		logger.Logf("No files found by embeddings or basic listing, trying content search...")
		relevantFiles = findRelevantFilesByContent(userIntent, logger)
		logger.Logf("Content search found %d files: %v", len(relevantFiles), relevantFiles)
	}

	// Final safety net - ensure we always have some files to analyze
	if len(relevantFiles) == 0 {
		logger.Logf("WARNING: No relevant files found by any method! Using fallback files...")
		// Force include the most likely files for code reviews
		candidateFiles := []string{"pkg/llm/api.go", "pkg/editor/editor.go", "cmd/review_staged.go", "pkg/orchestration/orchestrator.go"}
		for _, file := range candidateFiles {
			if _, err := os.Stat(file); err == nil {
				relevantFiles = append(relevantFiles, file)
			}
		}
		logger.Logf("Fallback selected %d files: %v", len(relevantFiles), relevantFiles)
	}

	prompt := fmt.Sprintf(`Analyze this user intent and classify it for optimal execution:

User Intent: %s

WORKSPACE ANALYSIS:
Project Type: %s
Total Files: %d
Available Go Files in Workspace:
%s

CRITICAL WORKSPACE CONSTRAINTS:
- This is a %s project - do NOT suggest non-%s files
- All file paths must be relative to project root
- Only suggest modifications to EXISTING files shown above
- Do NOT create new files unless explicitly requested
- Verify file extensions match project type (.go for Go projects)

Respond with JSON:
{
  "category": "code|fix|docs|test|review",
  "complexity": "simple|moderate|complex",
  "estimated_files": ["file1.go", "file2.go"],
  "requires_context": true|false
}

Classification Guidelines:
- "simple": Single file edit, clear target, specific change
- "moderate": 2-5 files, some analysis needed, well-defined scope
- "complex": Multiple files, requires planning, unclear scope

Only include files in estimated_files that are highly likely to be modified.
ALL files must be existing %s files from the workspace above.`,
		userIntent,
		workspaceAnalysis.ProjectType,
		len(relevantFiles),
		strings.Join(relevantFiles, "\n"),
		workspaceAnalysis.ProjectType,
		workspaceAnalysis.ProjectType,
		workspaceAnalysis.ProjectType)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at analyzing programming tasks. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 60*time.Second)
	if err != nil {
		logger.LogError(fmt.Errorf("orchestration model failed to analyze intent: %w", err))
		// Use fallback analysis since LLM failed
		logger.Logf("Using fallback heuristic analysis due to orchestration model failure")
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: analyzeIntentWithMinimalContext completed (LLM error, fallback). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  []string{}, // Don't try to infer files in fallback
			RequiresContext: true,
		}, 0, nil // No tokens used if LLM failed
	}

	// Estimate tokens used for intent analysis
	promptTokens := utils.EstimateTokens(messages[0].Content.(string) + " " + messages[1].Content.(string))
	responseTokens := utils.EstimateTokens(response)
	totalTokens := promptTokens + responseTokens

	// Clean response and parse JSON
	response = strings.TrimSpace(response)
	if response == "" {
		logger.Logf("LLM returned empty response for intent analysis. Falling back to heuristic analysis.")
		// Fallback to simple analysis if LLM fails
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: analyzeIntentWithMinimalContext completed (empty response, fallback). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  []string{}, // Don't try to infer files in fallback
			RequiresContext: true,
		}, totalTokens, nil
	}

	if strings.Contains(response, "```json") {
		parts := strings.Split(response, "```json")
		if len(parts) > 1 {
			jsonPart := parts[1]
			end := strings.Index(jsonPart, "```")
			if end > 0 {
				response = strings.TrimSpace(jsonPart[:end])
			}
		}
	}

	var analysis IntentAnalysis
	if err := json.Unmarshal([]byte(response), &analysis); err != nil {
		logger.LogError(fmt.Errorf("failed to parse intent analysis JSON from LLM: %w\nRaw response: %s", err, response))
		// Fallback to heuristic analysis if JSON parsing fails
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: analyzeIntentWithMinimalContext completed (JSON error, fallback). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  []string{}, // Don't try to infer files in fallback
			RequiresContext: true,
		}, totalTokens, nil
	}

	// Debug: log the parsed analysis
	logger.Logf("Parsed analysis - Category: %s, Complexity: %s, Files: %v", analysis.Category, analysis.Complexity, analysis.EstimatedFiles)

	// If LLM didn't provide files, fall back to embedding-based search
	if len(analysis.EstimatedFiles) == 0 {
		// Try embeddings first, fall back to content search if embeddings fail
		workspaceFileData, embErr := workspace.LoadWorkspaceFile()
		if embErr == nil {
			fullContextFiles, summaryContextFiles, embErr := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFileData, cfg, logger)
			embeddingFiles := append(fullContextFiles, summaryContextFiles...)

			if embErr != nil || len(embeddingFiles) == 0 {
				logger.Logf("Embedding search failed or returned no results, falling back to content search: %v", embErr)
				analysis.EstimatedFiles = findRelevantFilesByContent(userIntent, logger)
				logger.Logf("LLM provided no files, using content-based search: %v", analysis.EstimatedFiles)
			} else {
				analysis.EstimatedFiles = embeddingFiles
				logger.Logf("LLM provided no files, using embedding-based search: %v", analysis.EstimatedFiles)
			}
		} else {
			logger.Logf("Could not load workspace file, falling back to content search: %v", embErr)
			analysis.EstimatedFiles = findRelevantFilesByContent(userIntent, logger)
			logger.Logf("LLM provided no files, using content-based search: %v", analysis.EstimatedFiles)
		}
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: analyzeIntentWithMinimalContext completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
	return &analysis, totalTokens, nil
}

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

	// Debug: log contextFiles to see what we have
	logger.Logf("DEBUG: createDetailedEditPlan contextFiles: %v (length: %d)", contextFiles, len(contextFiles))

	// Analyze workspace patterns for intelligent task decomposition
	workspacePatterns := analyzeWorkspacePatterns(logger)
	refactoringGuidance := ""

	// Check if this is a large file refactoring task
	if isLargeFileRefactoringTask(userIntent, contextFiles, logger) {
		refactoringGuidance = generateRefactoringStrategy(userIntent, contextFiles, workspacePatterns, logger)
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
- This is a GO PROJECT - You must ONLY work with .go files
- ALL files in "files_to_edit" must be EXISTING .go files from the context above
- Do NOT create new files unless explicitly requested by user
- Do NOT suggest .ts, .js, .py, or any non-Go files
- File paths must be relative to project root
- Verify each file exists in the provided context before including it

INTELLIGENT REFACTORING GUIDANCE:
%s

AVAILABLE TOOLS AND CAPABILITIES:
The agent has access to the following tools during the editing process:
1. File editing (primary capability) - both full file and partial section editing
2. Terminal command execution - can run shell commands when needed
3. File system operations (read, write, check existence)
4. Go compilation and testing tools
5. Git operations for version control
6. File validation tools (syntax checking, compilation verification)
7. Automatic issue fixing capabilities

INTELLIGENT TOOL USAGE:
The orchestration model should leverage these tools strategically:
- Use validate_file tool to check syntax and compilation after changes
- Use edit_file_section tool for efficient targeted edits
- Use fix_validation_issues tool to automatically resolve common problems
- Use run_shell_command tool for build verification and testing
- Use read_file tool to examine current state before making changes

WHEN TO USE TERMINAL COMMANDS:
- To check dependencies or imports: go mod tidy, go list -m all
- To run tests after changes: go test ./...
- To check compilation: go build ./...
- To verify tools are available: which go, go version
- To examine file contents: cat, grep, find
- To check git status: git status, git diff

INTELLIGENT WORKFLOW PLANNING:
In your edit plan, you can specify if terminal commands should be run before or after certain edits.
For example:
- Check current dependencies before adding new imports
- Run tests after adding new functionality
- Verify compilation after structural changes

However, the PREFERRED approach is to let the agent use the appropriate tools during execution:
- The agent can call validate_file after each edit to ensure quality
- The agent can use edit_file_section for efficient partial edits
- The agent can automatically fix issues with fix_validation_issues
- Terminal commands should only be specified when they're essential to the workflow

THIS IS A GO PROJECT - DO NOT CREATE NON-GO FILES!

CRITICAL CONSTRAINTS:
1. **STAY STRICTLY WITHIN SCOPE**: Only make changes that directly fulfill the user's exact request
2. **NO SCOPE CREEP**: Do not add improvements, optimizations, or features not explicitly requested
3. **MINIMAL CHANGES**: Make the smallest possible changes to achieve the goal
4. **SINGLE PURPOSE**: Each edit should serve only the user's stated objective
5. **GO FILES ONLY**: This is a Go project. Only modify existing .go files. Do NOT create .ts, .js, or other non-Go files
6. **EXISTING FILES ONLY**: You MUST use existing files from the context above. Do NOT create new files
7. **VERIFY FILE EXISTENCE**: All files in "files_to_edit" must be actual Go files that exist in the codebase context
8. **NO SEARCH FLAGS**: Do NOT include #SG, #SEARCH, or similar flags in your response. Search grounding is handled separately via explicit tool calls.

PLANNING REQUIREMENTS:
1. **File Analysis**: Identify exactly which EXISTING .go files need to be modified 
2. **Edit Operations**: For each file, specify the exact changes needed
3. **Scope Justification**: Explain how each change directly serves the user request
4. **File Verification**: Only include .go files that exist in the current codebase context
5. **CONTEXT FILES**: Include ALL files that the editing model will need to understand interfaces, dependencies, or patterns referenced in the edit operations. The "files_to_edit" list will be used to provide context to the editing model.

CRITICAL CONTEXT REQUIREMENT:
Edit operations should be granular and self-contained with all necessary context included in the instructions. When referencing files, functions, interfaces, or types from other files, include the file path using hashtag syntax (e.g., #path/to/file.go) at the END of the instructions. This allows the downstream editing process to automatically provide those files as context.

INSTRUCTION QUALITY REQUIREMENTS:
1. **Self-Contained**: Each instruction should contain all details needed to implement the change
2. **Granular**: Break down complex changes into specific, actionable steps
3. **File References**: Use hashtag syntax (#path/to/file.go) for any files that need to be referenced
4. **Context-Rich**: Include function names, interface details, and patterns to follow
5. **No Assumptions**: Don't assume the editing model knows about other parts of the codebase

HASHTAG FILE REFERENCE SYNTAX:
- End instructions with: #path/to/file1.go #path/to/file2.go
- Use this for any file containing interfaces, types, patterns, or examples to follow
- The editing process will automatically load these files as context

RESPONSE FORMAT (JSON):
{
  "files_to_edit": ["path/to/existing/file.go"],   // Only files that will be MODIFIED
  "edit_operations": [
    {
      "file_path": "path/to/existing/file.go", 
      "description": "Specific change to make",
      "instructions": "Detailed, self-contained instructions with hashtag file references at the end: #path/to/context/file.go",
      "scope_justification": "How this change directly serves the user's request"
    }
  ],
  "context": "Minimal strategy focused only on the user's exact request",
  "scope_statement": "This plan addresses only: [restate user request]"
}

STRICT GUIDELINES:
- Each edit operation should target ONE existing file
- Instructions should be GRANULAR and SELF-CONTAINED with all necessary details
- Include specific function names, interface details, and implementation patterns
- Use hashtag syntax (#file.go) at the end of instructions for file references
- Focus ONLY on what the user explicitly requested
- Do NOT add related improvements or "nice to have" features
- Do NOT create new files or new directory structures
- Only modify existing code to achieve the specific goal
- Justify every change against the original user request`,
		userIntent,
		len(contextFiles),
		workspacePatterns.AverageFileSize,
		workspacePatterns.ModularityLevel,
		intentAnalysis.Category,
		intentAnalysis.Complexity,
		strings.Join(contextFiles, ", "),
		context,
		refactoringGuidance)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert software architect. Create detailed, actionable edit plans. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 3*time.Minute)
	if err != nil {
		logger.LogError(fmt.Errorf("orchestration model failed to create edit plan: %w", err))
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: createDetailedEditPlan completed (error). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

		// Fallback to simple plan based on intent analysis
		var fallbackOperations []EditOperation
		if len(contextFiles) > 0 {
			fallbackOperations = []EditOperation{
				{
					FilePath:     contextFiles[0],
					Description:  userIntent,
					Instructions: userIntent,
				},
			}
		} else {
			// No context files available
			fallbackOperations = []EditOperation{
				{
					FilePath:     "main.go", // Generic fallback
					Description:  userIntent,
					Instructions: userIntent,
				},
			}
		}

		return &EditPlan{
			FilesToEdit:    contextFiles,
			EditOperations: fallbackOperations,
			Context:        "Fallback plan due to orchestration model failure",
		}, 0, nil
	}

	// Log the orchestration model response for debugging
	logger.LogProcessStep("🎯 ORCHESTRATION MODEL RESPONSE:")
	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.LogProcessStep(fmt.Sprintf("Response length: %d characters", len(response)))
	// Show a preview of the response (first 500 chars)
	if len(response) > 500 {
		logger.LogProcessStep(fmt.Sprintf("Response preview: %s...", response[:500]))
	} else {
		logger.LogProcessStep(fmt.Sprintf("Full response: %s", response))
	}
	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Estimate tokens used
	promptTokens := utils.EstimateTokens(prompt)
	responseTokens := utils.EstimateTokens(response)
	totalTokens := promptTokens + responseTokens

	// Clean and parse response
	response = strings.TrimSpace(response)
	if strings.Contains(response, "```json") {
		parts := strings.Split(response, "```json")
		if len(parts) > 1 {
			jsonPart := parts[1]
			end := strings.Index(jsonPart, "```")
			if end > 0 {
				response = strings.TrimSpace(jsonPart[:end])
			}
		}
	}

	// Parse the response into a temporary structure for JSON unmarshaling
	var planData struct {
		FilesToEdit    []string `json:"files_to_edit"`
		EditOperations []struct {
			FilePath           string `json:"file_path"`
			Description        string `json:"description"`
			Instructions       string `json:"instructions"`
			ScopeJustification string `json:"scope_justification"`
		} `json:"edit_operations"`
		Context        string `json:"context"`
		ScopeStatement string `json:"scope_statement"`
	}

	if err := json.Unmarshal([]byte(response), &planData); err != nil {
		logger.LogError(fmt.Errorf("failed to parse edit plan JSON: %w\nRaw response: %s", err, response))

		// Check if we should escalate instead of falling back to poor quality raw user intent
		if shouldEscalateToOrchestration(err, logger) {
			logger.Logf("Escalating due to JSON parsing failure in createDetailedEditPlan")
			// Return nil plan and escalation error to trigger orchestration at higher level
			return nil, 0, fmt.Errorf("escalation required due to JSON parsing failure: %w", err)
		}

		// Fallback to simple plan
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: createDetailedEditPlan completed (JSON error, fallback). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

		// Handle empty contextFiles to avoid index out of range panic
		var fallbackOperations []EditOperation
		if len(contextFiles) > 0 {
			fallbackOperations = []EditOperation{
				{
					FilePath:           contextFiles[0],
					Description:        userIntent,
					Instructions:       userIntent,
					ScopeJustification: "Fallback operation due to JSON parsing failure",
				},
			}
		} else {
			// No context files available, create a generic operation
			fallbackOperations = []EditOperation{
				{
					FilePath:           "",
					Description:        userIntent,
					Instructions:       userIntent,
					ScopeJustification: "Fallback operation with no context files available",
				},
			}
		}

		return &EditPlan{
			FilesToEdit:    contextFiles,
			EditOperations: fallbackOperations,
			Context:        "Fallback plan due to JSON parsing failure",
		}, totalTokens, nil
	}

	// Convert to our EditPlan structure
	var operations []EditOperation
	for _, op := range planData.EditOperations {
		operations = append(operations, EditOperation{
			FilePath:           op.FilePath,
			Description:        op.Description,
			Instructions:       op.Instructions,
			ScopeJustification: op.ScopeJustification,
		})
	}

	editPlan := &EditPlan{
		FilesToEdit:    planData.FilesToEdit,
		EditOperations: operations,
		Context:        planData.Context,
		ScopeStatement: planData.ScopeStatement,
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: createDetailedEditPlan completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	logger.LogProcessStep(fmt.Sprintf("📋 Edit plan created: %d files, %d operations", len(editPlan.FilesToEdit), len(editPlan.EditOperations)))
	logger.LogProcessStep(fmt.Sprintf("🎯 Scope: %s", editPlan.ScopeStatement))
	logger.LogProcessStep(fmt.Sprintf("Strategy: %s", editPlan.Context))

	// Log detailed edit plan contents
	logger.LogProcessStep("📚 EDIT PLAN DETAILS (Self-Contained with Hashtag References):")
	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Log files to edit
	logger.LogProcessStep(fmt.Sprintf("Files to Edit (%d):", len(editPlan.FilesToEdit)))
	for i, file := range editPlan.FilesToEdit {
		logger.LogProcessStep(fmt.Sprintf("  %d. %s", i+1, file))
	}

	// Log each operation with its scope justification
	logger.LogProcessStep(fmt.Sprintf("Edit Operations (%d):", len(editPlan.EditOperations)))
	for i, op := range editPlan.EditOperations {
		logger.LogProcessStep(fmt.Sprintf("📝 Operation %d: %s", i+1, op.Description))
		logger.LogProcessStep(fmt.Sprintf("   🎯 Target: %s", op.FilePath))
		logger.LogProcessStep(fmt.Sprintf("   📋 Justification: %s", op.ScopeJustification))

		// Check for hashtag file references in instructions
		if strings.Contains(op.Instructions, "#") {
			logger.LogProcessStep("   ✅ Contains hashtag file references for context")
		} else {
			logger.LogProcessStep("   ℹ️  Self-contained instructions (no file references)")
		}

		logger.LogProcessStep(fmt.Sprintf("   📖 Instructions: %s", op.Instructions))
		if i < len(editPlan.EditOperations)-1 {
			logger.LogProcessStep("   ───────────────────────────────")
		}
	}
	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return editPlan, totalTokens, nil
}

// Fallback functions for when LLM analysis fails
func inferCategory(userIntent string) string {
	intentLower := strings.ToLower(userIntent)
	if strings.Contains(intentLower, "test") {
		return "test"
	}
	if strings.Contains(intentLower, "fix") || strings.Contains(intentLower, "bug") {
		return "fix"
	}
	if strings.Contains(intentLower, "comment") || strings.Contains(intentLower, "doc") {
		return "docs"
	}
	if strings.Contains(intentLower, "review") {
		return "review"
	}
	return "code"
}

func inferComplexity(userIntent string) string {
	intentLower := strings.ToLower(userIntent)
	complexWords := []string{"refactor", "architect", "multiple", "design"} // Removed "system"
	simpleWords := []string{"add", "comment", "fix typo", "single"}

	for _, word := range complexWords {
		if strings.Contains(intentLower, word) {
			return "complex"
		}
	}

	for _, word := range simpleWords {
		if strings.Contains(intentLower, word) {
			return "simple"
		}
	}

	return "moderate"
}

// findRelevantFilesRobust uses embeddings and fallback strategies to find relevant files
func findRelevantFilesRobust(userIntent string, cfg *config.Config, logger *utils.Logger) []string {
	// Try embeddings first
	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		if os.IsNotExist(err) {
			logger.Logf("No workspace file found for embeddings, will use fallback methods")
		} else {
			logger.LogError(fmt.Errorf("failed to load workspace for file discovery: %w", err))
		}
	} else {
		fullFiles, _, embErr := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFile, cfg, logger)
		if embErr != nil {
			logger.LogError(fmt.Errorf("embedding search failed: %w", embErr))
		} else if len(fullFiles) > 0 {
			logger.Logf("Embeddings found %d relevant files", len(fullFiles))
			return fullFiles
		}
	}

	// If embeddings failed, try content-based search
	logger.Logf("Embeddings found no files, trying content search...")
	contentFiles := findRelevantFilesByContent(userIntent, logger)
	if len(contentFiles) > 0 {
		return contentFiles
	}

	// If all else fails, use shell commands to find files
	logger.Logf("Content search found no files, trying shell-based discovery...")

	// We need workspace info for shell commands, but keep it lightweight
	workspaceInfo := &WorkspaceInfo{
		ProjectType:   "go", // Default for this project
		RootFiles:     []string{},
		AllFiles:      []string{},
		FilesByDir:    map[string][]string{},
		RelevantFiles: map[string]string{},
	}

	shellFiles := findFilesUsingShellCommands(userIntent, workspaceInfo, logger)
	if len(shellFiles) > 0 {
		return shellFiles
	}

	// Absolute fallback - return empty slice, let caller handle
	logger.Logf("All file discovery methods failed")
	return []string{}
}

// findRelevantFilesByContent searches for files containing relevant content based on the user intent
func findRelevantFilesByContent(userIntent string, logger *utils.Logger) []string {
	intentLower := strings.ToLower(userIntent)

	// Extract key terms from the intent
	searchTerms := extractSearchTerms(intentLower)
	if len(searchTerms) == 0 {
		logger.Logf("No search terms extracted from intent, returning empty list")
		return []string{} // Return empty instead of using project-specific inference
	}

	logger.Logf("Searching for files containing terms: %v", searchTerms)

	// Search for files containing these terms
	relevantFiles := make(map[string]int) // file -> relevance score

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue walking despite errors
		}

		// Skip non-source files and directories
		if info.IsDir() || !isSourceFile(path) {
			return nil
		}

		// Skip hidden files and common ignore patterns
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return nil // Continue despite read errors
		}

		contentLower := strings.ToLower(string(content))
		score := 0

		// Score based on search terms found in content
		for _, term := range searchTerms {
			if strings.Contains(contentLower, term) {
				score += 10
				// Bonus for terms in function names, types, etc.
				if strings.Contains(contentLower, "func "+term) ||
					strings.Contains(contentLower, "type "+term) ||
					strings.Contains(contentLower, term+"(") {
					score += 20
				}
			}
		}

		// Bonus for file path relevance
		pathLower := strings.ToLower(path)
		for _, term := range searchTerms {
			if strings.Contains(pathLower, term) {
				score += 15
			}
		}

		if score > 0 {
			relevantFiles[path] = score
			logger.Logf("Found relevant file: %s (score: %d)", path, score)
		}

		return nil
	})

	if err != nil {
		logger.LogError(fmt.Errorf("error walking directory for content search: %w", err))
		return []string{} // Return empty instead of using project-specific inference
	}

	// Sort files by relevance score
	type fileScore struct {
		path  string
		score int
	}

	var scored []fileScore
	for file, score := range relevantFiles {
		scored = append(scored, fileScore{file, score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Return top 5 most relevant files
	var result []string
	maxFiles := 5
	for i, fs := range scored {
		if i >= maxFiles {
			break
		}
		result = append(result, fs.path)
	}

	if len(result) == 0 {
		logger.Logf("No files found by content search")
		return []string{} // Return empty instead of using project-specific inference
	}

	logger.Logf("Content search found %d relevant files: %v", len(result), result)
	return result
}

// WorkspaceInfo represents comprehensive workspace structure
type WorkspaceInfo struct {
	ProjectType   string              // "go", "typescript", "python", etc.
	RootFiles     []string            // Files in root directory
	AllFiles      []string            // All source files
	FilesByDir    map[string][]string // Files organized by directory
	RelevantFiles map[string]string   // file path -> brief content summary
}

// buildWorkspaceStructure creates comprehensive workspace analysis
func buildWorkspaceStructure(logger *utils.Logger) (*WorkspaceInfo, error) {
	logger.Logf("Building comprehensive workspace structure...")

	info := &WorkspaceInfo{
		FilesByDir:    make(map[string][]string),
		RelevantFiles: make(map[string]string),
	}

	// Detect project type
	if _, err := os.Stat("go.mod"); err == nil {
		info.ProjectType = "go"
	} else if _, err := os.Stat("package.json"); err == nil {
		info.ProjectType = "typescript"
	} else if _, err := os.Stat("requirements.txt"); err == nil || hasFile("setup.py") {
		info.ProjectType = "python"
	} else {
		info.ProjectType = "other"
	}

	logger.Logf("Detected project type: %s", info.ProjectType)

	// Walk directory structure
	err := filepath.Walk(".", func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue despite errors
		}

		// Skip hidden and common ignore directories
		if strings.HasPrefix(filepath.Base(path), ".") ||
			strings.Contains(path, "node_modules") ||
			strings.Contains(path, "vendor") ||
			strings.Contains(path, "__pycache__") {
			if fileInfo.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !fileInfo.IsDir() && isSourceFile(path) {
			dir := filepath.Dir(path)
			info.AllFiles = append(info.AllFiles, path)
			info.FilesByDir[dir] = append(info.FilesByDir[dir], path)

			// Add root files
			if dir == "." {
				info.RootFiles = append(info.RootFiles, path)
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	logger.Logf("Found %d source files across %d directories", len(info.AllFiles), len(info.FilesByDir))
	return info, nil
}

// extractSearchTerms extracts key search terms from user intent
func extractSearchTerms(intentLower string) []string {
	var terms []string

	// Direct keyword extraction from intent
	words := strings.Fields(intentLower)
	for _, word := range words {
		// Clean punctuation
		word = strings.Trim(word, ".,!?;:")

		// Include relevant technical terms
		if len(word) > 3 && (strings.Contains(word, "orchestration") ||
			strings.Contains(word, "editing") ||
			strings.Contains(word, "model") ||
			strings.Contains(word, "review") ||
			strings.Contains(word, "code") ||
			strings.Contains(word, "llm") ||
			strings.Contains(word, "api") ||
			strings.Contains(word, "config") ||
			strings.Contains(word, "prompt") ||
			strings.Contains(word, "editor") ||
			strings.Contains(word, "embedding")) {
			terms = append(terms, word)
		}
	}

	// Add compound terms that might be written as one word
	if strings.Contains(intentLower, "codereviews") || strings.Contains(intentLower, "codereview") {
		terms = append(terms, "review", "code", "getcodereview")
	}

	if strings.Contains(intentLower, "orchestration model") {
		terms = append(terms, "orchestration", "model")
	}

	if strings.Contains(intentLower, "editing model") {
		terms = append(terms, "editing", "model", "editor")
	}

	// Remove duplicates
	uniqueTerms := make(map[string]bool)
	var result []string
	for _, term := range terms {
		if !uniqueTerms[term] && len(term) > 2 {
			uniqueTerms[term] = true
			result = append(result, term)
		}
	}

	return result
} // getBasicFileListing returns a simple list of files without full analysis
func getBasicFileListing(logger *utils.Logger) ([]string, error) {
	// This is a simplified version that just lists files without full workspace analysis
	var files []string

	// Walk the current directory to get file paths
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Logf("Error walking path %s: %v", path, err)
			return err
		}

		// Skip hidden directories and files
		if strings.HasPrefix(filepath.Base(path), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common directories to ignore
		skipDirs := []string{"node_modules", "vendor", "target", "build", "dist", "__pycache__"}
		for _, skip := range skipDirs {
			if strings.Contains(path, skip) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Only include source files
		if !info.IsDir() && isSourceFile(path) {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

// isSourceFile checks if a file is likely a source code file
func isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	sourceExts := []string{".go", ".py", ".js", ".ts", ".java", ".c", ".cpp", ".rs", ".rb", ".php", ".scala", ".kt"}

	for _, sourceExt := range sourceExts {
		if ext == sourceExt {
			return true
		}
	}
	return false
}

// executeEditPlan executes the detailed edit plan using the fast editing model
func executeEditPlan(editPlan *EditPlan, cfg *config.Config, logger *utils.Logger) (int, error) {
	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: executeEditPlan started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	totalTokens := 0

	logger.LogProcessStep(fmt.Sprintf("⚡ Executing %d edit operations from orchestration plan", len(editPlan.EditOperations)))
	logger.LogProcessStep(fmt.Sprintf("🎯 Scope: %s", editPlan.ScopeStatement))
	logger.LogProcessStep(fmt.Sprintf("Strategy: %s", editPlan.Context))

	// Track changes for final review
	var changesLog []string

	// For now, execute edits sequentially
	// TODO: Implement parallel execution for independent file edits
	for i, operation := range editPlan.EditOperations {
		logger.LogProcessStep(fmt.Sprintf("🔧 Edit %d/%d: %s (%s)", i+1, len(editPlan.EditOperations), operation.Description, operation.FilePath))
		logger.LogProcessStep(fmt.Sprintf("   📋 Scope Justification: %s", operation.ScopeJustification))

		// Track this change
		changesLog = append(changesLog, fmt.Sprintf("File: %s | Change: %s | Justification: %s",
			operation.FilePath, operation.Description, operation.ScopeJustification))

		// Create focused instructions for this specific edit
		// The orchestration model should provide self-contained instructions with hashtag file references
		editInstructions := buildFocusedEditInstructions(operation, logger)

		// Estimate tokens for this edit
		tokenEstimate := utils.EstimateTokens(editInstructions)
		totalTokens += tokenEstimate

		// Execute the edit using partial editing for efficiency, fallback to full file editing
		var err error

		// Try partial editing first for better efficiency
		if shouldUsePartialEdit(operation, logger) {
			logger.Logf("Attempting partial edit for %s", operation.FilePath)
			_, err = editor.ProcessPartialEdit(operation.FilePath, operation.Instructions, cfg, logger)
			if err != nil {
				logger.Logf("Partial edit failed, falling back to full file edit: %v", err)
				// Fall back to full file editing
				_, err = editor.ProcessCodeGeneration(operation.FilePath, editInstructions, cfg, "")
			} else {
				logger.LogProcessStep(fmt.Sprintf("✅ Edit %d completed with partial editing (efficient)", i+1))
				continue
			}
		} else {
			// Use full file editing directly
			logger.Logf("Using full file edit for %s", operation.FilePath)
			_, err = editor.ProcessCodeGeneration(operation.FilePath, editInstructions, cfg, "")
		}

		if err != nil {
			// Check if this is a "revisions applied" signal from the editor's review process
			if strings.Contains(err.Error(), "revisions applied, re-validating") {
				logger.LogProcessStep(fmt.Sprintf("✅ Edit %d completed with revision cycle", i+1))
				logger.Logf("Final status: %s", err.Error())
			} else {
				logger.LogError(fmt.Errorf("edit operation %d failed: %w", i+1, err))
				duration := time.Since(startTime)
				runtime.ReadMemStats(&m)
				logger.Logf("PERF: executeEditPlan completed (error). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
				return totalTokens, fmt.Errorf("edit operation %d failed: %w", i+1, err)
			}
		} else {
			logger.LogProcessStep(fmt.Sprintf("✅ Edit %d completed successfully", i+1))
		}
	}

	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: executeEditPlan completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	logger.LogProcessStep(fmt.Sprintf("🎉 All %d edit operations completed successfully", len(editPlan.EditOperations)))

	// Log summary of all changes made
	logger.LogProcessStep("📋 Summary of Changes Made:")
	for _, changeLog := range changesLog {
		logger.LogProcessStep(fmt.Sprintf("   • %s", changeLog))
	}

	return totalTokens, nil
}

// shouldUsePartialEdit determines whether to use partial editing or full file editing
// based on the operation characteristics and file size
// shouldUsePartialEdit determines whether to use partial editing or full file editing
// based on the operation characteristics and file size
func shouldUsePartialEdit(operation EditOperation, logger *utils.Logger) bool {
	// Check if file exists and get its size
	fileInfo, err := os.Stat(operation.FilePath)
	if err != nil {
		logger.Logf("Cannot stat file %s, using full file edit: %v", operation.FilePath, err)
		return false
	}

	// For very small files (< 1KB), partial editing overhead isn't worth it
	if fileInfo.Size() < 1024 {
		logger.Logf("File %s is small (%d bytes), using full file edit", operation.FilePath, fileInfo.Size())
		return false
	}

	// For very large files (> 50KB), partial editing is more efficient
	if fileInfo.Size() > 50*1024 {
		logger.Logf("File %s is large (%d bytes), using partial edit", operation.FilePath, fileInfo.Size())
		return true
	}

	// For medium files, check if the operation seems focused/targeted
	instructionsLower := strings.ToLower(operation.Instructions)
	description := strings.ToLower(operation.Description)

	// Keywords that suggest focused changes suitable for partial editing
	focusedKeywords := []string{
		"function", "method", "struct", "type", "variable",
		"add", "modify", "update", "change", "fix",
		"import", "constant", "field",
	}

	// Keywords that suggest broad changes requiring full file context
	broadKeywords := []string{
		"refactor", "restructure", "rewrite", "reorganize",
		"architecture", "design pattern", "interface",
		"multiple", "throughout", "entire",
	}

	focusedScore := 0
	broadScore := 0

	for _, keyword := range focusedKeywords {
		if strings.Contains(instructionsLower, keyword) || strings.Contains(description, keyword) {
			focusedScore++
		}
	}

	for _, keyword := range broadKeywords {
		if strings.Contains(instructionsLower, keyword) || strings.Contains(description, keyword) {
			broadScore++
		}
	}

	// If it seems focused, use partial editing
	if focusedScore > broadScore {
		logger.Logf("Operation seems focused (score: %d vs %d), using partial edit", focusedScore, broadScore)
		return true
	}

	// Default to full file editing for ambiguous cases
	logger.Logf("Operation seems broad or ambiguous (score: %d vs %d), using full file edit", focusedScore, broadScore)
	return false
}

// buildFocusedEditInstructions creates targeted instructions for a single file edit
// The orchestration model should provide self-contained instructions with hashtag file references
func buildFocusedEditInstructions(operation EditOperation, logger *utils.Logger) string {
	// Log inputs for debugging
	logger.LogProcessStep("🔧 BUILDING EDIT INSTRUCTIONS:")
	logger.LogProcessStep(fmt.Sprintf("Operation: %s", operation.Description))
	logger.LogProcessStep(fmt.Sprintf("Target File: %s", operation.FilePath))
	logger.LogProcessStep(fmt.Sprintf("Scope Justification: %s", operation.ScopeJustification))

	var instructions strings.Builder

	// Start with the specific operation instructions
	instructions.WriteString(fmt.Sprintf("Task: %s\n\n", operation.Instructions))

	// Add file-specific context
	instructions.WriteString(fmt.Sprintf("Target File: %s\n\n", operation.FilePath))

	// Add scope constraint
	instructions.WriteString(fmt.Sprintf("SCOPE REQUIREMENT: %s\n\n", operation.ScopeJustification))

	// Add focused guidance for fast editing model
	instructions.WriteString(`CRITICAL EDITING CONSTRAINTS:
- Make ONLY the changes specified in the task - NO ADDITIONAL IMPROVEMENTS
- Do NOT add features, optimizations, or enhancements not explicitly requested  
- Do NOT refactor code unless that was the specific request
- Do NOT fix unrelated issues or add "nice to have" changes
- STAY STRICTLY within the scope defined above
- Make TARGETED, PRECISE edits to achieve the specified goal
- Follow existing code patterns and conventions in the file
- Preserve all existing functionality unless explicitly changing it
- Focus only on the requested change, don't make unrelated improvements
- Ensure the change integrates naturally with the existing code

`)

	// The orchestration model should have provided self-contained instructions
	// with hashtag file references that the downstream editing process will handle
	instructions.WriteString("Note: Any file references with hashtag syntax will be automatically loaded as context.\n\n")
	instructions.WriteString("Please implement the requested change efficiently and precisely.\n")

	// Log the full context being sent to the LLM for debugging
	fullInstructions := instructions.String()
	logger.LogProcessStep("📋 FULL INSTRUCTIONS SENT TO EDITING MODEL:")
	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.LogProcessStep(fmt.Sprintf("Target File: %s", operation.FilePath))
	logger.LogProcessStep(fmt.Sprintf("Instructions Size: %d characters", len(fullInstructions)))
	logger.LogProcessStep("Self-contained: Using hashtag file references for context")

	// Check if instructions contain hashtag references
	if strings.Contains(operation.Instructions, "#") {
		logger.LogProcessStep("✅ Instructions contain hashtag file references - context will be loaded automatically")
	} else {
		logger.LogProcessStep("ℹ️  No hashtag file references found - instructions should be self-contained")
	}

	logger.LogProcessStep("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	return fullInstructions
}

// buildBasicFileContext is a fallback when workspace.json is not available
func buildBasicFileContext(contextFiles []string, logger *utils.Logger) string {
	var context strings.Builder
	context.WriteString("Relevant Files (Basic Context):\n")

	for _, file := range contextFiles {
		if content, err := os.ReadFile(file); err == nil {
			// Limit content size to reduce token usage
			contentStr := string(content)
			if len(contentStr) > 5000 { // Limit to ~5KB per file
				contentStr = contentStr[:5000] + "\n... (content truncated)"
			}

			context.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", file, contentStr))
		} else {
			logger.Logf("Could not read file %s for context: %v", file, err)
		}
	}

	return context.String()
}

// validateChangesWithIteration validates changes and iteratively fixes issues
func validateChangesWithIteration(intentAnalysis *IntentAnalysis, originalIntent string, cfg *config.Config, logger *utils.Logger, tokenUsage *TokenUsage) (int, error) {
	const maxIterations = 3
	totalValidationTokens := 0

	for iteration := 1; iteration <= maxIterations; iteration++ {
		logger.LogProcessStep(fmt.Sprintf("🔄 Validation iteration %d/%d", iteration, maxIterations))

		// Phase: Determine validation strategy
		strategyStartTime := time.Now()
		logger.Logf("DEBUG: Determining validation strategy...")
		validationStrategy, strategyTokens, err := determineValidationStrategy(intentAnalysis, cfg, logger)
		strategyDuration := time.Since(strategyStartTime)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to determine validation strategy (took %v): %w", strategyDuration, err))
			// Fall back to basic validation
			validationStrategy = getBasicValidationStrategy(intentAnalysis, logger)
			logger.Logf("DEBUG: Falling back to basic validation strategy.")
		} else {
			logger.Logf("DEBUG: Validation strategy determined (took %v). Project Type: %s, Steps: %d", strategyDuration, validationStrategy.ProjectType, len(validationStrategy.Steps))
		}
		totalValidationTokens += strategyTokens

		var validationResults []string
		var hasFailures bool

		// Phase: Run validation steps
		logger.Logf("DEBUG: Running %d validation steps...", len(validationStrategy.Steps))
		for _, step := range validationStrategy.Steps {
			stepStartTime := time.Now()

			// Notify user about what validation is being run
			fmt.Printf("🔍 Running validation: %s\n", step.Description)
			fmt.Printf("   Command: %s\n", step.Command)

			logger.LogProcessStep(fmt.Sprintf("Running validation: %s (Command: %s)", step.Description, step.Command))

			result := ""
			stepErr := error(nil)

			// Go's equivalent of try-catch for unexpected panics during a validation step
			func() {
				defer func() {
					if r := recover(); r != nil {
						stepErr = fmt.Errorf("panic during validation step '%s': %v", step.Description, r)
						logger.LogError(stepErr)
					}
				}()
				// Actual call to run the validation step
				result, stepErr = runValidationStep(step, logger)
			}()

			stepDuration := time.Since(stepStartTime)

			if stepErr != nil {
				if step.Required {
					validationResults = append(validationResults, fmt.Sprintf("❌ %s: %v (took %v)", step.Description, stepErr, stepDuration))
					fmt.Printf("   ❌ FAILED in %v: %v\n", stepDuration, stepErr)
					logger.Logf("Validation step '%s' FAILED (took %v): %v", step.Description, stepDuration, stepErr)
					hasFailures = true // Only mark as failure if step is required
				} else {
					validationResults = append(validationResults, fmt.Sprintf("⚠️ %s: %v (took %v) [OPTIONAL]", step.Description, stepErr, stepDuration))
					fmt.Printf("   ⚠️ WARNING in %v: %v (optional step)\n", stepDuration, stepErr)
					logger.Logf("Validation step '%s' WARNING (took %v): %v [OPTIONAL - not blocking]", step.Description, stepDuration, stepErr)
				}
			} else {
				validationResults = append(validationResults, fmt.Sprintf("✅ %s: %s (took %v)", step.Description, result, stepDuration))
				fmt.Printf("   ✅ PASSED in %v\n", stepDuration)
				logger.Logf("Validation step '%s' PASSED (took %v): %s", step.Description, stepDuration, result)
			}
		}

		// Phase: Analyze results and decide next action
		if !hasFailures {
			logger.LogProcessStep("✅ All validation steps passed!")
			return totalValidationTokens, nil
		}

		// If this is the last iteration, don't try to fix, just report failure
		if iteration == maxIterations {
			logger.LogProcessStep("❌ Max iterations reached, validation still failing. Final analysis...")
			analysisStartTime := time.Now()
			analysisTokens, err := analyzeValidationResults(validationResults, intentAnalysis, validationStrategy, cfg, logger)
			analysisDuration := time.Since(analysisStartTime)
			totalValidationTokens += analysisTokens

			if err != nil {
				// If analysis failed, treat as failure
				logger.LogError(fmt.Errorf("failed to analyze final validation results (took %v): %w", analysisDuration, err))
				return totalValidationTokens, fmt.Errorf("validation failed after %d iterations", maxIterations)
			} else {
				// Analysis succeeded - this means LLM determined we can proceed despite validation failures
				logger.Logf("DEBUG: Final validation analysis completed (took %v) - LLM approved proceeding", analysisDuration)
				return totalValidationTokens, nil
			}
		}

		// Phase: Attempt to fix issues automatically
		logger.LogProcessStep(fmt.Sprintf("🔧 Attempting to fix validation issues (iteration %d)", iteration))
		fixStartTime := time.Now()
		fixTokens, err := fixValidationIssues(validationResults, originalIntent, intentAnalysis, cfg, logger)
		fixDuration := time.Since(fixStartTime)
		totalValidationTokens += fixTokens

		if err != nil {
			logger.LogError(fmt.Errorf("failed to auto-fix validation issues (took %v): %w", fixDuration, err))
			// Continue to next iteration anyway, as some fixes might have been applied or it might be a transient error
		} else {
			logger.LogProcessStep(fmt.Sprintf("✅ Applied potential fixes (took %v), re-validating...", fixDuration))
		}
	}

	return totalValidationTokens, fmt.Errorf("validation failed after %d iterations", maxIterations)
}

// fixValidationIssues attempts to automatically fix validation failures using LLM analysis
func fixValidationIssues(validationResults []string, originalIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (int, error) {
	startTime := time.Now()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: fixValidationIssues started. Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)

	// Check if there are any failures to fix
	hasFailures := false
	for _, result := range validationResults {
		if strings.HasPrefix(result, "❌") {
			hasFailures = true
			break
		}
	}

	if !hasFailures {
		logger.Logf("No validation failures to fix")
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: fixValidationIssues completed (no issues). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return 0, nil
	}

	logger.LogProcessStep("🔧 Applying LLM-analyzed fixes...")

	// Step 1: Use orchestration model to analyze errors and create fix plan
	fixPlan, analysisTokens, err := analyzeValidationErrorsWithContext(validationResults, originalIntent, intentAnalysis, cfg, logger)
	if err != nil {
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: fixValidationIssues completed (analysis error). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return analysisTokens, fmt.Errorf("failed to analyze validation errors: %w", err)
	}

	// Step 2: Execute the fix plan using the editing model
	execTokens, err := executeValidationFixPlan(fixPlan, cfg, logger)
	if err != nil {
		duration := time.Since(startTime)
		runtime.ReadMemStats(&m)
		logger.Logf("PERF: fixValidationIssues completed (execution error). Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
		return analysisTokens + execTokens, fmt.Errorf("failed to execute fix plan: %w", err)
	}

	totalTokens := analysisTokens + execTokens
	duration := time.Since(startTime)
	runtime.ReadMemStats(&m)
	logger.Logf("PERF: fixValidationIssues completed. Took %v, Alloc: %v MiB, TotalAlloc: %v MiB, Sys: %v MiB, NumGC: %v", duration, m.Alloc/1024/1024, m.TotalAlloc/1024/1024, m.Sys/1024/1024, m.NumGC)
	return totalTokens, nil
}

// ValidationFixPlan represents a plan to fix validation errors
type ValidationFixPlan struct {
	ErrorAnalysis string   `json:"error_analysis"`
	AffectedFiles []string `json:"affected_files"`
	FixStrategy   string   `json:"fix_strategy"`
	Instructions  []string `json:"instructions"`
}

// analyzeValidationErrorsWithContext uses orchestration model to analyze validation errors and create a comprehensive fix plan
func analyzeValidationErrorsWithContext(validationResults []string, originalIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (*ValidationFixPlan, int, error) {
	// Extract error messages
	var errorMessages []string
	for _, result := range validationResults {
		if strings.HasPrefix(result, "❌") {
			errorMsg := strings.TrimPrefix(result, "❌ ")
			errorMessages = append(errorMessages, errorMsg)
		}
	}

	if len(errorMessages) == 0 {
		return nil, 0, fmt.Errorf("no error messages found in validation results")
	}

	// Use embeddings to find files related to the errors
	relevantFiles, err := findFilesRelatedToErrors(errorMessages, cfg, logger)
	if err != nil {
		logger.Logf("Warning: Could not find files using embeddings for errors: %v", err)
		relevantFiles = []string{}
	}

	// Get project file tree for context
	fileTree, err := getProjectFileTree()
	if err != nil {
		logger.Logf("Warning: Could not get project file tree: %v", err)
		fileTree = "Unable to load project structure"
	}

	// Build comprehensive analysis prompt
	prompt := fmt.Sprintf(`You are an expert Go developer analyzing validation errors to create a targeted fix plan.

ORIGINAL TASK: %s
TASK CATEGORY: %s

VALIDATION ERRORS:
%s

PROJECT FILE TREE:
%s

POTENTIALLY RELEVANT FILES (from embedding search):
%s

CONTEXT:
- This is a Go project with module "github.com/alantheprice/ledit"
- All imports must use full module paths
- Focus on minimal, targeted fixes that resolve the specific errors
- Consider both direct fixes and dependency issues

ANALYSIS REQUIREMENTS:
1. **Root Cause**: What is the underlying cause of these validation errors?
2. **Error Classification**: Are these errors related to the recent changes or pre-existing issues?
3. **Affected Files**: Which specific files need changes to fix these errors?
4. **Fix Strategy**: What is the minimal approach to resolve all errors?
5. **Implementation Plan**: Specific instructions for each file change needed

Respond with a JSON object containing your analysis and fix plan:
{
  "error_analysis": "Detailed analysis of what went wrong",
  "affected_files": ["list", "of", "files", "that", "need", "changes"],
  "fix_strategy": "High-level strategy for fixing the errors",
  "instructions": ["specific instruction 1", "specific instruction 2", "..."]
}`,
		originalIntent,
		intentAnalysis.Category,
		strings.Join(errorMessages, "\n"),
		fileTree,
		strings.Join(relevantFiles, "\n"))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert Go developer who excels at analyzing validation errors and creating targeted fix plans. Always respond with valid JSON."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 60*time.Second)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get orchestration model analysis: %w", err)
	}

	// Parse the JSON response
	var fixPlan ValidationFixPlan
	err = json.Unmarshal([]byte(response), &fixPlan)
	if err != nil {
		logger.Logf("Failed to parse JSON response, using fallback: %v", err)
		// Create a fallback plan
		fixPlan = ValidationFixPlan{
			ErrorAnalysis: "JSON parsing failed, using fallback analysis",
			AffectedFiles: relevantFiles,
			FixStrategy:   "Apply general fixes based on error patterns",
			Instructions:  []string{response}, // Use raw response as instruction
		}
	}

	tokens := utils.EstimateTokens(prompt + response)
	logger.Logf("Validation error analysis complete: %d affected files, strategy: %s", len(fixPlan.AffectedFiles), fixPlan.FixStrategy)

	return &fixPlan, tokens, nil
}

// executeValidationFixPlan executes the fix plan using the editing model
func executeValidationFixPlan(plan *ValidationFixPlan, cfg *config.Config, logger *utils.Logger) (int, error) {
	logger.Logf("Executing fix plan: %s", plan.FixStrategy)

	totalTokens := 0

	// If we have specific files to fix, target them individually
	if len(plan.AffectedFiles) > 0 {
		for _, filePath := range plan.AffectedFiles {
			// Check if file exists
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				logger.Logf("Skipping non-existent file: %s", filePath)
				continue
			}

			// Create targeted fix instructions for this file
			fileInstructions := fmt.Sprintf(`Fix validation errors in this file based on the following analysis and strategy:

ERROR ANALYSIS: %s

FIX STRATEGY: %s

SPECIFIC INSTRUCTIONS:
%s

Focus only on changes needed to resolve validation errors. Make minimal, targeted fixes.`,
				plan.ErrorAnalysis,
				plan.FixStrategy,
				strings.Join(plan.Instructions, "\n"))

			// Apply fix using partial edit
			logger.Logf("Applying fixes to file: %s", filePath)
			_, err := editor.ProcessPartialEdit(filePath, fileInstructions, cfg, logger)
			if err != nil {
				logger.Logf("Failed to apply fixes to %s: %v", filePath, err)
				// Try full file processing as fallback
				_, err = editor.ProcessCodeGeneration(filePath, fileInstructions, cfg, "")
				if err != nil {
					logger.Logf("Failed full file fix for %s: %v", filePath, err)
					continue
				}
			}

			// Estimate tokens used (rough approximation)
			totalTokens += utils.EstimateTokens(fileInstructions) / 2 // Divide by 2 since response is typically shorter
		}
	} else {
		// No specific files identified, use general fix approach
		generalInstructions := fmt.Sprintf(`Fix the validation errors based on this analysis:

ERROR ANALYSIS: %s
FIX STRATEGY: %s

INSTRUCTIONS:
%s

Apply fixes to resolve all validation errors.`,
			plan.ErrorAnalysis,
			plan.FixStrategy,
			strings.Join(plan.Instructions, "\n"))

		_, err := editor.ProcessCodeGeneration("", generalInstructions, cfg, "")
		if err != nil {
			return totalTokens, fmt.Errorf("failed to apply general fixes: %w", err)
		}

		totalTokens += utils.EstimateTokens(generalInstructions) / 2
	}

	logger.Logf("Fix plan execution completed for %d files", len(plan.AffectedFiles))
	return totalTokens, nil
}

// findFilesRelatedToErrors uses embeddings to find files that might be related to the validation errors
func findFilesRelatedToErrors(errorMessages []string, cfg *config.Config, logger *utils.Logger) ([]string, error) {
	// Load workspace file for embeddings
	workspaceFileData, err := workspace.LoadWorkspaceFile()
	if err != nil {
		return nil, fmt.Errorf("could not load workspace file: %w", err)
	}

	var allRelevantFiles []string

	// Search for files related to each error message
	for _, errorMsg := range errorMessages {
		// Extract key terms from error message for embedding search
		searchQuery := fmt.Sprintf("Error: %s", errorMsg)

		fullContextFiles, summaryContextFiles, err := workspace.GetFilesForContextUsingEmbeddings(searchQuery, workspaceFileData, cfg, logger)
		if err != nil {
			logger.Logf("Embedding search failed for error: %v", err)
			continue
		}

		// Combine and deduplicate files
		relevantFiles := append(fullContextFiles, summaryContextFiles...)
		for _, file := range relevantFiles {
			// Simple deduplication
			found := false
			for _, existing := range allRelevantFiles {
				if existing == file {
					found = true
					break
				}
			}
			if !found {
				allRelevantFiles = append(allRelevantFiles, file)
			}
		}
	}

	// Limit to reasonable number of files
	maxFiles := 10
	if len(allRelevantFiles) > maxFiles {
		allRelevantFiles = allRelevantFiles[:maxFiles]
	}

	return allRelevantFiles, nil
}

// getProjectFileTree returns a representation of the project file structure
func getProjectFileTree() (string, error) {
	var tree strings.Builder

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue despite errors
		}

		// Skip hidden files and common ignore patterns
		if strings.HasPrefix(filepath.Base(path), ".") && path != "." {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common build/temp directories
		skipDirs := []string{"vendor", "node_modules", "target", "build", "dist"}
		for _, skipDir := range skipDirs {
			if strings.Contains(path, skipDir) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Build tree representation
		depth := strings.Count(path, string(os.PathSeparator))
		indent := strings.Repeat("  ", depth)

		if info.IsDir() {
			tree.WriteString(fmt.Sprintf("%s%s/\n", indent, filepath.Base(path)))
		} else {
			tree.WriteString(fmt.Sprintf("%s%s\n", indent, filepath.Base(path)))
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return tree.String(), nil
}

// analyzeBuildErrorsAndCreateFix uses LLM to understand build errors and create targeted fixes
func analyzeBuildErrorsAndCreateFix(validationResults []string, originalIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (string, int, error) {
	// Extract the actual error messages from validation results
	var errorMessages []string
	for _, result := range validationResults {
		if strings.HasPrefix(result, "❌") {
			// Remove the ❌ prefix and add to error messages
			errorMsg := strings.TrimPrefix(result, "❌ ")
			errorMessages = append(errorMessages, errorMsg)
		}
	}

	if len(errorMessages) == 0 {
		return "", 0, fmt.Errorf("no error messages found in validation results")
	}

	prompt := fmt.Sprintf(`You are an expert Go developer helping to fix build errors and improve code quality.

ORIGINAL TASK: %s
TASK CATEGORY: %s

BUILD/VALIDATION ERRORS:
%s

PROJECT CONTEXT:
- This is a Go project with module "github.com/alantheprice/ledit"
- All import paths must use the full module path (e.g., "github.com/alantheprice/ledit/pkg/utils")
- Key APIs available:
  * Logger: GetLogger(bool).Log(string) for logging messages
  * Filesystem: use "github.com/alantheprice/ledit/pkg/filesystem" package for file operations
  * Follow existing patterns and conventions in the codebase

ANALYSIS INSTRUCTIONS:
1. **Primary Fix**: Analyze the build/validation errors and determine minimal fixes needed
2. **Error Classification**: Are these errors related to the recent changes or pre-existing issues?
3. **Test Assessment**: Based on the original task, determine if tests are needed:
   - For new utility functions (like CreateBackup): suggest unit tests
   - For new features/commands: suggest integration tests  
   - For bug fixes: suggest regression tests
4. **Code Quality**: Identify any obvious quality improvements that align with the original task

RESPONSE FORMAT:
Provide a comprehensive fix prompt that addresses:
- Immediate build errors (highest priority)
- Missing tests if appropriate for the task
- Any quality improvements that directly support the original task

Focus on making the code production-ready while maintaining minimal scope.

Create a detailed fix prompt:`,
		originalIntent,
		intentAnalysis.Category,
		strings.Join(errorMessages, "\n"))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert Go developer who excels at diagnosing and fixing build errors. Respond with a clear, actionable fix prompt."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		return "", 0, fmt.Errorf("failed to get orchestration model analysis of build errors: %w", err)
	}

	// Estimate tokens used
	tokens := utils.EstimateTokens(prompt + response)

	logger.Logf("LLM build error analysis: %s", response)

	return response, tokens, nil
}

// ProjectContext represents the detected project characteristics
type ProjectContext struct {
	Type         string // "go", "python", "node", "other"
	HasTests     bool
	HasLinting   bool
	BuildCommand string
	TestCommand  string
	LintCommand  string
}

// ValidationStep represents a single validation action
type ValidationStep struct {
	Type        string // "build", "test", "lint", "syntax"
	Command     string
	Description string
	Required    bool // If false, failure won't block
}

// ValidationStrategy represents the complete validation approach for a project
type ValidationStrategy struct {
	ProjectType string
	Steps       []ValidationStep
	Context     string // Additional context about why these steps were chosen
}

// hasFile checks if a file or directory exists
func hasFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// hasTestFiles checks if test files exist in the given directory for the specified language
func hasTestFiles(dir, language string) bool {
	var testPatterns []string
	switch language {
	case "go":
		testPatterns = []string{"*_test.go"}
	case "js", "ts":
		testPatterns = []string{"*.test.js", "*.spec.js"}
	case "py":
		testPatterns = []string{"test_*.py", "*_test.py"}
	default:
		return false
	}

	for _, pattern := range testPatterns {
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

// determineValidationStrategy uses LLM to determine the best validation approach
func determineValidationStrategy(intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (*ValidationStrategy, int, error) {
	// Detect basic project characteristics
	projectInfo := detectProjectInfo(logger)

	prompt := fmt.Sprintf(`You are an expert DevOps engineer. Analyze this project and determine the optimal validation strategy.

Project Information:
- Files present: %s
- Change category: %s
- Change complexity: %s
- Files being modified: %s

Based on this information, determine what validation commands should be run to ensure the changes are correct.

Respond with JSON:
{
  "project_type": "go|python|node|java|other",
  "steps": [
    {
      "type": "build|test|lint|syntax",
      "command": "exact command to run",
      "description": "human readable description",
      "required": true|false
    }
  ],
  "context": "explanation of why these steps were chosen"
}

Guidelines:
- **REQUIRED vs OPTIONAL**: Mark steps as required=true ONLY if failure prevents deployment/usage
- **Build failures**: Always required=true (prevents basic functionality)
- **Syntax errors**: Always required=true (code won't run)
- **Lint warnings**: Always required=false (pre-existing issues shouldn't block changes)
- **Missing tests**: Always required=false (absence of tests is not a failure)
- **Existing test failures**: Use required=false unless directly related to the change
- For Go projects: "go build ./..." (required=true), "go vet ./..." (required=false)
- For Python: "python -m py_compile" (required=true), "pytest" (required=false)
- For Node.js: "npm run build" (required=true), "npm test" (required=false), "npm run lint" (required=false)
- Consider the change type: docs/comments/small fixes need minimal required validation`,
		strings.Join(projectInfo.AvailableFiles, ", "),
		intentAnalysis.Category,
		intentAnalysis.Complexity,
		strings.Join(intentAnalysis.EstimatedFiles, ", "))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at DevOps and project validation. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		return nil, 0, fmt.Errorf("orchestration model failed to determine validation strategy: %w", err)
	}

	// Parse the response
	response = strings.TrimSpace(response)
	if strings.Contains(response, "```json") {
		parts := strings.Split(response, "```json")
		if len(parts) > 1 {
			jsonPart := parts[1]
			end := strings.Index(jsonPart, "```")
			if end > 0 {
				response = strings.TrimSpace(jsonPart[:end])
			}
		}
	}

	var strategy ValidationStrategy
	if err := json.Unmarshal([]byte(response), &strategy); err != nil {
		return nil, 0, fmt.Errorf("failed to parse validation strategy JSON: %w", err)
	}

	// Estimate tokens used
	tokens := utils.EstimateTokens(prompt + response)

	logger.Logf("LLM-determined validation strategy: %s project with %d steps", strategy.ProjectType, len(strategy.Steps))

	return &strategy, tokens, nil
}

// ProjectInfo represents detected project characteristics
type ProjectInfo struct {
	AvailableFiles  []string
	HasGoMod        bool
	HasPackageJSON  bool
	HasRequirements bool
	HasMakefile     bool
}

// detectProjectInfo gathers basic project information for LLM analysis
func detectProjectInfo(logger *utils.Logger) ProjectInfo {
	info := ProjectInfo{}

	// Check for common project files
	commonFiles := []string{"go.mod", "package.json", "requirements.txt", "pyproject.toml", "Makefile", "Dockerfile", "README.md"}

	for _, file := range commonFiles {
		if hasFile(file) {
			info.AvailableFiles = append(info.AvailableFiles, file)
			switch file {
			case "go.mod":
				info.HasGoMod = true
			case "package.json":
				info.HasPackageJSON = true
			case "requirements.txt":
				info.HasRequirements = true
			case "Makefile":
				info.HasMakefile = true
			}
		}
	}

	// Add some source files to give context
	if files, err := getBasicFileListing(logger); err == nil && len(files) > 0 {
		// Add up to 5 source files as examples
		count := 0
		for _, file := range files {
			if count >= 5 {
				break
			}
			info.AvailableFiles = append(info.AvailableFiles, file)
			count++
		}
	}

	return info
}

// getBasicValidationStrategy provides fallback validation when LLM fails
func getBasicValidationStrategy(intentAnalysis *IntentAnalysis, logger *utils.Logger) *ValidationStrategy {
	strategy := &ValidationStrategy{
		ProjectType: "unknown",
		Context:     "Fallback strategy when LLM analysis failed",
	}

	// Detect project type with simple heuristics
	if hasFile("go.mod") {
		strategy.ProjectType = "go"
		strategy.Steps = []ValidationStep{
			{Type: "syntax", Command: "go mod tidy", Description: "Ensures the go.mod file matches the source code's dependencies.", Required: false},
			{Type: "build", Command: "go build ./...", Description: "Compiles all packages in the project to ensure there are no build errors.", Required: true},
			{Type: "lint", Command: "go vet ./...", Description: "Runs the Go vet tool to check for suspicious constructs and potential errors.", Required: false},
		}
		// Add tests only if test files exist
		if hasTestFiles(".", "go") {
			strategy.Steps = append(strategy.Steps, ValidationStep{
				Type: "test", Command: "go test ./...", Description: "Runs all unit tests to verify functionality and prevent regressions.", Required: false,
			})
		}
	} else if hasFile("package.json") {
		strategy.ProjectType = "node"
		strategy.Steps = []ValidationStep{
			{Type: "syntax", Command: "node --check *.js", Description: "JavaScript syntax check", Required: true},
		}
		if hasTestFiles(".", "js") {
			strategy.Steps = append(strategy.Steps, ValidationStep{
				Type: "test", Command: "npm test", Description: "Runs Node.js tests", Required: false,
			})
		}
	} else if hasFile("requirements.txt") || hasFile("pyproject.toml") {
		strategy.ProjectType = "python"
		strategy.Steps = []ValidationStep{
			{Type: "syntax", Command: "python -m py_compile *.py", Description: "Python syntax check", Required: true},
		}
		if hasTestFiles(".", "py") {
			strategy.Steps = append(strategy.Steps, ValidationStep{
				Type: "test", Command: "python -m pytest", Description: "Runs Python tests", Required: false,
			})
		}
	} else {
		// Generic validation
		strategy.Steps = []ValidationStep{
			{Type: "syntax", Command: "echo 'No specific validation available'", Description: "Basic check", Required: false},
		}
	}

	logger.Logf("Using fallback validation strategy for %s project", strategy.ProjectType)
	return strategy
}

// runValidationStep executes a single validation step
func runValidationStep(step ValidationStep, logger *utils.Logger) (string, error) {
	// Split command into parts
	parts := strings.Fields(step.Command)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	logger.Logf("Running command: %s", step.Command)

	// Execute the command
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = "." // Run in current directory

	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	logger.Logf("Command output: %s", outputStr)
	logger.Logf("Command error: %v", err)

	if err != nil {
		return outputStr, fmt.Errorf("command failed: %w", err)
	}

	if outputStr == "" {
		return "Success (no output)", nil
	}

	return outputStr, nil
}

// analyzeValidationResults uses LLM to analyze validation results and decide whether to proceed
func analyzeValidationResults(validationResults []string, intentAnalysis *IntentAnalysis, validationStrategy *ValidationStrategy, cfg *config.Config, logger *utils.Logger) (int, error) {
	// Check if there are any failures
	hasFailures := false
	for _, result := range validationResults {
		if strings.HasPrefix(result, "❌") {
			hasFailures = true
			break
		}
	}

	if !hasFailures {
		logger.Logf("All validation steps passed successfully")
		return 0, nil
	}

	prompt := fmt.Sprintf(`You are an expert developer analyzing validation failures after implementing changes.

RECENT TASK: %s
TASK CATEGORY: %s
PROJECT TYPE: %s

VALIDATION RESULTS:
%s

ANALYSIS REQUIRED:
1. **Error Classification**: Are these failures related to the recent task, or pre-existing issues?
2. **Impact Assessment**: Do these failures affect the functionality added/modified by the recent task?
3. **Decision**: Should the validation pass, fail, or require fixes?

DECISION CRITERIA:
- **REQUIRED failures**: Only these should trigger FIX or FAIL decisions
- **OPTIONAL failures** (marked with ⚠️): These should typically be PASS (ignore pre-existing issues)
- **Build/syntax errors** (required=true): Must be addressed → FAIL if severe, FIX if simple
- **Lint warnings** (required=false): Usually pre-existing → PASS unless directly caused by changes
- **Missing or failing tests** (required=false): Not a blocker → PASS (missing tests ≠ failure)
- **Vet warnings** (required=false): Pre-existing linting issues → PASS

SPECIFIC EXAMPLES:
- "go vet" warnings about format strings → PASS (pre-existing lint issues)
- "go build" failures → FAIL or FIX (prevents functionality)
- "tests not found" or "no tests" → PASS (absence is not failure)
- Existing test failures unrelated to changes → PASS

Focus on: Did the changes work correctly? Ignore pre-existing project issues.

RESPONSE FORMAT:
DECISION: [PASS|FAIL|FIX]
REASONING: [One sentence explaining why]
ACTION: [What should be done next, if anything]

Focus on whether the recent changes achieved their goal successfully, not on fixing unrelated pre-existing issues.`,
		intentAnalysis.Category,
		intentAnalysis.Category,
		validationStrategy.ProjectType,
		strings.Join(validationResults, "\n"))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert developer who understands the difference between task-related failures and pre-existing issues. Make practical decisions about validation."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		return 0, fmt.Errorf("failed to get orchestration model analysis of validation results: %w", err)
	}

	// Log the analysis
	logger.LogProcessStep("🔍 Validation Analysis:")
	logger.Logf("%s", response)

	// Parse the decision from the response
	decision := parseValidationDecision(response, logger)

	// Act on the decision
	switch decision {
	case "PASS":
		logger.LogProcessStep("✅ LLM recommends proceeding despite validation failures (unrelated issues)")
		return utils.EstimateTokens(prompt + response), nil
	case "FIX":
		logger.LogProcessStep("🔧 LLM recommends attempting fixes for task-related issues")
		return utils.EstimateTokens(prompt + response), fmt.Errorf("validation failures require fixes")
	case "FAIL":
		logger.LogProcessStep("❌ LLM recommends failing due to critical task-related issues")
		return utils.EstimateTokens(prompt + response), fmt.Errorf("validation failed with critical task-related issues")
	default:
		logger.LogProcessStep("⚠️ Could not parse LLM decision, defaulting to conservative failure")
		return utils.EstimateTokens(prompt + response), fmt.Errorf("validation failed - could not determine if issues are task-related")
	}
}

// parseValidationDecision extracts the decision from the LLM response
func parseValidationDecision(response string, logger *utils.Logger) string {
	responseLower := strings.ToLower(response)

	// Look for decision indicators
	if strings.Contains(responseLower, "decision: pass") || strings.Contains(responseLower, "recommend pass") {
		return "PASS"
	}
	if strings.Contains(responseLower, "decision: fail") || strings.Contains(responseLower, "recommend fail") {
		return "FAIL"
	}
	if strings.Contains(responseLower, "decision: fix") || strings.Contains(responseLower, "recommend fix") {
		return "FIX"
	}

	// Fallback parsing based on keywords
	if strings.Contains(responseLower, "unrelated") || strings.Contains(responseLower, "pre-existing") {
		return "PASS"
	}
	if strings.Contains(responseLower, "critical") || strings.Contains(responseLower, "broke") {
		return "FAIL"
	}

	logger.Logf("Could not parse validation decision from response: %s", response)
	return "UNKNOWN"
}

// printTokenUsageSummary prints a summary of token usage for the agent execution
func printTokenUsageSummary(tokenUsage *TokenUsage, duration time.Duration) {
	fmt.Printf("\n💰 Token Usage Summary:\n")
	fmt.Printf("├─ Intent Analysis: %d tokens\n", tokenUsage.IntentAnalysis)
	fmt.Printf("├─ Planning (Orchestration): %d tokens\n", tokenUsage.Planning)
	fmt.Printf("├─ Code Generation (Editing): %d tokens\n", tokenUsage.CodeGeneration)
	fmt.Printf("├─ Validation: %d tokens\n", tokenUsage.Validation)
	fmt.Printf("└─ Total Usage: %d tokens\n", tokenUsage.Total)

	// Estimate cost (rough approximation for popular models)
	// This is a rough estimate - actual costs vary by provider and model
	estimatedCostCents := float64(tokenUsage.Total/1000) * 0.002 // ~$0.002 per 1k tokens for many models
	if estimatedCostCents < 0.01 {
		fmt.Printf("💵 Estimated Cost: <$0.01\n")
	} else {
		fmt.Printf("💵 Estimated Cost: ~$%.3f\n", estimatedCostCents)
	}

	// Performance metrics
	tokensPerSecond := float64(tokenUsage.Total) / duration.Seconds()
	fmt.Printf("⚡ Performance: %.1f tokens/second\n", tokensPerSecond)
}

// rewordPromptForBetterSearch uses workspace model to reword the user prompt for better file discovery
func rewordPromptForBetterSearch(userIntent string, workspaceInfo *WorkspaceInfo, cfg *config.Config, logger *utils.Logger) (string, error) {
	logger.Logf("Using workspace model to reword prompt for better file discovery...")

	prompt := fmt.Sprintf(`You are a %s codebase expert. The user wants to: "%s"

WORKSPACE CONTEXT:
Project Type: %s
Available files include: %v

The initial file search found very few relevant files. Rewrite the user's intent using technical terms and patterns that would be found in a %s codebase to help find the right files.

Focus on:
- Function names that might exist
- File naming patterns in %s projects  
- Technical terms specific to this domain
- Package/module names that might be relevant

Respond with ONLY the reworded search query, no explanation:`,
		workspaceInfo.ProjectType, userIntent, workspaceInfo.ProjectType,
		workspaceInfo.AllFiles[:min(10, len(workspaceInfo.AllFiles))], // Show sample files
		workspaceInfo.ProjectType, workspaceInfo.ProjectType)

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at understanding codebases and creating effective search queries."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.OrchestrationModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		logger.LogError(fmt.Errorf("workspace model failed to reword prompt: %w", err))
		return userIntent, err // Return original on failure
	}

	reworded := strings.TrimSpace(response)
	if reworded == "" {
		return userIntent, fmt.Errorf("empty reworded response")
	}

	return reworded, nil
}

// findFilesUsingShellCommands uses shell commands to find relevant files when other methods fail
func findFilesUsingShellCommands(userIntent string, workspaceInfo *WorkspaceInfo, logger *utils.Logger) []string {
	logger.Logf("Using shell commands to find files for: %s", userIntent)

	var foundFiles []string
	intentLower := strings.ToLower(userIntent)

	// Extract search terms from intent
	searchTerms := extractSearchTerms(intentLower)
	logger.Logf("Shell search terms: %v", searchTerms)

	for _, term := range searchTerms {
		if len(term) < 3 {
			continue // Skip very short terms
		}

		// Use grep to find files containing the term
		logger.Logf("Searching for files containing: %s", term)
		cmd := exec.Command("grep", "-r", "-l", "-i", term, "--include=*.go", ".")
		output, err := cmd.Output()

		if err != nil {
			logger.Logf("Grep search for '%s' failed: %v", term, err)
			continue
		}

		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line != "" && strings.HasSuffix(line, ".go") {
				cleanPath := strings.TrimPrefix(line, "./")
				foundFiles = append(foundFiles, cleanPath)
				logger.Logf("Shell search found: %s (contains '%s')", cleanPath, term)
			}
		}
	}

	// Remove duplicates and limit results
	seen := make(map[string]bool)
	var unique []string
	for _, file := range foundFiles {
		if !seen[file] && len(unique) < 5 { // Limit to 5 files
			seen[file] = true
			unique = append(unique, file)
		}
	}

	// If no files found with content search, try filename search
	if len(unique) == 0 {
		logger.Logf("No content matches, trying filename search...")
		for _, term := range searchTerms {
			cmd := exec.Command("find", ".", "-name", "*.go", "-path", fmt.Sprintf("*%s*", term))
			output, err := cmd.Output()

			if err != nil {
				continue
			}

			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			for _, line := range lines {
				if line != "" && strings.HasSuffix(line, ".go") {
					cleanPath := strings.TrimPrefix(line, "./")
					if !seen[cleanPath] && len(unique) < 5 {
						seen[cleanPath] = true
						unique = append(unique, cleanPath)
						logger.Logf("Filename search found: %s", cleanPath)
					}
				}
			}
		}
	}

	logger.Logf("Shell commands found %d unique files: %v", len(unique), unique)
	return unique
}

// WorkspacePatterns holds analysis of workspace organization patterns
type WorkspacePatterns struct {
	AverageFileSize      int
	PreferredPackageSize int
	ModularityLevel      string
	GoSpecificPatterns   map[string]string
}

// analyzeWorkspacePatterns analyzes the codebase to understand organizational preferences
func analyzeWorkspacePatterns(logger *utils.Logger) *WorkspacePatterns {
	patterns := &WorkspacePatterns{
		AverageFileSize:    0,
		ModularityLevel:    "medium",
		GoSpecificPatterns: make(map[string]string),
	}

	// Analyze Go files in the workspace
	goFiles, err := findGoFiles(".")
	if err != nil {
		logger.Logf("Warning: Could not analyze workspace patterns: %v", err)
		// Set sensible defaults for Go projects
		patterns.AverageFileSize = 200
		patterns.PreferredPackageSize = 500
		patterns.ModularityLevel = "high"
		patterns.GoSpecificPatterns["file_organization"] = "prefer_small_focused_files"
		patterns.GoSpecificPatterns["package_structure"] = "pkg_separation"
		return patterns
	}

	totalLines := 0
	largeFiles := 0

	for _, file := range goFiles {
		lines := countLines(file)
		totalLines += lines

		if lines > 500 {
			largeFiles++
		}
	}

	if len(goFiles) > 0 {
		patterns.AverageFileSize = totalLines / len(goFiles)
	}

	// Determine modularity preference based on file sizes
	if largeFiles > len(goFiles)/3 {
		patterns.ModularityLevel = "low"
		patterns.GoSpecificPatterns["refactoring_preference"] = "break_large_files"
	} else {
		patterns.ModularityLevel = "high"
		patterns.GoSpecificPatterns["refactoring_preference"] = "maintain_separation"
	}

	// Analyze package structure
	pkgDirs := findPackageDirectories(".")
	if len(pkgDirs) > 3 {
		patterns.GoSpecificPatterns["package_structure"] = "highly_modular"
	} else {
		patterns.GoSpecificPatterns["package_structure"] = "simple_structure"
	}

	patterns.PreferredPackageSize = patterns.AverageFileSize * 3 // Prefer packages with 3 average-sized files

	logger.Logf("Workspace Analysis: Avg file size: %d, Modularity: %s, Large files: %d/%d",
		patterns.AverageFileSize, patterns.ModularityLevel, largeFiles, len(goFiles))

	return patterns
}

// isLargeFileRefactoringTask determines if the task involves refactoring large files
func isLargeFileRefactoringTask(userIntent string, contextFiles []string, logger *utils.Logger) bool {
	intentLower := strings.ToLower(userIntent)

	// Check for refactoring keywords
	refactoringKeywords := []string{"refactor", "split", "break down", "reorganize", "move", "extract"}
	hasRefactoringIntent := false
	for _, keyword := range refactoringKeywords {
		if strings.Contains(intentLower, keyword) {
			hasRefactoringIntent = true
			break
		}
	}

	if !hasRefactoringIntent {
		return false
	}

	// Check if any of the context files are large
	for _, file := range contextFiles {
		if lines := countLines(file); lines > 1000 {
			logger.Logf("Detected large file refactoring task: %s has %d lines", file, lines)
			return true
		}
	}

	return false
}

// generateRefactoringStrategy creates a strategy for complex refactoring tasks
func generateRefactoringStrategy(userIntent string, contextFiles []string, patterns *WorkspacePatterns, logger *utils.Logger) string {
	strategy := []string{
		"INTELLIGENT REFACTORING STRATEGY:",
		fmt.Sprintf("- Workspace prefers files with ~%d lines (current average)", patterns.AverageFileSize),
		fmt.Sprintf("- Modularity level: %s", patterns.ModularityLevel),
	}

	// Analyze the target files
	for _, file := range contextFiles {
		lines := countLines(file)
		if lines > 1000 {
			strategy = append(strategy, fmt.Sprintf("- File %s (%d lines) should be broken into ~%d smaller files",
				file, lines, (lines/patterns.PreferredPackageSize)+1))
		}
	}

	// Add Go-specific guidance
	strategy = append(strategy, []string{
		"",
		"GO BEST PRACTICES FOR REFACTORING:",
		"1. Group related types and functions into logical packages",
		"2. Separate interfaces from implementations",
		"3. Create focused files: types.go, handlers.go, utils.go, etc.",
		"4. Maintain clear import dependencies",
		"5. Use meaningful package and file names",
		"",
		"EXECUTION APPROACH:",
		"- Create step-by-step plan with dependency order",
		"- Move types first, then interfaces, then implementations",
		"- Update imports in dependent files",
		"- Verify compilation after each major step",
	}...)

	return strings.Join(strategy, "\n")
}

// Helper functions for workspace analysis
func findGoFiles(dir string) ([]string, error) {
	var goFiles []string

	cmd := exec.Command("find", dir, "-name", "*.go", "-type", "f")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line != "" && !strings.Contains(line, "vendor/") && !strings.Contains(line, ".git/") {
			goFiles = append(goFiles, strings.TrimPrefix(line, "./"))
		}
	}

	return goFiles, nil
}

func countLines(filePath string) int {
	cmd := exec.Command("wc", "-l", filePath)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	parts := strings.Fields(string(output))
	if len(parts) > 0 {
		if lines, err := strconv.Atoi(parts[0]); err == nil {
			return lines
		}
	}

	return 0
}

func findPackageDirectories(dir string) []string {
	var pkgDirs []string

	cmd := exec.Command("find", dir, "-name", "*.go", "-type", "f", "-exec", "dirname", "{}", ";")
	output, err := cmd.Output()
	if err != nil {
		return pkgDirs
	}

	seen := make(map[string]bool)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		dir := strings.TrimPrefix(line, "./")
		if dir != "" && !seen[dir] && !strings.Contains(dir, "vendor/") && !strings.Contains(dir, ".git/") {
			seen[dir] = true
			pkgDirs = append(pkgDirs, dir)
		}
	}

	return pkgDirs
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// shouldEscalateToOrchestration determines if we should escalate to full orchestration
// based on the type of error encountered
func shouldEscalateToOrchestration(err error, logger *utils.Logger) bool {
	errorStr := err.Error()

	// Check for validation failures that indicate build/compilation issues
	validationFailureIndicators := []string{
		"validation failed",
		"command failed: exit status 1",
		"no required module provides package",
		"import cycle",
		"build failed",
		"compilation error",
		"undefined:",
		"undeclared name",
		"failed to create edit plan",
		"JSON parsing failure",
		"orchestration model failed",
	}

	for _, indicator := range validationFailureIndicators {
		if strings.Contains(errorStr, indicator) {
			logger.LogProcessStep(fmt.Sprintf("🔍 Detected escalation trigger: %s", indicator))
			return true
		}
	}

	// Also escalate for complex refactoring tasks that might need better coordination
	complexTaskIndicators := []string{
		"multiple files",
		"refactor",
		"restructure",
		"move",
		"split",
		"extract",
	}

	for _, indicator := range complexTaskIndicators {
		if strings.Contains(strings.ToLower(errorStr), indicator) {
			logger.LogProcessStep(fmt.Sprintf("🔍 Detected complex task requiring orchestration: %s", indicator))
			return true
		}
	}

	return false
}

// runFullOrchestration runs the complete orchestration workflow for complex tasks
func runFullOrchestration(userIntent string, cfg *config.Config, logger *utils.Logger) error {
	logger.LogProcessStep("🎯 Starting full orchestration workflow...")

	// Use the existing orchestration system
	err := orchestration.OrchestrateFeature(userIntent, cfg)
	if err != nil {
		logger.LogError(fmt.Errorf("orchestration execution failed: %w", err))
		return fmt.Errorf("orchestration execution failed: %w", err)
	}

	logger.LogProcessStep("✅ Full orchestration completed successfully")
	return nil
}
