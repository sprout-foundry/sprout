// Command patterns analysis and common functionality
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/llm"
)

// CommonPatterns defines the patterns identified across command files
type CommonPatterns struct {
	ConfigLoading  bool
	UISetup        bool
	LoggingSetup   bool
	ModelOverride  bool
	ErrorHandling  bool
	FlagManagement bool
}

// AnalyzeCommandPatterns returns analysis of common patterns across commands
func AnalyzeCommandPatterns() CommonPatterns {
	return CommonPatterns{
		ConfigLoading:  true, // All commands load config
		UISetup:        true, // Most commands set up UI
		LoggingSetup:   true, // Most commands set up logging
		ModelOverride:  true, // Many commands allow model override
		ErrorHandling:  true, // All commands need error handling
		FlagManagement: true, // All commands manage flags
	}
}

// StandardCommandTemplate provides a template for creating new commands
func StandardCommandTemplate(use, short, long string, runFunc func(*CommandConfig, []string) error) *BaseCommand {
	cmd := NewBaseCommand(use, short, long)
	cmd.SetRunFunc(runFunc)
	return cmd
}

// CommonFlagPatterns defines standard flag patterns used across commands
var CommonFlagPatterns = map[string]string{
	"skip-prompt": "Skip user confirmation prompts",
	"model":       "LLM model to use",
	"dry-run":     "Run in simulation mode",
	"verbose":     "Enable verbose output",
	"quiet":       "Suppress non-essential output",
}

// ApplyStandardFlags applies common flags to a command
func ApplyStandardFlags(cmd *BaseCommand, flags []string) {
	for _, flag := range flags {
		if description, exists := CommonFlagPatterns[flag]; exists {
			switch flag {
			case "skip-prompt":
				cmd.AddCustomBoolFlag(flag, "y", false, description)
			case "dry-run":
				cmd.AddCustomBoolFlag(flag, "d", false, description)
			case "model":
				cmd.AddCustomFlag(flag, "m", "", description)
			case "verbose":
				cmd.AddCustomBoolFlag(flag, "v", false, description)
			case "quiet":
				cmd.AddCustomBoolFlag(flag, "q", false, description)
			}
		}
	}
}

// ValidateCommandInput provides common input validation
func ValidateCommandInput(cfg *CommandConfig, args []string, requiresArgs bool) error {
	if requiresArgs && len(args) == 0 {
		return fmt.Errorf("command requires arguments")
	}

	if cfg.DryRun && cfg.Logger != nil {
		cfg.Logger.LogProcessStep("🧪 DRY RUN MODE - No actual changes will be made")
	}

	return nil
}

// SetupLLMConfig provides common LLM configuration setup
func SetupLLMConfig(cfg *CommandConfig, modelOverride string) error {
	if cfg.Config == nil {
		return fmt.Errorf("command config not initialized")
	}

	// Use model override if provided, otherwise use config default
	model := cfg.Model
	if modelOverride != "" {
		model = modelOverride
	}
	if model == "" {
		model = cfg.Config.EditingModel
	}

	// Update config with selected model
	cfg.Config.EditingModel = model
	return nil
}

// HandleCommandResult provides common result handling and logging
func HandleCommandResult(cfg *CommandConfig, result interface{}, err error) error {
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.LogError(err)
		}
		return err
	}

	if cfg.Logger != nil && result != nil {
		cfg.Logger.LogProcessStep(fmt.Sprintf("✅ Command completed successfully: %v", result))
	}

	return nil
}

// GetWorkingDirectory returns the working directory for commands
func GetWorkingDirectory() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	return wd, nil
}

// FormatCommandOutput provides consistent output formatting
func FormatCommandOutput(output interface{}, format string) string {
	switch format {
	case "json":
		return fmt.Sprintf(`{"output": "%v"}`, output)
	case "yaml":
		return fmt.Sprintf("output: %v", output)
	default:
		return fmt.Sprintf("%v", output)
	}
}

// CommonCommandExamples provides standard examples for commands
var CommonCommandExamples = map[string][]string{
	"model": {
		"ledit command --model gpt-4",
		"ledit command --model claude-3",
		"ledit command -m deepseek-coder",
	},
	"dry-run": {
		"ledit command --dry-run",
		"ledit command -d",
	},
	"skip-prompt": {
		"ledit command --skip-prompt",
		"ledit command -y",
	},
}

// GetExamplesForFlags returns examples for given flags
func GetExamplesForFlags(flags []string) string {
	var examples []string
	for _, flag := range flags {
		if flagExamples, exists := CommonCommandExamples[flag]; exists {
			examples = append(examples, flagExamples...)
		}
	}

	if len(examples) == 0 {
		return ""
	}

	return "Examples:\n  " + strings.Join(examples, "\n  ")
}

// CommandMetrics provides common metrics tracking for commands
type CommandMetrics struct {
	StartTime    int64
	TokenUsage   *llm.TokenUsage
	FilesChanged []string
	Errors       []string
}

// NewCommandMetrics creates a new metrics tracker
func NewCommandMetrics() *CommandMetrics {
	return &CommandMetrics{
		StartTime:    0, // TODO: Implement proper timing
		FilesChanged: []string{},
		Errors:       []string{},
	}
}

// RecordTokenUsage records token usage in metrics
func (m *CommandMetrics) RecordTokenUsage(usage *llm.TokenUsage) {
	m.TokenUsage = usage
}

// RecordFileChange records a file change in metrics
func (m *CommandMetrics) RecordFileChange(file string) {
	m.FilesChanged = append(m.FilesChanged, file)
}

// RecordError records an error in metrics
func (m *CommandMetrics) RecordError(err error) {
	m.Errors = append(m.Errors, err.Error())
}

// GetDuration returns the command execution duration
func (m *CommandMetrics) GetDuration() int64 {
	return 0 // TODO: Implement proper timing
}

// Summarize provides a summary of command metrics
func (m *CommandMetrics) Summarize() string {
	duration := m.GetDuration()
	summary := fmt.Sprintf("Duration: %dms", duration)

	if m.TokenUsage != nil {
		summary += fmt.Sprintf(", Tokens: %d", m.TokenUsage.TotalTokens)
	}

	if len(m.FilesChanged) > 0 {
		summary += fmt.Sprintf(", Files changed: %d", len(m.FilesChanged))
	}

	if len(m.Errors) > 0 {
		summary += fmt.Sprintf(", Errors: %d", len(m.Errors))
	}

	return summary
}
