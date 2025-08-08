package cmd

import (
	"encoding/json"
	"fmt"
	"os"
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
	cfg, err := config.LoadOrInitConfig(false)
	if err != nil {
		logger := utils.GetLogger(false) // Get a logger even if config fails
		logger.LogError(fmt.Errorf("failed to load config: %w", err))
		return fmt.Errorf("failed to load config: %w", err)
	}

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

	// Track code generation tokens
	tokenUsage.CodeGeneration = codeGenTokens
	tokenUsage.Total = tokenUsage.IntentAnalysis + tokenUsage.CodeGeneration

	return nil
}

// TokenUsage tracks token consumption throughout agent execution
type TokenUsage struct {
	IntentAnalysis int
	CodeGeneration int
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

	// Build enhanced instructions that include context
	enhancedInstructions := userIntent
	if len(contextFiles) > 0 {
		context := buildMinimalContext(contextFiles, logger)
		enhancedInstructions = fmt.Sprintf("%s\n\nRelevant context:\n%s", userIntent, context)
	}

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

// printTokenUsageSummary prints a summary of token usage for the agent execution
func printTokenUsageSummary(tokenUsage *TokenUsage, duration time.Duration) {
	fmt.Printf("\n💰 Token Usage Summary:\n")
	fmt.Printf("├─ Intent Analysis: %d tokens\n", tokenUsage.IntentAnalysis)
	fmt.Printf("├─ Code Generation: %d tokens\n", tokenUsage.CodeGeneration)
	fmt.Printf("└─ Total Usage: %d tokens\n", tokenUsage.Total)

	// Estimate cost (rough approximation for popular models)
	// This is a rough estimate - actual costs vary by provider and model
	estimatedCostCents := float64(tokenUsage.Total) * 0.002 // ~$0.002 per 1k tokens for many models
	if estimatedCostCents < 0.01 {
		fmt.Printf("💵 Estimated Cost: <$0.01\n")
	} else {
		fmt.Printf("💵 Estimated Cost: ~$%.3f\n", estimatedCostCents)
	}

	// Performance metrics
	tokensPerSecond := float64(tokenUsage.Total) / duration.Seconds()
	fmt.Printf("⚡ Performance: %.1f tokens/second\n", tokensPerSecond)
}
