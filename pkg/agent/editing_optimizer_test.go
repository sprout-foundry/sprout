package agent

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

func TestTaskComplexityAnalysis(t *testing.T) {
	cfg := &config.Config{
		EditingModel: "gpt-4",
	}
	logger := &utils.Logger{}
	service := NewOptimizedEditingService(cfg, logger)

	tests := []struct {
		name          string
		todo          *TodoItem
		expectQuick   bool
		expectComplex bool
	}{
		{
			name: "Simple single file change",
			todo: &TodoItem{
				Content:     "Fix typo in main.go",
				Description: "Update the comment on line 10",
			},
			expectQuick: true,
		},
		{
			name: "Complex refactoring",
			todo: &TodoItem{
				Content:     "Refactor the authentication system",
				Description: "Restructure auth across multiple files and update architecture",
			},
			expectComplex: true,
		},
		{
			name: "Multi-file update",
			todo: &TodoItem{
				Content:     "Update user.go, auth.go, and middleware.go",
				Description: "Add new security features across files",
			},
			expectComplex: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &SimplifiedAgentContext{
				Config: cfg,
			}

			factors := service.analyzeTaskComplexity(tt.todo, ctx)

			if tt.expectQuick && (!factors.isSingleFile || factors.isComplexOperation) {
				t.Errorf("Expected simple task, got complex analysis")
			}

			if tt.expectComplex && (factors.isSingleFile && !factors.isMultiFile && !factors.isComplexOperation) {
				t.Errorf("Expected complex task, got simple analysis")
			}
		})
	}
}

func TestStrategyDetermination(t *testing.T) {
	cfg := &config.Config{
		EditingModel: "gpt-4",
	}
	logger := &utils.Logger{}
	service := NewOptimizedEditingService(cfg, logger)

	tests := []struct {
		name             string
		todo             *TodoItem
		expectedStrategy EditingStrategy
	}{
		{
			name: "Quick edit task",
			todo: &TodoItem{
				Content:     "Add comment to main.go",
				Description: "Add a brief comment explaining the function",
			},
			expectedStrategy: StrategyQuick,
		},
		{
			name: "Complex refactoring task",
			todo: &TodoItem{
				Content:     "Refactor database connection handling",
				Description: "Restructure database connections across the entire application architecture",
			},
			expectedStrategy: StrategyFull,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &SimplifiedAgentContext{
				Config: cfg,
			}

			strategy := service.determineStrategy(tt.todo, ctx)

			if strategy != tt.expectedStrategy {
				t.Errorf("Expected strategy %v, got %v", tt.expectedStrategy, strategy)
			}
		})
	}
}

func TestMetricsTracking(t *testing.T) {
	cfg := &config.Config{
		EditingModel: "gpt-4",
	}
	logger := &utils.Logger{}
	service := NewOptimizedEditingService(cfg, logger)

	// Test token usage tracking
	usage := &types.TokenUsage{
		PromptTokens:     100,
		CompletionTokens: 200,
		TotalTokens:      300,
	}

	service.trackTokenUsage(usage, "gpt-4", "editing")

	metrics := service.GetMetrics()
	if metrics.TotalTokens != 300 {
		t.Errorf("Expected 300 total tokens, got %d", metrics.TotalTokens)
	}

	if metrics.EditingTokens != 300 {
		t.Errorf("Expected 300 editing tokens, got %d", metrics.EditingTokens)
	}

	if metrics.TotalCost == 0 {
		t.Errorf("Expected non-zero cost, got %f", metrics.TotalCost)
	}
}

func TestExtractTargetFile(t *testing.T) {
	cfg := &config.Config{}
	logger := &utils.Logger{}
	service := NewOptimizedEditingService(cfg, logger)

	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "Direct file mention",
			content:  "Update main.go with new imports",
			expected: "main.go",
		},
		{
			name:     "File in sentence",
			content:  "In the user.go file, add validation",
			expected: "user.go",
		},
		{
			name:     "No file mentioned",
			content:  "Add user validation",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.extractTargetFile(tt.content)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestRollbackFunctionality(t *testing.T) {
	cfg := &config.Config{}
	logger := &utils.Logger{}
	service := NewOptimizedEditingService(cfg, logger)

	// Test with empty revision IDs
	err := service.RollbackChanges()
	if err == nil {
		t.Error("Expected error when no revision IDs available, got nil")
	}

	// Test GetLastRevisionID with empty metrics
	lastID := service.GetLastRevisionID()
	if lastID != "" {
		t.Errorf("Expected empty revision ID, got %q", lastID)
	}

	// Simulate metrics with revision IDs
	service.metrics = &EditingMetrics{
		RevisionIDs: []string{"test-revision-1", "test-revision-2"},
	}

	lastID = service.GetLastRevisionID()
	if lastID != "test-revision-2" {
		t.Errorf("Expected 'test-revision-2', got %q", lastID)
	}

	// Note: Full rollback testing would require mocking changetracker
	// which is beyond the scope of this unit test
}

func TestEditingResultStructure(t *testing.T) {
	result := &EditingResult{
		Diff:        "sample diff",
		RevisionIDs: []string{"rev-1", "rev-2"},
		Strategy:    "Quick Edit",
		Metrics: &EditingMetrics{
			TotalTokens: 100,
			TotalCost:   0.05,
		},
	}

	if result.Diff != "sample diff" {
		t.Errorf("Expected 'sample diff', got %q", result.Diff)
	}

	if len(result.RevisionIDs) != 2 {
		t.Errorf("Expected 2 revision IDs, got %d", len(result.RevisionIDs))
	}

	if result.Strategy != "Quick Edit" {
		t.Errorf("Expected 'Quick Edit', got %q", result.Strategy)
	}
}
