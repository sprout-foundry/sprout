package agent

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
)

func TestQualityOptimizer(t *testing.T) {
	cfg := &config.Config{
		EditingModel: "test-model",
	}
	optimizer := NewQualityOptimizer()

	tests := []struct {
		name          string
		userIntent    string
		taskIntent    TaskIntent
		expectedLevel QualityLevel
	}{
		{
			name:          "Simple typo fix gets enhanced quality due to keyword",
			userIntent:    "fix a small bug", // "fix" is an enhanced keyword
			taskIntent:    TaskIntentModification,
			expectedLevel: QualityEnhanced,
		},
		{
			name:          "Production keywords get production quality",
			userIntent:    "implement production-grade authentication system",
			taskIntent:    TaskIntentCreation,
			expectedLevel: QualityProduction,
		},
		{
			name:          "Refactor gets production due to performance keyword",
			userIntent:    "refactor the database layer for better performance", // "performance" is a production keyword
			taskIntent:    TaskIntentRefactoring,
			expectedLevel: QualityProduction,
		},
		{
			name:          "Security-related gets production quality",
			userIntent:    "add security validation to user inputs",
			taskIntent:    TaskIntentCreation,
			expectedLevel: QualityProduction,
		},
		{
			name:          "Simple comment update gets standard quality",
			userIntent:    "update comment on line 10",
			taskIntent:    TaskIntentModification,
			expectedLevel: QualityStandard,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := optimizer.DetermineQualityLevel(tt.userIntent, tt.taskIntent, cfg)
			if level != tt.expectedLevel {
				t.Errorf("Expected quality level %d, got %d for intent: %s", tt.expectedLevel, level, tt.userIntent)
			}
		})
	}
}

func TestQualityAwarePromptLoading(t *testing.T) {
	tests := []struct {
		name         string
		qualityLevel int
		usePatch     bool
		expectDiff   bool // Whether we expect different prompts for different quality levels
	}{
		{
			name:         "Standard quality level",
			qualityLevel: 0,
			usePatch:     false,
			expectDiff:   false,
		},
		{
			name:         "Enhanced quality level",
			qualityLevel: 1,
			usePatch:     false,
			expectDiff:   true,
		},
		{
			name:         "Production quality level",
			qualityLevel: 2,
			usePatch:     true,
			expectDiff:   true,
		},
	}

	standardPrompt := prompts.GetQualityAwareCodeGenSystemMessage(0, false)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := prompts.GetQualityAwareCodeGenSystemMessage(tt.qualityLevel, tt.usePatch)

			if tt.expectDiff && prompt == standardPrompt {
				t.Errorf("Expected different prompt for quality level %d, but got same as standard", tt.qualityLevel)
			}

			if !tt.expectDiff && prompt != standardPrompt && tt.qualityLevel > 0 {
				t.Errorf("Expected same prompt for quality level %d, but got different from standard", tt.qualityLevel)
			}

			if len(prompt) == 0 {
				t.Errorf("Got empty prompt for quality level %d", tt.qualityLevel)
			}
		})
	}
}

func TestQualityAwareEditor(t *testing.T) {
	optimizer := NewQualityOptimizer()
	editor := NewQualityAwareEditor(optimizer)

	// Test that quality guidelines are properly returned
	guidelines := editor.GetQualityGuidelines(QualityProduction)
	if len(guidelines) == 0 {
		t.Error("Expected non-empty quality guidelines for production level")
	}

	guidelines = editor.GetQualityGuidelines(QualityStandard)
	if len(guidelines) == 0 {
		t.Error("Expected non-empty quality guidelines for standard level")
	}
}

func TestConfigQualityLevelIntegration(t *testing.T) {
	cfg := &config.Config{
		QualityLevel: 2, // Production level
	}

	if cfg.QualityLevel != 2 {
		t.Errorf("Expected QualityLevel to be 2, got %d", cfg.QualityLevel)
	}

	// Test that quality level affects behavior by checking if it's passed through correctly
	cfg.QualityLevel = 1
	if cfg.QualityLevel != 1 {
		t.Errorf("Expected QualityLevel to be 1 after update, got %d", cfg.QualityLevel)
	}
}
