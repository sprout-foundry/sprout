package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	logger.LogProcessStep("🚀 Starting cost-optimized agent execution...")

	startTime := time.Now()

	// Initialize token usage tracking
	tokenUsage := &TokenUsage{}

	// Use optimized agent logic instead of full orchestration
	if err := runOptimizedAgent(userIntent, cfg, logger, tokenUsage); err != nil {
		logger.LogError(fmt.Errorf("agent execution failed: %w", err))
		return fmt.Errorf("agent execution failed: %w", err)
	}

	duration := time.Since(startTime)

	// Print token usage summary
	printTokenUsageSummary(tokenUsage, duration)

	fmt.Printf("✅ Agent execution completed in %v\n", duration)
	return nil
}

// runOptimizedAgent runs the agent with minimal context to reduce costs
func runOptimizedAgent(userIntent string, cfg *config.Config, logger *utils.Logger, tokenUsage *TokenUsage) error {
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
	if len(intentAnalysis.EstimatedFiles) > 0 {
		logger.LogProcessStep(fmt.Sprintf("Files: %s", strings.Join(intentAnalysis.EstimatedFiles, ", ")))
	}

	// Phase 2: Progressive context loading based on complexity
	var contextFiles []string
	switch intentAnalysis.Complexity {
	case "simple":
		// For simple tasks, load only the specific files mentioned or detected
		contextFiles = intentAnalysis.EstimatedFiles
		// If no files estimated but we can infer some, add them
		if len(contextFiles) == 0 {
			inferred := inferFiles(userIntent)
			if len(inferred) > 0 {
				contextFiles = inferred
				logger.LogProcessStep(fmt.Sprintf("No files estimated by LLM, using inferred files: %s", strings.Join(inferred, ", ")))
			}
		}
		logger.LogProcessStep("🔍 Phase 2: Loading minimal context for simple task...")
	case "moderate":
		// For moderate tasks, use embedding-based selection with lower threshold
		logger.LogProcessStep("🔍 Phase 2: Loading focused context for moderate task...")
		contextFiles, err = getOptimizedContext(userIntent, intentAnalysis.EstimatedFiles, cfg, 5, logger) // Top 5 files
		if err != nil {
			logger.LogError(fmt.Errorf("failed to get optimized context: %w", err))
			return fmt.Errorf("failed to get optimized context: %w", err)
		}
	case "complex":
		// For complex tasks, fall back to orchestration but with reduced scope
		logger.LogProcessStep("🔍 Phase 2: Complex task detected, using orchestration with reduced scope...")
		if err := orchestration.OrchestrateFeature(userIntent, cfg); err != nil {
			logger.LogError(fmt.Errorf("orchestration failed for complex task: %w", err))
			return fmt.Errorf("orchestration failed: %w", err)
		}
		return nil // Orchestration handles the execution
	}

	// Phase 3: Execute with minimal context
	logger.LogProcessStep("⚡ Phase 3: Executing with optimized context...")
	codeGenTokens, err := executeWithMinimalContext(userIntent, contextFiles, cfg, logger)
	if err != nil {
		return err
	}

	// Phase 4: Validate changes with iterative fixing
	logger.LogProcessStep("🔍 Phase 4: Validating changes...")
	validationTokens, err := validateChangesWithIteration(intentAnalysis, userIntent, cfg, logger, tokenUsage)
	if err != nil {
		logger.LogError(fmt.Errorf("validation failed after iterations: %w", err))
		return fmt.Errorf("validation failed: %w", err)
	}

	// Track all token usage
	tokenUsage.CodeGeneration = codeGenTokens
	tokenUsage.Validation = validationTokens
	tokenUsage.Total = tokenUsage.IntentAnalysis + tokenUsage.CodeGeneration + tokenUsage.Validation

	return nil
}

// TokenUsage tracks token consumption throughout agent execution
type TokenUsage struct {
	IntentAnalysis int
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

// analyzeIntentWithMinimalContext analyzes user intent without loading full workspace
func analyzeIntentWithMinimalContext(userIntent string, cfg *config.Config, logger *utils.Logger) (*IntentAnalysis, int, error) {
	// Get basic file listing without full analysis
	files, err := getBasicFileListing(logger)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to get basic file listing for intent analysis: %w", err))
		return nil, 0, fmt.Errorf("failed to get file listing: %w", err)
	}

	prompt := fmt.Sprintf(`Analyze this user intent and classify it for optimal execution:

User Intent: %s

Available Files (basic listing):
%s

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

Only include files in estimated_files that are highly likely to be modified.`,
		userIntent, strings.Join(files, "\n"))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at analyzing programming tasks. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.WorkspaceModel, messages, "", cfg, 60*time.Second)
	if err != nil {
		logger.LogError(fmt.Errorf("LLM failed to analyze intent: %w", err))
		// Use fallback analysis since LLM failed
		logger.Logf("Using fallback heuristic analysis due to LLM failure")
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  inferFiles(userIntent),
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
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  inferFiles(userIntent),
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
		return &IntentAnalysis{
			Category:        inferCategory(userIntent),
			Complexity:      inferComplexity(userIntent),
			EstimatedFiles:  inferFiles(userIntent),
			RequiresContext: true,
		}, totalTokens, nil
	}

	// Debug: log the parsed analysis
	logger.Logf("Parsed analysis - Category: %s, Complexity: %s, Files: %v", analysis.Category, analysis.Complexity, analysis.EstimatedFiles)

	// If LLM didn't provide files, fall back to inference
	if len(analysis.EstimatedFiles) == 0 {
		analysis.EstimatedFiles = inferFiles(userIntent)
		logger.Logf("LLM provided no files, using inferred files: %v", analysis.EstimatedFiles)
	}

	return &analysis, totalTokens, nil
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
	complexWords := []string{"refactor", "architect", "multiple", "system", "design"}
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

func inferFiles(userIntent string) []string {
	intentLower := strings.ToLower(userIntent)
	filesSet := make(map[string]bool) // Use a set to avoid duplicates

	if strings.Contains(intentLower, "main") {
		filesSet["main.go"] = true
	}
	if strings.Contains(intentLower, "agent") {
		filesSet["cmd/agent.go"] = true
	}
	if strings.Contains(intentLower, "test") {
		// Would add test files
	}
	if strings.Contains(intentLower, "helper") || strings.Contains(intentLower, "util") {
		// Look for existing utils files
		filesSet["pkg/utils/utils.go"] = true
	}
	if strings.Contains(intentLower, "validate") || strings.Contains(intentLower, "validation") {
		filesSet["pkg/utils/utils.go"] = true
	}

	// Embedding-related file detection
	if strings.Contains(intentLower, "embedding") || strings.Contains(intentLower, "embeddings") {
		filesSet["pkg/llm/embeddings.go"] = true
		filesSet["pkg/embedding/embedding.go"] = true
	}
	if strings.Contains(intentLower, "jina") || strings.Contains(intentLower, "deepinfra") {
		filesSet["pkg/llm/embeddings.go"] = true
		filesSet["pkg/config/config.go"] = true
	}
	if strings.Contains(intentLower, "api key") || strings.Contains(intentLower, "apikey") {
		filesSet["pkg/config/config.go"] = true
	}

	// Convert set back to slice
	var files []string
	for file := range filesSet {
		files = append(files, file)
	}

	return files
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

// getOptimizedContext uses embeddings or simple heuristics to get minimal relevant context
func getOptimizedContext(userIntent string, estimatedFiles []string, cfg *config.Config, maxFiles int, logger *utils.Logger) ([]string, error) {
	logger.Logf("DEBUG: getOptimizedContext called with %d estimated files, maxFiles=%d", len(estimatedFiles), maxFiles)

	// If we have specific files from intent analysis, use those first
	if len(estimatedFiles) > 0 && len(estimatedFiles) <= maxFiles {
		logger.Logf("DEBUG: Using %d estimated files from intent analysis", len(estimatedFiles))
		return estimatedFiles, nil
	}

	logger.Logf("DEBUG: No usable estimated files, trying embedding search")
	// Force embeddings for agent mode since they provide much better file selection
	// Try embedding search first, fall back to pattern matching if it fails
	return getTopRelevantFiles(userIntent, maxFiles, cfg, logger)
}

// getTopRelevantFiles uses embeddings to find most relevant files
func getTopRelevantFiles(userIntent string, maxFiles int, cfg *config.Config, logger *utils.Logger) ([]string, error) {
	logger.Logf("DEBUG: Starting embedding search for intent: %s", userIntent)

	// Load workspace structure first
	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		logger.LogError(fmt.Errorf("failed to load workspace for embedding search: %w", err))
		logger.Logf("Falling back to pattern matching due to workspace loading error")
		return getRelevantFilesByPattern(userIntent, maxFiles, logger)
	}

	logger.Logf("DEBUG: Loaded workspace with %d files", len(workspaceFile.Files))

	// Use the existing embedding-based file selection
	fullContextFiles, summaryContextFiles, err := workspace.GetFilesForContextUsingEmbeddings(userIntent, workspaceFile, cfg, logger)
	if err != nil {
		logger.LogError(fmt.Errorf("embedding search failed: %w", err))
		logger.Logf("Falling back to pattern matching due to embedding search error")
		return getRelevantFilesByPattern(userIntent, maxFiles, logger)
	}

	logger.Logf("DEBUG: Embedding search returned %d full context files, %d summary files", len(fullContextFiles), len(summaryContextFiles))

	// Combine full context and summary context files, prioritizing full context
	var relevantFiles []string
	relevantFiles = append(relevantFiles, fullContextFiles...)
	relevantFiles = append(relevantFiles, summaryContextFiles...)

	// Limit to maxFiles
	if len(relevantFiles) > maxFiles {
		relevantFiles = relevantFiles[:maxFiles]
	}

	logger.Logf("Embedding search found %d relevant files (limited to %d): %v", len(relevantFiles), maxFiles, relevantFiles)
	return relevantFiles, nil
}

// getRelevantFilesByPattern uses simple pattern matching to find relevant files
func getRelevantFilesByPattern(userIntent string, maxFiles int, logger *utils.Logger) ([]string, error) {
	files, err := getBasicFileListing(logger)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to get basic file listing for pattern matching: %w", err))
		return nil, err
	}

	// Score files based on simple heuristics
	type fileScore struct {
		path  string
		score int
	}

	var scoredFiles []fileScore
	intentLower := strings.ToLower(userIntent)

	for _, file := range files {
		score := 0
		fileName := strings.ToLower(filepath.Base(file))

		// High priority for main function references
		if strings.Contains(intentLower, "main function") || strings.Contains(intentLower, "main()") {
			if fileName == "main.go" || strings.Contains(fileName, "main") {
				score += 50
			}
			// Also check if file contains main function
			if content, err := os.ReadFile(file); err == nil {
				if strings.Contains(string(content), "func main()") {
					score += 40
				}
			} else {
				logger.Logf("Could not read file %s for content check: %v", file, err)
			}
		}

		// Score based on keywords in intent
		keywords := []string{"main", "error", "test", "config", "handler", "service", "model", "util", "embedding", "embeddings"}
		for _, keyword := range keywords {
			if strings.Contains(intentLower, keyword) && strings.Contains(fileName, keyword) {
				score += 10
			}
		}

		// Special scoring for embedding-related terms
		if strings.Contains(intentLower, "embedding") || strings.Contains(intentLower, "jina") || strings.Contains(intentLower, "deepinfra") {
			if strings.Contains(file, "embedding") || strings.Contains(file, "llm") {
				score += 20
			}
		}

		// Special scoring for API provider changes (jina/deepinfra)
		if strings.Contains(intentLower, "jina") || strings.Contains(intentLower, "deepinfra") {
			if strings.Contains(file, "llm") || strings.Contains(file, "config") || strings.Contains(file, "api") {
				score += 15
			}
		}

		// Score based on file type relevance
		if strings.Contains(intentLower, "test") && strings.Contains(fileName, "test") {
			score += 15
		}
		if strings.Contains(intentLower, "comment") && fileName == "main.go" {
			score += 20
		}

		if score > 0 {
			scoredFiles = append(scoredFiles, fileScore{file, score})
		}
	}

	// If no scored files and this looks like a main function task, include main.go
	if len(scoredFiles) == 0 && strings.Contains(intentLower, "main") {
		for _, file := range files {
			if filepath.Base(file) == "main.go" {
				scoredFiles = append(scoredFiles, fileScore{file, 30})
				break
			}
		}
	}

	// Sort by score and return top files
	sort.Slice(scoredFiles, func(i, j int) bool {
		return scoredFiles[i].score > scoredFiles[j].score
	})

	var result []string
	for i, sf := range scoredFiles {
		if i >= maxFiles {
			break
		}
		result = append(result, sf.path)
	}

	return result, nil
}

// executeWithMinimalContext executes the task with only the specified context files
func executeWithMinimalContext(userIntent string, contextFiles []string, cfg *config.Config, logger *utils.Logger) (int, error) {
	logger.LogProcessStep(fmt.Sprintf("Executing with %d context files: %s", len(contextFiles), strings.Join(contextFiles, ", ")))

	// Build enhanced instructions that include context and clear guidance
	enhancedInstructions := buildEnhancedInstructions(userIntent, contextFiles, logger)

	// Estimate tokens for the enhanced instructions
	tokenEstimate := utils.EstimateTokens(enhancedInstructions)

	// Use the editor package for simple code generation
	_, err := editor.ProcessCodeGeneration("", enhancedInstructions, cfg, "")
	if err != nil {
		logger.LogError(fmt.Errorf("code generation failed during minimal context execution: %w", err))
		return tokenEstimate, err
	}
	return tokenEstimate, nil
}

// buildEnhancedInstructions creates enhanced instructions with context and guidance
func buildEnhancedInstructions(userIntent string, contextFiles []string, logger *utils.Logger) string {
	var instructions strings.Builder

	// Start with the main intent
	instructions.WriteString(fmt.Sprintf("Task: %s\n\n", userIntent))

	// Add specific guidance for code editing
	instructions.WriteString(`IMPORTANT EDITING GUIDELINES:
- Make TARGETED, PRECISE edits - do not rewrite entire files
- For adding flags to CLI commands, follow the existing patterns in the codebase
- When adding a new flag variable, also add the corresponding flag registration in init()
- Always preserve existing code structure and formatting
- If adding flag support, look at how other commands implement --skip-prompt

`)

	// Add context if available
	if len(contextFiles) > 0 {
		instructions.WriteString("RELEVANT CODE CONTEXT:\n")
		context := buildMinimalContext(contextFiles, logger)
		instructions.WriteString(context)
		instructions.WriteString("\n")
	}

	// Add examples of existing patterns if the intent involves flags
	if strings.Contains(strings.ToLower(userIntent), "flag") || strings.Contains(strings.ToLower(userIntent), "skip") {
		instructions.WriteString(addFlagPatternExamples(logger))
	}

	return instructions.String()
}

// addFlagPatternExamples adds examples of how flags are implemented in other commands
func addFlagPatternExamples(logger *utils.Logger) string {
	var examples strings.Builder

	examples.WriteString("EXISTING FLAG IMPLEMENTATION PATTERNS:\n")
	examples.WriteString("Example from code.go:\n")
	examples.WriteString(`var (
	skipPrompt     bool
)

func init() {
	codeCmd.Flags().BoolVar(&skipPrompt, "skip-prompt", false, "Skip user prompt for applying changes")
}

// In the command function:
cfg, err := config.LoadOrInitConfig(skipPrompt)
cfg.SkipPrompt = skipPrompt

`)

	examples.WriteString("Follow this exact pattern for implementing skip-prompt flag.\n\n")

	return examples.String()
}

// buildMinimalContext creates a minimal context string from the specified files
func buildMinimalContext(contextFiles []string, logger *utils.Logger) string {
	if len(contextFiles) == 0 {
		return ""
	}

	var context strings.Builder
	context.WriteString("Relevant Files:\n")

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

		// Use LLM to determine appropriate validation strategy for this project
		validationStrategy, strategyTokens, err := determineValidationStrategy(intentAnalysis, cfg, logger)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to determine validation strategy: %w", err))
			// Fall back to basic validation
			validationStrategy = getBasicValidationStrategy(intentAnalysis, logger)
		}
		totalValidationTokens += strategyTokens

		var validationResults []string
		var hasFailures bool

		// Run validation steps
		for _, step := range validationStrategy.Steps {
			logger.LogProcessStep(fmt.Sprintf("Running validation: %s", step.Description))

			result, err := runValidationStep(step, logger)
			if err != nil {
				validationResults = append(validationResults, fmt.Sprintf("❌ %s: %v", step.Description, err))
				logger.Logf("Validation step failed: %s - %v", step.Description, err)
				hasFailures = true
			} else {
				validationResults = append(validationResults, fmt.Sprintf("✅ %s: %s", step.Description, result))
				logger.Logf("Validation step passed: %s", step.Description)
			}
		}

		// If no failures, we're done
		if !hasFailures {
			logger.LogProcessStep("✅ All validation steps passed!")
			return totalValidationTokens, nil
		}

		// If this is the last iteration, don't try to fix
		if iteration == maxIterations {
			logger.LogProcessStep("❌ Max iterations reached, validation still failing")
			analysisTokens, _ := analyzeValidationResults(validationResults, intentAnalysis, validationStrategy, cfg, logger)
			totalValidationTokens += analysisTokens
			return totalValidationTokens, fmt.Errorf("validation failed after %d iterations", maxIterations)
		}

		// Try to fix the issues automatically
		logger.LogProcessStep(fmt.Sprintf("🔧 Attempting to fix validation issues (iteration %d)", iteration))
		fixTokens, err := fixValidationIssues(validationResults, originalIntent, intentAnalysis, cfg, logger)
		totalValidationTokens += fixTokens

		if err != nil {
			logger.LogError(fmt.Errorf("failed to auto-fix validation issues: %w", err))
			// Continue to next iteration anyway
		} else {
			logger.LogProcessStep("✅ Applied potential fixes, re-validating...")
		}
	}

	return totalValidationTokens, fmt.Errorf("validation failed after %d iterations", maxIterations)
}

// fixValidationIssues attempts to automatically fix common validation failures
func fixValidationIssues(validationResults []string, originalIntent string, intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (int, error) {
	// Analyze the failures to understand what needs fixing
	failureAnalysis := analyzeFailures(validationResults, logger)

	if len(failureAnalysis) == 0 {
		logger.Logf("No fixable issues identified")
		return 0, nil
	}

	// Create a fix prompt based on the specific failures
	fixPrompt := buildFixPrompt(failureAnalysis, originalIntent, validationResults)

	logger.LogProcessStep(fmt.Sprintf("🔧 Applying fixes for: %s", strings.Join(failureAnalysis, ", ")))

	// Use the editor to apply fixes
	_, err := editor.ProcessCodeGeneration("", fixPrompt, cfg, "")
	if err != nil {
		return 0, fmt.Errorf("failed to apply fixes: %w", err)
	}

	// Estimate tokens used for the fix
	tokens := utils.EstimateTokens(fixPrompt)
	return tokens, nil
}

// analyzeFailures categorizes validation failures into fixable types
func analyzeFailures(validationResults []string, logger *utils.Logger) []string {
	var issues []string

	for _, result := range validationResults {
		if !strings.HasPrefix(result, "❌") {
			continue
		}

		resultLower := strings.ToLower(result)

		// Check for common build issues
		if strings.Contains(resultLower, "redeclared") || strings.Contains(resultLower, "already declared") {
			issues = append(issues, "variable_redeclaration")
		}
		if strings.Contains(resultLower, "undefined:") {
			issues = append(issues, "undefined_variable")
		}
		if strings.Contains(resultLower, "cannot find") || strings.Contains(resultLower, "does not exist") {
			issues = append(issues, "missing_import")
		}
		if strings.Contains(resultLower, "syntax error") {
			issues = append(issues, "syntax_error")
		}
		if strings.Contains(resultLower, "unused") {
			issues = append(issues, "unused_variable")
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	var unique []string
	for _, issue := range issues {
		if !seen[issue] {
			seen[issue] = true
			unique = append(unique, issue)
		}
	}

	logger.Logf("Identified fixable issues: %v", unique)
	return unique
}

// buildFixPrompt creates a focused prompt to fix specific validation issues
func buildFixPrompt(issues []string, originalIntent string, validationResults []string) string {
	var prompt strings.Builder

	prompt.WriteString("VALIDATION ISSUE FIX REQUIRED:\n\n")
	prompt.WriteString(fmt.Sprintf("Original task: %s\n\n", originalIntent))

	prompt.WriteString("VALIDATION FAILURES:\n")
	for _, result := range validationResults {
		if strings.HasPrefix(result, "❌") {
			prompt.WriteString(fmt.Sprintf("- %s\n", result))
		}
	}
	prompt.WriteString("\n")

	prompt.WriteString("ISSUES TO FIX:\n")
	for _, issue := range issues {
		switch issue {
		case "variable_redeclaration":
			prompt.WriteString("- VARIABLE REDECLARATION: Use command-specific variable names (e.g., agentSkipPrompt instead of skipPrompt)\n")
		case "undefined_variable":
			prompt.WriteString("- UNDEFINED VARIABLE: Ensure all variables are properly declared before use\n")
		case "missing_import":
			prompt.WriteString("- MISSING IMPORT: Add required import statements\n")
		case "syntax_error":
			prompt.WriteString("- SYNTAX ERROR: Fix Go syntax issues\n")
		case "unused_variable":
			prompt.WriteString("- UNUSED VARIABLE: Remove or use declared variables\n")
		}
	}

	prompt.WriteString("\nFIXING GUIDELINES:\n")
	prompt.WriteString("- Make MINIMAL, TARGETED fixes to resolve the specific build errors\n")
	prompt.WriteString("- For variable naming conflicts, use command-specific prefixes (e.g., agentSkipPrompt, codeSkipPrompt)\n")
	prompt.WriteString("- Preserve all existing functionality\n")
	prompt.WriteString("- Only fix the reported errors, don't make unnecessary changes\n")
	prompt.WriteString("- Follow Go naming conventions\n\n")

	prompt.WriteString("Please fix ONLY the validation errors reported above.\n")

	return prompt.String()
}

// validateChanges validates the changes made by running appropriate build/test commands
func validateChanges(intentAnalysis *IntentAnalysis, cfg *config.Config, logger *utils.Logger) (int, error) {
	// Use LLM to determine appropriate validation strategy for this project
	validationStrategy, strategyTokens, err := determineValidationStrategy(intentAnalysis, cfg, logger)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to determine validation strategy: %w", err))
		// Fall back to basic validation
		validationStrategy = getBasicValidationStrategy(intentAnalysis, logger)
	}

	var totalValidationTokens int = strategyTokens
	var validationResults []string

	for _, step := range validationStrategy.Steps {
		logger.LogProcessStep(fmt.Sprintf("Running validation: %s", step.Description))

		result, err := runValidationStep(step, logger)
		if err != nil {
			validationResults = append(validationResults, fmt.Sprintf("❌ %s: %v", step.Description, err))
			logger.Logf("Validation step failed: %s - %v", step.Description, err)
		} else {
			validationResults = append(validationResults, fmt.Sprintf("✅ %s: %s", step.Description, result))
			logger.Logf("Validation step passed: %s", step.Description)
		}
	}

	// Use LLM to analyze validation results and suggest fixes if needed
	if len(validationResults) > 0 {
		analysisTokens, err := analyzeValidationResults(validationResults, intentAnalysis, validationStrategy, cfg, logger)
		totalValidationTokens += analysisTokens
		if err != nil {
			logger.LogError(fmt.Errorf("failed to analyze validation results: %w", err))
		}
	}

	return totalValidationTokens, nil
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
- For Go projects: typically need "go build" and "go vet" at minimum
- For Python: "python -m py_compile" for syntax, "pytest" for tests
- For Node.js: "npm run build", "npm test", "npm run lint"
- Always prioritize essential checks (syntax, build) over optional ones (tests, linting)
- Consider the change type: docs changes need minimal validation, code changes need thorough validation`,
		strings.Join(projectInfo.AvailableFiles, ", "),
		intentAnalysis.Category,
		intentAnalysis.Complexity,
		strings.Join(intentAnalysis.EstimatedFiles, ", "))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert at DevOps and project validation. Respond only with valid JSON."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.WorkspaceModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		return nil, 0, fmt.Errorf("LLM failed to determine validation strategy: %w", err)
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
			{Type: "build", Command: "go build ./...", Description: "Build Go project", Required: true},
			{Type: "lint", Command: "go vet ./...", Description: "Go static analysis", Required: false},
		}
	} else if hasFile("package.json") {
		strategy.ProjectType = "node"
		strategy.Steps = []ValidationStep{
			{Type: "syntax", Command: "node --check *.js", Description: "JavaScript syntax check", Required: true},
		}
	} else if hasFile("requirements.txt") || hasFile("pyproject.toml") {
		strategy.ProjectType = "python"
		strategy.Steps = []ValidationStep{
			{Type: "syntax", Command: "python -m py_compile *.py", Description: "Python syntax check", Required: true},
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

// analyzeValidationResults uses LLM to analyze validation results and suggest fixes
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

	prompt := fmt.Sprintf(`The following validation steps were run after making code changes:

Original Intent: %s
Change Category: %s
Project Type: %s
Validation Context: %s

Validation Results:
%s

Please analyze these results and provide:
1. Assessment of the failures - are they related to the recent changes?
2. Whether the failures are critical or can be ignored (e.g., pre-existing issues)
3. Specific fixes needed (if any)
4. Whether the changes should be rolled back

Consider that some failures might be pre-existing issues not related to the current changes.
Keep your response concise and actionable.`,
		intentAnalysis.Category,
		intentAnalysis.Category,
		validationStrategy.ProjectType,
		validationStrategy.Context,
		strings.Join(validationResults, "\n"))

	messages := []prompts.Message{
		{Role: "system", Content: "You are an expert developer analyzing build/test failures. Focus on whether failures are related to recent changes."},
		{Role: "user", Content: prompt},
	}

	_, response, err := llm.GetLLMResponse(cfg.WorkspaceModel, messages, "", cfg, 30*time.Second)
	if err != nil {
		return 0, fmt.Errorf("failed to get LLM analysis of validation results: %w", err)
	}

	// Log the analysis
	logger.LogProcessStep("🔍 Validation Analysis:")
	logger.Logf("%s", response)

	// Estimate tokens used
	tokens := utils.EstimateTokens(prompt + response)
	return tokens, nil
}

// printTokenUsageSummary prints a summary of token usage for the agent execution
func printTokenUsageSummary(tokenUsage *TokenUsage, duration time.Duration) {
	fmt.Printf("\n💰 Token Usage Summary:\n")
	fmt.Printf("├─ Intent Analysis: %d tokens\n", tokenUsage.IntentAnalysis)
	fmt.Printf("├─ Code Generation: %d tokens\n", tokenUsage.CodeGeneration)
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
