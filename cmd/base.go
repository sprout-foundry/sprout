package cmd

import (
	"log"
	"os"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/trace"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/spf13/cobra"
)

// CommandConfig represents the common configuration shared across commands
type CommandConfig struct {
	SkipPrompt    bool
	Model         string
	DryRun        bool
	Logger        *utils.Logger
	Config        *configuration.Config
	TraceSession  *trace.TraceSession
	TraceDatasetDir string
}

// BaseCommand provides common functionality for all CLI commands
type BaseCommand struct {
	cmd   *cobra.Command
	cfg   *CommandConfig
	flags CommandFlags
}

// CommandFlags defines common flags used across commands
type CommandFlags struct {
	SkipPrompt      *bool
	Model           *string
	DryRun         *bool
	TraceDatasetDir *string
}

// NewBaseCommand creates a new base command with common functionality
func NewBaseCommand(use, short, long string) *BaseCommand {
	base := &BaseCommand{
		cmd: &cobra.Command{
			Use:   use,
			Short: short,
			Long:  long,
		},
		cfg: &CommandConfig{},
	}

	// Initialize common flags
	base.flags.SkipPrompt = base.cmd.Flags().Bool("skip-prompt", false, "Skip user confirmation prompts")
	base.flags.DryRun = base.cmd.Flags().Bool("dry-run", false, "Run in simulation mode")
	base.flags.TraceDatasetDir = base.cmd.Flags().String("trace-dataset-dir", "", "Enable dataset trace mode and write to directory (also settable via LEDIT_TRACE_DATASET_DIR env var)")

	return base
}

// GetCommand returns the underlying cobra command
func (b *BaseCommand) GetCommand() *cobra.Command {
	return b.cmd
}

// Initialize sets up common command infrastructure
func (b *BaseCommand) Initialize() error {
	// Load configuration
	cfg, err := configuration.LoadOrInitConfig(*b.flags.SkipPrompt)
	if err != nil {
		return err
	}

	// UI has been removed from the project

	// Create logger
	logger := utils.GetLogger(*b.flags.SkipPrompt)

	// Initialize trace session if requested
	traceDir := getTraceDatasetDir(*b.flags.TraceDatasetDir)
	var traceSession *trace.TraceSession
	if traceDir != "" {
		// Use provider/model from flags, will be refined later
		provider := getProviderFromConfig(cfg)
		model := getModelFromConfig(cfg, *b.flags.Model)
		traceSession, err = trace.NewTraceSession(traceDir, provider, model)
		if err != nil {
			return err
		}
		logger.Logf("Dataset tracing enabled: %s", traceSession.GetRunID())
	}

	// Update command config
	b.cfg = &CommandConfig{
		SkipPrompt:    *b.flags.SkipPrompt,
		Model:         *b.flags.Model,
		DryRun:        *b.flags.DryRun,
		Logger:        logger,
		Config:        cfg,
		TraceSession:  traceSession,
		TraceDatasetDir: traceDir,
	}

	return nil
}

// getTraceDatasetDir returns trace dataset directory from flag or env var
func getTraceDatasetDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	// Check env var as fallback
	dir, ok := os.LookupEnv("LEDIT_TRACE_DATASET_DIR")
	if ok && dir != "" {
		return dir
	}
	return ""
}

// getProviderFromConfig extracts provider name from config
func getProviderFromConfig(cfg *configuration.Config) string {
	if cfg == nil {
		return ""
	}
	// Extract from provider settings
	// Use LastUsedProvider if available, otherwise use first from Priority
	if cfg.LastUsedProvider != "" {
		return cfg.LastUsedProvider
	}
	if len(cfg.ProviderPriority) > 0 {
		return cfg.ProviderPriority[0]
	}
	return ""
}

// getModelFromConfig extracts model name from config
func getModelFromConfig(cfg *configuration.Config, flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if cfg == nil {
		return ""
	}
	// Get from ProviderModels using LastUsedProvider
	if cfg.LastUsedProvider != "" {
		if model, ok := cfg.ProviderModels[cfg.LastUsedProvider]; ok {
			return model
		}
	}
	return ""
}

// AddCustomFlag adds a custom flag to the command
func (b *BaseCommand) AddCustomFlag(name, shorthand, defaultValue, description string) *string {
	return b.cmd.Flags().StringP(name, shorthand, defaultValue, description)
}

// SetRunFunc sets the command's run function with common initialization
func (b *BaseCommand) SetRunFunc(fn func(*CommandConfig, []string) error) {
	b.cmd.Run = func(cmd *cobra.Command, args []string) {
		if err := b.Initialize(); err != nil {
			log.Printf("Error: failed to initialize command: %v", err)
			return
		}

		if err := fn(b.cfg, args); err != nil {
			log.Printf("Error: command execution failed: %v", err)
		}
	}
}
