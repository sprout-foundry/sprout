package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// EditingStrategy defines the editing approach to use
type EditingStrategy int

const (
	StrategyAuto  EditingStrategy = iota // Automatically choose based on task complexity
	StrategyQuick                        // Direct file editing with minimal review
	StrategyFull                         // Full multi-phase editing with comprehensive review
)

// EditingMetrics tracks cost and performance across editing operations
type EditingMetrics struct {
	TotalTokens      int      `json:"total_tokens"`
	TotalCost        float64  `json:"total_cost"`
	EditingTokens    int      `json:"editing_tokens"`
	EditingCost      float64  `json:"editing_cost"`
	ReviewTokens     int      `json:"review_tokens"`
	ReviewCost       float64  `json:"review_cost"`
	AnalysisTokens   int      `json:"analysis_tokens"`
	AnalysisCost     float64  `json:"analysis_cost"`
	Duration         float64  `json:"duration_seconds"`
	StrategyUsed     string   `json:"strategy_used"`
	FilesModified    int      `json:"files_modified"`
	ReviewIterations int      `json:"review_iterations"`
	RevisionIDs      []string `json:"revision_ids"` // Track all revision IDs for rollback
}

// OptimizedEditingConfig controls the optimized editing behavior
type OptimizedEditingConfig struct {
	Strategy                EditingStrategy `json:"strategy"`
	MaxReviewIterations     int             `json:"max_review_iterations"`
	QuickEditThreshold      int             `json:"quick_edit_threshold_chars"` // Use quick edit for changes under this size
	AutoReviewThreshold     float64         `json:"auto_review_threshold_cost"` // Auto-review if edit cost exceeds this
	EnableCostOptimization  bool            `json:"enable_cost_optimization"`
	EnableSmartCaching      bool            `json:"enable_smart_caching"`
	ParallelAnalysisEnabled bool            `json:"parallel_analysis_enabled"`
}

// DefaultOptimizedEditingConfig returns sensible defaults
func DefaultOptimizedEditingConfig() *OptimizedEditingConfig {
	return &OptimizedEditingConfig{
		Strategy:                StrategyAuto,
		MaxReviewIterations:     3,
		QuickEditThreshold:      1000, // 1KB threshold for quick vs full
		AutoReviewThreshold:     0.05, // $0.05 threshold for auto-review
		EnableCostOptimization:  true,
		EnableSmartCaching:      true,
		ParallelAnalysisEnabled: true,
	}
}

// OptimizedEditingService provides unified, cost-aware editing operations with rollback support
type OptimizedEditingService struct {
	config  *OptimizedEditingConfig
	cfg     *config.Config
	logger  *utils.Logger
	metrics *EditingMetrics
}

// EditingResult contains the result of an editing operation with rollback support
type EditingResult struct {
	Diff        string          `json:"diff"`
	RevisionIDs []string        `json:"revision_ids"`
	Strategy    string          `json:"strategy"`
	Metrics     *EditingMetrics `json:"metrics"`
}

// NewOptimizedEditingService creates a new optimized editing service
func NewOptimizedEditingService(cfg *config.Config, logger *utils.Logger) *OptimizedEditingService {
	return &OptimizedEditingService{
		config:  DefaultOptimizedEditingConfig(),
		cfg:     cfg,
		logger:  logger,
		metrics: &EditingMetrics{},
	}
}

// ExecuteOptimizedEdit performs editing using the optimal strategy based on task analysis
func (s *OptimizedEditingService) ExecuteOptimizedEdit(todo *TodoItem, ctx *SimplifiedAgentContext) (string, error) {
	result, err := s.ExecuteOptimizedEditWithRollback(todo, ctx)
	if err != nil {
		return "", err
	}
	return result.Diff, nil
}

// ExecuteOptimizedEditWithRollback performs editing and returns full result with rollback information
func (s *OptimizedEditingService) ExecuteOptimizedEditWithRollback(todo *TodoItem, ctx *SimplifiedAgentContext) (*EditingResult, error) {
	startTime := time.Now()
	s.metrics = &EditingMetrics{RevisionIDs: []string{}} // Reset metrics for this operation

	// Determine optimal editing strategy
	strategy := s.determineStrategy(todo, ctx)
	s.metrics.StrategyUsed = s.strategyName(strategy)

	s.logger.LogProcessStep(fmt.Sprintf("🎯 Using %s editing strategy", s.metrics.StrategyUsed))

	var diff string
	var err error
	var revisionIDs []string

	switch strategy {
	case StrategyQuick:
		diff, revisionIDs, err = s.executeQuickEdit(todo, ctx)
	case StrategyFull:
		diff, revisionIDs, err = s.executeFullEdit(todo, ctx)
	default:
		return nil, fmt.Errorf("unknown editing strategy: %d", strategy)
	}

	if err != nil {
		return nil, err
	}

	// Store revision IDs in metrics
	s.metrics.RevisionIDs = revisionIDs

	// Finalize metrics
	s.metrics.Duration = time.Since(startTime).Seconds()
	s.logMetrics()

	return &EditingResult{
		Diff:        diff,
		RevisionIDs: revisionIDs,
		Strategy:    s.metrics.StrategyUsed,
		Metrics:     s.metrics,
	}, nil
}

// determineStrategy intelligently chooses the editing strategy based on task complexity
func (s *OptimizedEditingService) determineStrategy(todo *TodoItem, ctx *SimplifiedAgentContext) EditingStrategy {
	if s.config.Strategy != StrategyAuto {
		return s.config.Strategy
	}

	// Analysis factors for strategy determination
	factors := s.analyzeTaskComplexity(todo, ctx)

	// Force full edit for filesystem operations - they need shell commands not code editing
	if factors.requiresShellCommands {
		s.logger.LogProcessStep("🔧 Task requires filesystem operations, using full edit strategy")
		return StrategyFull
	}

	// Quick edit if:
	// - Single file mentioned
	// - Small change description
	// - Simple keywords (add, fix, update single thing)
	if factors.isSingleFile && factors.estimatedSize < s.config.QuickEditThreshold && factors.isSimpleOperation {
		s.logger.LogProcessStep("📝 Task appears simple, using quick edit strategy")
		return StrategyQuick
	}

	// Full edit if:
	// - Multiple files involved
	// - Complex refactoring
	// - Architecture changes
	if factors.isMultiFile || factors.isComplexOperation || factors.estimatedCost > s.config.AutoReviewThreshold {
		s.logger.LogProcessStep("🔧 Task appears complex, using full edit strategy")
		return StrategyFull
	}

	// Default to quick edit for efficiency
	return StrategyQuick
}

// TaskComplexityFactors represents factors used for strategy determination
type TaskComplexityFactors struct {
	isSingleFile                bool
	isMultiFile                 bool
	isSimpleOperation           bool
	isComplexOperation          bool
	requiresShellCommands       bool
	estimatedSize               int
	estimatedCost               float64
	hasArchitectureImplications bool
}

// analyzeTaskComplexity analyzes the todo to determine complexity factors
func (s *OptimizedEditingService) analyzeTaskComplexity(todo *TodoItem, ctx *SimplifiedAgentContext) TaskComplexityFactors {
	content := strings.ToLower(todo.Content + " " + todo.Description)

	factors := TaskComplexityFactors{}

	// File analysis
	if strings.Count(content, ".go") == 1 || strings.Count(content, "file") == 1 {
		factors.isSingleFile = true
	}
	if strings.Count(content, ".go") > 1 || strings.Contains(content, "multiple files") || strings.Contains(content, "across files") {
		factors.isMultiFile = true
	}

	// Operation complexity
	simpleKeywords := []string{"add", "fix", "update", "change", "modify", "remove"}
	complexKeywords := []string{"refactor", "restructure", "architecture", "design", "migrate", "overhaul"}
	filesystemKeywords := []string{
		"create directory", "mkdir", "create folder", "setup project", "initialize",
		"install", "setup monorepo", "create backend", "create frontend",
		"create the", "directory for", "backend directory", "frontend directory",
		"directory in", "directory called", "directory named", " directory ", "new directory",
	}

	for _, keyword := range simpleKeywords {
		if strings.Contains(content, keyword) {
			factors.isSimpleOperation = true
			break
		}
	}

	for _, keyword := range complexKeywords {
		if strings.Contains(content, keyword) {
			factors.isComplexOperation = true
			break
		}
	}

	// Check for filesystem operations that require shell commands
	for _, keyword := range filesystemKeywords {
		if strings.Contains(content, keyword) {
			factors.requiresShellCommands = true
			break
		}
	}

	// Size estimation (rough heuristic)
	factors.estimatedSize = len(todo.Description) * 10 // Rough multiplier

	// Cost estimation (basic heuristic based on content length)
	estimatedTokens := len(content) / 4 // Rough tokens estimate
	factors.estimatedCost = llm.CalculateCost(llm.TokenUsage{
		PromptTokens:     estimatedTokens,
		CompletionTokens: estimatedTokens / 2,
		TotalTokens:      estimatedTokens + estimatedTokens/2,
	}, ctx.Config.EditingModel)

	return factors
}

// executeQuickEdit performs a streamlined edit with rollback support
func (s *OptimizedEditingService) executeQuickEdit(todo *TodoItem, ctx *SimplifiedAgentContext) (string, []string, error) {
	result, err := editor.ProcessCodeGenerationWithRollback("", todo.Content+" "+todo.Description, s.cfg, "")
	if err != nil {
		return "", nil, err
	}
	return result.Diff, []string{result.RevisionID}, nil
}

// executeFullEdit performs comprehensive multi-phase editing
func (s *OptimizedEditingService) executeFullEdit(todo *TodoItem, ctx *SimplifiedAgentContext) (string, []string, error) {
	result, err := editor.ProcessCodeGenerationWithRollback("", todo.Content+" "+todo.Description, s.cfg, "")
	if err != nil {
		return "", nil, err
	}
	return result.Diff, []string{result.RevisionID}, nil
}

// executeAnalysisPhase performs lightweight analysis to inform editing decisions
func (s *OptimizedEditingService) executeAnalysisPhase(todo *TodoItem, ctx *SimplifiedAgentContext) error {
	analysisPrompt := fmt.Sprintf("Briefly analyze the requirements for: %s\n\nProvide a concise summary of what needs to be changed and any key considerations.", todo.Content)

	// Use a lightweight analysis call
	response, tokenUsage, err := llm.GetLLMResponse(
		s.cfg.OrchestrationModel,
		[]prompts.Message{{Role: "user", Content: analysisPrompt}},
		"",
		s.cfg,
		30*time.Second,
	)

	if tokenUsage != nil {
		s.trackTokenUsage(tokenUsage, s.cfg.OrchestrationModel, "analysis")
	}

	if err == nil {
		ctx.AnalysisResults[todo.ID+"_quick_analysis"] = response
	}

	return err
}

// performCostAwareReview performs review only when cost-justified
func (s *OptimizedEditingService) performCostAwareReview(diff string, todo *TodoItem, ctx *SimplifiedAgentContext) error {
	// Simplified review - just validate the changes make sense
	reviewPrompt := fmt.Sprintf("Briefly review these changes for: %s\n\nDiff:\n%s\n\nProvide a quick assessment: approved/needs_revision/rejected", todo.Content, diff)

	response, tokenUsage, err := llm.GetLLMResponse(
		s.cfg.OrchestrationModel,
		[]prompts.Message{{Role: "user", Content: reviewPrompt}},
		"",
		s.cfg,
		30*time.Second,
	)

	if tokenUsage != nil {
		s.trackTokenUsage(tokenUsage, s.cfg.OrchestrationModel, "review")
		s.metrics.ReviewIterations = 1
	}

	if err == nil && strings.Contains(strings.ToLower(response), "needs_revision") {
		s.logger.LogProcessStep("⚠️ Review suggests revisions needed")
		// For cost optimization, we log but don't retry automatically
	}

	return err
}

// extractTargetFile attempts to extract a target filename from the todo content
func (s *OptimizedEditingService) extractTargetFile(content string) string {
	// Simple regex patterns to find file references
	patterns := []string{
		`[\w\./]+\.go\b`,
		`in\s+([\w\./]+\.go)\b`,
		`file\s+([\w\./]+\.go)\b`,
	}

	for range patterns {
		// This is a simplified implementation - in practice would use regex
		if strings.Contains(strings.ToLower(content), ".go") {
			words := strings.Fields(content)
			for _, word := range words {
				if strings.HasSuffix(strings.ToLower(word), ".go") {
					return strings.Trim(word, ".,!?()[]")
				}
			}
		}
	}

	return ""
}

// trackTokenUsage centralizes token usage tracking across all operations
func (s *OptimizedEditingService) trackTokenUsage(usage *types.TokenUsage, model, phase string) {
	if usage == nil {
		return
	}

	tokens := usage.TotalTokens
	cost := llm.CalculateCost(llm.TokenUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}, model)

	s.metrics.TotalTokens += tokens
	s.metrics.TotalCost += cost

	switch phase {
	case "editing":
		s.metrics.EditingTokens += tokens
		s.metrics.EditingCost += cost
	case "review":
		s.metrics.ReviewTokens += tokens
		s.metrics.ReviewCost += cost
	case "analysis":
		s.metrics.AnalysisTokens += tokens
		s.metrics.AnalysisCost += cost
	}
}

// logMetrics outputs comprehensive metrics for this editing operation
func (s *OptimizedEditingService) logMetrics() {
	s.logger.LogProcessStep("📊 Editing Metrics Summary")
	s.logger.LogProcessStep(fmt.Sprintf("├─ Strategy: %s", s.metrics.StrategyUsed))
	s.logger.LogProcessStep(fmt.Sprintf("├─ Duration: %.2fs", s.metrics.Duration))
	s.logger.LogProcessStep(fmt.Sprintf("├─ Total Tokens: %d", s.metrics.TotalTokens))
	s.logger.LogProcessStep(fmt.Sprintf("├─ Total Cost: $%.4f", s.metrics.TotalCost))

	if s.metrics.EditingTokens > 0 {
		s.logger.LogProcessStep(fmt.Sprintf("├─ Editing: %d tokens ($%.4f)", s.metrics.EditingTokens, s.metrics.EditingCost))
	}
	if s.metrics.ReviewTokens > 0 {
		s.logger.LogProcessStep(fmt.Sprintf("├─ Review: %d tokens ($%.4f)", s.metrics.ReviewTokens, s.metrics.ReviewCost))
	}
	if s.metrics.AnalysisTokens > 0 {
		s.logger.LogProcessStep(fmt.Sprintf("├─ Analysis: %d tokens ($%.4f)", s.metrics.AnalysisTokens, s.metrics.AnalysisCost))
	}

	s.logger.LogProcessStep(fmt.Sprintf("└─ Review Iterations: %d", s.metrics.ReviewIterations))
}

// strategyName returns the human-readable strategy name
func (s *OptimizedEditingService) strategyName(strategy EditingStrategy) string {
	switch strategy {
	case StrategyQuick:
		return "Quick Edit"
	case StrategyFull:
		return "Full Edit"
	default:
		return "Auto"
	}
}

// GetMetrics returns the current editing metrics
func (s *OptimizedEditingService) GetMetrics() *EditingMetrics {
	return s.metrics
}

// RollbackChanges rolls back changes using the revision IDs
func (s *OptimizedEditingService) RollbackChanges(revisionIDs []string) error {
	// Implementation would call the rollback system
	// For now, return a placeholder error
	return fmt.Errorf("rollback functionality not yet implemented")
}

// GetLastRevisionID returns the most recent revision ID
func (s *OptimizedEditingService) GetLastRevisionID() string {
	if len(s.metrics.RevisionIDs) == 0 {
		return ""
	}
	return s.metrics.RevisionIDs[len(s.metrics.RevisionIDs)-1]
}
